package export

import (
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	ExcelPrimaryColor   = "#1A237E"
	ExcelAccentColor    = "#00BCD4"
	ExcelSuccessColor   = "#4CAF50"
	ExcelWarningColor   = "#FF9800"
	ExcelDangerColor    = "#F44336"
	ExcelLightBgColor   = "#F5F7FA"
	ExcelDarkTextColor  = "#212121"
	ExcelMediumTextColor = "#616161"
	ExcelLightTextColor = "#757575"
	ExcelBorderColor    = "#E0E0E0"
)

func excelPrimaryFill() excelize.Fill {
	return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{ExcelPrimaryColor}}
}

func excelAccentFill() excelize.Fill {
	return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{ExcelAccentColor}}
}

func excelLightFill() excelize.Fill {
	return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{ExcelLightBgColor}}
}

func excelWhiteFill() excelize.Fill {
	return excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#FFFFFF"}}
}

func excelThinBorder() []excelize.Border {
	return []excelize.Border{
		{Type: "top", Color: ExcelBorderColor, Style: 1},
		{Type: "bottom", Color: ExcelBorderColor, Style: 1},
		{Type: "left", Color: ExcelBorderColor, Style: 1},
		{Type: "right", Color: ExcelBorderColor, Style: 1},
	}
}

func excelCenterAlignment() *excelize.Alignment {
	return &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
}

func excelLeftAlignment() *excelize.Alignment {
	return &excelize.Alignment{Horizontal: "left", Vertical: "center", WrapText: true}
}

var yaHeiFont = "Microsoft YaHei"

func WriteExcelSheet(f *excelize.File, sheet string, qr *QueryResult) {
	writeExcelSheetMode(f, sheet, qr, true)
}

func writeExcelSheetMode(f *excelize.File, sheet string, qr *QueryResult, useStream bool) {
	if useStream {
		sw, err := f.NewStreamWriter(sheet)
		if err != nil {
			return
		}
		header := make([]any, len(qr.Columns))
		for i, c := range qr.Columns {
			header[i] = c
		}
		sw.SetRow("A1", header)
		for rowIdx, row := range qr.Data {
			rowData := make([]any, len(qr.Columns))
			for colIdx, col := range qr.Columns {
				if v, ok := row[col]; ok {
					rowData[colIdx] = v
				}
			}
			sw.SetRow(fmt.Sprintf("A%d", rowIdx+2), rowData)
		}
		sw.Flush()
	} else {
		for i, c := range qr.Columns {
			cell := fmt.Sprintf("%s1", ColLetter(i))
			f.SetCellValue(sheet, cell, c)
		}
		for rowIdx, row := range qr.Data {
			for colIdx, col := range qr.Columns {
				cell := fmt.Sprintf("%s%d", ColLetter(colIdx), rowIdx+2)
				if v, ok := row[col]; ok {
					SetCellAutoNative(f, sheet, cell, v)
				}
			}
		}
	}
}

func SetCellAutoNative(f *excelize.File, sheet, cell string, v any) {
	switch val := v.(type) {
	case float64:
		f.SetCellValue(sheet, cell, val)
	case float32:
		f.SetCellValue(sheet, cell, float64(val))
	case int:
		f.SetCellValue(sheet, cell, val)
	case int64:
		f.SetCellValue(sheet, cell, val)
	case int32:
		f.SetCellValue(sheet, cell, int64(val))
	case []byte:
		s := string(val)
		if n, err := parseFloat(s); err == nil {
			f.SetCellValue(sheet, cell, n)
		} else {
			f.SetCellValue(sheet, cell, s)
		}
	case string:
		if n, err := parseFloat(val); err == nil {
			f.SetCellValue(sheet, cell, n)
		} else {
			f.SetCellValue(sheet, cell, val)
		}
	default:
		f.SetCellValue(sheet, cell, fmt.Sprintf("%v", v))
	}
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func ExcelSetTitleArea(f *excelize.File, sheet, title string, colCount, rowCount int) {
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 16, Bold: true, Color: ExcelPrimaryColor, Family: yaHeiFont},
		Alignment: excelLeftAlignment(),
	})
	subStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: ExcelLightTextColor, Family: yaHeiFont},
		Alignment: excelLeftAlignment(),
	})
	barStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelPrimaryFill(),
	})

	startCell, _ := excelize.CoordinatesToCellName(1, 1)
	endCell, _ := excelize.CoordinatesToCellName(colCount, 1)
	f.MergeCell(sheet, startCell, endCell)
	f.SetCellValue(sheet, startCell, title)
	f.SetCellStyle(sheet, startCell, endCell, titleStyle)

	startCell, _ = excelize.CoordinatesToCellName(1, 2)
	endCell, _ = excelize.CoordinatesToCellName(colCount, 2)
	f.MergeCell(sheet, startCell, endCell)
	f.SetCellValue(sheet, startCell, fmt.Sprintf("生成时间：%s  |  数据行数：%d", time.Now().Format("2006-01-02 15:04:05"), rowCount))
	f.SetCellStyle(sheet, startCell, endCell, subStyle)

	for i := 0; i < colCount; i++ {
		cell, _ := excelize.CoordinatesToCellName(i+1, 3)
		f.SetCellStyle(sheet, cell, cell, barStyle)
	}
	f.SetRowHeight(sheet, 1, 32)
	f.SetRowHeight(sheet, 2, 20)
	f.SetRowHeight(sheet, 3, 3)
}

func ExcelWriteStyledTable(f *excelize.File, sheet string, qr *QueryResult, startRow int) {
	headerStyleID := createHeaderStyle(f)
	rowEvenStyleID := createRowEvenStyle(f)
	rowOddStyleID := createRowOddStyle(f)

	for i, c := range qr.Columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, startRow)
		f.SetCellValue(sheet, cell, c)
		f.SetCellStyle(sheet, cell, cell, headerStyleID)
	}
	f.SetRowHeight(sheet, startRow, 28)

	for rowIdx, row := range qr.Data {
		currentRow := startRow + 1 + rowIdx
		for colIdx, col := range qr.Columns {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, currentRow)
			if v, ok := row[col]; ok {
				SetCellAutoNative(f, sheet, cell, v)
			}
			if rowIdx%2 == 0 {
				f.SetCellStyle(sheet, cell, cell, rowEvenStyleID)
			} else {
				f.SetCellStyle(sheet, cell, cell, rowOddStyleID)
			}
		}
		f.SetRowHeight(sheet, currentRow, 22)
	}

	for i := range qr.Columns {
		colName, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, colName, colName, ExcelAutoWidth(qr, i))
	}
}

func createHeaderStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 11, Bold: true, Color: "#FFFFFF", Family: yaHeiFont},
		Fill:      excelPrimaryFill(),
		Alignment: excelCenterAlignment(),
		Border: []excelize.Border{
			{Type: "top", Color: ExcelPrimaryColor, Style: 1},
			{Type: "bottom", Color: "#FFFFFF", Style: 2},
			{Type: "left", Color: ExcelPrimaryColor, Style: 1},
			{Type: "right", Color: ExcelPrimaryColor, Style: 1},
		},
	})
	return style
}

func createRowEvenStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: ExcelDarkTextColor, Family: yaHeiFont},
		Fill:      excelLightFill(),
		Alignment: excelLeftAlignment(),
		Border:    excelThinBorder(),
	})
	return style
}

func createRowOddStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: ExcelDarkTextColor, Family: yaHeiFont},
		Fill:      excelWhiteFill(),
		Alignment: excelLeftAlignment(),
		Border:    excelThinBorder(),
	})
	return style
}

func ExcelDashboardSheet(f *excelize.File, sheet string, qr *QueryResult) {
	dashTitleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 20, Bold: true, Color: ExcelPrimaryColor, Family: yaHeiFont},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	})
	dashSubStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: ExcelLightTextColor, Family: yaHeiFont},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	})
	dashSectionStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 13, Bold: true, Color: ExcelPrimaryColor, Family: yaHeiFont},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#EDEFF7"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "bottom", Color: ExcelAccentColor, Style: 2},
		},
	})
	kpiLabelStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 9, Color: ExcelMediumTextColor, Family: yaHeiFont},
		Alignment: excelCenterAlignment(),
	})
	kpiValueStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 24, Bold: true, Color: ExcelPrimaryColor, Family: yaHeiFont},
		Alignment: excelCenterAlignment(),
		Fill:      excelLightFill(),
		Border: []excelize.Border{
			{Type: "bottom", Color: ExcelAccentColor, Style: 2},
			{Type: "left", Color: ExcelBorderColor, Style: 1},
			{Type: "right", Color: ExcelBorderColor, Style: 1},
			{Type: "top", Color: ExcelBorderColor, Style: 1},
		},
	})
	kpiAccentStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 24, Bold: true, Color: ExcelAccentColor, Family: yaHeiFont},
		Alignment: excelCenterAlignment(),
		Fill:      excelLightFill(),
		Border: []excelize.Border{
			{Type: "bottom", Color: ExcelAccentColor, Style: 2},
			{Type: "left", Color: ExcelBorderColor, Style: 1},
			{Type: "right", Color: ExcelBorderColor, Style: 1},
			{Type: "top", Color: ExcelBorderColor, Style: 1},
		},
	})

	f.SetCellStyle(sheet, "A1", "J1", dashTitleStyle)
	f.SetCellValue(sheet, "A1", "\u2606 数据分析仪表盘")
	f.SetRowHeight(sheet, 1, 40)
	f.MergeCell(sheet, "A1", "J1")

	f.SetCellStyle(sheet, "A2", "J2", dashSubStyle)
	f.SetCellValue(sheet, "A2", fmt.Sprintf("\u25C6 生成时间：%s  |  数据行数：%d  |  字段数：%d", time.Now().Format("2006-01-02 15:04:05"), len(qr.Data), len(qr.Columns)))
	f.MergeCell(sheet, "A2", "J2")
	f.SetRowHeight(sheet, 2, 22)

	f.SetCellStyle(sheet, "A4", "J4", dashSectionStyle)
	f.SetCellValue(sheet, "A4", "\u25B8 核心指标概览")
	f.MergeCell(sheet, "A4", "J4")
	f.SetRowHeight(sheet, 4, 30)

	numericCols := DetectNumericCols(qr)
	col := 1
	for _, nc := range numericCols {
		if col > 7 {
			break
		}
		_, max, avg, count := CalcNumericStats(qr, nc)
		if count == 0 {
			continue
		}

		cellLabel, _ := excelize.CoordinatesToCellName(col, 6)
		cellValue, _ := excelize.CoordinatesToCellName(col, 7)
		cellTotal, _ := excelize.CoordinatesToCellName(col, 8)

		f.SetCellStyle(sheet, cellLabel, cellLabel, kpiLabelStyle)
		f.SetCellValue(sheet, cellLabel, nc)
		f.SetRowHeight(sheet, 6, 20)

		valueStyle := kpiValueStyle
		if col%3 == 0 {
			valueStyle = kpiAccentStyle
		}
		f.SetCellStyle(sheet, cellValue, cellValue, valueStyle)
		f.SetCellValue(sheet, cellValue, fmt.Sprintf("%.1f", avg))
		f.SetRowHeight(sheet, 7, 40)

		f.SetCellStyle(sheet, cellTotal, cellTotal, kpiLabelStyle)
		f.SetCellValue(sheet, cellTotal, fmt.Sprintf("峰值 %.1f", max))
		f.SetRowHeight(sheet, 8, 20)

		f.SetColWidth(sheet, ColLetter(col-1), ColLetter(col-1), 20)
		col++
	}

	infoRow := 11
	f.SetCellStyle(sheet, fmt.Sprintf("A%d", infoRow), fmt.Sprintf("J%d", infoRow), dashSectionStyle)
	f.SetCellValue(sheet, fmt.Sprintf("A%d", infoRow), "\u25B8 字段统计分析")
	f.MergeCell(sheet, fmt.Sprintf("A%d", infoRow), fmt.Sprintf("J%d", infoRow))
	f.SetRowHeight(sheet, infoRow, 30)

	infoRow += 2
	detailStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: ExcelDarkTextColor, Family: yaHeiFont},
		Alignment: excelLeftAlignment(),
		Fill:      excelLightFill(),
		Border:    excelThinBorder(),
	})

	for i, nc := range numericCols {
		if i >= 10 {
			break
		}
		min, max, avg, count := CalcNumericStats(qr, nc)
		if count == 0 {
			continue
		}
		startCell, _ := excelize.CoordinatesToCellName(1, infoRow)
		endCell, _ := excelize.CoordinatesToCellName(6, infoRow)
		f.MergeCell(sheet, startCell, endCell)
		f.SetCellStyle(sheet, startCell, endCell, detailStyle)
		f.SetCellValue(sheet, startCell, fmt.Sprintf("  %s — 均值：%.2f  |  最小：%.2f  |  最大：%.2f  |  总计：%.2f", nc, avg, min, max, avg*float64(count)))
		f.SetRowHeight(sheet, infoRow, 22)
		infoRow++

		if i%2 == 0 {
			altStyle, _ := f.NewStyle(&excelize.Style{
				Font:      &excelize.Font{Size: 10, Color: ExcelDarkTextColor, Family: yaHeiFont},
				Alignment: excelLeftAlignment(),
				Fill:      excelWhiteFill(),
				Border:    excelThinBorder(),
			})
			startCell2, _ := excelize.CoordinatesToCellName(1, infoRow)
			endCell2, _ := excelize.CoordinatesToCellName(6, infoRow)
			f.MergeCell(sheet, startCell2, endCell2)
			f.SetCellStyle(sheet, startCell2, endCell2, altStyle)
		}
	}
}

func ExcelChartTypeToNative(t string) excelize.ChartType {
	switch strings.ToLower(t) {
	case "bar", "column":
		return excelize.Col
	case "stackedbar", "stacked_bar":
		return excelize.ColStacked
	case "pie":
		return excelize.Pie
	case "doughnut", "donut":
		return excelize.Doughnut
	case "scatter":
		return excelize.Scatter
	case "area":
		return excelize.Area
	case "stackedarea", "stacked_area":
		return excelize.AreaStacked
	case "radar":
		return excelize.Radar
	case "line", "":
		fallthrough
	default:
		return excelize.Line
	}
}

func CreateExcelChart(sheet, chartType, chartTitle string, series []excelize.ChartSeries, cell string) *excelize.Chart {
	titleRuns := []excelize.RichTextRun{
		{
			Text: chartTitle,
			Font: &excelize.Font{Size: 16, Bold: true, Color: ExcelPrimaryColor, Family: yaHeiFont},
		},
	}

	chart := &excelize.Chart{
		Type:   ExcelChartTypeToNative(chartType),
		Series: series,
		Format: excelize.GraphicOptions{
			ScaleX:          2.0,
			ScaleY:          2.0,
			PrintObject:     boolPtr(true),
			LockAspectRatio: true,
		},
		Title: titleRuns,
		Legend: excelize.ChartLegend{
			Position:      "bottom",
			ShowLegendKey: false,
		},
		PlotArea: excelize.ChartPlotArea{
			ShowVal: true,
			ShowCatName: true,
			ShowSerName: len(series) > 1,
		},
	}

	return chart
}

func boolPtr(b bool) *bool {
	return &b
}

func NormalizeCharts(input *ExportExcelWithChartInput) []ExcelChart {
	if len(input.Charts) > 0 {
		return input.Charts
	}
	chartType := input.ChartType
	if chartType == "" {
		chartType = "bar"
	}
	chartTitle := input.ChartTitle
	if chartTitle == "" {
		chartTitle = fmt.Sprintf("%s vs %s", input.XAxisField, input.YAxisField)
	}
	return []ExcelChart{{
		ChartType:  chartType,
		XAxisField: input.XAxisField,
		ChartTitle: chartTitle,
		Series: []ExcelChartSeriesDef{{
			YAxisField: input.YAxisField,
			Name:       input.YAxisField,
		}},
	}}
}

func AddDataBarConditionalFormat(f *excelize.File, sheet, cellRange string, colIdx int) {
	format := &excelize.ConditionalFormatOptions{
		Type:     "data_bar",
		Criteria: "=",
		Value:    "0",
	}

	ref := fmt.Sprintf("%s:%s", cellRange, cellRange)
	_ = f.SetConditionalFormat(sheet, ref, []excelize.ConditionalFormatOptions{*format})
}

func CreateAnalysisSummarySheet(f *excelize.File, sheet string, qr *QueryResult) {
	summaryTitleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 18, Bold: true, Color: ExcelPrimaryColor, Family: yaHeiFont},
		Alignment: excelLeftAlignment(),
	})
	cardTitleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 12, Bold: true, Color: "#FFFFFF", Family: yaHeiFont},
		Fill:      excelPrimaryFill(),
		Alignment: excelCenterAlignment(),
		Border:    excelThinBorder(),
	})
	cardValueStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 14, Bold: true, Color: ExcelPrimaryColor, Family: yaHeiFont},
		Fill:      excelLightFill(),
		Alignment: excelCenterAlignment(),
		Border:    excelThinBorder(),
	})
	cardSubStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: ExcelMediumTextColor, Family: yaHeiFont},
		Fill:      excelWhiteFill(),
		Alignment: excelCenterAlignment(),
		Border:    excelThinBorder(),
	})

	f.SetCellStyle(sheet, "A1", "E1", summaryTitleStyle)
	f.SetCellValue(sheet, "A1", fmt.Sprintf("\u25C6 分析概览 — %s", time.Now().Format("2006-01-02")))
	f.MergeCell(sheet, "A1", "E1")
	f.SetRowHeight(sheet, 1, 36)

	headers := []string{"指标", "数值", "说明"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 3)
		f.SetCellStyle(sheet, cell, cell, cardTitleStyle)
		f.SetCellValue(sheet, cell, h)
	}
	f.SetRowHeight(sheet, 3, 26)

	analysisRows := []struct{ label, value, desc string }{
		{"总记录数", fmt.Sprintf("%d", len(qr.Data)), "数据集整体规模"},
		{"字段数量", fmt.Sprintf("%d", len(qr.Columns)), "包含的数据维度"},
		{"数值字段", fmt.Sprintf("%d", len(DetectNumericCols(qr))), "可用于统计分析的字段"},
	}

	for i, item := range analysisRows {
		row := 4 + i
		labelCell, _ := excelize.CoordinatesToCellName(1, row)
		valueCell, _ := excelize.CoordinatesToCellName(2, row)
		descCell, _ := excelize.CoordinatesToCellName(3, row)

		f.SetCellStyle(sheet, labelCell, labelCell, cardValueStyle)
		f.SetCellValue(sheet, labelCell, item.label)
		f.SetCellStyle(sheet, valueCell, valueCell, cardValueStyle)
		f.SetCellValue(sheet, valueCell, item.value)
		f.SetCellStyle(sheet, descCell, descCell, cardSubStyle)
		f.SetCellValue(sheet, descCell, item.desc)
		f.SetRowHeight(sheet, row, 24)

		lineStyle, _ := f.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{ExcelAccentColor}},
		})
		lineCell, _ := excelize.CoordinatesToCellName(4, row)
		f.SetCellStyle(sheet, lineCell, lineCell, lineStyle)
	}

	f.SetColWidth(sheet, "A", "A", 16)
	f.SetColWidth(sheet, "B", "B", 14)
	f.SetColWidth(sheet, "C", "C", 30)
	f.SetColWidth(sheet, "D", "D", 3)

	numericCols := DetectNumericCols(qr)
	if len(numericCols) > 0 {
		statRow := 9
		statHeaderStyle, _ := f.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Size: 11, Bold: true, Color: "#FFFFFF", Family: yaHeiFont},
			Fill:      excelPrimaryFill(),
			Alignment: excelCenterAlignment(),
			Border:    excelThinBorder(),
		})

		statHeaders := []string{"字段名", "最小值", "最大值", "平均值", "总计"}
		for i, h := range statHeaders {
			cell, _ := excelize.CoordinatesToCellName(i+1, statRow)
			f.SetCellStyle(sheet, cell, cell, statHeaderStyle)
			f.SetCellValue(sheet, cell, h)
		}
		f.SetRowHeight(sheet, statRow, 26)

		for i, nc := range numericCols {
			if i >= 15 {
				break
			}
			min, max, avg, count := CalcNumericStats(qr, nc)
			if count == 0 {
				continue
			}
			row := statRow + 1 + i
			stats := []string{nc, fmt.Sprintf("%.2f", min), fmt.Sprintf("%.2f", max), fmt.Sprintf("%.2f", avg), fmt.Sprintf("%.2f", avg*float64(count))}
			for j, val := range stats {
				cell, _ := excelize.CoordinatesToCellName(j+1, row)
				if j > 0 {
					f.SetCellValue(sheet, cell, parseStringToFloat(val))
				} else {
					f.SetCellValue(sheet, cell, val)
				}
				fill := excelLightFill()
				if i%2 == 1 {
					fill = excelWhiteFill()
				}
				altStyle, _ := f.NewStyle(&excelize.Style{
					Font:      &excelize.Font{Size: 10, Color: ExcelDarkTextColor, Family: yaHeiFont},
					Fill:      fill,
					Alignment: excelCenterAlignment(),
					Border:    excelThinBorder(),
				})
				f.SetCellStyle(sheet, cell, cell, altStyle)
			}
			f.SetRowHeight(sheet, row, 22)
		}
	}

	f.SetColWidth(sheet, "E", "E", 18)
}

func parseStringToFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
