package report

import (
	"fmt"
	"strings"
	"time"
)

// RenderDOCX 渲染原生 Word 文档：封面、目录（TOC 域 + 可点击缓存条目）、页眉页脚页码、
// 多级标题（导航窗格可用）、指标卡、提示框、原生可编辑图表（内嵌数据工作簿）、表格（表头重复、斑马纹）。
func RenderDOCX(doc *Document, theme Theme) ([]byte, error) {
	// Word 报告正文始终使用浅色排版（深色主题仅影响强调色），保证打印与阅读效果
	t := theme
	if t.Dark {
		t = businessTheme
		t.Primary, t.Accent = "0B3D91", theme.Primary
	}
	w := &docxWriter{doc: doc, t: t, rels: &relsBuilder{}}
	return w.build()
}

type docxWriter struct {
	doc      *Document
	t        Theme
	rels     *relsBuilder
	pkg      *zipPackage
	body     strings.Builder
	charts   int
	figN     int
	tblN     int
	bookmark int
	docPr    int
	numIDs   []int // 有序列表的 num 实例（每个列表重新编号）
	nextNum  int
	headIDs  []string
}

const (
	docxPageW   = 11906
	docxMarginL = 1304
	docxMarginR = 1304
	docxTextW   = docxPageW - docxMarginL - docxMarginR // 9298 twips
)

func (w *docxWriter) build() ([]byte, error) {
	w.pkg = newZipPackage()
	w.nextNum = 2 // numId 1 = 无序列表

	rStyles := w.rels.add(relStyles, "styles.xml", false)
	_ = rStyles
	w.rels.add(relNumbering, "numbering.xml", false)
	w.rels.add(relSettings, "settings.xml", false)
	rHeader := w.rels.add(relHeader, "header1.xml", false)
	rFooter := w.rels.add(relFooter, "footer1.xml", false)

	w.cover()
	w.toc()
	for _, b := range w.doc.Blocks {
		w.block(b)
	}

	var doc strings.Builder
	doc.WriteString(xmlHeader)
	doc.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart"><w:body>`)
	doc.WriteString(w.body.String())
	fmt.Fprintf(&doc, `<w:sectPr><w:headerReference w:type="default" r:id="%s"/><w:footerReference w:type="default" r:id="%s"/><w:pgSz w:w="%d" w:h="16838"/><w:pgMar w:top="1440" w:right="%d" w:bottom="1440" w:left="%d" w:header="720" w:footer="720" w:gutter="0"/><w:titlePg/></w:sectPr>`,
		rHeader, rFooter, docxPageW, docxMarginR, docxMarginL)
	doc.WriteString(`</w:body></w:document>`)

	p := w.pkg
	p.addString("word/document.xml", doc.String())
	p.addString("word/styles.xml", w.stylesXML())
	p.addString("word/numbering.xml", w.numberingXML())
	p.addString("word/settings.xml", xmlHeader+`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:zoom w:percent="100"/><w:defaultTabStop w:val="420"/><w:characterSpacingControl w:val="compressPunctuation"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`)
	p.addString("word/header1.xml", w.headerXML())
	p.addString("word/footer1.xml", w.footerXML())
	p.addString("word/_rels/document.xml.rels", w.rels.xml())
	p.addString("docProps/core.xml", corePropsXML(w.doc.Title))
	p.addString("docProps/app.xml", appPropsXML("WebSQL Report"))
	root := &relsBuilder{}
	root.add(relOfficeDoc, "word/document.xml", false)
	root.add(relCoreProps, "docProps/core.xml", false)
	root.add(relExtProps, "docProps/app.xml", false)
	p.addString("_rels/.rels", root.xml())
	p.addString("[Content_Types].xml", w.contentTypes())
	return p.bytes()
}

func (w *docxWriter) contentTypes() string {
	var sb strings.Builder
	sb.WriteString(xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	sb.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="xlsx" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"/>`)
	for name, ct := range map[string]string{
		"/word/document.xml":  "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml",
		"/word/styles.xml":    "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml",
		"/word/numbering.xml": "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml",
		"/word/settings.xml":  "application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml",
		"/word/header1.xml":   "application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml",
		"/word/footer1.xml":   "application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml",
		"/docProps/core.xml":  "application/vnd.openxmlformats-package.core-properties+xml",
		"/docProps/app.xml":   "application/vnd.openxmlformats-officedocument.extended-properties+xml",
	} {
		fmt.Fprintf(&sb, `<Override PartName="%s" ContentType="%s"/>`, name, ct)
	}
	for i := 1; i <= w.charts; i++ {
		fmt.Fprintf(&sb, `<Override PartName="/word/charts/chart%d.xml" ContentType="%s"/>`, i, ctChart)
	}
	sb.WriteString(`</Types>`)
	return sb.String()
}

// ─── 段落与行内 ─────────────────────────────────────────────

type runStyle struct {
	size   int // 半磅
	color  string
	bold   bool
	italic bool
	font   string
}

func (w *docxWriter) run(text string, rs runStyle) string {
	var props strings.Builder
	if rs.font != "" {
		fmt.Fprintf(&props, `<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`, rs.font, rs.font, w.t.FontFamily, rs.font)
	}
	if rs.bold {
		props.WriteString(`<w:b/><w:bCs/>`)
	}
	if rs.italic {
		props.WriteString(`<w:i/>`)
	}
	if rs.color != "" {
		fmt.Fprintf(&props, `<w:color w:val="%s"/>`, rs.color)
	}
	if rs.size > 0 {
		fmt.Fprintf(&props, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, rs.size, rs.size)
	}
	rpr := ""
	if props.Len() > 0 {
		rpr = "<w:rPr>" + props.String() + "</w:rPr>"
	}
	var sb strings.Builder
	parts := strings.Split(text, "\n")
	for i, part := range parts {
		if i > 0 {
			sb.WriteString(`<w:r>` + rpr + `<w:br/></w:r>`)
		}
		if part != "" {
			fmt.Fprintf(&sb, `<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r>`, rpr, xmlEscape(part))
		}
	}
	return sb.String()
}

// inlineRuns 行内 Markdown → runs（粗体/斜体/代码/删除线/超链接）
func (w *docxWriter) inlineRuns(text string, base runStyle) string {
	var sb strings.Builder
	for _, s := range ParseInline(text) {
		var props strings.Builder
		font := base.font
		if s.Code {
			font = w.t.MonoFont
		}
		if font != "" {
			fmt.Fprintf(&props, `<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`, font, font, w.t.FontFamily, font)
		}
		if s.Bold || base.bold {
			props.WriteString(`<w:b/><w:bCs/>`)
		}
		if s.Italic || base.italic {
			props.WriteString(`<w:i/>`)
		}
		if s.Strike {
			props.WriteString(`<w:strike/>`)
		}
		// 注意 w:rPr 子元素必须遵循 schema 顺序：rFonts, b, i, strike, color, sz, u, shd
		color := base.color
		if s.Link != "" {
			color = w.t.Accent
		}
		if color != "" {
			fmt.Fprintf(&props, `<w:color w:val="%s"/>`, color)
		}
		if base.size > 0 {
			fmt.Fprintf(&props, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, base.size, base.size)
		}
		if s.Link != "" {
			props.WriteString(`<w:u w:val="single"/>`)
		}
		if s.Code {
			fmt.Fprintf(&props, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, "F1F3F5")
		}
		rpr := ""
		if props.Len() > 0 {
			rpr = "<w:rPr>" + props.String() + "</w:rPr>"
		}
		var runs strings.Builder
		for i, part := range strings.Split(s.Text, "\n") {
			if i > 0 {
				runs.WriteString(`<w:r>` + rpr + `<w:br/></w:r>`)
			}
			if part != "" {
				fmt.Fprintf(&runs, `<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r>`, rpr, xmlEscape(part))
			}
		}
		switch {
		case s.Link != "" && strings.HasPrefix(s.Link, "#"):
			fmt.Fprintf(&sb, `<w:hyperlink w:anchor="%s">%s</w:hyperlink>`, xmlEscape(strings.TrimPrefix(s.Link, "#")), runs.String())
		case s.Link != "" && !strings.HasPrefix(s.Link, "/"):
			id := w.rels.add(relHyperlink, s.Link, true)
			fmt.Fprintf(&sb, `<w:hyperlink r:id="%s">%s</w:hyperlink>`, id, runs.String())
		default:
			sb.WriteString(runs.String())
		}
	}
	return sb.String()
}

func (w *docxWriter) para(style, pPrExtra, content string) {
	w.body.WriteString("<w:p>")
	if style != "" || pPrExtra != "" {
		w.body.WriteString("<w:pPr>")
		if style != "" {
			fmt.Fprintf(&w.body, `<w:pStyle w:val="%s"/>`, style)
		}
		w.body.WriteString(pPrExtra)
		w.body.WriteString("</w:pPr>")
	}
	w.body.WriteString(content)
	w.body.WriteString("</w:p>")
}

func (w *docxWriter) pageBreak() {
	w.body.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
}

// ─── 封面与目录 ─────────────────────────────────────────────

func (w *docxWriter) cover() {
	t := w.t
	date := w.doc.Date
	if date == "" {
		date = time.Now().Format("2006年01月02日")
	}
	w.para("", `<w:spacing w:before="2600" w:after="120"/>`, w.run("DATA ANALYSIS REPORT", runStyle{size: 20, color: t.Accent, bold: true}))
	w.para("CoverTitle", fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="24" w:space="12" w:color="%s"/></w:pBdr>`, t.Accent),
		w.run(w.doc.Title, runStyle{}))
	if w.doc.Subtitle != "" {
		w.para("CoverSubtitle", "", w.run(w.doc.Subtitle, runStyle{}))
	}
	w.para("", `<w:spacing w:before="1800"/>`, "")
	meta := []string{"生成日期：" + date}
	if w.doc.Author != "" {
		meta = append(meta, "编制："+w.doc.Author)
	}
	if w.doc.Organization != "" {
		meta = append(meta, "单位："+w.doc.Organization)
	}
	meta = append(meta, "数据来源：WebSQL 智能分析")
	for _, m := range meta {
		w.para("", `<w:spacing w:after="80"/>`, w.run(m, runStyle{size: 22, color: t.Muted}))
	}
	w.pageBreak()
}

func (w *docxWriter) toc() {
	heads := w.doc.Headings(3)
	w.headIDs = make([]string, len(heads))
	for i := range heads {
		w.headIDs[i] = fmt.Sprintf("_Toc%06d", i+1)
	}
	if len(heads) < 3 {
		return
	}
	w.para("TOCHeading", "", w.run("目　录", runStyle{}))
	w.body.WriteString(`<w:p><w:pPr><w:pStyle w:val="TOC1"/></w:pPr><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r></w:p>`)
	for i, h := range heads {
		style := fmt.Sprintf("TOC%d", h.Level)
		w.para(style, "", fmt.Sprintf(`<w:hyperlink w:anchor="%s" w:history="1">%s</w:hyperlink>`, w.headIDs[i], w.run(PlainText(h.Text), runStyle{})))
	}
	w.body.WriteString(`<w:p><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`)
	w.para("", "", w.run("提示：在 Word 中右键目录选择「更新域」可生成页码。", runStyle{size: 16, color: w.t.Muted, italic: true}))
	w.pageBreak()
}

// ─── 块 ─────────────────────────────────────────────────────

func (w *docxWriter) block(b Block) {
	t := w.t
	switch b.Kind {
	case KindHeading:
		level := b.Level
		if level > 4 {
			level = 4
		}
		content := w.inlineRuns(b.Text, runStyle{})
		if level <= 3 && w.bookmark < len(w.headIDs) {
			id := w.bookmark
			w.bookmark++
			content = fmt.Sprintf(`<w:bookmarkStart w:id="%d" w:name="%s"/>%s<w:bookmarkEnd w:id="%d"/>`, id, w.headIDs[id], content, id)
		}
		w.para(fmt.Sprintf("Heading%d", level), "", content)
	case KindParagraph:
		w.para("", "", w.inlineRuns(b.Text, runStyle{}))
	case KindList:
		w.list(b.Items, b.Ordered, 0)
	case KindQuote:
		w.para("Quote", "", w.inlineRuns(b.Text, runStyle{}))
	case KindCallout:
		w.callout(b.Style, firstNonEmpty(b.Title, CalloutLabel(b.Style)), b.Text)
	case KindDivider:
		w.para("", fmt.Sprintf(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="%s"/></w:pBdr>`, t.Border), "")
	case KindPageBreak:
		w.pageBreak()
	case KindCode:
		lines := strings.Split(strings.TrimRight(b.Code, "\n"), "\n")
		for i, l := range lines {
			extra := `<w:keepNext/>`
			if i == len(lines)-1 {
				extra = ""
			}
			w.para("CodeBlock", extra, w.run(l, runStyle{}))
		}
	case KindMermaid:
		w.para("Caption", "", w.run("图示（Mermaid 源码，可在 HTML 版报告中查看渲染效果）", runStyle{}))
		for _, l := range strings.Split(b.Code, "\n") {
			w.para("CodeBlock", "", w.run(l, runStyle{}))
		}
	case KindKPI:
		w.kpis(b.KPIs)
	case KindChart:
		if b.Error != "" || !b.Chart.HasData() {
			w.callout("warning", "图表未能生成", firstNonEmpty(b.Error, "无数据"))
			return
		}
		w.chart(b.Chart)
	case KindTable:
		if b.Error != "" || b.Table == nil {
			w.callout("warning", "数据表未能生成", firstNonEmpty(b.Error, "无数据"))
			return
		}
		w.table(b.Title, b.Table)
	}
}

func (w *docxWriter) list(items []ListItem, ordered bool, level int) {
	numID := 1
	if ordered {
		numID = w.nextNum
		w.numIDs = append(w.numIDs, numID)
		w.nextNum++
	}
	w.listItems(items, numID, level)
}

func (w *docxWriter) listItems(items []ListItem, numID, level int) {
	for _, it := range items {
		w.para("ListParagraph", fmt.Sprintf(`<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, level, numID), w.inlineRuns(it.Text, runStyle{}))
		if len(it.Children) > 0 {
			w.listItems(it.Children, 1, level+1)
		}
	}
}

func (w *docxWriter) cellXML(content, fill string, widthTwips int, borders string, margins bool) string {
	var sb strings.Builder
	sb.WriteString("<w:tc><w:tcPr>")
	fmt.Fprintf(&sb, `<w:tcW w:w="%d" w:type="dxa"/>`, widthTwips)
	sb.WriteString(borders)
	if fill != "" {
		fmt.Fprintf(&sb, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, fill)
	}
	if margins {
		sb.WriteString(`<w:tcMar><w:top w:w="120" w:type="dxa"/><w:left w:w="180" w:type="dxa"/><w:bottom w:w="120" w:type="dxa"/><w:right w:w="180" w:type="dxa"/></w:tcMar>`)
	}
	sb.WriteString(`<w:vAlign w:val="center"/></w:tcPr>`)
	sb.WriteString(content)
	sb.WriteString("</w:tc>")
	return sb.String()
}

func pXML(pPr, runs string) string {
	return "<w:p><w:pPr>" + pPr + "</w:pPr>" + runs + "</w:p>"
}

func (w *docxWriter) kpis(kpis []KPI) {
	t := w.t
	perRow := len(kpis)
	if perRow > 4 {
		perRow = 4
	}
	if perRow == 0 {
		return
	}
	colW := docxTextW / perRow
	w.body.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:tblCellSpacing w:w="60" w:type="dxa"/><w:tblBorders><w:top w:val="nil"/><w:left w:val="nil"/><w:bottom w:val="nil"/><w:right w:val="nil"/><w:insideH w:val="nil"/><w:insideV w:val="nil"/></w:tblBorders><w:tblLayout w:type="fixed"/><w:tblLook w:val="0000"/></w:tblPr><w:tblGrid>`)
	for i := 0; i < perRow; i++ {
		fmt.Fprintf(&w.body, `<w:gridCol w:w="%d"/>`, colW)
	}
	w.body.WriteString(`</w:tblGrid>`)
	for start := 0; start < len(kpis); start += perRow {
		w.body.WriteString(`<w:tr><w:trPr><w:cantSplit/></w:trPr>`)
		for i := 0; i < perRow; i++ {
			idx := start + i
			if idx >= len(kpis) {
				w.body.WriteString(w.cellXML(pXML("", ""), "", colW, "", false))
				continue
			}
			k := kpis[idx]
			var content strings.Builder
			content.WriteString(pXML(`<w:spacing w:after="40"/>`, w.run(k.Label, runStyle{size: 18, color: t.Muted})))
			// 数值字号随卡片宽度与文本长度自适应，避免「¥1,234.5万」在窄卡片中折行
			vsize := 36
			for vsize > 20 && DisplayWidth(k.Value)*vsize*10*52/100 > colW-360 {
				vsize -= 2
			}
			content.WriteString(pXML(`<w:spacing w:after="40"/>`, w.run(k.Value, runStyle{size: vsize, color: t.Primary, bold: true})))
			if k.Change != "" {
				color, arrow := t.Muted, ""
				switch k.Trend {
				case "up":
					color, arrow = t.Up, "▲ "
				case "down":
					color, arrow = t.Down, "▼ "
				}
				content.WriteString(pXML(`<w:spacing w:after="20"/>`, w.run(arrow+k.Change, runStyle{size: 19, color: color, bold: true})))
			}
			if k.Note != "" {
				content.WriteString(pXML(`<w:spacing w:after="0"/>`, w.run(k.Note, runStyle{size: 16, color: t.Muted})))
			}
			borders := fmt.Sprintf(`<w:tcBorders><w:top w:val="single" w:sz="24" w:space="0" w:color="%s"/><w:left w:val="nil"/><w:bottom w:val="nil"/><w:right w:val="nil"/></w:tcBorders>`, t.Accent)
			w.body.WriteString(w.cellXML(content.String(), t.Surface, colW, borders, true))
		}
		w.body.WriteString(`</w:tr>`)
	}
	w.body.WriteString(`</w:tbl>`)
	w.para("", `<w:spacing w:after="60"/>`, "")
}

func (w *docxWriter) callout(style, title, text string) {
	color := CalloutColor(style)
	var content strings.Builder
	content.WriteString(pXML(`<w:spacing w:after="60"/>`, w.inlineRuns(title, runStyle{bold: true, color: color})))
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") {
			l = "• " + l[2:]
		}
		content.WriteString(pXML(`<w:spacing w:after="40"/>`, w.inlineRuns(l, runStyle{})))
	}
	borders := fmt.Sprintf(`<w:tcBorders><w:top w:val="nil"/><w:left w:val="single" w:sz="36" w:space="0" w:color="%s"/><w:bottom w:val="nil"/><w:right w:val="nil"/></w:tcBorders>`, color)
	w.body.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:tblBorders><w:top w:val="nil"/><w:left w:val="nil"/><w:bottom w:val="nil"/><w:right w:val="nil"/><w:insideH w:val="nil"/><w:insideV w:val="nil"/></w:tblBorders><w:tblLook w:val="0000"/></w:tblPr>`)
	fmt.Fprintf(&w.body, `<w:tblGrid><w:gridCol w:w="%d"/></w:tblGrid><w:tr><w:trPr><w:cantSplit/></w:trPr>`, docxTextW)
	w.body.WriteString(w.cellXML(content.String(), Tint(color, 0.9), docxTextW, borders, true))
	w.body.WriteString(`</w:tr></w:tbl>`)
	w.para("", `<w:spacing w:after="60"/>`, "")
}

func (w *docxWriter) table(title string, tb *Table) {
	t := w.t
	w.tblN++
	caption := fmt.Sprintf("表 %d", w.tblN)
	if title != "" {
		caption += "　" + PlainText(title)
	}
	w.para("Caption", `<w:keepNext/>`, w.run(caption, runStyle{}))

	nCols := len(tb.Headers)
	if nCols == 0 {
		return
	}
	numeric := numericColumns(tb)
	// 列宽按内容显示宽度分配（首列与文本列更宽），总宽度等于版心宽度
	weights := make([]float64, nCols)
	total := 0.0
	for i, h := range tb.Headers {
		mw := DisplayWidth(PlainText(h))
		for ri, r := range tb.Rows {
			if ri > 50 {
				break
			}
			if i < len(r) {
				if dw := DisplayWidth(PlainText(r[i])); dw > mw {
					mw = dw
				}
			}
		}
		if mw < 4 {
			mw = 4
		}
		if mw > 40 {
			mw = 40
		}
		weights[i] = float64(mw)
		total += weights[i]
	}
	widths := make([]int, nCols)
	for i := range widths {
		widths[i] = int(float64(docxTextW) * weights[i] / total)
	}
	fontSize := 19
	if nCols > 6 {
		fontSize = 17
	}
	if nCols > 9 {
		fontSize = 15
	}

	fmt.Fprintf(&w.body, `<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:tblBorders><w:top w:val="single" w:sz="8" w:space="0" w:color="%s"/><w:left w:val="nil"/><w:bottom w:val="single" w:sz="8" w:space="0" w:color="%s"/><w:right w:val="nil"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="%s"/><w:insideV w:val="nil"/></w:tblBorders><w:tblLayout w:type="fixed"/><w:tblCellMar><w:top w:w="60" w:type="dxa"/><w:left w:w="100" w:type="dxa"/><w:bottom w:w="60" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tblCellMar><w:tblLook w:val="04A0"/></w:tblPr><w:tblGrid>`,
		t.Primary, t.Primary, t.Border)
	for _, wd := range widths {
		fmt.Fprintf(&w.body, `<w:gridCol w:w="%d"/>`, wd)
	}
	w.body.WriteString(`</w:tblGrid>`)

	jc := func(i int) string {
		a := ""
		if i < len(tb.Align) {
			a = tb.Align[i]
		}
		if a == "" && numeric[i] {
			a = "right"
		}
		switch a {
		case "right":
			return `<w:jc w:val="right"/>`
		case "center":
			return `<w:jc w:val="center"/>`
		}
		return ""
	}
	// 表头（跨页重复）
	w.body.WriteString(`<w:tr><w:trPr><w:tblHeader/><w:cantSplit/></w:trPr>`)
	for i, h := range tb.Headers {
		w.body.WriteString(w.cellXML(pXML(`<w:spacing w:after="0"/>`+jc(i), w.inlineRuns(h, runStyle{size: fontSize, color: t.HeaderText, bold: true})), t.HeaderBg, widths[i], "", false))
	}
	w.body.WriteString(`</w:tr>`)
	for ri, row := range tb.Rows {
		fill := ""
		if ri%2 == 1 {
			fill = t.Zebra
		}
		w.body.WriteString(`<w:tr><w:trPr><w:cantSplit/></w:trPr>`)
		for i := 0; i < nCols; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			w.body.WriteString(w.cellXML(pXML(`<w:spacing w:after="0"/>`+jc(i), w.inlineRuns(cell, runStyle{size: fontSize})), fill, widths[i], "", false))
		}
		w.body.WriteString(`</w:tr>`)
	}
	w.body.WriteString(`</w:tbl>`)
	if tb.TotalRows > len(tb.Rows) {
		w.para("", `<w:spacing w:before="40"/>`, w.run(fmt.Sprintf("注：共 %d 行，此处展示前 %d 行，完整数据请导出 Excel。", tb.TotalRows, len(tb.Rows)), runStyle{size: 16, color: t.Muted, italic: true}))
	} else {
		w.para("", `<w:spacing w:after="60"/>`, "")
	}
}

func (w *docxWriter) chart(c *ChartSpec) {
	oc := officeChartSpec(c)
	w.charts++
	idx := w.charts
	chartName := fmt.Sprintf("charts/chart%d.xml", idx)
	chartRels := &relsBuilder{}
	wbID := ""
	if wb, err := buildChartWorkbook(oc); err == nil {
		wbName := fmt.Sprintf("embeddings/Microsoft_Excel_Worksheet%d.xlsx", idx)
		w.pkg.add("word/"+wbName, wb)
		wbID = chartRels.add(relPackage, "../"+wbName, false)
	}
	w.pkg.add("word/"+chartName, buildChartXML(oc, chartXMLOpts{theme: w.t, title: "", workbookID: wbID, baseSize: 1000}))
	if wbID != "" {
		w.pkg.addString(fmt.Sprintf("word/charts/_rels/chart%d.xml.rels", idx), chartRels.xml())
	}
	rid := w.rels.add(relChart, chartName, false)

	cx, cy := 5760000, 3240000 // 16cm × 9cm
	if c.IsPieLike() || c.Type == "radar" {
		cy = 3600000
	}
	w.docPr++
	w.para("", `<w:keepNext/><w:spacing w:after="0"/><w:jc w:val="center"/>`, fmt.Sprintf(
		`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:effectExtent l="0" t="0" r="0" b="0"/><wp:docPr id="%d" name="Chart %d"/><wp:cNvGraphicFramePr/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart r:id="%s"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, w.docPr, w.docPr, rid))
	w.figN++
	caption := fmt.Sprintf("图 %d", w.figN)
	if c.Title != "" {
		caption += "　" + c.Title
	}
	if c.Unit != "" {
		caption += "（单位：" + c.Unit + "）"
	}
	w.para("Caption", "", w.run(caption, runStyle{}))
}

// ─── 部件 ─────────────────────────────────────────────────────

func (w *docxWriter) headerXML() string {
	return xmlHeader + `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		fmt.Sprintf(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="4" w:space="4" w:color="%s"/></w:pBdr><w:jc w:val="right"/></w:pPr>%s</w:p>`, w.t.Border, w.run(w.doc.Title, runStyle{size: 16, color: w.t.Muted})) +
		`</w:hdr>`
}

func (w *docxWriter) footerXML() string {
	m := runStyle{size: 16, color: w.t.Muted}
	return xmlHeader + `<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:p><w:pPr><w:jc w:val="center"/></w:pPr>` +
		w.run("第 ", m) + `<w:fldSimple w:instr=" PAGE ">` + w.run("1", m) + `</w:fldSimple>` + w.run(" 页 / 共 ", m) +
		`<w:fldSimple w:instr=" NUMPAGES ">` + w.run("1", m) + `</w:fldSimple>` + w.run(" 页", m) + `</w:p></w:ftr>`
}

func (w *docxWriter) numberingXML() string {
	var sb strings.Builder
	sb.WriteString(xmlHeader + `<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	bullets := []string{"•", "◦", "▪"}
	sb.WriteString(`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for lvl := 0; lvl < 3; lvl++ {
		fmt.Fprintf(&sb, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr><w:rPr><w:color w:val="%s"/></w:rPr></w:lvl>`,
			lvl, bullets[lvl], 420+lvl*420, w.t.Accent)
	}
	sb.WriteString(`</w:abstractNum>`)
	sb.WriteString(`<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>`)
	fmts := []struct{ fmt, text string }{{"decimal", "%1."}, {"lowerLetter", "%2)"}, {"lowerRoman", "%3."}}
	for lvl := 0; lvl < 3; lvl++ {
		fmt.Fprintf(&sb, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="400"/></w:pPr></w:lvl>`,
			lvl, fmts[lvl].fmt, fmts[lvl].text, 420+lvl*420)
	}
	sb.WriteString(`</w:abstractNum>`)
	sb.WriteString(`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`)
	for _, id := range w.numIDs {
		fmt.Fprintf(&sb, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`, id)
	}
	sb.WriteString(`</w:numbering>`)
	return sb.String()
}

func (w *docxWriter) stylesXML() string {
	t := w.t
	font := t.FontFamily
	pStyle := func(id, name, based, pPr, rPr string, extra string) string {
		b := ""
		if based != "" {
			b = `<w:basedOn w:val="` + based + `"/>`
		}
		return fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="%s"><w:name w:val="%s"/>%s%s<w:qFormat/><w:pPr>%s</w:pPr><w:rPr>%s</w:rPr></w:style>`, id, name, b, extra, pPr, rPr)
	}
	heading := func(level, size int, color string, before, after int) string {
		return pStyle(fmt.Sprintf("Heading%d", level), fmt.Sprintf("heading %d", level), "Normal",
			fmt.Sprintf(`<w:keepNext/><w:keepLines/><w:spacing w:before="%d" w:after="%d" w:line="300" w:lineRule="auto"/><w:outlineLvl w:val="%d"/>`, before, after, level-1),
			fmt.Sprintf(`<w:b/><w:bCs/><w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/>`, color, size, size), `<w:next w:val="Normal"/>`)
	}
	var sb strings.Builder
	sb.WriteString(xmlHeader + `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	fmt.Fprintf(&sb, `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="%[1]s" w:hAnsi="%[1]s" w:eastAsia="%[1]s" w:cs="%[1]s"/><w:color w:val="%[2]s"/><w:sz w:val="21"/><w:szCs w:val="21"/><w:lang w:val="en-US" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="360" w:lineRule="auto"/><w:jc w:val="both"/></w:pPr></w:pPrDefault></w:docDefaults>`, font, t.Text)
	sb.WriteString(`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`)
	sb.WriteString(`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/><w:uiPriority w:val="1"/><w:semiHidden/></w:style>`)
	sb.WriteString(`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/><w:semiHidden/><w:tblPr><w:tblInd w:w="0" w:type="dxa"/><w:tblCellMar><w:top w:w="0" w:type="dxa"/><w:left w:w="108" w:type="dxa"/><w:bottom w:w="0" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	sb.WriteString(heading(1, 36, t.Primary, 480, 200))
	sb.WriteString(heading(2, 30, t.Primary, 360, 160))
	sb.WriteString(heading(3, 26, t.Accent, 280, 120))
	sb.WriteString(heading(4, 23, t.Text, 200, 100))
	sb.WriteString(pStyle("CoverTitle", "Cover Title", "Normal", `<w:spacing w:after="360" w:line="240" w:lineRule="auto"/><w:jc w:val="left"/>`, fmt.Sprintf(`<w:b/><w:color w:val="%s"/><w:sz w:val="60"/><w:szCs w:val="60"/>`, t.Primary), ""))
	sb.WriteString(pStyle("CoverSubtitle", "Cover Subtitle", "Normal", `<w:spacing w:after="200"/><w:jc w:val="left"/>`, fmt.Sprintf(`<w:color w:val="%s"/><w:sz w:val="30"/><w:szCs w:val="30"/>`, t.Muted), ""))
	sb.WriteString(pStyle("TOCHeading", "TOC Heading", "Normal", `<w:spacing w:before="200" w:after="300"/><w:jc w:val="center"/>`, fmt.Sprintf(`<w:b/><w:color w:val="%s"/><w:sz w:val="32"/><w:szCs w:val="32"/>`, t.Primary), ""))
	for lvl := 1; lvl <= 3; lvl++ {
		b := ""
		if lvl == 1 {
			b = "<w:b/>"
		}
		sb.WriteString(pStyle(fmt.Sprintf("TOC%d", lvl), fmt.Sprintf("toc %d", lvl), "Normal",
			fmt.Sprintf(`<w:tabs><w:tab w:val="right" w:leader="dot" w:pos="%d"/></w:tabs><w:spacing w:after="80"/><w:ind w:left="%d"/><w:jc w:val="left"/>`, docxTextW, (lvl-1)*420), b, ""))
	}
	sb.WriteString(pStyle("Caption", "caption", "Normal", `<w:spacing w:before="60" w:after="200"/><w:jc w:val="center"/>`, fmt.Sprintf(`<w:color w:val="%s"/><w:sz w:val="18"/><w:szCs w:val="18"/>`, t.Muted), ""))
	sb.WriteString(pStyle("ChartTitle", "Chart Title", "Normal", `<w:spacing w:before="200" w:after="60"/><w:jc w:val="center"/>`, fmt.Sprintf(`<w:b/><w:color w:val="%s"/><w:sz w:val="22"/><w:szCs w:val="22"/>`, t.Text), ""))
	sb.WriteString(pStyle("Quote", "Quote", "Normal", fmt.Sprintf(`<w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="%s"/></w:pBdr><w:shd w:val="clear" w:color="auto" w:fill="%s"/><w:spacing w:before="120" w:after="120"/><w:ind w:left="240" w:right="240"/>`, t.Border, t.Surface), fmt.Sprintf(`<w:i/><w:color w:val="%s"/>`, t.Muted), ""))
	sb.WriteString(pStyle("CodeBlock", "Code Block", "Normal", `<w:shd w:val="clear" w:color="auto" w:fill="F5F7FA"/><w:spacing w:after="0" w:line="264" w:lineRule="auto"/><w:ind w:left="120" w:right="120"/><w:jc w:val="left"/>`, fmt.Sprintf(`<w:rFonts w:ascii="%[1]s" w:hAnsi="%[1]s" w:cs="%[1]s"/><w:sz w:val="18"/><w:szCs w:val="18"/>`, t.MonoFont), ""))
	sb.WriteString(pStyle("ListParagraph", "List Paragraph", "Normal", `<w:spacing w:after="60"/><w:ind w:left="420"/><w:contextualSpacing/>`, "", ""))
	sb.WriteString(`</w:styles>`)
	return sb.String()
}
