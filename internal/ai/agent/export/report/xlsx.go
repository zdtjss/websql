package report

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// SheetData 数据工作表内容（保留原始类型，数字以数值写入）
type SheetData struct {
	Name    string
	Headers []string
	Rows    [][]any
}

// DataChart 基于数据工作表列的原生图表
type DataChart struct {
	Type        string
	Title       string
	XCol        int
	YCols       []int
	SeriesNames []string
}

// XLSXOptions Excel 渲染参数
type XLSXOptions struct {
	Title      string
	Theme      Theme
	Summary    *Document // 可选：「分析摘要」工作表（指标卡/文字/表格/图表）
	Data       *SheetData
	DataCharts []DataChart
	SQL        string // 「说明」工作表中记录的取数 SQL
}

type xlsxStyles struct {
	title, subtitle, header, cell, cellAlt, num, numAlt, kpiLabel, kpiValue, kpiUp, kpiDown, kpiFlat, text, heading, muted, note int
}

// RenderXLSX 渲染 Excel：分析摘要（可选）→ 数据明细（表头样式/冻结首行/筛选/数字格式/列宽）→ 图表 → 说明
func RenderXLSX(o XLSXOptions) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	t := o.Theme
	if t.Dark {
		t = businessTheme
	}
	st, err := buildXLSXStyles(f, t)
	if err != nil {
		return nil, err
	}

	first := true
	useSheet := func(name string) string {
		name = safeSheetName(name)
		if first {
			_ = f.SetSheetName("Sheet1", name)
			first = false
		} else {
			_, _ = f.NewSheet(name)
		}
		return name
	}

	chartDataSheet := ""
	if o.Summary != nil && len(o.Summary.Blocks) > 0 {
		sheet := useSheet("分析摘要")
		chartDataSheet = "_图表数据"
		if err := writeSummarySheet(f, sheet, chartDataSheet, o, st, t); err != nil {
			return nil, err
		}
	}

	dataSheet := ""
	if o.Data != nil {
		dataSheet = useSheet(firstNonEmpty(o.Data.Name, "数据明细"))
		writeDataSheet(f, dataSheet, o.Data, st)
	}

	if dataSheet != "" && len(o.DataCharts) > 0 && len(o.Data.Rows) > 0 {
		sheet := useSheet("图表")
		_ = f.SetCellValue(sheet, "B2", firstNonEmpty(o.Title, "数据图表"))
		_ = f.SetCellStyle(sheet, "B2", "B2", st.title)
		_ = f.SetRowHeight(sheet, 2, 32)
		rowsN := len(o.Data.Rows)
		if rowsN > 500 {
			rowsN = 500
		}
		for i, dc := range o.DataCharts {
			anchor := fmt.Sprintf("%s%d", map[int]string{0: "B", 1: "L"}[i%2], 4+(i/2)*22)
			ch := dataChartToExcel(dc, dataSheet, o.Data.Headers, rowsN, t)
			if ch == nil {
				continue
			}
			if err := f.AddChart(sheet, anchor, ch); err != nil {
				return nil, fmt.Errorf("添加图表失败：%w", err)
			}
		}
	}

	info := useSheet("说明")
	writeInfoSheet(f, info, o, st)

	f.SetActiveSheet(0)
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func safeSheetName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		name = "Sheet"
	}
	if utf8.RuneCountInString(name) > 31 {
		name = string([]rune(name)[:31])
	}
	return name
}

func buildXLSXStyles(f *excelize.File, t Theme) (*xlsxStyles, error) {
	font := t.FontFamily
	border := []excelize.Border{
		{Type: "bottom", Color: t.Border, Style: 1},
	}
	mk := func(s *excelize.Style) int {
		id, err := f.NewStyle(s)
		if err != nil {
			return 0
		}
		return id
	}
	fill := func(hex string) excelize.Fill {
		return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{hex}}
	}
	st := &xlsxStyles{}
	st.title = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 18, Bold: true, Color: t.Primary}})
	st.subtitle = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 11, Color: t.Muted}})
	st.header = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 11, Bold: true, Color: t.HeaderText}, Fill: fill(t.HeaderBg),
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}, Border: border})
	st.cell = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10}, Alignment: &excelize.Alignment{Vertical: "center"}, Border: border})
	st.cellAlt = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10}, Fill: fill(t.Zebra), Alignment: &excelize.Alignment{Vertical: "center"}, Border: border})
	numFmt := "#,##0.##"
	st.num = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10}, CustomNumFmt: &numFmt, Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}, Border: border})
	st.numAlt = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10}, Fill: fill(t.Zebra), CustomNumFmt: &numFmt, Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}, Border: border})
	cardBorder := []excelize.Border{{Type: "top", Color: t.Accent, Style: 5}}
	st.kpiLabel = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Color: t.Muted}, Fill: fill(t.Surface), Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "bottom"}, Border: cardBorder})
	st.kpiValue = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 18, Bold: true, Color: t.Primary}, Fill: fill(t.Surface), Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}})
	st.kpiUp = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Bold: true, Color: t.Up}, Fill: fill(t.Surface), Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "top"}})
	st.kpiDown = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Bold: true, Color: t.Down}, Fill: fill(t.Surface), Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "top"}})
	st.kpiFlat = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Color: t.Muted}, Fill: fill(t.Surface), Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "top"}})
	st.text = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 11}, Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	st.heading = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 13, Bold: true, Color: t.Primary}, Border: []excelize.Border{{Type: "bottom", Color: t.Accent, Style: 2}}})
	st.muted = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 10, Color: t.Muted, Italic: true}, Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	st.note = mk(&excelize.Style{Font: &excelize.Font{Family: font, Size: 11}, Fill: fill(Tint(t.Accent, 0.88)), Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}, Border: []excelize.Border{{Type: "left", Color: t.Accent, Style: 5}}})
	return st, nil
}

func writeDataSheet(f *excelize.File, sheet string, d *SheetData, st *xlsxStyles) {
	nCols := len(d.Headers)
	if nCols == 0 {
		return
	}
	for i, h := range d.Headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	last, _ := excelize.CoordinatesToCellName(nCols, 1)
	_ = f.SetCellStyle(sheet, "A1", last, st.header)
	_ = f.SetRowHeight(sheet, 1, 24)

	numeric := make([]bool, nCols)
	for c := 0; c < nCols; c++ {
		vals := make([]string, 0, 50)
		for r := 0; r < len(d.Rows) && r < 200; r++ {
			if c < len(d.Rows[r]) {
				vals = append(vals, CellString(d.Rows[r][c]))
			}
		}
		numeric[c] = IsNumericColumn(vals)
	}
	widths := make([]float64, nCols)
	for c, h := range d.Headers {
		widths[c] = float64(DisplayWidth(h)) + 4
	}
	for r, row := range d.Rows {
		rowNum := r + 2
		for c := 0; c < nCols; c++ {
			var v any
			if c < len(row) {
				v = row[c]
			}
			cell, _ := excelize.CoordinatesToCellName(c+1, rowNum)
			if numeric[c] {
				if s, ok := v.(string); ok {
					if f64, ok := ParseNumber(s); ok && !strings.HasSuffix(strings.TrimSpace(s), "%") {
						v = f64
					}
				} else if b, ok := v.([]byte); ok {
					if f64, ok := ParseNumber(string(b)); ok {
						v = f64
					}
				}
			} else if b, ok := v.([]byte); ok {
				v = string(b)
			}
			_ = f.SetCellValue(sheet, cell, v)
			if r < 300 {
				if w := float64(DisplayWidth(CellString(v))) + 2; w > widths[c] {
					widths[c] = w
				}
			}
		}
	}
	if n := len(d.Rows); n > 0 {
		for c := 0; c < nCols; c++ {
			col, _ := excelize.ColumnNumberToName(c + 1)
			odd, even := st.cell, st.cellAlt
			if numeric[c] {
				odd, even = st.num, st.numAlt
			}
			// 斑马纹：逐行设置样式（大数据量时仅对前 5000 行设置，避免文件过大）
			limit := n
			if limit > 5000 {
				limit = 5000
			}
			for r := 0; r < limit; r++ {
				s := odd
				if r%2 == 1 {
					s = even
				}
				cell := fmt.Sprintf("%s%d", col, r+2)
				_ = f.SetCellStyle(sheet, cell, cell, s)
			}
		}
	}
	for c := 0; c < nCols; c++ {
		col, _ := excelize.ColumnNumberToName(c + 1)
		w := math.Min(math.Max(widths[c], 8), 50)
		_ = f.SetColWidth(sheet, col, col, w)
	}
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	lastCell, _ := excelize.CoordinatesToCellName(nCols, len(d.Rows)+1)
	_ = f.AutoFilter(sheet, "A1:"+lastCell, nil)
}

func excelChartType(t string) excelize.ChartType {
	switch NormalizeChartType(t) {
	case "hbar":
		return excelize.Bar
	case "stacked":
		return excelize.ColStacked
	case "line":
		return excelize.Line
	case "area":
		return excelize.Area
	case "pie":
		return excelize.Pie
	case "donut":
		return excelize.Doughnut
	case "scatter":
		return excelize.Scatter
	case "radar":
		return excelize.Radar
	}
	return excelize.Col
}

func dataChartToExcel(dc DataChart, sheet string, headers []string, rows int, t Theme) *excelize.Chart {
	if dc.XCol < 0 || dc.XCol >= len(headers) || len(dc.YCols) == 0 {
		return nil
	}
	xCol, _ := excelize.ColumnNumberToName(dc.XCol + 1)
	q := "'" + strings.ReplaceAll(sheet, "'", "''") + "'"
	var series []excelize.ChartSeries
	for i, yc := range dc.YCols {
		if yc < 0 || yc >= len(headers) {
			continue
		}
		col, _ := excelize.ColumnNumberToName(yc + 1)
		name := fmt.Sprintf("%s!$%s$1", q, col)
		if i < len(dc.SeriesNames) && dc.SeriesNames[i] != "" {
			name = dc.SeriesNames[i]
		}
		s := excelize.ChartSeries{
			Name:       name,
			Categories: fmt.Sprintf("%s!$%s$2:$%s$%d", q, xCol, xCol, rows+1),
			Values:     fmt.Sprintf("%s!$%s$2:$%s$%d", q, col, col, rows+1),
		}
		ct := NormalizeChartType(dc.Type)
		if ct != "pie" && ct != "donut" {
			s.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{t.SeriesColor(i)}}
			if ct == "line" || ct == "scatter" || ct == "radar" {
				s.Line = excelize.LineOptions{Width: 2.25}
				s.Marker = excelize.ChartMarker{Symbol: "circle", Size: 6}
			}
		}
		series = append(series, s)
	}
	if len(series) == 0 {
		return nil
	}
	ct := NormalizeChartType(dc.Type)
	if (ct == "pie" || ct == "donut") && len(series) > 1 {
		series = series[:1]
	}
	title := dc.Title
	if title == "" {
		title = headers[dc.YCols[0]]
	}
	ch := &excelize.Chart{
		Type:      excelChartType(dc.Type),
		Series:    series,
		Title:     excelize.ChartTitle{Paragraph: []excelize.RichTextRun{{Text: title, Font: &excelize.Font{Bold: true, Size: 12, Family: t.FontFamily}}}},
		Legend:    excelize.ChartLegend{Position: "bottom"},
		Dimension: excelize.ChartDimension{Width: 620, Height: 400},
		PlotArea:  excelize.ChartPlotArea{ShowVal: rows <= 12 && ct != "pie" && ct != "donut", ShowPercent: ct == "pie" || ct == "donut"},
		YAxis:     excelize.ChartAxis{MajorGridLines: true},
		ShowBlanksAs: "gap",
	}
	if ct == "donut" {
		ch.HoleSize = 55
	}
	return ch
}

// writeSummarySheet 把报告 IR 写入「分析摘要」工作表
func writeSummarySheet(f *excelize.File, sheet, chartSheet string, o XLSXOptions, st *xlsxStyles, t Theme) error {
	doc := o.Summary
	_ = f.SetColWidth(sheet, "A", "A", 2)
	_ = f.SetColWidth(sheet, "B", "I", 16)
	_ = f.SetCellValue(sheet, "B2", firstNonEmpty(doc.Title, o.Title))
	_ = f.SetCellStyle(sheet, "B2", "B2", st.title)
	_ = f.SetRowHeight(sheet, 2, 34)
	sub := "生成时间：" + time.Now().Format("2006-01-02 15:04")
	if doc.Subtitle != "" {
		sub = doc.Subtitle + "　·　" + sub
	}
	_ = f.SetCellValue(sheet, "B3", sub)
	_ = f.SetCellStyle(sheet, "B3", "B3", st.subtitle)
	row := 5
	chartRow := 1
	chartCount := 0
	mergeText := func(r int, text string, style int, height float64) {
		_ = f.MergeCell(sheet, fmt.Sprintf("B%d", r), fmt.Sprintf("I%d", r))
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", r), text)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("B%d", r), fmt.Sprintf("I%d", r), style)
		if height > 0 {
			_ = f.SetRowHeight(sheet, r, height)
		}
	}
	textHeight := func(text string) float64 {
		lines := 0
		for _, l := range strings.Split(text, "\n") {
			lines += int(math.Max(1, math.Ceil(float64(DisplayWidth(l))/115)))
		}
		return math.Max(18, float64(lines)*16)
	}
	for _, b := range doc.Blocks {
		switch b.Kind {
		case KindHeading:
			row++
			mergeText(row, PlainText(b.Text), st.heading, 24)
			row += 2
		case KindParagraph, KindQuote:
			txt := PlainText(b.Text)
			mergeText(row, txt, st.text, textHeight(txt))
			row++
		case KindList:
			var lines []string
			for i, it := range b.Items {
				prefix := "• "
				if b.Ordered {
					prefix = fmt.Sprintf("%d. ", i+1)
				}
				lines = append(lines, prefix+PlainText(it.Text))
				for _, c := range it.Children {
					lines = append(lines, "    ◦ "+PlainText(c.Text))
				}
			}
			for _, l := range lines {
				mergeText(row, l, st.text, textHeight(l))
				row++
			}
		case KindCallout:
			txt := firstNonEmpty(b.Title, CalloutLabel(b.Style))
			if b.Text != "" {
				txt += "：" + PlainText(b.Text)
			}
			mergeText(row, txt, st.note, textHeight(txt)+6)
			row += 2
		case KindKPI:
			for start := 0; start < len(b.KPIs); start += 4 {
				for i := 0; i < 4 && start+i < len(b.KPIs); i++ {
					k := b.KPIs[start+i]
					c1, _ := excelize.ColumnNumberToName(2 + i*2)
					c2, _ := excelize.ColumnNumberToName(3 + i*2)
					for r, v := range []struct {
						val   string
						style int
					}{{k.Label, st.kpiLabel}, {k.Value, st.kpiValue}, {kpiChangeText(k), map[string]int{"up": st.kpiUp, "down": st.kpiDown}[k.Trend]}} {
						style := v.style
						if style == 0 {
							style = st.kpiFlat
						}
						cellA, cellB := fmt.Sprintf("%s%d", c1, row+r), fmt.Sprintf("%s%d", c2, row+r)
						_ = f.MergeCell(sheet, cellA, cellB)
						_ = f.SetCellValue(sheet, cellA, v.val)
						_ = f.SetCellStyle(sheet, cellA, cellB, style)
					}
				}
				_ = f.SetRowHeight(sheet, row, 20)
				_ = f.SetRowHeight(sheet, row+1, 30)
				_ = f.SetRowHeight(sheet, row+2, 20)
				row += 4
			}
		case KindTable:
			if b.Table == nil {
				continue
			}
			if b.Title != "" {
				mergeText(row, PlainText(b.Title), st.heading, 20)
				row++
			}
			for i, h := range b.Table.Headers {
				cell, _ := excelize.CoordinatesToCellName(2+i, row)
				_ = f.SetCellValue(sheet, cell, PlainText(h))
				_ = f.SetCellStyle(sheet, cell, cell, st.header)
			}
			row++
			numeric := numericColumns(b.Table)
			for ri, r := range b.Table.Rows {
				for i := range b.Table.Headers {
					cell, _ := excelize.CoordinatesToCellName(2+i, row)
					v := ""
					if i < len(r) {
						v = PlainText(r[i])
					}
					style := st.cell
					if ri%2 == 1 {
						style = st.cellAlt
					}
					if numeric[i] {
						if f64, ok := ParseNumber(v); ok && !strings.HasSuffix(v, "%") {
							_ = f.SetCellValue(sheet, cell, f64)
							style = map[bool]int{true: st.numAlt, false: st.num}[ri%2 == 1]
							_ = f.SetCellStyle(sheet, cell, cell, style)
							continue
						}
					}
					_ = f.SetCellValue(sheet, cell, v)
					_ = f.SetCellStyle(sheet, cell, cell, style)
				}
				row++
			}
			row++
		case KindChart:
			if b.Chart == nil || !b.Chart.HasData() {
				continue
			}
			c := b.Chart
			// 图表数据写入隐藏工作表，图表引用该区域
			if chartCount == 0 {
				_, _ = f.NewSheet(chartSheet)
				_ = f.SetSheetVisible(chartSheet, false)
			}
			chartCount++
			startRow := chartRow
			_ = f.SetCellValue(chartSheet, fmt.Sprintf("A%d", startRow), "分类")
			for j, s := range c.Series {
				col, _ := excelize.ColumnNumberToName(j + 2)
				_ = f.SetCellValue(chartSheet, fmt.Sprintf("%s%d", col, startRow), s.Name)
			}
			for i, catName := range c.Categories {
				_ = f.SetCellValue(chartSheet, fmt.Sprintf("A%d", startRow+1+i), catName)
				for j, s := range c.Series {
					col, _ := excelize.ColumnNumberToName(j + 2)
					if i < len(s.Data) {
						_ = f.SetCellValue(chartSheet, fmt.Sprintf("%s%d", col, startRow+1+i), s.Data[i])
					}
				}
			}
			q := "'" + chartSheet + "'"
			var series []excelize.ChartSeries
			for j := range c.Series {
				col, _ := excelize.ColumnNumberToName(j + 2)
				s := excelize.ChartSeries{
					Name:       fmt.Sprintf("%s!$%s$%d", q, col, startRow),
					Categories: fmt.Sprintf("%s!$A$%d:$A$%d", q, startRow+1, startRow+len(c.Categories)),
					Values:     fmt.Sprintf("%s!$%s$%d:$%s$%d", q, col, startRow+1, col, startRow+len(c.Categories)),
				}
				if !c.IsPieLike() {
					s.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{t.SeriesColor(j)}}
				}
				series = append(series, s)
			}
			chartRow += len(c.Categories) + 2
			ch := &excelize.Chart{
				Type:         excelChartType(c.Type),
				Series:       series,
				Title:        excelize.ChartTitle{Paragraph: []excelize.RichTextRun{{Text: firstNonEmpty(c.Title, "图表"), Font: &excelize.Font{Bold: true, Size: 12, Family: t.FontFamily}}}},
				Legend:       excelize.ChartLegend{Position: "bottom"},
				Dimension:    excelize.ChartDimension{Width: 760, Height: 360},
				PlotArea:     excelize.ChartPlotArea{ShowPercent: c.IsPieLike(), ShowVal: !c.IsPieLike() && len(c.Categories)*len(c.Series) <= 12},
				YAxis:        excelize.ChartAxis{MajorGridLines: true},
				ShowBlanksAs: "gap",
			}
			if c.Type == "donut" {
				ch.HoleSize = 55
			}
			if err := f.AddChart(sheet, fmt.Sprintf("B%d", row), ch); err != nil {
				return fmt.Errorf("添加图表失败：%w", err)
			}
			row += 20
		case KindCode, KindMermaid:
			mergeText(row, b.Code, st.muted, textHeight(b.Code))
			row++
		}
	}
	return nil
}

func kpiChangeText(k KPI) string {
	switch k.Trend {
	case "up":
		return "▲ " + k.Change
	case "down":
		return "▼ " + k.Change
	}
	if k.Change == "" {
		return k.Note
	}
	return k.Change
}

func writeInfoSheet(f *excelize.File, sheet string, o XLSXOptions, st *xlsxStyles) {
	_ = f.SetColWidth(sheet, "A", "A", 16)
	_ = f.SetColWidth(sheet, "B", "B", 100)
	rows := [][2]string{
		{"报告标题", firstNonEmpty(o.Title, "数据导出")},
		{"生成时间", time.Now().Format("2006-01-02 15:04:05")},
		{"生成方式", "WebSQL 智能分析（服务端原生渲染）"},
	}
	if o.Data != nil {
		rows = append(rows, [2]string{"数据行数", fmt.Sprintf("%d", len(o.Data.Rows))})
		rows = append(rows, [2]string{"数据列数", fmt.Sprintf("%d", len(o.Data.Headers))})
	}
	if o.SQL != "" {
		rows = append(rows, [2]string{"取数 SQL", o.SQL})
	}
	for i, r := range rows {
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", i+1), r[0])
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", i+1), r[1])
		_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", i+1), fmt.Sprintf("A%d", i+1), st.header)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("B%d", i+1), fmt.Sprintf("B%d", i+1), st.text)
	}
}
