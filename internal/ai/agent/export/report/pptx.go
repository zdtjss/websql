package report

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// RenderPPTX 渲染原生 PowerPoint（16:9）：封面、目录、章节页、指标卡页、原生可编辑图表页
// （可附洞察面板）、分页表格、要点页（按版面容量自动分页）、致谢页。
func RenderPPTX(doc *Document, theme Theme) ([]byte, error) {
	b := &pptxBuilder{doc: doc, t: theme, pkg: newZipPackage()}
	b.plan()
	return b.build()
}

// PlanSlides 返回演示稿页与源块的映射，供对话式页级编辑定位。
// 页由 plan() 程序化生成（封面/目录/章节页/KPI/图表/表格/要点页/致谢），
// 因此映射必须在同一排版算法下现算，不能持久化。
func PlanSlides(doc *Document) []SlideRef {
	if doc == nil {
		return nil
	}
	b := &pptxBuilder{doc: doc, t: businessTheme, pkg: newZipPackage()}
	b.plan()
	return b.slideRefs
}

const (
	slideW   = 12192000
	slideH   = 6858000
	emuPt    = 12700
	marginX  = 640080  // 0.7in
	bodyTop  = 1371600 // 1.5in
	bodyBot  = 6217920 // 6.8in
	bodyW    = slideW - 2*marginX
	bodyH    = bodyBot - bodyTop
	lineGap  = 1.25 // 行高系数
	maxTable = 10   // 每页表格行数
	maxCols  = 8
)

type pptxSlide struct {
	shapes strings.Builder
	rels   *relsBuilder
	nextID int
	bg     string
}

type pptxBuilder struct {
	doc    *Document
	t      Theme
	pkg    *zipPackage
	slides []*pptxSlide
	charts int
	// 版面规划
	curTitle  string
	curKicker string
	pending   []Block
	sectionNo int
	multiH1   bool
	// 页与源块映射：curIdx/curEnd 为当前正在排版的块下标，pendingIdx 与 pending 一一对应。
	curIdx     int
	curEnd     int
	pendingIdx []int
	slideRefs  []SlideRef
}

// markSlide 记录最近创建的一页与源块的对应关系（块下标为 0-based，-1 表示无源块）。
func (b *pptxBuilder) markSlide(kind, title string, start, end int) {
	b.slideRefs = append(b.slideRefs, SlideRef{
		Index: len(b.slides), Title: title, Kind: kind, StartBlock: start, EndBlock: end,
	})
}

func (b *pptxBuilder) newSlide(bg string) *pptxSlide {
	s := &pptxSlide{rels: &relsBuilder{}, nextID: 2, bg: bg}
	s.rels.add(relSlideLay, "../slideLayouts/slideLayout1.xml", false)
	b.slides = append(b.slides, s)
	return s
}

func (s *pptxSlide) id() int {
	s.nextID++
	return s.nextID
}

// ─── 基础图形 ─────────────────────────────────────────────────

type textRun struct {
	text   string
	size   float64
	color  string
	bold   bool
	italic bool
	mono   bool
}

type textPara struct {
	runs   []textRun
	align  string // l|ctr|r
	bullet string // 项目符号字符；空表示无
	level  int
	spcAft float64 // 段后（磅）
}

func (b *pptxBuilder) runXML(r textRun) string {
	font := b.t.FontFamily
	if r.mono {
		font = b.t.MonoFont
	}
	attrs := fmt.Sprintf(` lang="zh-CN" sz="%d"`, int(r.size*100))
	if r.bold {
		attrs += ` b="1"`
	}
	if r.italic {
		attrs += ` i="1"`
	}
	return fmt.Sprintf(`<a:r><a:rPr%s dirty="0">%s<a:latin typeface="%s"/><a:ea typeface="%s"/><a:cs typeface="%s"/></a:rPr><a:t>%s</a:t></a:r>`,
		attrs, dmlSolid(r.color), font, b.t.FontFamily, font, xmlEscape(r.text))
}

func (b *pptxBuilder) parasXML(paras []textPara) string {
	var sb strings.Builder
	for _, p := range paras {
		algn := p.align
		if algn == "" {
			algn = "l"
		}
		sb.WriteString(`<a:p>`)
		ppr := fmt.Sprintf(`<a:pPr algn="%s"`, algn)
		if p.bullet != "" {
			indent := 285750
			ppr += fmt.Sprintf(` marL="%d" indent="-%d"`, indent*(p.level+1), indent)
		}
		ppr += `><a:lnSpc><a:spcPct val="115000"/></a:lnSpc>`
		if p.spcAft > 0 {
			ppr += fmt.Sprintf(`<a:spcAft><a:spcPts val="%d"/></a:spcAft>`, int(p.spcAft*100))
		}
		if p.bullet != "" {
			ppr += fmt.Sprintf(`<a:buClr><a:srgbClr val="%s"/></a:buClr><a:buFont typeface="Arial"/><a:buChar char="%s"/>`, b.t.Accent, xmlEscape(p.bullet))
		} else {
			ppr += `<a:buNone/>`
		}
		ppr += `</a:pPr>`
		sb.WriteString(ppr)
		for _, r := range p.runs {
			if r.text == "" {
				continue
			}
			sb.WriteString(b.runXML(r))
		}
		if len(p.runs) > 0 {
			fmt.Fprintf(&sb, `<a:endParaRPr lang="zh-CN" sz="%d" dirty="0"/>`, int(p.runs[0].size*100))
		}
		sb.WriteString(`</a:p>`)
	}
	return sb.String()
}

// textBox 文本框（anchor: t|ctr|b）
func (b *pptxBuilder) textBox(s *pptxSlide, x, y, w, h int, paras []textPara, anchor string) {
	if anchor == "" {
		anchor = "t"
	}
	id := s.id()
	fmt.Fprintf(&s.shapes, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Text %d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>`,
		id, id, x, y, w, h)
	fmt.Fprintf(&s.shapes, `<p:txBody><a:bodyPr wrap="square" lIns="0" tIns="0" rIns="0" bIns="0" anchor="%s"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`, anchor, b.parasXML(paras))
}

// shape 几何图形（可带文本）
func (b *pptxBuilder) shape(s *pptxSlide, prst string, x, y, w, h int, fill, line string, alpha int, paras []textPara, anchor string) {
	id := s.id()
	geom := fmt.Sprintf(`<a:prstGeom prst="%s"><a:avLst/></a:prstGeom>`, prst)
	if prst == "roundRect" {
		geom = `<a:prstGeom prst="roundRect"><a:avLst><a:gd name="adj" fmla="val 7000"/></a:avLst></a:prstGeom>`
	}
	fillXML := `<a:noFill/>`
	if fill != "" {
		if alpha > 0 && alpha < 100 {
			fillXML = fmt.Sprintf(`<a:solidFill><a:srgbClr val="%s"><a:alpha val="%d"/></a:srgbClr></a:solidFill>`, fill, alpha*1000)
		} else {
			fillXML = dmlSolid(fill)
		}
	}
	lineXML := `<a:ln><a:noFill/></a:ln>`
	if line != "" {
		lineXML = `<a:ln w="12700">` + dmlSolid(line) + `</a:ln>`
	}
	fmt.Fprintf(&s.shapes, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Shape %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>%s%s%s</p:spPr>`,
		id, id, x, y, w, h, geom, fillXML, lineXML)
	if len(paras) > 0 {
		if anchor == "" {
			anchor = "ctr"
		}
		fmt.Fprintf(&s.shapes, `<p:txBody><a:bodyPr wrap="square" lIns="91440" tIns="45720" rIns="91440" bIns="45720" anchor="%s"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody>`, anchor, b.parasXML(paras))
	}
	s.shapes.WriteString(`</p:sp>`)
}

func (b *pptxBuilder) runsFromInline(text string, size float64, color string, bold bool) []textRun {
	var runs []textRun
	for _, sp := range ParseInline(text) {
		c := color
		if sp.Bold && !bold {
			c = b.t.Accent
			if !b.t.Dark {
				c = b.t.Primary
			}
		}
		runs = append(runs, textRun{text: sp.Text, size: size, color: c, bold: bold || sp.Bold, italic: sp.Italic, mono: sp.Code})
	}
	return runs
}

// estimateLines 估算文本在给定宽度下的行数
func estimateLines(text string, sizePt float64, widthEMU int) int {
	charW := sizePt * 0.52 * emuPt // DisplayWidth 单位宽度
	perLine := float64(widthEMU) / charW
	if perLine < 4 {
		perLine = 4
	}
	lines := 0
	for _, l := range strings.Split(text, "\n") {
		lines += int(math.Max(1, math.Ceil(float64(DisplayWidth(l))/perLine)))
	}
	return lines
}

func heightFor(lines int, sizePt float64) int {
	return int(float64(lines) * sizePt * lineGap * emuPt)
}

// ─── 页面装饰 ─────────────────────────────────────────────────

func (b *pptxBuilder) titleBar(s *pptxSlide, title, kicker string) {
	t := b.t
	y := 420000
	if kicker != "" {
		b.textBox(s, marginX, 300000, bodyW, 280000, []textPara{{runs: []textRun{{text: kicker, size: 12, color: t.Accent, bold: true}}}}, "b")
		y = 600000
	}
	size := 28.0
	if DisplayWidth(title) > 44 {
		size = 22
	}
	b.textBox(s, marginX, y, bodyW, 560000, []textPara{{runs: []textRun{{text: TruncateRunes(PlainText(title), 60), size: size, color: t.Text, bold: true}}}}, "ctr")
	b.shape(s, "rect", marginX, y+600000, 820000, 45720, t.Accent, "", 0, nil, "")
}

func (b *pptxBuilder) footer(s *pptxSlide, page int) {
	t := b.t
	b.textBox(s, marginX, 6400800, bodyW/2, 250000, []textPara{{runs: []textRun{{text: TruncateRunes(b.doc.Title, 40), size: 10, color: t.Muted}}}}, "ctr")
	b.textBox(s, slideW-marginX-1200000, 6400800, 1200000, 250000, []textPara{{align: "r", runs: []textRun{{text: fmt.Sprintf("%d", page), size: 10, color: t.Muted}}}}, "ctr")
}

// ─── 版面规划 ─────────────────────────────────────────────────

func (b *pptxBuilder) plan() {
	h1 := 0
	for _, bl := range b.doc.Blocks {
		if bl.Kind == KindHeading && bl.Level == 1 {
			h1++
		}
	}
	b.multiH1 = h1 >= 2

	b.coverSlide()
	b.agendaSlide()

	blocks := b.doc.Blocks
	for i := 0; i < len(blocks); i++ {
		b.curIdx, b.curEnd = i, i
		bl := blocks[i]
		switch bl.Kind {
		case KindHeading:
			b.flush()
			switch {
			case bl.Level == 1 && b.multiH1:
				b.sectionNo++
				b.sectionSlide(b.sectionNo, PlainText(bl.Text))
				b.curTitle, b.curKicker = PlainText(bl.Text), ""
			case bl.Level <= 2:
				b.curTitle, b.curKicker = PlainText(bl.Text), ""
			default:
				if b.curKicker == "" {
					b.curKicker = b.curTitle
				}
				b.curTitle = PlainText(bl.Text)
			}
		case KindKPI:
			// 指标卡前的简短导语（≤4 行）与指标卡同页展示，避免单独成页
			var lead []Block
			if len(b.pending) > 0 && b.pendingLines() <= 4 {
				lead, b.pending = b.pending, nil
				b.pendingIdx = nil
			} else {
				b.flush()
			}
			b.kpiSlides(bl.KPIs, lead)
		case KindChart:
			b.flush()
			// 紧随图表的短文本作为洞察面板
			var insights []Block
			lines := 0
			for j := i + 1; j < len(blocks); j++ {
				nb := blocks[j]
				if nb.Kind != KindParagraph && nb.Kind != KindList && nb.Kind != KindCallout {
					break
				}
				l := b.blockLines(nb, 14, bodyW*34/100)
				if lines+l > 14 {
					break
				}
				lines += l
				insights = append(insights, nb)
				i = j
			}
			b.curEnd = i
			b.chartSlide(bl, insights)
		case KindTable:
			b.flush()
			b.tableSlides(bl)
		case KindPageBreak:
			b.flush()
		case KindDivider:
		default:
			b.pending = append(b.pending, bl)
			b.pendingIdx = append(b.pendingIdx, i)
			if b.pendingLines() > b.capacityLines() {
				last := b.pending[len(b.pending)-1]
				lastIdx := b.pendingIdx[len(b.pendingIdx)-1]
				b.pending = b.pending[:len(b.pending)-1]
				b.pendingIdx = b.pendingIdx[:len(b.pendingIdx)-1]
				b.flush()
				b.pending = []Block{last}
				b.pendingIdx = []int{lastIdx}
			}
		}
	}
	b.flush()
	b.closingSlide()
}

const bodyFont = 18.0

func (b *pptxBuilder) capacityLines() int {
	h, unit := float64(bodyH), bodyFont*lineGap*emuPt
	return int(h / unit)
}

// blockLines 估算块占用行数（含段间距折算）
func (b *pptxBuilder) blockLines(bl Block, size float64, width int) int {
	switch bl.Kind {
	case KindParagraph, KindQuote:
		return estimateLines(PlainText(bl.Text), size, width) + 1
	case KindList:
		n := 0
		for _, it := range bl.Items {
			n += estimateLines(PlainText(it.Text), size, width-300000)
			for _, c := range it.Children {
				n += estimateLines(PlainText(c.Text), size-2, width-600000)
			}
		}
		return n + 1
	case KindCallout:
		return estimateLines(PlainText(bl.Text), size-2, width-400000) + 3
	case KindCode, KindMermaid:
		n := strings.Count(bl.Code, "\n") + 1
		if n > 14 {
			n = 14
		}
		return int(math.Ceil(float64(n)*0.75)) + 2
	}
	return 1
}

func (b *pptxBuilder) pendingLines() int {
	n := 0
	for _, bl := range b.pending {
		n += b.blockLines(bl, bodyFont, bodyW)
	}
	return n
}

func (b *pptxBuilder) pageTitle(defaultTitle string) string {
	if b.curTitle != "" {
		return b.curTitle
	}
	return defaultTitle
}

// flush 把累积的文本块输出为要点页
func (b *pptxBuilder) flush() {
	if len(b.pending) == 0 {
		return
	}
	blocks := b.pending
	start, end := b.curIdx, b.curIdx
	if len(b.pendingIdx) > 0 {
		start, end = b.pendingIdx[0], b.pendingIdx[len(b.pendingIdx)-1]
	}
	b.pending = nil
	b.pendingIdx = nil
	title := b.pageTitle("概述")
	s := b.newSlide(b.t.Background)
	b.titleBar(s, title, b.curKicker)
	y := bodyTop
	for _, bl := range blocks {
		h := b.renderFlowBlock(s, bl, marginX, y, bodyW, bodyFont)
		y += h + 120000
	}
	b.footer(s, len(b.slides))
	b.curKicker = ""
	b.markSlide("flow", title, start, end)
}

// renderFlowBlock 在 (x,y) 处绘制文本类块，返回占用高度
func (b *pptxBuilder) renderFlowBlock(s *pptxSlide, bl Block, x, y, w int, size float64) int {
	t := b.t
	switch bl.Kind {
	case KindParagraph:
		lines := estimateLines(PlainText(bl.Text), size, w)
		h := heightFor(lines, size)
		b.textBox(s, x, y, w, h, []textPara{{runs: b.runsFromInline(bl.Text, size, t.Text, false)}}, "t")
		return h
	case KindQuote:
		lines := estimateLines(PlainText(bl.Text), size-1, w-200000)
		h := heightFor(lines, size-1) + 80000
		b.shape(s, "rect", x, y, 54864, h, t.Accent, "", 0, nil, "")
		b.textBox(s, x+200000, y+40000, w-200000, h-40000, []textPara{{runs: []textRun{{text: PlainText(bl.Text), size: size - 1, color: t.Muted, italic: true}}}}, "t")
		return h
	case KindList:
		var paras []textPara
		lines := 0
		for i, it := range bl.Items {
			bullet := "•"
			if bl.Ordered {
				bullet = fmt.Sprintf("%d.", i+1)
			}
			paras = append(paras, textPara{bullet: bullet, runs: b.runsFromInline(it.Text, size, t.Text, false), spcAft: 4})
			lines += estimateLines(PlainText(it.Text), size, w-300000)
			for _, c := range it.Children {
				paras = append(paras, textPara{bullet: "◦", level: 1, runs: b.runsFromInline(c.Text, size-2, t.Muted, false), spcAft: 2})
				lines += estimateLines(PlainText(c.Text), size-2, w-600000)
			}
		}
		h := heightFor(lines, size) + len(paras)*50800
		b.textBox(s, x, y, w, h, paras, "t")
		return h
	case KindCallout:
		color := CalloutColor(bl.Style)
		fill := Tint(color, 0.88)
		textColor := t.Text
		if t.Dark {
			fill = Shade(color, 0.72)
			textColor = "FFFFFF"
		}
		title := firstNonEmpty(bl.Title, CalloutLabel(bl.Style))
		paras := []textPara{{runs: []textRun{{text: PlainText(title), size: size - 2, color: color, bold: true}}, spcAft: 4}}
		lines := 1
		for _, l := range strings.Split(bl.Text, "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") {
				paras = append(paras, textPara{bullet: "•", runs: b.runsFromInline(l[2:], size-3, textColor, false)})
			} else {
				paras = append(paras, textPara{runs: b.runsFromInline(l, size-3, textColor, false)})
			}
			lines += estimateLines(PlainText(l), size-3, w-400000)
		}
		h := heightFor(lines, size-2) + 200000
		b.shape(s, "roundRect", x, y, w, h, fill, "", 0, paras, "t")
		b.shape(s, "rect", x, y, 64008, h, color, "", 0, nil, "")
		return h
	case KindCode, KindMermaid:
		lines := strings.Split(strings.TrimRight(bl.Code, "\n"), "\n")
		if len(lines) > 14 {
			lines = append(lines[:13], "…")
		}
		var paras []textPara
		if bl.Kind == KindMermaid {
			paras = append(paras, textPara{runs: []textRun{{text: "图示源码（可在 HTML 版报告中查看渲染效果）", size: 11, color: t.Muted, italic: true}}})
		}
		for _, l := range lines {
			paras = append(paras, textPara{runs: []textRun{{text: l, size: 11, color: t.Text, mono: true}}})
		}
		h := heightFor(len(paras), 11) + 160000
		b.shape(s, "roundRect", x, y, w, h, t.Surface, t.Border, 0, paras, "t")
		return h
	}
	return 0
}

// ─── 页面类型 ─────────────────────────────────────────────────

func (b *pptxBuilder) coverSlide() {
	t := b.t
	s := b.newSlide(t.Background)
	// 装饰：右上角大圆 + 左侧强调条 + 底部色带
	b.shape(s, "ellipse", slideW-4200000, -1800000, 5600000, 5600000, t.Accent, "", 12, nil, "")
	b.shape(s, "ellipse", slideW-2600000, 2800000, 3000000, 3000000, t.Primary, "", 10, nil, "")
	b.shape(s, "rect", marginX, 2240000, 91440, 1600000, t.Accent, "", 0, nil, "")
	b.textBox(s, marginX+300000, 1700000, 9000000, 400000, []textPara{{runs: []textRun{{text: "DATA ANALYSIS REPORT", size: 14, color: t.Accent, bold: true}}}}, "b")
	size := 40.0
	if DisplayWidth(b.doc.Title) > 30 {
		size = 32
	}
	b.textBox(s, marginX+300000, 2200000, 9600000, 1300000, []textPara{{runs: []textRun{{text: b.doc.Title, size: size, color: t.Text, bold: true}}}}, "ctr")
	if b.doc.Subtitle != "" {
		b.textBox(s, marginX+300000, 3500000, 9600000, 600000, []textPara{{runs: []textRun{{text: b.doc.Subtitle, size: 20, color: t.Muted}}}}, "t")
	}
	date := b.doc.Date
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	meta := date
	if b.doc.Author != "" {
		meta += "　|　" + b.doc.Author
	}
	if b.doc.Organization != "" {
		meta += "　|　" + b.doc.Organization
	}
	b.textBox(s, marginX+300000, 5400000, 9000000, 400000, []textPara{{runs: []textRun{{text: meta, size: 14, color: t.Muted}}}}, "ctr")
	b.shape(s, "rect", 0, slideH-91440, slideW, 91440, t.Accent, "", 0, nil, "")
	b.markSlide("cover", b.doc.Title, -1, -1)
}

func (b *pptxBuilder) agendaSlide() {
	level := 2
	if b.multiH1 {
		level = 1
	}
	var items []string
	for _, h := range b.doc.Headings(level) {
		if h.Level == level {
			items = append(items, PlainText(h.Text))
		}
	}
	if len(items) < 3 {
		return
	}
	if len(items) > 12 {
		items = items[:12]
	}
	t := b.t
	s := b.newSlide(t.Background)
	b.titleBar(s, "目录", "CONTENTS")
	cols := 1
	if len(items) > 6 {
		cols = 2
	}
	perCol := int(math.Ceil(float64(len(items)) / float64(cols)))
	colW := bodyW / cols
	rowH := int(math.Min(float64(bodyH)/float64(perCol), 720000))
	for i, it := range items {
		col, row := i/perCol, i%perCol
		x := marginX + col*colW
		y := bodyTop + row*rowH
		b.shape(s, "roundRect", x, y+rowH/2-230000, 560000, 460000, t.Accent, "", 0,
			[]textPara{{align: "ctr", runs: []textRun{{text: fmt.Sprintf("%02d", i+1), size: 16, color: pickOn(t.Accent), bold: true}}}}, "ctr")
		b.textBox(s, x+760000, y, colW-900000, rowH, []textPara{{runs: []textRun{{text: TruncateRunes(it, 28), size: 20, color: t.Text}}}}, "ctr")
	}
	b.footer(s, len(b.slides))
	b.markSlide("agenda", "目录", -1, -1)
}

func (b *pptxBuilder) sectionSlide(no int, title string) {
	t := b.t
	s := b.newSlide(t.Background)
	b.shape(s, "ellipse", -1500000, 3600000, 4200000, 4200000, t.Accent, "", 10, nil, "")
	b.textBox(s, 1800000, 1900000, 3000000, 1400000, []textPara{{runs: []textRun{{text: fmt.Sprintf("%02d", no), size: 80, color: t.Accent, bold: true}}}}, "ctr")
	b.shape(s, "rect", 1800000, 3450000, 1200000, 60960, t.Accent, "", 0, nil, "")
	b.textBox(s, 1800000, 3650000, 9000000, 1000000, []textPara{{runs: []textRun{{text: title, size: 36, color: t.Text, bold: true}}}}, "t")
	b.footer(s, len(b.slides))
	b.markSlide("section", title, b.curIdx, b.curIdx)
}

func (b *pptxBuilder) kpiSlides(kpis []KPI, lead []Block) {
	t := b.t
	for start := 0; start < len(kpis); start += 8 {
		end := start + 8
		if end > len(kpis) {
			end = len(kpis)
		}
		page := kpis[start:end]
		title := b.pageTitle("核心指标概览")
		s := b.newSlide(t.Background)
		b.titleBar(s, title, b.curKicker)
		areaTop, areaH := bodyTop, bodyH
		if start == 0 && len(lead) > 0 {
			y := bodyTop
			for _, lb := range lead {
				y += b.renderFlowBlock(s, lb, marginX, y, bodyW, 16) + 80000
			}
			areaTop, areaH = y+120000, bodyBot-y-120000
		}
		cols := len(page)
		rows := 1
		if cols > 4 {
			rows = 2
			cols = int(math.Ceil(float64(len(page)) / 2))
		}
		gap := 228600
		cardW := (bodyW - gap*(cols-1)) / cols
		cardH := 2600000
		if rows == 2 {
			cardH = 2250000
		}
		if maxH := (areaH - (rows-1)*gap) / rows; cardH > maxH {
			cardH = maxH
		}
		y0 := areaTop + (areaH-rows*cardH-(rows-1)*gap)/2
		for i, k := range page {
			col, row := i%cols, i/cols
			x := marginX + col*(cardW+gap)
			y := y0 + row*(cardH+gap)
			b.shape(s, "roundRect", x, y, cardW, cardH, t.Surface, t.Border, 0, nil, "")
			b.shape(s, "rect", x+228600, y, cardW-457200, 54864, t.Accent, "", 0, nil, "")
			inner := cardW - 457200
			b.textBox(s, x+228600, y+cardH*10/100, inner, 420000, []textPara{{runs: []textRun{{text: TruncateRunes(k.Label, 18), size: 14, color: t.Muted}}}}, "ctr")
			vsize := 32.0
			for vsize > 16 && float64(DisplayWidth(k.Value))*vsize*0.52*emuPt > float64(inner) {
				vsize -= 2
			}
			b.textBox(s, x+228600, y+cardH*27/100, inner, cardH*32/100, []textPara{{runs: []textRun{{text: k.Value, size: vsize, color: t.Text, bold: true}}}}, "ctr")
			if k.Change != "" {
				color, arrow := t.Muted, ""
				switch k.Trend {
				case "up":
					color, arrow = t.Up, "▲ "
				case "down":
					color, arrow = t.Down, "▼ "
				}
				b.textBox(s, x+228600, y+cardH*62/100, inner, 380000, []textPara{{runs: []textRun{{text: arrow + k.Change, size: 16, color: color, bold: true}}}}, "ctr")
			}
			if k.Note != "" && cardH > 2000000 {
				b.textBox(s, x+228600, y+cardH-520000, inner, 400000, []textPara{{runs: []textRun{{text: TruncateRunes(k.Note, 30), size: 11, color: t.Muted}}}}, "ctr")
			}
		}
		b.footer(s, len(b.slides))
		b.markSlide("kpi", title, b.curIdx, b.curIdx)
	}
	b.curKicker = ""
}

func (b *pptxBuilder) chartSlide(bl Block, insights []Block) {
	t := b.t
	s := b.newSlide(t.Background)
	title := b.pageTitle(firstNonEmpty(bl.Chart.Title, "图表分析"))
	b.titleBar(s, title, b.curKicker)
	b.curKicker = ""
	if bl.Error != "" || !bl.Chart.HasData() {
		b.renderFlowBlock(s, Block{Kind: KindCallout, Style: "warning", Title: "图表未能生成", Text: firstNonEmpty(bl.Error, "无数据")}, marginX, bodyTop, bodyW, bodyFont)
		b.footer(s, len(b.slides))
		b.markSlide("chart", title, b.curIdx, b.curEnd)
		return
	}
	chartW := bodyW
	if len(insights) > 0 {
		chartW = bodyW * 62 / 100
	}
	// 图内标题：与页面标题不同时才显示
	inner := ""
	if bl.Chart.Title != "" && bl.Chart.Title != title {
		inner = bl.Chart.Title
	}
	if bl.Chart.Unit != "" {
		if inner != "" {
			inner += "（" + bl.Chart.Unit + "）"
		} else {
			inner = "单位：" + bl.Chart.Unit
		}
	}
	b.chartFrame(s, bl.Chart, marginX, bodyTop, chartW, bodyH, inner)
	if len(insights) > 0 {
		px := marginX + chartW + 228600
		pw := bodyW - chartW - 228600
		b.shape(s, "roundRect", px, bodyTop, pw, bodyH, t.Surface, t.Border, 0, nil, "")
		b.textBox(s, px+228600, bodyTop+180000, pw-457200, 400000, []textPara{{runs: []textRun{{text: "洞察", size: 16, color: t.Accent, bold: true}}}}, "ctr")
		y := bodyTop + 680000
		for _, ib := range insights {
			h := b.renderFlowBlock(s, ib, px+228600, y, pw-457200, 14)
			y += h + 100000
		}
	}
	b.footer(s, len(b.slides))
	b.markSlide("chart", title, b.curIdx, b.curEnd)
}

func (b *pptxBuilder) chartFrame(s *pptxSlide, c *ChartSpec, x, y, w, h int, innerTitle string) {
	oc := officeChartSpec(c)
	b.charts++
	idx := b.charts
	chartRels := &relsBuilder{}
	wbID := ""
	if wb, err := buildChartWorkbook(oc); err == nil {
		name := fmt.Sprintf("ppt/embeddings/Microsoft_Excel_Worksheet%d.xlsx", idx)
		b.pkg.add(name, wb)
		wbID = chartRels.add(relPackage, fmt.Sprintf("../embeddings/Microsoft_Excel_Worksheet%d.xlsx", idx), false)
	}
	b.pkg.add(fmt.Sprintf("ppt/charts/chart%d.xml", idx), buildChartXML(oc, chartXMLOpts{theme: b.t, title: innerTitle, workbookID: wbID, baseSize: 1200, transparent: true}))
	if wbID != "" {
		b.pkg.addString(fmt.Sprintf("ppt/charts/_rels/chart%d.xml.rels", idx), chartRels.xml())
	}
	rid := s.rels.add(relChart, fmt.Sprintf("../charts/chart%d.xml", idx), false)
	id := s.id()
	fmt.Fprintf(&s.shapes, `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="Chart %d"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="%s"/></a:graphicData></a:graphic></p:graphicFrame>`,
		id, id, x, y, w, h, rid)
}

func (b *pptxBuilder) tableSlides(bl Block) {
	t := b.t
	title := b.pageTitle(firstNonEmpty(PlainText(bl.Title), "数据明细"))
	if bl.Error != "" || bl.Table == nil || len(bl.Table.Headers) == 0 {
		s := b.newSlide(t.Background)
		b.titleBar(s, title, b.curKicker)
		b.renderFlowBlock(s, Block{Kind: KindCallout, Style: "warning", Title: "数据表未能生成", Text: firstNonEmpty(bl.Error, "无数据")}, marginX, bodyTop, bodyW, bodyFont)
		b.footer(s, len(b.slides))
		b.curKicker = ""
		b.markSlide("table", title, b.curIdx, b.curIdx)
		return
	}
	tb := bl.Table
	headers := tb.Headers
	colsTruncated := false
	if len(headers) > maxCols {
		headers = headers[:maxCols]
		colsTruncated = true
	}
	nCols := len(headers)
	numeric := numericColumns(tb)
	rows := tb.Rows
	pages := int(math.Max(1, math.Ceil(float64(len(rows))/maxTable)))
	if pages > 5 {
		pages = 5 // 演示文稿最多 5 页表格，完整数据见附件
	}
	fontSize := 12.0
	if nCols > 5 {
		fontSize = 11
	}
	if nCols > 7 {
		fontSize = 10
	}
	// 列宽按内容显示宽度分配
	weights := make([]float64, nCols)
	total := 0.0
	for i, h := range headers {
		mw := DisplayWidth(PlainText(h))
		for ri, r := range rows {
			if ri > 40 {
				break
			}
			if i < len(r) {
				if dw := DisplayWidth(TruncateRunes(PlainText(r[i]), 30)); dw > mw {
					mw = dw
				}
			}
		}
		weights[i] = math.Max(float64(mw), 4)
		total += weights[i]
	}
	for p := 0; p < pages; p++ {
		start, end := p*maxTable, (p+1)*maxTable
		if end > len(rows) {
			end = len(rows)
		}
		s := b.newSlide(t.Background)
		pt := title
		if pages > 1 {
			pt = fmt.Sprintf("%s（%d/%d）", title, p+1, pages)
		}
		b.titleBar(s, pt, b.curKicker)
		rowH := 380000
		var tbl strings.Builder
		tbl.WriteString(`<a:tbl><a:tblPr firstRow="1" bandRow="1"/><a:tblGrid>`)
		for i := range headers {
			fmt.Fprintf(&tbl, `<a:gridCol w="%d"/>`, int(float64(bodyW)*weights[i]/total))
		}
		tbl.WriteString(`</a:tblGrid>`)
		cell := func(text string, color, fill string, bold bool, align string) string {
			var c strings.Builder
			c.WriteString(`<a:tc><a:txBody><a:bodyPr/><a:lstStyle/>`)
			c.WriteString(b.parasXML([]textPara{{align: align, runs: []textRun{{text: TruncateRunes(PlainText(text), 40), size: fontSize, color: color, bold: bold}}}}))
			c.WriteString(`</a:txBody><a:tcPr marL="68580" marR="68580" marT="34290" marB="34290" anchor="ctr">`)
			c.WriteString(`<a:lnL w="0"><a:noFill/></a:lnL><a:lnR w="0"><a:noFill/></a:lnR><a:lnT w="0"><a:noFill/></a:lnT>`)
			c.WriteString(`<a:lnB w="6350">` + dmlSolid(t.Border) + `</a:lnB>`)
			if fill != "" {
				c.WriteString(dmlSolid(fill))
			} else {
				c.WriteString(`<a:noFill/>`)
			}
			c.WriteString(`</a:tcPr></a:tc>`)
			return c.String()
		}
		align := func(i int) string {
			if i < len(tb.Align) && tb.Align[i] == "center" {
				return "ctr"
			}
			if (i < len(tb.Align) && tb.Align[i] == "right") || (i < len(numeric) && numeric[i]) {
				return "r"
			}
			return "l"
		}
		fmt.Fprintf(&tbl, `<a:tr h="%d">`, rowH)
		for i, h := range headers {
			tbl.WriteString(cell(h, t.HeaderText, t.HeaderBg, true, align(i)))
		}
		tbl.WriteString(`</a:tr>`)
		for ri, r := range rows[start:end] {
			fill := ""
			if ri%2 == 1 {
				fill = t.Zebra
			}
			fmt.Fprintf(&tbl, `<a:tr h="%d">`, rowH)
			for i := range headers {
				v := ""
				if i < len(r) {
					v = r[i]
				}
				tbl.WriteString(cell(v, t.Text, fill, false, align(i)))
			}
			tbl.WriteString(`</a:tr>`)
		}
		tbl.WriteString(`</a:tbl>`)
		id := s.id()
		tblH := rowH * (end - start + 1)
		fmt.Fprintf(&s.shapes, `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="Table %d"/><p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">%s</a:graphicData></a:graphic></p:graphicFrame>`,
			id, id, marginX, bodyTop, bodyW, tblH, tbl.String())
		totalRows := tb.TotalRows
		if totalRows < len(rows) {
			totalRows = len(rows)
		}
		var notes []string
		if p == pages-1 && (totalRows > end || colsTruncated) {
			if totalRows > end {
				notes = append(notes, fmt.Sprintf("共 %d 行，演示仅展示前 %d 行", totalRows, end))
			}
			if colsTruncated {
				notes = append(notes, fmt.Sprintf("共 %d 列，仅展示前 %d 列", len(tb.Headers), maxCols))
			}
		}
		if len(notes) > 0 {
			b.textBox(s, marginX, bodyTop+tblH+120000, bodyW, 300000, []textPara{{runs: []textRun{{text: "※ " + strings.Join(notes, "；") + "，完整数据请导出 Excel", size: 11, color: t.Muted, italic: true}}}}, "t")
		}
		b.footer(s, len(b.slides))
		b.markSlide("table", pt, b.curIdx, b.curIdx)
	}
	b.curKicker = ""
}

func (b *pptxBuilder) closingSlide() {
	t := b.t
	s := b.newSlide(t.Background)
	b.shape(s, "ellipse", slideW/2-3200000, slideH/2-3200000, 6400000, 6400000, t.Accent, "", 8, nil, "")
	b.textBox(s, 0, 2500000, slideW, 1000000, []textPara{{align: "ctr", runs: []textRun{{text: "谢谢观看", size: 44, color: t.Text, bold: true}}}}, "ctr")
	b.textBox(s, 0, 3500000, slideW, 400000, []textPara{{align: "ctr", runs: []textRun{{text: "THANK YOU", size: 16, color: t.Accent, bold: true}}}}, "ctr")
	b.textBox(s, 0, 5600000, slideW, 400000, []textPara{{align: "ctr", runs: []textRun{{text: "本报告由 WebSQL 智能分析生成 · " + time.Now().Format("2006-01-02"), size: 12, color: t.Muted}}}}, "ctr")
	b.markSlide("closing", "致谢页", -1, -1)
}

// pickOn 返回在给定底色上可读的文字颜色
func pickOn(bg string) string {
	r, g, bl := parseHex(bg)
	if 0.299*float64(r)+0.587*float64(g)+0.114*float64(bl) > 150 {
		return "0A1628"
	}
	return "FFFFFF"
}

// ─── 打包 ─────────────────────────────────────────────────────

func (b *pptxBuilder) build() ([]byte, error) {
	p := b.pkg
	pres := &relsBuilder{}
	masterRID := pres.add(relSlideMas, "slideMasters/slideMaster1.xml", false)
	var sldIDs strings.Builder
	for i, s := range b.slides {
		rid := pres.add(relSlide, fmt.Sprintf("slides/slide%d.xml", i+1), false)
		fmt.Fprintf(&sldIDs, `<p:sldId id="%d" r:id="%s"/>`, 256+i, rid)
		p.addString(fmt.Sprintf("ppt/slides/slide%d.xml", i+1), b.slideXML(s))
		p.addString(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1), s.rels.xml())
	}
	pres.add(relPresProps, "presProps.xml", false)
	pres.add(relViewProps, "viewProps.xml", false)
	pres.add(relTheme, "theme/theme1.xml", false)
	pres.add(relTableStyle, "tableStyles.xml", false)

	const ns = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`
	p.addString("ppt/presentation.xml", xmlHeader+`<p:presentation `+ns+` saveSubsetFonts="1">`+
		`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="`+masterRID+`"/></p:sldMasterIdLst>`+
		`<p:sldIdLst>`+sldIDs.String()+`</p:sldIdLst>`+
		fmt.Sprintf(`<p:sldSz cx="%d" cy="%d"/><p:notesSz cx="6858000" cy="9144000"/>`, slideW, slideH)+
		`<p:defaultTextStyle><a:defPPr><a:defRPr lang="zh-CN"/></a:defPPr></p:defaultTextStyle></p:presentation>`)
	p.addString("ppt/_rels/presentation.xml.rels", pres.xml())
	p.addString("ppt/presProps.xml", xmlHeader+`<p:presentationPr `+ns+`/>`)
	p.addString("ppt/viewProps.xml", xmlHeader+`<p:viewPr `+ns+`><p:gridSpacing cx="76200" cy="76200"/></p:viewPr>`)
	p.addString("ppt/tableStyles.xml", xmlHeader+`<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}"/>`)
	p.addString("ppt/theme/theme1.xml", b.themeXML())

	emptyTree := `<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr></p:spTree>`
	lvl := func(tag string, size int) string {
		return fmt.Sprintf(`<p:%s><a:lvl1pPr><a:defRPr sz="%d"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="+mn-lt"/><a:ea typeface="+mn-ea"/></a:defRPr></a:lvl1pPr></p:%s>`, tag, size, b.t.Text, tag)
	}
	p.addString("ppt/slideMasters/slideMaster1.xml", xmlHeader+`<p:sldMaster `+ns+`><p:cSld><p:bg><p:bgPr>`+dmlSolid(b.t.Background)+`<a:effectLst/></p:bgPr></p:bg>`+emptyTree+`</p:cSld>`+
		`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>`+
		`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>`+
		`<p:txStyles>`+lvl("titleStyle", 3200)+lvl("bodyStyle", 1800)+lvl("otherStyle", 1800)+`</p:txStyles></p:sldMaster>`)
	mr := &relsBuilder{}
	mr.add(relSlideLay, "../slideLayouts/slideLayout1.xml", false)
	mr.add(relTheme, "../theme/theme1.xml", false)
	p.addString("ppt/slideMasters/_rels/slideMaster1.xml.rels", mr.xml())
	p.addString("ppt/slideLayouts/slideLayout1.xml", xmlHeader+`<p:sldLayout `+ns+` type="blank" preserve="1"><p:cSld name="Blank">`+emptyTree+`</p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`)
	lr := &relsBuilder{}
	lr.add(relSlideMas, "../slideMasters/slideMaster1.xml", false)
	p.addString("ppt/slideLayouts/_rels/slideLayout1.xml.rels", lr.xml())

	p.addString("docProps/core.xml", corePropsXML(b.doc.Title))
	p.addString("docProps/app.xml", appPropsXML("WebSQL Report"))
	root := &relsBuilder{}
	root.add(relOfficeDoc, "ppt/presentation.xml", false)
	root.add(relCoreProps, "docProps/core.xml", false)
	root.add(relExtProps, "docProps/app.xml", false)
	p.addString("_rels/.rels", root.xml())
	p.addString("[Content_Types].xml", b.contentTypes())
	return p.bytes()
}

func (b *pptxBuilder) slideXML(s *pptxSlide) string {
	return xmlHeader + `<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">` +
		`<p:cSld><p:bg><p:bgPr>` + dmlSolid(s.bg) + `<a:effectLst/></p:bgPr></p:bg><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>` +
		s.shapes.String() + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

func (b *pptxBuilder) contentTypes() string {
	var sb strings.Builder
	sb.WriteString(xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	sb.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="xlsx" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"/>`)
	pml := "application/vnd.openxmlformats-officedocument.presentationml."
	for name, ct := range map[string]string{
		"/ppt/presentation.xml":              pml + "presentation.main+xml",
		"/ppt/slideMasters/slideMaster1.xml": pml + "slideMaster+xml",
		"/ppt/slideLayouts/slideLayout1.xml": pml + "slideLayout+xml",
		"/ppt/presProps.xml":                 pml + "presProps+xml",
		"/ppt/viewProps.xml":                 pml + "viewProps+xml",
		"/ppt/tableStyles.xml":               pml + "tableStyles+xml",
		"/ppt/theme/theme1.xml":              "application/vnd.openxmlformats-officedocument.theme+xml",
		"/docProps/core.xml":                 "application/vnd.openxmlformats-package.core-properties+xml",
		"/docProps/app.xml":                  "application/vnd.openxmlformats-officedocument.extended-properties+xml",
	} {
		fmt.Fprintf(&sb, `<Override PartName="%s" ContentType="%s"/>`, name, ct)
	}
	for i := range b.slides {
		fmt.Fprintf(&sb, `<Override PartName="/ppt/slides/slide%d.xml" ContentType="%sslide+xml"/>`, i+1, pml)
	}
	for i := 1; i <= b.charts; i++ {
		fmt.Fprintf(&sb, `<Override PartName="/ppt/charts/chart%d.xml" ContentType="%s"/>`, i, ctChart)
	}
	sb.WriteString(`</Types>`)
	return sb.String()
}

func (b *pptxBuilder) themeXML() string {
	t := b.t
	dk1, lt1 := "000000", "FFFFFF"
	if t.Dark {
		dk1, lt1 = "FFFFFF", t.Background
	}
	pal := func(i int) string { return t.SeriesColor(i) }
	font := t.FontFamily
	fill3 := dmlSolid(lt1) + dmlSolid(t.Surface) + dmlSolid(t.Accent)
	ln := func(w int) string {
		return fmt.Sprintf(`<a:ln w="%d"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln>`, w)
	}
	return xmlHeader + `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="WebSQL"><a:themeElements>` +
		`<a:clrScheme name="WebSQL">` +
		`<a:dk1><a:srgbClr val="` + dk1 + `"/></a:dk1><a:lt1><a:srgbClr val="` + lt1 + `"/></a:lt1>` +
		`<a:dk2><a:srgbClr val="` + t.Primary + `"/></a:dk2><a:lt2><a:srgbClr val="` + t.Surface + `"/></a:lt2>` +
		fmt.Sprintf(`<a:accent1><a:srgbClr val="%s"/></a:accent1><a:accent2><a:srgbClr val="%s"/></a:accent2><a:accent3><a:srgbClr val="%s"/></a:accent3><a:accent4><a:srgbClr val="%s"/></a:accent4><a:accent5><a:srgbClr val="%s"/></a:accent5><a:accent6><a:srgbClr val="%s"/></a:accent6>`,
			pal(0), pal(1), pal(2), pal(3), pal(4), pal(5)) +
		`<a:hlink><a:srgbClr val="` + t.Accent + `"/></a:hlink><a:folHlink><a:srgbClr val="` + t.Muted + `"/></a:folHlink></a:clrScheme>` +
		`<a:fontScheme name="WebSQL"><a:majorFont><a:latin typeface="` + font + `"/><a:ea typeface="` + font + `"/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="` + font + `"/><a:ea typeface="` + font + `"/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
		`<a:fmtScheme name="WebSQL"><a:fillStyleLst>` + fill3 + `</a:fillStyleLst>` +
		`<a:lnStyleLst>` + ln(6350) + ln(12700) + ln(19050) + `</a:lnStyleLst>` +
		`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
		`<a:bgFillStyleLst>` + fill3 + `</a:bgFillStyleLst></a:fmtScheme>` +
		`</a:themeElements><a:objectDefaults/><a:extraClrSchemeLst/></a:theme>`
}
