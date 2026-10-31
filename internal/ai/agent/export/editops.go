package export

// 对话式文件编辑：结构化操作（块/章节/表格行/页）。
//
// 设计要点：
//   - 报告类产物的真源是 Markdown，块 ID 与源行区间由 report.ParseMarkdown 派生；
//   - 操作按「源行区间」转换成行级编辑，再**从后往前**应用，避免偏移串位；
//   - 不持久化 IR：每次编辑都重新解析，块 ID 与 get_export_artifact 返回的 outline 一致；
//   - 所有写入值在落盘前经 mask 处理（由调用方注入当前 Masker）。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"websql/internal/ai/agent/export/report"
)

// EditOperation 结构化编辑操作。
type EditOperation struct {
	Op string `json:"op" jsonschema:"required" jsonschema_description:"操作类型：replace|insert|delete|move|patch"`
	// Kind 目标类型：block|section|table_row|slide|cell|row|column|sheet|code|payload。
	Kind     string `json:"kind" jsonschema:"required" jsonschema_description:"目标类型：block(块)|section(章节)|table_row(表格行)|slide(页)|cell(单元格)|row(行)|column(列)|sheet(工作表)|code(代码)|payload(Skill载荷)"`
	ID       string `json:"id" jsonschema_description:"目标块 ID（get_export_artifact 的 outline 返回，如 blk_0007_paragraph）"`
	Sheet    string `json:"sheet" jsonschema_description:"工作表名（Excel 操作必填）"`
	Ref      string `json:"ref" jsonschema_description:"单元格引用（Excel，如 B7）"`
	Position string `json:"position" jsonschema_description:"插入位置：before|after（block/slide 插入时必填）"`
	// Index 数据行序号（table_row，1-based）；页序号（slide，1-based）；行号（Excel，1-based）。
	Index int    `json:"index" jsonschema_description:"序号：表格数据行/页/Excel 行号，均为 1-based"`
	Value string `json:"value" jsonschema_description:"写入内容：块/章节使用 Markdown；单元格使用纯文本；sheet 使用新名称；code 使用完整脚本；表格行使用 a|b|c 或 JSON 数组"`
	// Patch 仅 kind=patch 时使用（文本/Skill payload 的 find/replace）。
	Patch *ExportPatch `json:"patch" jsonschema_description:"局部替换（kind=patch 时使用）：find 必须在源内容中命中"`
}

// EditDiff 编辑差异摘要。
type EditDiff struct {
	Added    int      `json:"added"`
	Removed  int      `json:"removed"`
	Changed  int      `json:"changed"`
	Items    []string `json:"items,omitempty"`
	HashSame bool     `json:"hashSame"`
}

// String 生成用于回执的一行摘要。
func (d *EditDiff) String() string {
	if d == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if d.Added > 0 {
		parts = append(parts, fmt.Sprintf("新增 %d", d.Added))
	}
	if d.Removed > 0 {
		parts = append(parts, fmt.Sprintf("删除 %d", d.Removed))
	}
	if d.Changed > 0 {
		parts = append(parts, fmt.Sprintf("修改 %d", d.Changed))
	}
	if len(parts) == 0 {
		parts = append(parts, "无实际变更")
	}
	msg := strings.Join(parts, "、")
	if len(d.Items) > 0 {
		msg += "（" + strings.Join(d.Items, "；") + "）"
	}
	return msg
}

type reportEdit struct {
	start int // 0-based inclusive
	end   int // 0-based exclusive
	lines []string
	order int
}

type tableEditState struct {
	blockID string
	table   *report.Table
	span    [2]int // 1-based 起始/结束行号
}

// applyReportOperations 在 Markdown 源上应用结构化操作。
// isPPT 为 true 时额外支持 slide.*（通过 report.PlanSlides 翻译为块操作）。
func applyReportOperations(content string, doc *report.Document, ops []EditOperation, mask func(string) string, isPPT bool) (string, *EditDiff, []OperationRecord, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if doc == nil {
		return "", nil, nil, fmt.Errorf("缺少文档结构，无法编辑")
	}
	lines := strings.Split(content, "\n")
	edits := make([]reportEdit, 0, len(ops))
	patches := make([]ExportPatch, 0)
	records := make([]OperationRecord, 0, len(ops))
	tableStates := map[string]*tableEditState{}
	order := 0

	maskValue := func(s string) string {
		if mask == nil {
			return s
		}
		return mask(s)
	}

	appendEdit := func(start, end int, newLines []string, rec OperationRecord) {
		// 行号裁剪到合法范围
		if start < 0 {
			start = 0
		}
		if end < start {
			end = start
		}
		if end > len(lines) {
			end = len(lines)
		}
		edits = append(edits, reportEdit{start: start, end: end, lines: newLines, order: order})
		order++
		records = append(records, rec)
	}

	blockSpan := func(id string) (report.Block, error) {
		_, b := report.BlockByID(doc, id)
		if b == nil {
			return report.Block{}, fmt.Errorf("未找到块 %s（请先用 get_export_artifact 的 outline 确认块 ID）", id)
		}
		if b.StartLine <= 0 || b.EndLine <= 0 {
			return report.Block{}, fmt.Errorf("块 %s 缺少源行区间，无法编辑", id)
		}
		return *b, nil
	}

	for _, op := range ops {
		opName := strings.ToLower(strings.TrimSpace(op.Op))
		kind := strings.ToLower(strings.TrimSpace(op.Kind))
		id := strings.TrimSpace(op.ID)

		switch kind {
		case "patch":
			if op.Patch == nil || strings.TrimSpace(op.Patch.Find) == "" {
				return "", nil, nil, fmt.Errorf("patch 操作必须提供 find")
			}
			patches = append(patches, ExportPatch{Find: op.Patch.Find, Replace: maskValue(op.Patch.Replace), All: op.Patch.All})
			records = append(records, OperationRecord{Op: "patch", Kind: "content", Detail: truncateForRecord(op.Patch.Find)})
			continue
		case "slide":
			if !isPPT {
				return "", nil, nil, fmt.Errorf("slide 操作仅适用于 PPT 产物")
			}
			slides := report.PlanSlides(doc)
			if op.Index < 1 || op.Index > len(slides) {
				return "", nil, nil, fmt.Errorf("页序号 %d 超出范围（共 %d 页）", op.Index, len(slides))
			}
			ref := slides[op.Index-1]
			if ref.StartBlock < 0 || ref.EndBlock < ref.StartBlock || ref.EndBlock >= len(doc.Blocks) {
				return "", nil, nil, fmt.Errorf("第 %d 页是%s，不支持页级编辑；请改用 block 操作", op.Index, ref.Title)
			}
			startB := doc.Blocks[ref.StartBlock]
			endB := doc.Blocks[ref.EndBlock]
			switch opName {
			case "delete":
				// 章节页删除整节（标题+内容），其余页删除映射到的块。
				if ref.Kind == "section" && startB.Kind == report.KindHeading {
					startIdx, endIdx, secErr := report.SectionBlockRange(doc, startB.ID)
					if secErr != nil {
						return "", nil, nil, secErr
					}
					appendEdit(doc.Blocks[startIdx].StartLine-1, doc.Blocks[endIdx-1].EndLine, nil, OperationRecord{Op: "delete", Kind: "section", Target: ref.Title, Detail: titleDetail(ref)})
					continue
				}
				appendEdit(startB.StartLine-1, endB.EndLine, nil, OperationRecord{Op: "delete", Kind: "slide", Target: ref.Title, Detail: titleDetail(ref)})
			case "insert":
				pos := strings.ToLower(strings.TrimSpace(op.Position))
				at := endB.EndLine
				if pos == "before" {
					at = startB.StartLine - 1
				}
				appendEdit(at, at, markdownLines(maskValue(op.Value)), OperationRecord{Op: "insert", Kind: "slide", Target: ref.Title, Detail: titleDetail(ref)})
			case "replace":
				appendEdit(startB.StartLine-1, endB.EndLine, markdownLines(maskValue(op.Value)), OperationRecord{Op: "replace", Kind: "slide", Target: ref.Title, Detail: titleDetail(ref)})
			default:
				return "", nil, nil, fmt.Errorf("页操作仅支持 delete|insert|replace，收到 %q", op.Op)
			}
			continue
		case "table_row":
			b, err := blockSpan(id)
			if err != nil {
				return "", nil, nil, err
			}
			if b.Kind != report.KindTable || b.Table == nil {
				return "", nil, nil, fmt.Errorf("块 %s 不是可直接编辑的表格（SQL 绑定表格请修改 SQL 或重新导出）", id)
			}
			st := tableStates[id]
			if st == nil {
				clone := *b.Table
				clone.Headers = append([]string(nil), b.Table.Headers...)
				clone.Rows = cloneRows(b.Table.Rows)
				st = &tableEditState{blockID: id, table: &clone, span: [2]int{b.StartLine, b.EndLine}}
				tableStates[id] = st
			}
			if err := applyTableRowOp(st.table, opName, op.Index, maskValue(op.Value)); err != nil {
				return "", nil, nil, err
			}
			records = append(records, OperationRecord{Op: opName, Kind: "table_row", Target: id, Detail: fmt.Sprintf("第%d行", op.Index)})
			continue
		}

		// 块/章节操作
		switch opName {
		case "delete":
			if kind == "section" || kind == "block" {
				var startLine, endLine int
				if kind == "section" {
					startIdx, endIdx, err := report.SectionBlockRange(doc, id)
					if err != nil {
						return "", nil, nil, err
					}
					startLine = doc.Blocks[startIdx].StartLine
					endLine = doc.Blocks[endIdx-1].EndLine
					appendEdit(startLine-1, endLine, nil, OperationRecord{Op: "delete", Kind: "section", Target: id, Detail: fmt.Sprintf("删除%d块", endIdx-startIdx)})
				} else {
					b, err := blockSpan(id)
					if err != nil {
						return "", nil, nil, err
					}
					appendEdit(b.StartLine-1, b.EndLine, nil, OperationRecord{Op: "delete", Kind: "block", Target: id, Detail: report.BlockLabel(b)})
				}
				continue
			}
			return "", nil, nil, fmt.Errorf("delete 仅支持 block|section|table_row|slide，收到 %q", kind)
		case "replace":
			b, err := blockSpan(id)
			if err != nil {
				return "", nil, nil, err
			}
			appendEdit(b.StartLine-1, b.EndLine, markdownLines(maskValue(op.Value)), OperationRecord{Op: "replace", Kind: "block", Target: id, Detail: report.BlockLabel(b)})
		case "insert":
			b, err := blockSpan(id)
			if err != nil {
				return "", nil, nil, err
			}
			at := b.EndLine
			if strings.ToLower(strings.TrimSpace(op.Position)) == "before" {
				at = b.StartLine - 1
			}
			appendEdit(at, at, markdownLines(maskValue(op.Value)), OperationRecord{Op: "insert", Kind: "block", Target: id, Detail: report.BlockLabel(b)})
		case "move":
			// move 需要目标位置：op.Value 承载目标块 ID，Position 决定前后。
			src, err := blockSpan(id)
			if err != nil {
				return "", nil, nil, err
			}
			dstID := strings.TrimSpace(op.Value)
			dst, err := blockSpan(dstID)
			if err != nil {
				return "", nil, nil, fmt.Errorf("move 目标块无效：%w", err)
			}
			if src.StartLine >= dst.StartLine && src.EndLine <= dst.EndLine {
				return "", nil, nil, fmt.Errorf("不能把块移动到自身或子块内")
			}
			at := dst.EndLine
			if strings.ToLower(strings.TrimSpace(op.Position)) == "before" {
				at = dst.StartLine - 1
			}
			moved := append([]string(nil), lines[src.StartLine-1:src.EndLine]...)
			// 行编辑统一从后往前应用：若插入点在源块之后，插入先于删除执行，
			// 删除造成的行号位移会自然作用于插入结果；若插入点在源块之前，
			// 删除先执行也不会影响插入位置。因此两处编辑均使用原始坐标。
			appendEdit(at, at, moved, OperationRecord{Op: "move", Kind: "block", Target: id, Detail: "移动到 " + dstID})
			appendEdit(src.StartLine-1, src.EndLine, nil, OperationRecord{Op: "move", Kind: "block", Target: id, Detail: "移出原位置"})
		default:
			return "", nil, nil, fmt.Errorf("不支持的块操作 %q（支持 replace|insert|delete|move）", op.Op)
		}
	}

	// 表格行编辑：每个表格块只替换一次
	for _, st := range tableStates {
		md := report.TableToMarkdown(st.table)
		if strings.TrimSpace(md) == "" {
			return "", nil, nil, fmt.Errorf("表格 %s 编辑后为空", st.blockID)
		}
		appendEdit(st.span[0]-1, st.span[1], strings.Split(md, "\n"), OperationRecord{Op: "update", Kind: "table", Target: st.blockID, Detail: "表格行编辑"})
	}

	// 从后往前应用，保证行号不串位；同一位置按声明顺序稳定。
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start > edits[j].start
		}
		return edits[i].order > edits[j].order
	})
	out := lines
	for _, e := range edits {
		next := make([]string, 0, len(out)-(e.end-e.start)+len(e.lines))
		next = append(next, out[:e.start]...)
		next = append(next, e.lines...)
		next = append(next, out[e.end:]...)
		out = next
	}
	result := strings.Join(out, "\n")

	// 文本 patch 最后应用（以编辑后的内容为准）
	for i, p := range patches {
		if !strings.Contains(result, p.Find) {
			return "", nil, nil, fmt.Errorf("第 %d 个 patch 未命中编辑后的内容，请确认 find 与源内容一致（区分大小写）", i+1)
		}
		if p.All {
			result = strings.ReplaceAll(result, p.Find, p.Replace)
		} else {
			result = strings.Replace(result, p.Find, p.Replace, 1)
		}
	}

	beforeDoc := doc
	afterDoc := report.BuildDocument(doc.Title, doc.Subtitle, result)
	diff := DiffDocuments(beforeDoc, afterDoc)
	diff.HashSame = result == content
	return result, diff, records, nil
}

func cloneRows(rows [][]string) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		out[i] = append([]string(nil), r...)
	}
	return out
}

func applyTableRowOp(t *report.Table, opName string, index int, value string) error {
	if t == nil {
		return fmt.Errorf("表格为空")
	}
	cols := len(t.Headers)
	cells := parseRowCells(value)
	if opName == "insert" {
		if index < 1 || index > len(t.Rows)+1 {
			return fmt.Errorf("插入行序号 %d 超出范围（1~%d）", index, len(t.Rows)+1)
		}
		row := make([]string, cols)
		copy(row, cells)
		t.Rows = append(t.Rows, nil)
		copy(t.Rows[index:], t.Rows[index-1:])
		t.Rows[index-1] = row
		return nil
	}
	if index < 1 || index > len(t.Rows) {
		return fmt.Errorf("行序号 %d 超出范围（1~%d）", index, len(t.Rows))
	}
	switch opName {
	case "delete":
		t.Rows = append(t.Rows[:index-1], t.Rows[index:]...)
		return nil
	case "replace", "update":
		row := make([]string, cols)
		copy(row, cells)
		t.Rows[index-1] = row
		return nil
	default:
		return fmt.Errorf("表格行仅支持 insert|replace|delete，收到 %q", opName)
	}
}

// parseRowCells 支持 "a | b | c" 与 JSON 数组两种写法。
func parseRowCells(value string) []string {
	v := strings.TrimSpace(value)
	if strings.HasPrefix(v, "[") {
		var arr []any
		if err := json.Unmarshal([]byte(v), &arr); err == nil {
			out := make([]string, 0, len(arr))
			for _, it := range arr {
				out = append(out, report.CellString(it))
			}
			return out
		}
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func markdownLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func truncateForRecord(s string) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return string(runes)
}

func titleDetail(ref report.SlideRef) string {
	return fmt.Sprintf("第%d页", ref.Index)
}

// ─── 块级 diff（LCS）────────────────────────────────────────────

type blockFingerprint struct {
	kind string
	text string
}

func fingerprint(b report.Block) blockFingerprint {
	var sb strings.Builder
	sb.WriteString(string(b.Kind))
	sb.WriteByte('|')
	sb.WriteString(b.Text)
	sb.WriteByte('|')
	sb.WriteString(b.Title)
	sb.WriteByte('|')
	sb.WriteString(b.Code)
	if b.Table != nil {
		sb.WriteString("|T:")
		sb.WriteString(strings.Join(b.Table.Headers, ","))
		sb.WriteString(fmt.Sprintf("|%d", len(b.Table.Rows)))
		for _, r := range b.Table.Rows {
			sb.WriteString("|")
			sb.WriteString(strings.Join(r, ","))
		}
	}
	if b.Chart != nil {
		sb.WriteString("|C:")
		sb.WriteString(b.Chart.Type)
		sb.WriteString(b.Chart.Title)
		sb.WriteString(fmt.Sprintf("|%d", len(b.Chart.Series)))
	}
	for _, k := range b.KPIs {
		sb.WriteString("|K:")
		sb.WriteString(k.Label)
		sb.WriteString(k.Value)
		sb.WriteString(k.Change)
	}
	return blockFingerprint{kind: string(b.Kind), text: sb.String()}
}

// DiffDocuments 用 LCS 比较块序列，返回新增/删除/修改数量与样例。
func DiffDocuments(before, after *report.Document) *EditDiff {
	d := &EditDiff{}
	if before == nil || after == nil {
		return d
	}
	a := make([]blockFingerprint, len(before.Blocks))
	b := make([]blockFingerprint, len(after.Blocks))
	for i := range before.Blocks {
		a[i] = fingerprint(before.Blocks[i])
	}
	for i := range after.Blocks {
		b[i] = fingerprint(after.Blocks[i])
	}
	// 规模保护：超大文档退化为数量差
	if len(a)*len(b) > 250000 {
		d.Added = maxInt(0, len(b)-len(a))
		d.Removed = maxInt(0, len(a)-len(b))
		d.Changed = minInt(len(a), len(b))
		return d
	}
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i].text == b[j].text {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	type pair struct{ ai, bi int }
	var matched []pair
	for i, j := 0, 0; i < len(a) && j < len(b); {
		if a[i].text == b[j].text {
			matched = append(matched, pair{i, j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	// 未匹配区间：同类型配对为“修改”，其余计新增/删除
	prevA, prevB := 0, 0
	handleGap := func(aStart, aEnd, bStart, bEnd int) {
		removed := aEnd - aStart
		added := bEnd - bStart
		paired := minInt(removed, added)
		d.Changed += paired
		d.Removed += removed - paired
		d.Added += added - paired
		for k := 0; k < paired && len(d.Items) < 8; k++ {
			d.Items = append(d.Items, "修改 "+report.BlockLabel(after.Blocks[bStart+k]))
		}
		for k := paired; k < removed && len(d.Items) < 8; k++ {
			d.Items = append(d.Items, "删除 "+report.BlockLabel(before.Blocks[aStart+k]))
		}
		for k := paired; k < added && len(d.Items) < 8; k++ {
			d.Items = append(d.Items, "新增 "+report.BlockLabel(after.Blocks[bStart+k]))
		}
	}
	for _, m := range matched {
		handleGap(prevA, m.ai, prevB, m.bi)
		prevA, prevB = m.ai+1, m.bi+1
	}
	handleGap(prevA, len(a), prevB, len(b))
	return d
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// diffString 把 diff 转为回执字符串（nil 安全）。
func diffString(d *EditDiff) string {
	if d == nil {
		return ""
	}
	return d.String()
}

// OperationNeedsApproval 判断操作列表是否包含删除类操作（供权限中间件使用）。
func OperationNeedsApproval(ops []EditOperation) bool {
	return needsApproval(ops)
}

// needsApproval 判断操作列表是否包含删除类操作（需服务端确认）。
func needsApproval(ops []EditOperation) bool {
	for _, op := range ops {
		opName := strings.ToLower(strings.TrimSpace(op.Op))
		kind := strings.ToLower(strings.TrimSpace(op.Kind))
		if opName == "delete" || opName == "move" {
			switch kind {
			case "block", "section", "table_row", "slide", "row", "column", "sheet":
				return true
			}
		}
	}
	return false
}

// DescribeDestructiveOperations 只描述需要确认的删除/移动操作。
func DescribeDestructiveOperations(ops []EditOperation) string {
	filtered := make([]EditOperation, 0, len(ops))
	for _, op := range ops {
		opName := strings.ToLower(strings.TrimSpace(op.Op))
		kind := strings.ToLower(strings.TrimSpace(op.Kind))
		if opName == "delete" || opName == "move" {
			switch kind {
			case "block", "section", "table_row", "slide", "row", "column", "sheet":
				filtered = append(filtered, op)
			}
		}
	}
	return DescribeOperations(filtered)
}

// DescribeOperations 生成需要用户确认的操作清单（用于审批弹窗/回执）。
func DescribeOperations(ops []EditOperation) string {
	var lines []string
	for _, op := range ops {
		opName := strings.ToLower(strings.TrimSpace(op.Op))
		kind := strings.ToLower(strings.TrimSpace(op.Kind))
		target := strings.TrimSpace(op.ID)
		switch kind {
		case "slide":
			target = fmt.Sprintf("第%d页", op.Index)
		case "cell":
			target = strings.TrimSpace(op.Sheet) + "!" + strings.TrimSpace(op.Ref)
		case "sheet":
			target = strings.TrimSpace(op.Sheet)
		case "table_row":
			target = fmt.Sprintf("%s 第%d行", target, op.Index)
		}
		if target == "" {
			target = strings.TrimSpace(op.Value)
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", opName, kind, target))
	}
	return strings.Join(lines, "\n")
}
