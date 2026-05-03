// Package agentv2 — 数据导出工具集合
//
// 设计思路：
//   - Excel: 使用 excelize/v2，支持数据表 + 内嵌图表
//   - Word:  直接生成 Office Open XML（DOCX = ZIP of XML），模板填充
//   - Chart: 使用 go-chart/v2 渲染 PNG 图片
//   - PPT:   直接生成 Office Open XML（PPTX = ZIP of XML），模板填充
//
// 所有导出工具共享 queryForExport 获取数据。
package agentv2

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	dbutils "go-web/utils/db"

	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
)

// ──────────────────────────────────────────────
// 导出工具 Input/Output 结构体
// ──────────────────────────────────────────────

type ExportExcelInput struct {
	SQL      string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
}

type ExportExcelOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

type ExcelChartSeriesDef struct {
	YAxisField string `json:"yAxisField" jsonschema:"required" jsonschema_description:"Y 轴字段名"`
	Name       string `json:"name" jsonschema_description:"系列名称，用于图例显示"`
}

type ExcelChart struct {
	ChartType  string                `json:"chartType" jsonschema:"required" jsonschema_description:"图表类型: line, bar, pie, scatter, area, stackedBar, doughnut, radar"`
	XAxisField string                `json:"xAxisField" jsonschema:"required" jsonschema_description:"X 轴字段名"`
	Series     []ExcelChartSeriesDef `json:"series" jsonschema:"required" jsonschema_description:"Y 轴系列，可指定多个字段生成多系列图表"`
	ChartTitle string                `json:"chartTitle" jsonschema_description:"图表标题"`
	SheetName  string                `json:"sheetName" jsonschema_description:"图表所在 Sheet 名称，为空则自动分配"`
}

type ExportExcelWithChartInput struct {
	SQL      string       `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName string       `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Charts   []ExcelChart `json:"charts" jsonschema_description:"图表定义列表，每个元素会在独立 Sheet 中生成一个图表。支持多条折线/柱状（通过 series 指定多个 Y 字段）"`
	// 兼容旧版调用
	ChartType  string `json:"chartType" jsonschema_description:"图表类型: line, bar, pie, scatter, area, stackedBar, doughnut, radar"`
	XAxisField string `json:"xAxisField" jsonschema_description:"X 轴字段名"`
	YAxisField string `json:"yAxisField" jsonschema_description:"Y 轴字段名"`
	ChartTitle string `json:"chartTitle" jsonschema_description:"图表标题"`
}

type ExportExcelWithChartOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

type ExportPPTInput struct {
	SQL       string `json:"sql" jsonschema_description:"用于导出的 SELECT SQL（与 content 二选一，仅 Excel 必须提供 SQL）"`
	Content   string `json:"content" jsonschema_description:"演示内容（Markdown 格式，与 sql 二选一。按 ## 标题自动分页）"`
	FileName  string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Title     string `json:"title" jsonschema_description:"PPT 标题"`
	SlideType string `json:"slideType" jsonschema_description:"幻灯片类型: summary, table, chart"`
}

type ExportPPTOutput struct {
	Message     string `json:"message"`
	SlideCount  int    `json:"slideCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

type ExportAnalysisImageInput struct {
	SQL        string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName   string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	ChartType  string `json:"chartType" jsonschema_description:"图表类型: line, bar, pie"`
	XAxisField string `json:"xAxisField" jsonschema:"required" jsonschema_description:"X 轴字段名"`
	YAxisField string `json:"yAxisField" jsonschema:"required" jsonschema_description:"Y 轴字段名"`
	Title      string `json:"title" jsonschema_description:"图表标题"`
}

type ExportAnalysisImageOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
	Format      string `json:"format"`
}

type ExportAnalysisDocxInput struct {
	SQL          string `json:"sql" jsonschema_description:"用于导出的 SELECT SQL（与 content 二选一，仅 Excel 必须提供 SQL）"`
	Content      string `json:"content" jsonschema_description:"分析报告内容（Markdown 格式，与 sql 二选一。可包含标题、段落、列表、代码块、mermaid 图表等）"`
	FileName     string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Title        string `json:"title" jsonschema_description:"报告标题"`
	IncludeChart bool   `json:"includeChart" jsonschema_description:"是否包含图表（仅 sql 模式有效）"`
}

type ExportAnalysisDocxOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

// ──────────────────────────────────────────────
// 公共：查询数据 + 文件名处理
// ──────────────────────────────────────────────

// queryResult 查询结果
type queryResult struct {
	Columns []string
	Data    []map[string]any
}

// queryForExport 执行 SQL 并返回列名 + 数据
func queryForExport(conn *sqlx.DB, sql string) (*queryResult, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, fmt.Errorf("SQL 不能为空")
	}
	// 去除注释后检查 SQL 类型
	stripped := stripSQLComments(sql)
	upper := strings.ToUpper(stripped)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return nil, fmt.Errorf("导出仅支持 SELECT 查询")
	}
	// WITH CTE 中不允许包含写操作
	if strings.HasPrefix(upper, "WITH") {
		writeKeywords := []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "TRUNCATE ", "ALTER ", "CREATE ", "REPLACE ", "MERGE "}
		for _, kw := range writeKeywords {
			if strings.Contains(upper, kw) {
				return nil, fmt.Errorf("导出查询不允许包含写操作（%s）", strings.TrimSpace(kw))
			}
		}
	}

	rows, err := conn.Queryx(sql)
	if err != nil {
		return nil, fmt.Errorf("查询失败：%w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("获取列信息失败：%w", err)
	}

	data := dbutils.GetResultRowsForExport(conn.DriverName(), rows)
	return &queryResult{Columns: cols, Data: data}, nil
}

// sanitizeFileName 清理文件名，去除常见扩展名
func sanitizeFileName(name, defaultPrefix string) string {
	if name == "" {
		return fmt.Sprintf("%s_%s", defaultPrefix, time.Now().Format("20060102_150405"))
	}
	for _, ext := range []string{".xlsx", ".xls", ".csv", ".docx", ".pptx", ".png", ".jpg"} {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

// ensureExportsDir 确保 exports 目录存在
func ensureExportsDir() {
	os.MkdirAll("exports", 0755)
}

// colIndex 查找列名在列列表中的索引
func colIndex(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

// colLetter 将 0-based 列索引转为 Excel 列字母 (0→A, 1→B, ..., 25→Z, 26→AA)
func colLetter(idx int) string {
	result := ""
	for {
		result = string(rune('A'+idx%26)) + result
		idx = idx/26 - 1
		if idx < 0 {
			break
		}
	}
	return result
}

// ──────────────────────────────────────────────
// Excel 导出（纯数据）
// ──────────────────────────────────────────────

func NewExportExcelFunc(connID string) func(ctx context.Context, input *ExportExcelInput) (*ExportExcelOutput, error) {
	return func(ctx context.Context, input *ExportExcelInput) (*ExportExcelOutput, error) {
		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在")
		}

		qr, err := queryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		f := excelize.NewFile()
		defer f.Close()

		writeExcelSheet(f, "Sheet1", qr)

		fileName := sanitizeFileName(input.FileName, "export")
		ensureExportsDir()
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		if err := f.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Excel 失败：%w", err)
		}

		url := fmt.Sprintf("/exports/%s.xlsx", fileName)
		log.Printf("[Tool:export_excel] 成功 - rows=%d, url=%s\n", len(qr.Data), url)

		return &ExportExcelOutput{
			Message:     fmt.Sprintf("已导出 %d 条数据，[点击下载](%s)", len(qr.Data), url),
			RowCount:    len(qr.Data),
			DownloadURL: url,
			FileType:    "excel",
		}, nil
	}
}

// ──────────────────────────────────────────────
// Excel + 图表导出
// ──────────────────────────────────────────────

func NewExportExcelWithChartFunc(connID string) func(ctx context.Context, input *ExportExcelWithChartInput) (*ExportExcelWithChartOutput, error) {
	return func(ctx context.Context, input *ExportExcelWithChartInput) (*ExportExcelWithChartOutput, error) {
		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在")
		}

		qr, err := queryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		rowCount := len(qr.Data)
		headerRow := 4
		dataStartRow := 5
		dataEndRow := rowCount + 4

		charts := normalizeCharts(input)

		f := excelize.NewFile()
		defer f.Close()

		dataSheet := "数据概览"
		f.SetSheetName("Sheet1", dataSheet)

		reportTitle := "数据分析报告"
		if len(charts) > 0 && charts[0].ChartTitle != "" {
			reportTitle = charts[0].ChartTitle
		}
		excelSetTitleArea(f, dataSheet, reportTitle, len(qr.Columns))
		excelWriteStyledTable(f, dataSheet, qr, 4)

		dashSheet := "分析总览"
		_, _ = f.NewSheet(dashSheet)
		excelDashboardSheet(f, dashSheet, qr)

		chartCount := 0
		for _, chart := range charts {
			xIdx := findFieldIndex(qr.Columns, chart.XAxisField)
			if xIdx == -1 {
				continue
			}
			xCol := colLetter(xIdx)

			var chartSeries []excelize.ChartSeries
			for _, series := range chart.Series {
				yIdx := findFieldIndex(qr.Columns, series.YAxisField)
				if yIdx == -1 {
					continue
				}
				yCol := colLetter(yIdx)
				name := series.Name
				if name == "" {
					name = series.YAxisField
				}
				chartSeries = append(chartSeries, excelize.ChartSeries{
					Name:       fmt.Sprintf("'%s'!$%s$%d", dataSheet, yCol, headerRow),
					Categories: fmt.Sprintf("'%s'!$%s$%d:$%s$%d", dataSheet, xCol, dataStartRow, xCol, dataEndRow),
					Values:     fmt.Sprintf("'%s'!$%s$%d:$%s$%d", dataSheet, yCol, dataStartRow, yCol, dataEndRow),
				})
			}

			if len(chartSeries) == 0 {
				continue
			}

			sheetName := chart.SheetName
			if sheetName == "" {
				chartCount++
				sheetName = fmt.Sprintf("图表%d", chartCount)
			}
			_, _ = f.NewSheet(sheetName)

			chartTitle := chart.ChartTitle
			if chartTitle == "" {
				chartTitle = chart.XAxisField
			}

			excelChart := &excelize.Chart{
				Type:   getChartType(chart.ChartType),
				Series: chartSeries,
				Format: excelize.GraphicOptions{
					ScaleX: 2.0,
					ScaleY: 2.0,
				},
				Title: []excelize.RichTextRun{
					{Text: chartTitle, Font: &excelize.Font{Size: 18, Bold: true, Color: "#1A237E"}},
				},
				Legend: excelize.ChartLegend{
					Position:      "bottom",
					ShowLegendKey: false,
				},
				PlotArea: excelize.ChartPlotArea{
					ShowVal: true,
				},
			}

			if err := f.AddChart(sheetName, "B2", excelChart); err != nil {
				log.Printf("[Tool:export_excel_chart] 添加图表 [%s] 失败 - err=%v\n", sheetName, err)
			}
		}

		f.SetActiveSheet(0)

		fileName := sanitizeFileName(input.FileName, "chart")
		ensureExportsDir()
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		if err := f.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Excel 失败：%w", err)
		}

		url := fmt.Sprintf("/exports/%s.xlsx", fileName)
		totalCharts := chartCount
		if totalCharts == 0 {
			totalCharts = len(charts)
		}
		log.Printf("[Tool:export_excel_chart] 成功 - rows=%d, charts=%d, url=%s\n", rowCount, totalCharts, url)

		msg := fmt.Sprintf("已生成含 %d 个图表的 Excel（%d 条数据），[点击下载](%s)", totalCharts, rowCount, url)
		if totalCharts == 1 && len(charts[0].Series) > 1 {
			msg = fmt.Sprintf("已生成含 %d 条折线/柱状图表的 Excel（%d 条数据），[点击下载](%s)", len(charts[0].Series), rowCount, url)
		}

		return &ExportExcelWithChartOutput{
			Message:     msg,
			RowCount:    rowCount,
			DownloadURL: url,
			FileType:    "excel_with_chart",
		}, nil
	}
}

func normalizeCharts(input *ExportExcelWithChartInput) []ExcelChart {
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

func findFieldIndex(columns []string, field string) int {
	idx := colIndex(columns, field)
	if idx >= 0 {
		return idx
	}
	lower := strings.ToLower(field)
	for i, c := range columns {
		if strings.Contains(strings.ToLower(c), lower) {
			return i
		}
	}
	return -1
}

func excelSetTitleArea(f *excelize.File, sheet, title string, colCount int) {
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 16, Bold: true, Color: "#1A237E", Family: "Microsoft YaHei"},
	})
	subStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 10, Color: "#757575", Family: "Microsoft YaHei"},
	})
	barStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#1A237E"}},
	})

	startCell, _ := excelize.CoordinatesToCellName(1, 1)
	endCell, _ := excelize.CoordinatesToCellName(colCount, 1)
	f.MergeCell(sheet, startCell, endCell)
	f.SetCellValue(sheet, startCell, title)
	f.SetCellStyle(sheet, startCell, endCell, titleStyle)

	startCell, _ = excelize.CoordinatesToCellName(1, 2)
	endCell, _ = excelize.CoordinatesToCellName(colCount, 2)
	f.MergeCell(sheet, startCell, endCell)
	f.SetCellValue(sheet, startCell, fmt.Sprintf("生成时间：%s  |  数据行数：%d", time.Now().Format("2006-01-02 15:04:05"), 0))
	f.SetCellStyle(sheet, startCell, endCell, subStyle)

	for i := 0; i < colCount; i++ {
		cell, _ := excelize.CoordinatesToCellName(i+1, 3)
		f.SetCellStyle(sheet, cell, cell, barStyle)
	}
	f.SetRowHeight(sheet, 1, 32)
	f.SetRowHeight(sheet, 2, 20)
	f.SetRowHeight(sheet, 3, 3)
}

func excelWriteStyledTable(f *excelize.File, sheet string, qr *queryResult, startRow int) {
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 11, Bold: true, Color: "#FFFFFF", Family: "Microsoft YaHei"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#1A237E"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "top", Color: "#1A237E", Style: 1},
			{Type: "bottom", Color: "#1A237E", Style: 1},
			{Type: "left", Color: "#1A237E", Style: 1},
			{Type: "right", Color: "#1A237E", Style: 1},
		},
	})
	rowEvenStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 10, Color: "#212121", Family: "Microsoft YaHei"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#F5F7FA"}},
		Border: []excelize.Border{
			{Type: "top", Color: "#E8ECF1", Style: 1},
			{Type: "bottom", Color: "#E8ECF1", Style: 1},
			{Type: "left", Color: "#E8ECF1", Style: 1},
			{Type: "right", Color: "#E8ECF1", Style: 1},
		},
	})
	rowOddStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 10, Color: "#212121", Family: "Microsoft YaHei"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#FFFFFF"}},
		Border: []excelize.Border{
			{Type: "top", Color: "#E8ECF1", Style: 1},
			{Type: "bottom", Color: "#E8ECF1", Style: 1},
			{Type: "left", Color: "#E8ECF1", Style: 1},
			{Type: "right", Color: "#E8ECF1", Style: 1},
		},
	})

	for i, c := range qr.Columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, startRow)
		f.SetCellValue(sheet, cell, c)
		f.SetCellStyle(sheet, cell, cell, headerStyle)
	}
	f.SetRowHeight(sheet, startRow, 28)

	for rowIdx, row := range qr.Data {
		currentRow := startRow + 1 + rowIdx
		for colIdx, col := range qr.Columns {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, currentRow)
			if v, ok := row[col]; ok {
				setCellAuto(f, sheet, cell, v)
			}
			if rowIdx%2 == 0 {
				f.SetCellStyle(sheet, cell, cell, rowEvenStyle)
			} else {
				f.SetCellStyle(sheet, cell, cell, rowOddStyle)
			}
		}
		f.SetRowHeight(sheet, currentRow, 22)
	}

	for i := range qr.Columns {
		colName, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, colName, colName, excelAutoWidth(qr, i))
	}
}

func excelAutoWidth(qr *queryResult, colIdx int) float64 {
	maxW := float64(len([]rune(qr.Columns[colIdx]))) * 1.5
	for _, row := range qr.Data {
		val := fmt.Sprintf("%v", row[qr.Columns[colIdx]])
		w := float64(len([]rune(val))) * 1.2
		if w > maxW {
			maxW = w
		}
	}
	if maxW < 8 {
		return 8
	}
	if maxW > 36 {
		return 36
	}
	return maxW + 2
}

func excelDashboardSheet(f *excelize.File, sheet string, qr *queryResult) {
	dashTitleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 18, Bold: true, Color: "#1A237E", Family: "Microsoft YaHei"},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	})
	dashSubStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 10, Color: "#757575", Family: "Microsoft YaHei"},
	})
	dashSectionStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 13, Bold: true, Color: "#1A237E", Family: "Microsoft YaHei"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#EDEFF7"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "bottom", Color: "#1A237E", Style: 1},
		},
	})
	kpiLabelStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 9, Color: "#757575", Family: "Microsoft YaHei"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	kpiValueStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 22, Bold: true, Color: "#1A237E", Family: "Microsoft YaHei"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	kpiAccentStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 22, Bold: true, Color: "#00BCD4", Family: "Microsoft YaHei"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	kpiBorderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{Type: "left", Color: "#EDEFF7", Style: 1},
			{Type: "right", Color: "#EDEFF7", Style: 1},
			{Type: "top", Color: "#EDEFF7", Style: 1},
			{Type: "bottom", Color: "#EDEFF7", Style: 1},
		},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#FFFFFF"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	f.SetCellStyle(sheet, "A1", "H1", dashTitleStyle)
	f.SetCellValue(sheet, "A1", "数据分析仪表盘")
	f.SetRowHeight(sheet, 1, 36)
	f.SetCellStyle(sheet, "A2", "H2", dashSubStyle)
	f.SetCellValue(sheet, "A2", fmt.Sprintf("生成时间：%s  |  数据行数：%d  |  字段数：%d", time.Now().Format("2006-01-02 15:04:05"), len(qr.Data), len(qr.Columns)))
	f.SetRowHeight(sheet, 2, 22)

	f.SetCellStyle(sheet, "A4", "H4", dashSectionStyle)
	f.SetCellValue(sheet, "A4", "▎核心指标")
	f.MergeCell(sheet, "A4", "H4")
	f.SetRowHeight(sheet, 4, 28)

	numericCols := detectNumericCols(qr)
	col := 1
	for _, nc := range numericCols {
		if col > 6 {
			break
		}
		_, max, avg, count := calcNumericStats(qr, nc)
		if count == 0 {
			continue
		}

		cellLabel, _ := excelize.CoordinatesToCellName(col, 6)
		cellValue, _ := excelize.CoordinatesToCellName(col, 7)
		f.SetCellStyle(sheet, cellLabel, cellLabel, kpiLabelStyle)
		f.SetCellValue(sheet, cellLabel, nc)
		f.SetRowHeight(sheet, 6, 20)

		valueStyle := kpiValueStyle
		if col%3 == 0 {
			valueStyle = kpiAccentStyle
		}
		f.SetCellStyle(sheet, cellValue, cellValue, valueStyle)
		f.SetCellValue(sheet, cellValue, fmt.Sprintf("%.1f", avg))
		f.SetRowHeight(sheet, 7, 38)

		detailCell, _ := excelize.CoordinatesToCellName(col, 8)
		f.SetCellStyle(sheet, detailCell, detailCell, kpiLabelStyle)
		f.SetCellValue(sheet, detailCell, fmt.Sprintf("最高 %.1f  ·  总计 %.1f", max, avg*float64(count)))
		f.SetRowHeight(sheet, 8, 18)

		for r := 5; r <= 9; r++ {
			for c := col; c <= col; c++ {
				cell, _ := excelize.CoordinatesToCellName(c, r)
				f.SetCellStyle(sheet, cell, cell, kpiBorderStyle)
			}
		}
		f.SetColWidth(sheet, colLetter(col-1), colLetter(col-1), 22)

		col++
	}

	infoRow := 11
	f.SetCellStyle(sheet, fmt.Sprintf("A%d", infoRow), fmt.Sprintf("H%d", infoRow), dashSectionStyle)
	f.SetCellValue(sheet, fmt.Sprintf("A%d", infoRow), "▎字段统计")
	f.MergeCell(sheet, fmt.Sprintf("A%d", infoRow), fmt.Sprintf("H%d", infoRow))
	f.SetRowHeight(sheet, infoRow, 28)

	infoRow += 2
	for i, nc := range numericCols {
		if i >= 8 {
			break
		}
		min, max, avg, count := calcNumericStats(qr, nc)
		if count == 0 {
			continue
		}
		cell, _ := excelize.CoordinatesToCellName(1, infoRow)
		f.SetCellStyle(sheet, cell, cell, dashSubStyle)
		f.SetCellValue(sheet, cell, fmt.Sprintf("%s：均值 %.2f  最小 %.2f  最大 %.2f", nc, avg, min, max))
		f.SetRowHeight(sheet, infoRow, 20)
		infoRow++
	}
}

func getChartType(t string) excelize.ChartType {
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

func generateDocxCharts(qr *queryResult, title, fileName string) []string {
	numericCols := detectNumericCols(qr)
	if len(numericCols) == 0 {
		return nil
	}

	if len(qr.Data) == 0 || len(qr.Columns) < 2 {
		return nil
	}

	xCol := qr.Columns[0]
	var paths []string

	if len(numericCols) > 1 {
		seriesNames := numericCols
		if len(seriesNames) > 4 {
			seriesNames = seriesNames[:4]
		}

		var seriesList []chartSeries
		for _, col := range seriesNames {
			xVals, yVals, labels := extractXYSeries(qr, xCol, col)
			seriesList = append(seriesList, chartSeries{
				Name:    col,
				XValues: xVals,
				YValues: yVals,
				XLabels: labels,
			})
		}
		if len(seriesList) > 1 {
			linePath := fmt.Sprintf("exports/%s_chart_multi.png", fileName)
			if err := renderMultiChartPNG(seriesList, title+" · 趋势对比", "line", linePath); err != nil {
				log.Printf("[Tool:export_docx] 多系列图表生成失败: %v\n", err)
			} else {
				paths = append(paths, linePath)
			}
		}
	}

	if len(numericCols) >= 1 {
		var seriesList []chartSeries
		for _, col := range numericCols {
			if len(seriesList) >= 3 {
				break
			}
			xVals, yVals, labels := extractXYSeries(qr, xCol, col)
			seriesList = append(seriesList, chartSeries{
				Name:    col,
				XValues: xVals,
				YValues: yVals,
				XLabels: labels,
			})
		}
		barPath := fmt.Sprintf("exports/%s_chart_bar.png", fileName)
		if err := renderMultiChartPNG(seriesList, title+" · 指标对比", "bar", barPath); err != nil {
			log.Printf("[Tool:export_docx] 柱状图生成失败: %v\n", err)
		} else {
			paths = append(paths, barPath)
		}
	}

	return paths
}

func generatePptCharts(qr *queryResult, title, fileName string) []string {
	numericCols := detectNumericCols(qr)
	if len(numericCols) == 0 || len(qr.Data) == 0 || len(qr.Columns) < 2 {
		return nil
	}

	xCol := qr.Columns[0]
	var paths []string

	if len(numericCols) > 1 {
		seriesNames := numericCols
		if len(seriesNames) > 3 {
			seriesNames = seriesNames[:3]
		}

		var seriesList []chartSeries
		for _, col := range seriesNames {
			xVals, yVals, labels := extractXYSeries(qr, xCol, col)
			seriesList = append(seriesList, chartSeries{
				Name:    col,
				XValues: xVals,
				YValues: yVals,
				XLabels: labels,
			})
		}
		if len(seriesList) > 0 {
			chartPath := fmt.Sprintf("exports/%s_ppt_chart.png", fileName)
			chartType := "line"
			if len(seriesList) == 1 {
				chartType = "bar"
			}
			if err := renderMultiChartPNG(seriesList, title+" · 关键指标", chartType, chartPath); err != nil {
				log.Printf("[Tool:export_ppt] 图表生成失败: %v\n", err)
			} else {
				paths = append(paths, chartPath)
			}
		}
	}

	return paths
}

func extractXYSeries(qr *queryResult, xCol, yCol string) (xVals, yVals []float64, labels []string) {
	xIdx := colIndex(qr.Columns, xCol)
	yIdx := colIndex(qr.Columns, yCol)
	if xIdx < 0 || yIdx < 0 {
		return nil, nil, nil
	}

	for i, row := range qr.Data {
		yVal, err := toFloat64(row[qr.Columns[yIdx]])
		if err != nil {
			yVal = 0
		}
		xVals = append(xVals, float64(i))
		yVals = append(yVals, yVal)
		labels = append(labels, fmt.Sprintf("%v", row[qr.Columns[xIdx]]))
	}
	return
}

// ──────────────────────────────────────────────
// Word 报告导出（DOCX = ZIP of XML）
// ──────────────────────────────────────────────

func NewExportAnalysisDocxFunc(connID string) func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
	return func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
		title := input.Title
		if title == "" {
			title = "数据分析报告"
		}

		fileName := sanitizeFileName(input.FileName, "report")
		ensureExportsDir()
		filePath := fmt.Sprintf("exports/%s.docx", fileName)

		if input.Content != "" {
			if err := generateDocxFromContent(input.Content, title, filePath); err != nil {
				return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
			}
			url := fmt.Sprintf("/exports/%s.docx", fileName)
			log.Printf("[Tool:export_docx] 成功 (content) - url=%s\n", url)
			return &ExportAnalysisDocxOutput{
				Message:     fmt.Sprintf("已生成 Word 分析报告，[点击下载](%s)", url),
				DownloadURL: url,
				FileType:    "docx",
			}, nil
		}

		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在")
		}

		qr, err := queryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		var chartImagePaths []string
		if input.IncludeChart && len(qr.Columns) >= 2 && len(qr.Data) > 0 {
			chartImagePaths = generateDocxCharts(qr, title, fileName)
		}

		if err := generateDocx(qr, title, chartImagePaths, filePath); err != nil {
			return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
		}

		for _, p := range chartImagePaths {
			os.Remove(p)
		}

		url := fmt.Sprintf("/exports/%s.docx", fileName)
		log.Printf("[Tool:export_docx] 成功 - rows=%d, url=%s\n", len(qr.Data), url)

		return &ExportAnalysisDocxOutput{
			Message:     fmt.Sprintf("已生成 Word 报告（%d 条数据），[点击下载](%s)", len(qr.Data), url),
			DownloadURL: url,
			FileType:    "docx",
		}, nil
	}
}

// ──────────────────────────────────────────────
// PPT 导出（PPTX = ZIP of XML）
// ──────────────────────────────────────────────

func NewExportPPTFunc(connID string) func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
	return func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
		title := input.Title
		if title == "" {
			title = "数据报告"
		}

		fileName := sanitizeFileName(input.FileName, "slides")
		ensureExportsDir()
		filePath := fmt.Sprintf("exports/%s.pptx", fileName)

		if input.Content != "" {
			slideCount, err := generatePptxFromContent(input.Content, title, filePath)
			if err != nil {
				return nil, fmt.Errorf("生成 PPT 失败：%w", err)
			}
			url := fmt.Sprintf("/exports/%s.pptx", fileName)
			log.Printf("[Tool:export_ppt] 成功 (content) - slides=%d, url=%s\n", slideCount, url)
			return &ExportPPTOutput{
				Message:     fmt.Sprintf("已生成 PPT（%d 页），[点击下载](%s)", slideCount, url),
				SlideCount:  slideCount,
				DownloadURL: url,
				FileType:    "ppt",
			}, nil
		}

		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在")
		}

		qr, err := queryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		chartPaths := generatePptCharts(qr, title, fileName)

		slideCount, err := generatePptx(qr, title, chartPaths, filePath)
		if err != nil {
			return nil, fmt.Errorf("生成 PPT 失败：%w", err)
		}

		for _, p := range chartPaths {
			os.Remove(p)
		}

		url := fmt.Sprintf("/exports/%s.pptx", fileName)
		log.Printf("[Tool:export_ppt] 成功 - slides=%d, url=%s\n", slideCount, url)

		return &ExportPPTOutput{
			Message:     fmt.Sprintf("已生成 PPT（%d 页），[点击下载](%s)", slideCount, url),
			SlideCount:  slideCount,
			DownloadURL: url,
			FileType:    "ppt",
		}, nil
	}
}

// ──────────────────────────────────────────────
// Excel 写入辅助
// ──────────────────────────────────────────────

// writeExcelSheet 将查询结果写入指定 sheet
// 当 useStream=true 时使用 StreamWriter（高性能但不支持图表引用），否则使用普通写入
func writeExcelSheet(f *excelize.File, sheet string, qr *queryResult) {
	writeExcelSheetMode(f, sheet, qr, true)
}

func writeExcelSheetMode(f *excelize.File, sheet string, qr *queryResult, useStream bool) {
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
		// 普通写入模式：兼容图表数据引用
		for i, c := range qr.Columns {
			cell := fmt.Sprintf("%s1", colLetter(i))
			f.SetCellValue(sheet, cell, c)
		}
		for rowIdx, row := range qr.Data {
			for colIdx, col := range qr.Columns {
				cell := fmt.Sprintf("%s%d", colLetter(colIdx), rowIdx+2)
				if v, ok := row[col]; ok {
					setCellAuto(f, sheet, cell, v)
				}
			}
		}
	}
}

// setCellAuto 智能设置单元格值，尽量保留数值类型（避免图表读到文本 0）
func setCellAuto(f *excelize.File, sheet, cell string, v any) {
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
		// 数据库驱动常返回 []byte，尝试转数值
		s := string(val)
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			f.SetCellValue(sheet, cell, n)
		} else {
			f.SetCellValue(sheet, cell, s)
		}
	case string:
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			f.SetCellValue(sheet, cell, n)
		} else {
			f.SetCellValue(sheet, cell, val)
		}
	default:
		f.SetCellValue(sheet, cell, fmt.Sprintf("%v", v))
	}
}

// ──────────────────────────────────────────────
// Markdown 解析（供 DOCX/PPTX 内容导出共用）
// ──────────────────────────────────────────────

type mdBlock struct {
	Type    string // "h1", "h2", "h3", "paragraph", "list", "code", "mermaid", "table"
	Content string
	Lang    string
}

func parseMarkdownBlocks(content string) []mdBlock {
	var blocks []mdBlock
	lines := strings.Split(content, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimPrefix(trimmed, "```")
			var codeLines []string
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				codeLines = append(codeLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			blockType := "code"
			if strings.TrimSpace(lang) == "mermaid" {
				blockType = "mermaid"
			}
			blocks = append(blocks, mdBlock{
				Type:    blockType,
				Content: strings.Join(codeLines, "\n"),
				Lang:    strings.TrimSpace(lang),
			})
			continue
		}

		if strings.HasPrefix(trimmed, "### ") {
			blocks = append(blocks, mdBlock{Type: "h3", Content: strings.TrimPrefix(trimmed, "### ")})
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			blocks = append(blocks, mdBlock{Type: "h2", Content: strings.TrimPrefix(trimmed, "## ")})
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			blocks = append(blocks, mdBlock{Type: "h1", Content: strings.TrimPrefix(trimmed, "# ")})
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "|") {
			var tableLines []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				tableLines = append(tableLines, lines[i])
				i++
			}
			blocks = append(blocks, mdBlock{Type: "table", Content: strings.Join(tableLines, "\n")})
			continue
		}

		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			var items []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if strings.HasPrefix(t, "- ") {
					items = append(items, strings.TrimPrefix(t, "- "))
					i++
				} else if strings.HasPrefix(t, "* ") {
					items = append(items, strings.TrimPrefix(t, "* "))
					i++
				} else if t == "" {
					i++
					break
				} else {
					break
				}
			}
			blocks = append(blocks, mdBlock{Type: "list", Content: strings.Join(items, "\n")})
			continue
		}

		if strings.HasPrefix(trimmed, "1. ") || strings.HasPrefix(trimmed, "1) ") {
			var items []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if matched, _ := regexp.MatchString(`^\d+[.)]\s`, t); matched {
					re := regexp.MustCompile(`^\d+[.)]\s*`)
					items = append(items, re.ReplaceAllString(t, ""))
					i++
				} else if t == "" {
					i++
					break
				} else {
					break
				}
			}
			blocks = append(blocks, mdBlock{Type: "list", Content: strings.Join(items, "\n")})
			continue
		}

		var paraLines []string
		for i < len(lines) {
			t := strings.TrimSpace(lines[i])
			if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "```") ||
				strings.HasPrefix(t, "|") || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
				break
			}
			if matched, _ := regexp.MatchString(`^\d+[.)]\s`, t); matched {
				break
			}
			paraLines = append(paraLines, lines[i])
			i++
		}
		if len(paraLines) > 0 {
			blocks = append(blocks, mdBlock{Type: "paragraph", Content: strings.Join(paraLines, " ")})
		}
	}
	return blocks
}

var reMarkdownBold = regexp.MustCompile(`\*\*(.+?)\*\*`)
var reMarkdownItalic = regexp.MustCompile(`\*(.+?)\*`)
var reMarkdownCode = regexp.MustCompile("`(.+?)`")
var reMarkdownLink = regexp.MustCompile(`\[(.+?)\]\(.+?\)`)

func stripMarkdownFormatting(s string) string {
	s = reMarkdownBold.ReplaceAllString(s, "$1")
	s = reMarkdownItalic.ReplaceAllString(s, "$1")
	s = reMarkdownCode.ReplaceAllString(s, "$1")
	s = reMarkdownLink.ReplaceAllString(s, "$1")
	return s
}

func isTableSeparator(line string) bool {
	trimmed := strings.ReplaceAll(strings.TrimSpace(line), "|", "")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return false
	}
	for _, c := range trimmed {
		if c != '-' && c != ':' && c != ' ' {
			return false
		}
	}
	return true
}
