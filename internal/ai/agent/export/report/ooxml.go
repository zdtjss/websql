package report

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// zipPackage 按顺序写入的 OPC 包
type zipPackage struct {
	names []string
	data  map[string][]byte
}

func newZipPackage() *zipPackage { return &zipPackage{data: map[string][]byte{}} }

func (p *zipPackage) add(name string, content []byte) {
	if _, ok := p.data[name]; !ok {
		p.names = append(p.names, name)
	}
	p.data[name] = content
}

func (p *zipPackage) addString(name, content string) { p.add(name, []byte(content)) }

func (p *zipPackage) bytes() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range p.names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(p.data[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// relsBuilder 关系文件
type relsBuilder struct {
	items []string
	n     int
}

func (r *relsBuilder) add(typ, target string, external bool) string {
	r.n++
	id := fmt.Sprintf("rId%d", r.n)
	mode := ""
	if external {
		mode = ` TargetMode="External"`
	}
	r.items = append(r.items, fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"%s/>`, id, typ, xmlEscape(target), mode))
	return id
}

func (r *relsBuilder) xml() string {
	return xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		strings.Join(r.items, "") + `</Relationships>`
}

const (
	relOfficeDoc  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relCoreProps  = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	relExtProps   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"
	relStyles     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relNumbering  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	relSettings   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings"
	relHeader     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	relFooter     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
	relHyperlink  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
	relChart      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"
	relPackage    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/package"
	relTheme      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme"
	relSlide      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"
	relSlideLay   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout"
	relSlideMas   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster"
	relPresProps  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/presProps"
	relViewProps  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/viewProps"
	relTableStyle = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/tableStyles"

	ctChart = "application/vnd.openxmlformats-officedocument.drawingml.chart+xml"
)

func corePropsXML(title string) string {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	return xmlHeader + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + xmlEscape(title) + `</dc:title><dc:creator>WebSQL</dc:creator><cp:lastModifiedBy>WebSQL</cp:lastModifiedBy>` +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified></cp:coreProperties>`
}

func appPropsXML(app string) string {
	return xmlHeader + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>` + app + `</Application></Properties>`
}

// ─── DrawingML 原生图表 ─────────────────────────────────────────

type chartXMLOpts struct {
	theme       Theme
	title       string // 为空则不显示图内标题
	workbookID  string // 内嵌工作簿关系 ID（为空则不写 externalData）
	baseSize    int    // 字号（百分之一磅）
	transparent bool   // 图表区透明（PPT 深色背景）
}

func dmlSolid(hex string) string {
	return `<a:solidFill><a:srgbClr val="` + hex + `"/></a:solidFill>`
}

func dmlTxPr(size int, color, font string, bold bool) string {
	b := ""
	if bold {
		b = ` b="1"`
	}
	return fmt.Sprintf(`<c:txPr><a:bodyPr/><a:lstStyle/><a:p><a:pPr><a:defRPr sz="%d"%s>%s<a:latin typeface="%s"/><a:ea typeface="%s"/></a:defRPr></a:pPr><a:endParaRPr lang="zh-CN"/></a:p></c:txPr>`,
		size, b, dmlSolid(color), font, font)
}

func colLetter(idx int) string {
	s := ""
	for idx >= 0 {
		s = string(rune('A'+idx%26)) + s
		idx = idx/26 - 1
	}
	return s
}

func strCache(vals []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<c:strCache><c:ptCount val="%d"/>`, len(vals))
	for i, v := range vals {
		fmt.Fprintf(&sb, `<c:pt idx="%d"><c:v>%s</c:v></c:pt>`, i, xmlEscape(v))
	}
	sb.WriteString(`</c:strCache>`)
	return sb.String()
}

func numCache(vals []float64) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<c:numCache><c:formatCode>General</c:formatCode><c:ptCount val="%d"/>`, len(vals))
	for i, v := range vals {
		fmt.Fprintf(&sb, `<c:pt idx="%d"><c:v>%s</c:v></c:pt>`, i, FormatPlainNumber(v))
	}
	sb.WriteString(`</c:numCache>`)
	return sb.String()
}

// buildChartXML 生成 c:chartSpace 部件（元素顺序严格遵循 OOXML schema 序列）
func buildChartXML(c *ChartSpec, o chartXMLOpts) []byte {
	t := o.theme
	if o.baseSize == 0 {
		o.baseSize = 1000
	}
	n := len(c.Categories)
	last := n + 1
	catRef := fmt.Sprintf("Sheet1!$A$2:$A$%d", last)
	textColor := t.Text
	mutedColor := t.Muted

	// 数字格式：全部为整数时用 #,##0，否则保留两位小数（#,##0.## 在 Office 中会显示成「400.」）
	numFmt := "#,##0"
	for _, s := range c.Series {
		for _, v := range s.Data {
			if v != math.Trunc(v) {
				numFmt = "#,##0.00"
			}
		}
	}

	serTx := func(j int) string {
		return fmt.Sprintf(`<c:tx><c:strRef><c:f>Sheet1!$%s$1</c:f>%s</c:strRef></c:tx>`, colLetter(j+1), strCache([]string{c.Series[j].Name}))
	}
	serCat := func() string {
		return fmt.Sprintf(`<c:cat><c:strRef><c:f>%s</c:f>%s</c:strRef></c:cat>`, catRef, strCache(c.Categories))
	}
	serVal := func(j int) string {
		col := colLetter(j + 1)
		return fmt.Sprintf(`<c:val><c:numRef><c:f>Sheet1!$%s$2:$%s$%d</c:f>%s</c:numRef></c:val>`, col, col, last, numCache(c.Series[j].Data))
	}
	dLbls := func(pos string, show bool) string {
		if !show {
			return ""
		}
		p := ""
		if pos != "" {
			p = `<c:dLblPos val="` + pos + `"/>`
		}
		return `<c:dLbls><c:numFmt formatCode="` + numFmt + `" sourceLinked="0"/><c:spPr><a:noFill/><a:ln><a:noFill/></a:ln></c:spPr>` +
			dmlTxPr(o.baseSize-100, textColor, t.FontFamily, false) + p +
			`<c:showLegendKey val="0"/><c:showVal val="1"/><c:showCatName val="0"/><c:showSerName val="0"/><c:showPercent val="0"/><c:showBubbleSize val="0"/></c:dLbls>`
	}
	axLine := `<c:spPr><a:ln w="9525">` + dmlSolid(t.Grid) + `</a:ln></c:spPr>`
	catAx := func(pos string, deleteAx bool) string {
		del := "0"
		if deleteAx {
			del = "1"
		}
		return `<c:catAx><c:axId val="500001"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="` + del + `"/><c:axPos val="` + pos + `"/>` +
			`<c:numFmt formatCode="General" sourceLinked="0"/><c:majorTickMark val="none"/><c:minorTickMark val="none"/><c:tickLblPos val="nextTo"/>` + axLine +
			dmlTxPr(o.baseSize-100, mutedColor, t.FontFamily, false) +
			`<c:crossAx val="500002"/><c:crosses val="autoZero"/><c:auto val="1"/><c:lblAlgn val="ctr"/><c:lblOffset val="100"/><c:noMultiLvlLbl val="0"/></c:catAx>`
	}
	valAx := func(id, cross int, pos string, grid bool) string {
		g := ""
		if grid {
			g = `<c:majorGridlines><c:spPr><a:ln w="9525">` + dmlSolid(t.Grid) + `<a:prstDash val="dash"/></a:ln></c:spPr></c:majorGridlines>`
		}
		return fmt.Sprintf(`<c:valAx><c:axId val="%d"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="%s"/>%s`, id, pos, g) +
			`<c:numFmt formatCode="` + numFmt + `" sourceLinked="0"/><c:majorTickMark val="none"/><c:minorTickMark val="none"/><c:tickLblPos val="nextTo"/><c:spPr><a:ln><a:noFill/></a:ln></c:spPr>` +
			dmlTxPr(o.baseSize-100, mutedColor, t.FontFamily, false) +
			fmt.Sprintf(`<c:crossAx val="%d"/><c:crosses val="autoZero"/><c:crossBetween val="between"/></c:valAx>`, cross)
	}

	var plot strings.Builder
	showLabels := n*len(c.Series) <= 12
	switch c.Type {
	case "pie", "donut":
		tag := "c:pieChart"
		if c.Type == "donut" {
			tag = "c:doughnutChart"
		}
		plot.WriteString("<" + tag + `><c:varyColors val="1"/>`)
		plot.WriteString(`<c:ser><c:idx val="0"/><c:order val="0"/>` + serTx(0))
		for i := 0; i < n; i++ {
			fmt.Fprintf(&plot, `<c:dPt><c:idx val="%d"/><c:bubble3D val="0"/><c:spPr>%s<a:ln w="12700">%s</a:ln></c:spPr></c:dPt>`, i, dmlSolid(t.SeriesColor(i)), dmlSolid(t.Background))
		}
		pos := `<c:dLblPos val="bestFit"/>`
		if c.Type == "donut" {
			pos = "" // 环形图不支持 dLblPos
		}
		plot.WriteString(`<c:dLbls><c:numFmt formatCode="0.0%" sourceLinked="0"/><c:spPr><a:noFill/><a:ln><a:noFill/></a:ln></c:spPr>` +
			dmlTxPr(o.baseSize-100, "FFFFFF", t.FontFamily, true) + pos +
			`<c:showLegendKey val="0"/><c:showVal val="0"/><c:showCatName val="0"/><c:showSerName val="0"/><c:showPercent val="1"/><c:showBubbleSize val="0"/><c:showLeaderLines val="1"/></c:dLbls>`)
		plot.WriteString(serCat() + serVal(0) + `</c:ser>`)
		plot.WriteString(`<c:firstSliceAng val="0"/>`)
		if c.Type == "donut" {
			plot.WriteString(`<c:holeSize val="58"/>`)
		}
		plot.WriteString("</" + tag + ">")
	case "line", "area":
		tag := "c:lineChart"
		if c.Type == "area" {
			tag = "c:areaChart"
		}
		plot.WriteString("<" + tag + `><c:grouping val="standard"/><c:varyColors val="0"/>`)
		for j := range c.Series {
			color := t.SeriesColor(j)
			fmt.Fprintf(&plot, `<c:ser><c:idx val="%d"/><c:order val="%d"/>%s`, j, j, serTx(j))
			if c.Type == "area" {
				fmt.Fprintf(&plot, `<c:spPr><a:solidFill><a:srgbClr val="%s"><a:alpha val="45000"/></a:srgbClr></a:solidFill><a:ln w="19050">%s</a:ln></c:spPr>`, color, dmlSolid(color))
				plot.WriteString(serCat() + serVal(j) + `</c:ser>`)
			} else {
				fmt.Fprintf(&plot, `<c:spPr><a:ln w="28575" cap="rnd">%s<a:round/></a:ln></c:spPr>`, dmlSolid(color))
				fmt.Fprintf(&plot, `<c:marker><c:symbol val="circle"/><c:size val="6"/><c:spPr>%s<a:ln w="9525">%s</a:ln></c:spPr></c:marker>`, dmlSolid(t.Background), dmlSolid(color))
				plot.WriteString(dLbls("t", showLabels && len(c.Series) == 1))
				plot.WriteString(serCat() + serVal(j) + `<c:smooth val="0"/></c:ser>`)
			}
		}
		if c.Type == "line" {
			plot.WriteString(`<c:marker val="1"/>`)
		}
		plot.WriteString(`<c:axId val="500001"/><c:axId val="500002"/></` + tag + ">")
		plot.WriteString(catAx("b", false) + valAx(500002, 500001, "l", true))
	case "scatter":
		plot.WriteString(`<c:scatterChart><c:scatterStyle val="lineMarker"/><c:varyColors val="0"/>`)
		numericX := true
		xs := make([]float64, n)
		for i, s := range c.Categories {
			v, ok := ParseNumber(s)
			if !ok {
				numericX = false
				break
			}
			xs[i] = v
		}
		for j := range c.Series {
			color := t.SeriesColor(j)
			fmt.Fprintf(&plot, `<c:ser><c:idx val="%d"/><c:order val="%d"/>%s<c:spPr><a:ln w="25400"><a:noFill/></a:ln></c:spPr>`, j, j, serTx(j))
			fmt.Fprintf(&plot, `<c:marker><c:symbol val="circle"/><c:size val="7"/><c:spPr>%s<a:ln w="9525">%s</a:ln></c:spPr></c:marker>`, dmlSolid(color), dmlSolid(color))
			if numericX {
				fmt.Fprintf(&plot, `<c:xVal><c:numRef><c:f>%s</c:f>%s</c:numRef></c:xVal>`, catRef, numCache(xs))
			} else {
				fmt.Fprintf(&plot, `<c:xVal><c:strRef><c:f>%s</c:f>%s</c:strRef></c:xVal>`, catRef, strCache(c.Categories))
			}
			col := colLetter(j + 1)
			fmt.Fprintf(&plot, `<c:yVal><c:numRef><c:f>Sheet1!$%s$2:$%s$%d</c:f>%s</c:numRef></c:yVal><c:smooth val="0"/></c:ser>`, col, col, last, numCache(c.Series[j].Data))
		}
		plot.WriteString(`<c:axId val="500001"/><c:axId val="500002"/></c:scatterChart>`)
		plot.WriteString(valAx(500001, 500002, "b", false) + valAx(500002, 500001, "l", true))
	case "radar":
		plot.WriteString(`<c:radarChart><c:radarStyle val="marker"/><c:varyColors val="0"/>`)
		for j := range c.Series {
			color := t.SeriesColor(j)
			fmt.Fprintf(&plot, `<c:ser><c:idx val="%d"/><c:order val="%d"/>%s<c:spPr><a:ln w="25400">%s</a:ln></c:spPr>`, j, j, serTx(j), dmlSolid(color))
			fmt.Fprintf(&plot, `<c:marker><c:symbol val="circle"/><c:size val="5"/><c:spPr>%s</c:spPr></c:marker>`, dmlSolid(color))
			plot.WriteString(serCat() + serVal(j) + `</c:ser>`)
		}
		plot.WriteString(`<c:axId val="500001"/><c:axId val="500002"/></c:radarChart>`)
		plot.WriteString(catAx("b", false) + valAx(500002, 500001, "l", true))
	default: // bar / hbar / stacked
		dir, grouping := "col", "clustered"
		if c.Type == "hbar" {
			dir = "bar"
		}
		if c.Type == "stacked" {
			grouping = "stacked"
		}
		fmt.Fprintf(&plot, `<c:barChart><c:barDir val="%s"/><c:grouping val="%s"/><c:varyColors val="0"/>`, dir, grouping)
		for j := range c.Series {
			fmt.Fprintf(&plot, `<c:ser><c:idx val="%d"/><c:order val="%d"/>%s<c:spPr>%s</c:spPr><c:invertIfNegative val="0"/>`, j, j, serTx(j), dmlSolid(t.SeriesColor(j)))
			pos := "outEnd"
			if grouping == "stacked" {
				pos = "ctr"
			}
			plot.WriteString(dLbls(pos, showLabels))
			plot.WriteString(serCat() + serVal(j) + `</c:ser>`)
		}
		gap := "80"
		if len(c.Series) == 1 {
			gap = "60"
		}
		plot.WriteString(`<c:gapWidth val="` + gap + `"/>`)
		if grouping == "stacked" {
			plot.WriteString(`<c:overlap val="100"/>`)
		}
		plot.WriteString(`<c:axId val="500001"/><c:axId val="500002"/></c:barChart>`)
		if dir == "bar" {
			plot.WriteString(catAx("l", false) + valAx(500002, 500001, "b", true))
		} else {
			plot.WriteString(catAx("b", false) + valAx(500002, 500001, "l", true))
		}
	}

	var sb strings.Builder
	sb.WriteString(xmlHeader)
	sb.WriteString(`<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	sb.WriteString(`<c:date1904 val="0"/><c:lang val="zh-CN"/><c:roundedCorners val="0"/><c:chart>`)
	if o.title != "" {
		fmt.Fprintf(&sb, `<c:title><c:tx><c:rich><a:bodyPr/><a:lstStyle/><a:p><a:pPr><a:defRPr sz="%d" b="1">%s<a:latin typeface="%s"/><a:ea typeface="%s"/></a:defRPr></a:pPr><a:r><a:rPr lang="zh-CN" sz="%d" b="1">%s<a:latin typeface="%s"/><a:ea typeface="%s"/></a:rPr><a:t>%s</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title><c:autoTitleDeleted val="0"/>`,
			o.baseSize+300, dmlSolid(textColor), t.FontFamily, t.FontFamily, o.baseSize+300, dmlSolid(textColor), t.FontFamily, t.FontFamily, xmlEscape(o.title))
	} else {
		sb.WriteString(`<c:autoTitleDeleted val="1"/>`)
	}
	sb.WriteString(`<c:plotArea><c:layout/>`)
	sb.WriteString(plot.String())
	sb.WriteString(`<c:spPr><a:noFill/><a:ln><a:noFill/></a:ln></c:spPr></c:plotArea>`)
	if len(c.Series) > 1 || c.IsPieLike() {
		pos := "b"
		if c.IsPieLike() {
			pos = "r"
		}
		sb.WriteString(`<c:legend><c:legendPos val="` + pos + `"/><c:overlay val="0"/>` + dmlTxPr(o.baseSize, textColor, t.FontFamily, false) + `</c:legend>`)
	}
	sb.WriteString(`<c:plotVisOnly val="1"/><c:dispBlanksAs val="gap"/></c:chart>`)
	if o.transparent {
		sb.WriteString(`<c:spPr><a:noFill/><a:ln><a:noFill/></a:ln></c:spPr>`)
	} else {
		sb.WriteString(`<c:spPr>` + dmlSolid(t.Background) + `<a:ln><a:noFill/></a:ln></c:spPr>`)
	}
	sb.WriteString(dmlTxPr(o.baseSize, textColor, t.FontFamily, false))
	if o.workbookID != "" {
		sb.WriteString(`<c:externalData r:id="` + o.workbookID + `"><c:autoUpdate val="0"/></c:externalData>`)
	}
	sb.WriteString(`</c:chartSpace>`)
	return []byte(sb.String())
}

// officeChartSpec 返回用于 Office 原生图表的规格副本：
// 条形图（hbar）在 Office 中第一个分类位于底部，这里反转顺序使阅读顺序与 HTML 一致。
func officeChartSpec(c *ChartSpec) *ChartSpec {
	cc := *c
	cc.Normalize()
	if cc.Type != "hbar" {
		return &cc
	}
	n := len(cc.Categories)
	cats := make([]string, n)
	for i := range cc.Categories {
		cats[n-1-i] = cc.Categories[i]
	}
	cc.Categories = cats
	series := make([]Series, len(cc.Series))
	for j, s := range cc.Series {
		d := make([]float64, len(s.Data))
		for i := range s.Data {
			d[len(s.Data)-1-i] = s.Data[i]
		}
		series[j] = Series{Name: s.Name, Data: d}
	}
	cc.Series = series
	return &cc
}

// buildChartWorkbook 生成图表内嵌数据工作簿（Office 中「编辑数据」可直接修改）
func buildChartWorkbook(c *ChartSpec) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"
	_ = f.SetCellValue(sheet, "A1", "分类")
	for j, s := range c.Series {
		_ = f.SetCellValue(sheet, colLetter(j+1)+"1", s.Name)
	}
	for i, cat := range c.Categories {
		row := i + 2
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", row), cat)
		for j, s := range c.Series {
			if i < len(s.Data) {
				_ = f.SetCellValue(sheet, fmt.Sprintf("%s%d", colLetter(j+1), row), s.Data[i])
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
