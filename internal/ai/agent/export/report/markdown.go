package report

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// 扩展 Markdown 语法（模型编写报告使用）：
//
//	# / ## / ### / ####           标题（# 一级章节）
//	普通段落、**粗体**、*斜体*、`代码`、[链接](https://...)
//	- / * / 1.                    列表（支持一层缩进嵌套、任务列表 [ ] / [x]）
//	| a | b |                     表格（支持 :--- / :---: / ---: 对齐）
//	> 文本                         引用
//	> [!NOTE] 标题                 提示框：NOTE/INFO、TIP/SUCCESS、IMPORTANT、WARNING、CAUTION/DANGER
//	```kpi   [{"label":..,"value":..,"change":"+12%","trend":"up"}]
//	```chart {"type":"bar","title":..,"categories":[..],"series":[{"name":..,"data":[..]}]}
//	         {"type":"line","sql":"SELECT 月份, 金额 FROM ...","x":"月份","y":["金额"]}
//	```table {"sql":"SELECT ...","title":"...","limit":20}
//	```mermaid                    pie / xychart-beta 自动转为原生图表，其余保留为图示源码
//	$$ ... $$                     数学公式（HTML 中渲染）
//	---                           分隔线；<!-- pagebreak --> 分页

var (
	headingRe   = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	hrRe        = regexp.MustCompile(`^\s{0,3}(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	bulletRe    = regexp.MustCompile(`^(\s*)([-*+])\s+(.*)$`)
	orderedRe   = regexp.MustCompile(`^(\s*)(\d{1,3})[.)]\s+(.*)$`)
	tableSepRe  = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
	calloutRe   = regexp.MustCompile(`(?i)^\[!(NOTE|INFO|TIP|SUCCESS|IMPORTANT|WARNING|CAUTION|DANGER|ERROR)\]\s*(.*)$`)
	pageBreakRe = regexp.MustCompile(`(?i)^\s*(<!--\s*page\s*-?break\s*-->|\\pagebreak|\\newpage)\s*$`)
	mdPieRowRe  = regexp.MustCompile(`^\s*"([^"]+)"\s*:\s*([-+0-9.eE]+)\s*$`)
)

// ParseMarkdown 把扩展 Markdown 解析为块序列。
// 每个块记录 1-based 源行区间（StartLine/EndLine），并派生稳定 ID（按位置命名），
// 供对话式编辑按块定位、按源行区间做最小改动。
func ParseMarkdown(src string) []Block {
	p := &mdParser{lines: strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")}
	p.parse()
	AssignBlockIDs(p.blocks)
	return p.blocks
}

type mdParser struct {
	lines  []string
	pos    int
	blocks []Block
	para   []string
	// start 当前块起始行下标（0-based）；paraStart 段落起始行下标。
	start     int
	paraStart int
}

// add 追加块并写入源行区间（EndLine 为已消费到的行号，1-based）。
func (p *mdParser) add(b Block) {
	b.StartLine = p.start + 1
	b.EndLine = p.pos
	if b.EndLine < b.StartLine {
		b.EndLine = b.StartLine
	}
	p.blocks = append(p.blocks, b)
}

func (p *mdParser) flushPara() {
	if len(p.para) == 0 {
		return
	}
	text := joinLines(p.para)
	start := p.paraStart
	p.para = nil
	if strings.TrimSpace(text) != "" {
		b := Block{Kind: KindParagraph, Text: text}
		b.StartLine = start + 1
		b.EndLine = p.pos
		if b.EndLine < b.StartLine {
			b.EndLine = b.StartLine
		}
		p.blocks = append(p.blocks, b)
	}
}

func (p *mdParser) parse() {
	for p.pos < len(p.lines) {
		p.start = p.pos
		line := p.lines[p.pos]
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			p.flushPara()
			p.pos++
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			p.flushPara()
			p.parseFence(trimmed)
		case trimmed == "$$" || (strings.HasPrefix(trimmed, "$$") && !strings.HasSuffix(trimmed[2:], "$$")):
			p.flushPara()
			p.parseMathBlock(trimmed)
		case pageBreakRe.MatchString(trimmed):
			p.flushPara()
			p.add(Block{Kind: KindPageBreak})
			p.pos++
		case strings.HasPrefix(trimmed, "<!--"):
			p.flushPara()
			p.skipComment()
		case headingRe.MatchString(trimmed):
			p.flushPara()
			m := headingRe.FindStringSubmatch(trimmed)
			level := len(m[1])
			if level > 4 {
				level = 4
			}
			if strings.TrimSpace(m[2]) != "" {
				p.pos++
				p.add(Block{Kind: KindHeading, Level: level, Text: strings.TrimSpace(m[2])})
				continue
			}
			p.pos++
		case hrRe.MatchString(line):
			p.flushPara()
			p.add(Block{Kind: KindDivider})
			p.pos++
		case strings.HasPrefix(trimmed, ">"):
			p.flushPara()
			p.parseQuote()
		case strings.Contains(trimmed, "|") && p.pos+1 < len(p.lines) && tableSepRe.MatchString(p.lines[p.pos+1]):
			p.flushPara()
			p.parseTable()
		case bulletRe.MatchString(line) || orderedRe.MatchString(line):
			p.flushPara()
			p.parseList()
		default:
			if len(p.para) == 0 {
				p.paraStart = p.pos
			}
			p.para = append(p.para, strings.TrimRight(line, " \t"))
			p.pos++
		}
	}
	p.flushPara()
}

func (p *mdParser) skipComment() {
	for p.pos < len(p.lines) {
		done := strings.Contains(p.lines[p.pos], "-->")
		p.pos++
		if done {
			return
		}
	}
}

func (p *mdParser) parseFence(open string) {
	marker := open[:3]
	info := strings.TrimSpace(strings.TrimLeft(open, "`~"))
	lang := ""
	if f := strings.Fields(info); len(f) > 0 {
		lang = strings.ToLower(f[0])
	}
	p.pos++
	var body []string
	for p.pos < len(p.lines) {
		if strings.HasPrefix(strings.TrimSpace(p.lines[p.pos]), marker) {
			p.pos++
			break
		}
		body = append(body, p.lines[p.pos])
		p.pos++
	}
	code := strings.Join(body, "\n")

	switch lang {
	case "chart", "echarts", "chart-json":
		spec, err := ParseChartJSON(code)
		if err != nil {
			p.add(Block{Kind: KindCallout, Style: "warning", Title: "图表未能生成", Text: err.Error()})
			return
		}
		p.add(Block{Kind: KindChart, Chart: spec})
	case "kpi", "kpis", "metrics", "metric":
		kpis, err := ParseKPIs(code)
		if err != nil {
			p.add(Block{Kind: KindCallout, Style: "warning", Title: "指标卡未能生成", Text: err.Error()})
			return
		}
		p.add(Block{Kind: KindKPI, KPIs: kpis})
	case "table", "datatable", "data-table":
		b, err := ParseDataTableJSON(code)
		if err != nil {
			p.add(Block{Kind: KindCallout, Style: "warning", Title: "数据表未能生成", Text: err.Error()})
			return
		}
		p.add(*b)
	case "mermaid":
		if spec := MermaidToChart(code); spec != nil {
			p.add(Block{Kind: KindChart, Chart: spec})
			return
		}
		p.add(Block{Kind: KindMermaid, Code: code, Lang: "mermaid"})
	default:
		p.add(Block{Kind: KindCode, Code: code, Lang: lang})
	}
}

func (p *mdParser) parseMathBlock(open string) {
	var body []string
	rest := strings.TrimSpace(strings.TrimPrefix(open, "$$"))
	if rest != "" {
		body = append(body, rest)
	}
	p.pos++
	for p.pos < len(p.lines) {
		t := strings.TrimSpace(p.lines[p.pos])
		p.pos++
		if strings.HasSuffix(t, "$$") {
			if s := strings.TrimSpace(strings.TrimSuffix(t, "$$")); s != "" {
				body = append(body, s)
			}
			break
		}
		body = append(body, p.lines[p.pos-1])
	}
	p.add(Block{Kind: KindCode, Lang: "math", Code: strings.Join(body, "\n")})
}

func (p *mdParser) parseQuote() {
	var lines []string
	for p.pos < len(p.lines) {
		t := strings.TrimSpace(p.lines[p.pos])
		if !strings.HasPrefix(t, ">") {
			break
		}
		t = strings.TrimPrefix(t, ">")
		t = strings.TrimPrefix(t, " ")
		lines = append(lines, t)
		p.pos++
	}
	if len(lines) == 0 {
		return
	}
	if m := calloutRe.FindStringSubmatch(strings.TrimSpace(lines[0])); m != nil {
		style := calloutStyle(m[1])
		body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		p.add(Block{Kind: KindCallout, Style: style, Title: strings.TrimSpace(m[2]), Text: body})
		return
	}
	p.add(Block{Kind: KindQuote, Text: strings.TrimSpace(strings.Join(lines, "\n"))})
}

func calloutStyle(tag string) string {
	switch strings.ToUpper(tag) {
	case "TIP", "SUCCESS":
		return "tip"
	case "IMPORTANT":
		return "important"
	case "WARNING":
		return "warning"
	case "CAUTION", "DANGER", "ERROR":
		return "caution"
	}
	return "note"
}

// CalloutLabel 提示框默认标题
func CalloutLabel(style string) string {
	switch style {
	case "tip":
		return "提示"
	case "important":
		return "重要"
	case "warning":
		return "注意"
	case "caution":
		return "警告"
	}
	return "说明"
}

func splitTableRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(t); i++ {
		if t[i] == '\\' && i+1 < len(t) && t[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if t[i] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(t[i])
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

func (p *mdParser) parseTable() {
	headers := splitTableRow(p.lines[p.pos])
	sep := splitTableRow(p.lines[p.pos+1])
	align := make([]string, len(headers))
	for i := range headers {
		if i < len(sep) {
			s := strings.TrimSpace(sep[i])
			switch {
			case strings.HasPrefix(s, ":") && strings.HasSuffix(s, ":"):
				align[i] = "center"
			case strings.HasSuffix(s, ":"):
				align[i] = "right"
			case strings.HasPrefix(s, ":"):
				align[i] = "left"
			}
		}
	}
	p.pos += 2
	var rows [][]string
	for p.pos < len(p.lines) {
		t := strings.TrimSpace(p.lines[p.pos])
		if t == "" || !strings.Contains(t, "|") {
			break
		}
		rows = append(rows, splitTableRow(t))
		p.pos++
	}
	p.add(Block{Kind: KindTable, Table: &Table{Headers: headers, Rows: AlignRows(rows, len(headers)), Align: align}})
}

func indentWidth(s string) int {
	w := 0
	for _, r := range s {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 4
		default:
			return w
		}
	}
	return w
}

func (p *mdParser) parseList() {
	first := p.lines[p.pos]
	ordered := orderedRe.MatchString(first) && !bulletRe.MatchString(first)
	baseIndent := indentWidth(first)
	var items []ListItem

	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if strings.TrimSpace(line) == "" {
			// 空行后若仍是列表项则继续，否则结束
			if p.pos+1 < len(p.lines) && (bulletRe.MatchString(p.lines[p.pos+1]) || orderedRe.MatchString(p.lines[p.pos+1])) {
				p.pos++
				continue
			}
			break
		}
		var indent int
		var text string
		isItem := false
		if m := bulletRe.FindStringSubmatch(line); m != nil && !hrRe.MatchString(line) {
			indent, text, isItem = indentWidth(m[1]), m[3], true
		} else if m := orderedRe.FindStringSubmatch(line); m != nil {
			indent, text, isItem = indentWidth(m[1]), m[3], true
		}
		if !isItem {
			t := strings.TrimSpace(line)
			// 列表项的续行（缩进文本），非缩进的普通行结束列表
			if indentWidth(line) > baseIndent && len(items) > 0 && !headingRe.MatchString(t) {
				appendContinuation(&items, t)
				p.pos++
				continue
			}
			break
		}
		text = taskPrefix(strings.TrimSpace(text))
		if indent >= baseIndent+2 && len(items) > 0 {
			last := &items[len(items)-1]
			last.Children = append(last.Children, ListItem{Text: text})
		} else {
			items = append(items, ListItem{Text: text})
		}
		p.pos++
	}
	if len(items) > 0 {
		p.add(Block{Kind: KindList, Ordered: ordered, Items: items})
	}
}

func appendContinuation(items *[]ListItem, text string) {
	last := &(*items)[len(*items)-1]
	if n := len(last.Children); n > 0 {
		last.Children[n-1].Text = joinLines([]string{last.Children[n-1].Text, text})
		return
	}
	last.Text = joinLines([]string{last.Text, text})
}

func taskPrefix(text string) string {
	switch {
	case strings.HasPrefix(text, "[ ] "):
		return "☐ " + text[4:]
	case strings.HasPrefix(text, "[x] "), strings.HasPrefix(text, "[X] "):
		return "☑ " + text[4:]
	}
	return text
}

// joinLines 合并段落行：行尾两个空格或反斜杠视为硬换行；中文之间不插空格
func joinLines(lines []string) string {
	var sb strings.Builder
	for i, l := range lines {
		hard := strings.HasSuffix(l, "  ") || strings.HasSuffix(l, "\\")
		t := strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(l, " "), "\\"))
		if i > 0 && sb.Len() > 0 {
			prev := sb.String()
			if !strings.HasSuffix(prev, "\n") {
				lastRune, _ := lastRuneOf(prev)
				firstRune, _ := firstRuneOf(t)
				if !(isCJK(lastRune) && isCJK(firstRune)) {
					sb.WriteByte(' ')
				}
			}
		}
		sb.WriteString(t)
		if hard && i < len(lines)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func lastRuneOf(s string) (rune, bool) {
	r := []rune(s)
	if len(r) == 0 {
		return 0, false
	}
	return r[len(r)-1], true
}

func firstRuneOf(s string) (rune, bool) {
	for _, r := range s {
		return r, true
	}
	return 0, false
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF)
}

// ─── Mermaid → 原生图表 ─────────────────────────────────────────

// MermaidToChart 把 mermaid 的 pie / xychart-beta 转为原生图表规格；其他图示返回 nil
func MermaidToChart(src string) *ChartSpec {
	lines := strings.Split(strings.TrimSpace(src), "\n")
	if len(lines) == 0 {
		return nil
	}
	head := strings.TrimSpace(lines[0])
	switch {
	case strings.HasPrefix(head, "pie"):
		return mermaidPie(head, lines[1:])
	case strings.HasPrefix(head, "xychart"):
		return mermaidXY(lines[1:])
	}
	return nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

func mermaidPie(head string, rest []string) *ChartSpec {
	spec := &ChartSpec{Type: "pie"}
	if idx := strings.Index(head, "title"); idx >= 0 {
		spec.Title = unquote(head[idx+5:])
	}
	var vals []float64
	for _, l := range rest {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "title") {
			spec.Title = unquote(t[5:])
			continue
		}
		if m := mdPieRowRe.FindStringSubmatch(t); m != nil {
			if v, err := strconv.ParseFloat(m[2], 64); err == nil {
				spec.Categories = append(spec.Categories, m[1])
				vals = append(vals, v)
			}
		}
	}
	if len(vals) == 0 {
		return nil
	}
	spec.Series = []Series{{Name: firstNonEmpty(spec.Title, "占比"), Data: vals}}
	spec.Normalize()
	return spec
}

func parseBracketList(s string) []string {
	s = strings.TrimSpace(s)
	l, r := strings.IndexByte(s, '['), strings.LastIndexByte(s, ']')
	if l < 0 || r <= l {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s[l+1:r], ",") {
		if v := unquote(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func mermaidXY(rest []string) *ChartSpec {
	spec := &ChartSpec{}
	hasBar, hasLine := false, false
	for _, l := range rest {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "title"):
			spec.Title = unquote(t[5:])
		case strings.HasPrefix(t, "x-axis"):
			if cats := parseBracketList(t); len(cats) > 0 {
				spec.Categories = cats
			} else if f := strings.Fields(strings.TrimPrefix(t, "x-axis")); len(f) > 0 && !strings.Contains(f[0], "-->") {
				spec.XLabel = unquote(f[0])
			}
		case strings.HasPrefix(t, "y-axis"):
			body := strings.TrimSpace(strings.TrimPrefix(t, "y-axis"))
			if strings.HasPrefix(body, "\"") {
				if end := strings.Index(body[1:], "\""); end >= 0 {
					spec.YLabel = body[1 : end+1]
				}
			}
		case strings.HasPrefix(t, "bar") || strings.HasPrefix(t, "line"):
			kind := "bar"
			if strings.HasPrefix(t, "line") {
				kind, hasLine = "line", true
			} else {
				hasBar = true
			}
			var data []float64
			for _, v := range parseBracketList(t) {
				f, _ := ParseNumber(v)
				data = append(data, f)
			}
			if len(data) > 0 {
				name := spec.YLabel
				if name == "" || len(spec.Series) > 0 {
					name = map[string]string{"bar": "柱", "line": "线"}[kind] + strconv.Itoa(len(spec.Series)+1)
				}
				spec.Series = append(spec.Series, Series{Name: name, Data: data})
			}
		}
	}
	if len(spec.Series) == 0 {
		return nil
	}
	if hasLine && !hasBar {
		spec.Type = "line"
	} else {
		spec.Type = "bar"
	}
	if spec.Unit == "" && spec.YLabel != "" {
		spec.Unit = spec.YLabel
	}
	spec.Normalize()
	return spec
}

// ─── 文档构建 ─────────────────────────────────────────────────

// BuildDocument 由标题与扩展 Markdown 构建文档；若正文首个一级标题与报告标题相同（或未给标题），
// 将其提升为报告标题，避免封面与正文重复。
func BuildDocument(title, subtitle, markdown string) *Document {
	doc := &Document{Title: strings.TrimSpace(title), Subtitle: strings.TrimSpace(subtitle)}
	doc.Blocks = ParseMarkdown(markdown)
	for i, b := range doc.Blocks {
		if b.Kind != KindHeading {
			if b.Kind == KindParagraph || b.Kind == KindDivider {
				continue
			}
			break
		}
		if b.Level == 1 && (doc.Title == "" || PlainText(b.Text) == doc.Title) {
			if doc.Title == "" {
				doc.Title = PlainText(b.Text)
			}
			doc.Blocks = append(doc.Blocks[:i:i], doc.Blocks[i+1:]...)
		}
		break
	}
	if doc.Title == "" {
		doc.Title = "数据分析报告"
	}
	return doc
}

// Headings 返回用于目录的标题（level<=maxLevel）
func (d *Document) Headings(maxLevel int) []Block {
	var out []Block
	for _, b := range d.Blocks {
		if b.Kind == KindHeading && b.Level <= maxLevel {
			out = append(out, b)
		}
	}
	return out
}
