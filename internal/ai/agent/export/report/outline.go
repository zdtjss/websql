package report

import (
	"fmt"
	"strings"
)

// 块级编辑的结构视图（Outline）与通用辅助。
//
// 设计：Outline 只是给模型看的派生视图，不落盘；块 ID 与源行区间都由
// ParseMarkdown 在每次解析时重新生成，保证生成/检查/编辑看到同一份结构。

// AssignBlockIDs 按位置为块分配稳定 ID（blk_0001_heading）。
func AssignBlockIDs(blocks []Block) {
	for i := range blocks {
		blocks[i].ID = fmt.Sprintf("blk_%04d_%s", i+1, blocks[i].Kind)
	}
}

// BlockIDIndex 返回块 ID 到下标/块的映射。
func BlockIDIndex(doc *Document) map[string]int {
	out := make(map[string]int, len(doc.Blocks))
	if doc == nil {
		return out
	}
	for i, b := range doc.Blocks {
		if b.ID != "" {
			out[b.ID] = i
		}
	}
	return out
}

// BlockByID 按 ID 查找块。
func BlockByID(doc *Document, id string) (int, *Block) {
	if doc == nil {
		return -1, nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return -1, nil
	}
	for i := range doc.Blocks {
		if doc.Blocks[i].ID == id {
			return i, &doc.Blocks[i]
		}
	}
	return -1, nil
}

// BlockLabel 返回块的可读名称，用于 diff/回执。
func BlockLabel(b Block) string {
	switch b.Kind {
	case KindHeading:
		return fmt.Sprintf("标题「%s」", PlainText(b.Text))
	case KindTable:
		if b.Table != nil {
			return fmt.Sprintf("表格「%s」(%d行)", b.Title, len(b.Table.Rows))
		}
		return "表格"
	case KindChart:
		if b.Chart != nil {
			return fmt.Sprintf("图表「%s」(%s)", b.Chart.Title, b.Chart.Type)
		}
		return "图表"
	case KindKPI:
		return fmt.Sprintf("指标卡(%d)", len(b.KPIs))
	case KindCallout:
		return fmt.Sprintf("提示框「%s」", b.Title)
	case KindList:
		return "列表"
	case KindCode, KindMermaid:
		return "代码块"
	case KindPageBreak:
		return "分页符"
	case KindDivider:
		return "分隔线"
	case KindQuote:
		return "引用"
	default:
		text := PlainText(b.Text)
		if len([]rune(text)) > 24 {
			text = string([]rune(text)[:24]) + "…"
		}
		return "段落「" + text + "」"
	}
}

// OutlineBlock 块摘要。
type OutlineBlock struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Level     int    `json:"level,omitempty"`
	Text      string `json:"text,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Columns   int    `json:"columns,omitempty"`
	ChartType string `json:"chartType,omitempty"`
	KPI       int    `json:"kpis,omitempty"`
}

// OutlineSection 章节（一级/二级标题下的块）。
type OutlineSection struct {
	HeadingID string         `json:"headingId,omitempty"`
	Title     string         `json:"title"`
	Level     int            `json:"level,omitempty"`
	Blocks    []OutlineBlock `json:"blocks"`
}

// OutlineSummary 结构统计。
type OutlineSummary struct {
	Blocks   int `json:"blocks"`
	Sections int `json:"sections"`
	Tables   int `json:"tables"`
	Charts   int `json:"charts"`
	KPIs     int `json:"kpis"`
	Slides   int `json:"slides,omitempty"`
}

// SlideRef 演示稿页与源块的映射（Index 从 1 开始；StartBlock/EndBlock 为块下标，-1 表示无源块）。
type SlideRef struct {
	Index      int    `json:"index"`
	Title      string `json:"title"`
	Kind       string `json:"kind,omitempty"`
	StartBlock int    `json:"startBlock"`
	EndBlock   int    `json:"endBlock"`
}

// Outline 报告结构视图。
type Outline struct {
	Title    string           `json:"title"`
	Sections []OutlineSection `json:"sections"`
	Summary  OutlineSummary   `json:"summary"`
	Slides   []SlideRef       `json:"slides,omitempty"`
}

// BuildOutline 由文档构建结构视图；level 为章节切分级别（默认按一级标题，无一级标题时按二级）。
func BuildOutline(doc *Document) *Outline {
	if doc == nil {
		return nil
	}
	out := &Outline{Title: doc.Title}
	level := outlineSectionLevel(doc.Blocks)
	var current *OutlineSection
	flush := func() {
		if current != nil {
			out.Sections = append(out.Sections, *current)
			current = nil
		}
	}
	for _, b := range doc.Blocks {
		ob := outlineBlock(b)
		out.Summary.Blocks++
		switch b.Kind {
		case KindTable:
			out.Summary.Tables++
		case KindChart:
			out.Summary.Charts++
		case KindKPI:
			out.Summary.KPIs += len(b.KPIs)
		}
		if b.Kind == KindHeading && b.Level <= level {
			flush()
			current = &OutlineSection{HeadingID: b.ID, Title: PlainText(b.Text), Level: b.Level}
			continue
		}
		if current == nil {
			current = &OutlineSection{Title: "（正文）"}
		}
		current.Blocks = append(current.Blocks, ob)
	}
	flush()
	out.Summary.Sections = len(out.Sections)
	return out
}

// outlineSectionLevel 选择章节切分级别：有一级标题用一级，否则用二级，再否则用全部标题。
func outlineSectionLevel(blocks []Block) int {
	hasH1, hasH2 := false, false
	for _, b := range blocks {
		if b.Kind != KindHeading {
			continue
		}
		if b.Level == 1 {
			hasH1 = true
		}
		if b.Level == 2 {
			hasH2 = true
		}
	}
	switch {
	case hasH1:
		return 1
	case hasH2:
		return 2
	default:
		return 4
	}
}

func outlineBlock(b Block) OutlineBlock {
	ob := OutlineBlock{ID: b.ID, Kind: string(b.Kind), Label: BlockLabel(b), Level: b.Level}
	switch b.Kind {
	case KindHeading:
		ob.Text = PlainText(b.Text)
	case KindParagraph:
		text := PlainText(b.Text)
		if runes := []rune(text); len(runes) > 80 {
			text = string(runes[:80]) + "…"
		}
		ob.Text = text
	case KindTable:
		if b.Table != nil {
			ob.Rows = len(b.Table.Rows)
			ob.Columns = len(b.Table.Headers)
			ob.Text = strings.Join(b.Table.Headers, " | ")
		}
	case KindChart:
		if b.Chart != nil {
			ob.ChartType = b.Chart.Type
			ob.Text = b.Chart.Title
		}
	case KindKPI:
		ob.KPI = len(b.KPIs)
		var labels []string
		for _, k := range b.KPIs {
			labels = append(labels, k.Label)
		}
		ob.Text = strings.Join(labels, "、")
	}
	return ob
}

// SectionBlockRange 返回章节标题对应的块区间 [start,end)（含下级标题，直到同级或更高级标题）。
func SectionBlockRange(doc *Document, headingID string) (int, int, error) {
	idx, b := BlockByID(doc, headingID)
	if idx < 0 || b == nil {
		return 0, 0, fmt.Errorf("未找到块 %s", headingID)
	}
	if b.Kind != KindHeading {
		return 0, 0, fmt.Errorf("块 %s 不是标题，无法按章节删除", headingID)
	}
	end := len(doc.Blocks)
	for j := idx + 1; j < len(doc.Blocks); j++ {
		nb := doc.Blocks[j]
		if nb.Kind == KindHeading && nb.Level <= b.Level {
			end = j
			break
		}
	}
	return idx, end, nil
}

// TableToMarkdown 把表格块还原为 Markdown（仅用于表格行级编辑后的局部替换）。
func TableToMarkdown(t *Table) string {
	if t == nil || len(t.Headers) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("| " + strings.Join(escapeTableCells(t.Headers), " | ") + " |\n")
	seps := make([]string, len(t.Headers))
	for i := range seps {
		switch {
		case i < len(t.Align) && t.Align[i] == "center":
			seps[i] = ":---:"
		case i < len(t.Align) && t.Align[i] == "right":
			seps[i] = "---:"
		case i < len(t.Align) && t.Align[i] == "left":
			seps[i] = ":---"
		default:
			seps[i] = "---"
		}
	}
	sb.WriteString("| " + strings.Join(seps, " | ") + " |\n")
	for _, row := range t.Rows {
		cells := make([]string, len(t.Headers))
		copy(cells, row)
		sb.WriteString("| " + strings.Join(escapeTableCells(cells), " | ") + " |\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func escapeTableCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.ReplaceAll(strings.ReplaceAll(c, "|", "\\|"), "\n", " ")
	}
	return out
}
