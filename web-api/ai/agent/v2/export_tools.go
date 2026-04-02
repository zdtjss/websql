// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2 - 导出工具集合
package agentv2

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	dbutils "go-web/utils/db"

	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/xuri/excelize/v2"
)

// === 导出工具输入/输出结构体 ===

// ExportExcelInput 导出 Excel 表格的输入
type ExportExcelInput struct {
	SQL      string `json:"sql"`
	FileName string `json:"fileName"`
}

// ExportExcelOutput 导出结果
type ExportExcelOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"` // "excel"
}

// ExportExcelWithChartInput 导出带图表的 Excel 的输入
type ExportExcelWithChartInput struct {
	SQL        string `json:"sql"`
	FileName   string `json:"fileName"`
	ChartType  string `json:"chartType"` // line, bar, pie, scatter
	XAxisField string `json:"xAxisField"`
	YAxisField string `json:"yAxisField"`
	ChartTitle string `json:"chartTitle"`
}

// ExportExcelWithChartOutput 导出结果
type ExportExcelWithChartOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"` // "excel_with_chart"
}

// ExportPPTInput 导出 PPT 的输入
type ExportPPTInput struct {
	SQL       string `json:"sql"`
	FileName  string `json:"fileName"`
	Title     string `json:"title"`
	SlideType string `json:"slideType"` // summary, table, chart
}

// ExportPPTOutput 导出结果
type ExportPPTOutput struct {
	Message     string `json:"message"`
	SlideCount  int    `json:"slideCount"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"` // "ppt"
}

// ExportAnalysisImageInput 导出分析图表的输入
type ExportAnalysisImageInput struct {
	SQL        string `json:"sql"`
	FileName   string `json:"fileName"`
	ChartType  string `json:"chartType"` // line, bar, pie, scatter, heatmap
	XAxisField string `json:"xAxisField"`
	YAxisField string `json:"yAxisField"`
	Title      string `json:"title"`
}

// ExportAnalysisImageOutput 导出结果
type ExportAnalysisImageOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"` // "image"
	Format      string `json:"format"`   // "png" or "jpg"
}

// ExportAnalysisDocxInput 导出 Word 分析报告的输入
type ExportAnalysisDocxInput struct {
	SQL          string   `json:"sql"`
	FileName     string   `json:"fileName"`
	Title        string   `json:"title"`
	Sections     []string `json:"sections"`
	IncludeChart bool     `json:"includeChart"`
}

// ExportAnalysisDocxOutput 导出结果
type ExportAnalysisDocxOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"` // "docx"
}

// === 导出工具实现函数 ===

// NewExportExcelFunc 创建导出 Excel 表格的工具函数
func NewExportExcelFunc(connID string) func(ctx context.Context, input *ExportExcelInput) (*ExportExcelOutput, error) {
	return func(ctx context.Context, input *ExportExcelInput) (*ExportExcelOutput, error) {
		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在")
		}

		sql := strings.TrimSpace(input.SQL)

		// 导出工具只验证是否为空
		if sql == "" {
			return nil, fmt.Errorf("SQL 不能为空")
		}

		// 不限制 SQL 类型，因为导出可能需要复杂的查询
		// 但建议以 SELECT 开头
		if !strings.HasPrefix(strings.ToUpper(sql), "SELECT") {
			return nil, fmt.Errorf("导出功能仅支持 SELECT 查询语句")
		}

		rows, err := conn.Queryx(sql)
		if err != nil {
			return nil, fmt.Errorf("导出查询失败：%w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("获取列信息失败：%w", err)
		}

		// 创建 Excel 文件
		excel := excelize.NewFile()
		defer excel.Close()

		sw, err := excel.NewStreamWriter("Sheet1")
		if err != nil {
			return nil, fmt.Errorf("创建 Excel 写入器失败：%w", err)
		}

		// 写表头
		header := make([]interface{}, len(cols))
		for i, colName := range cols {
			header[i] = colName
		}
		sw.SetRow("A1", header)

		// 写数据
		data := dbutils.GetResultRowsForExport(conn.DriverName(), rows)
		for rowIdx, row := range data {
			rowData := make([]interface{}, len(cols))
			for colIdx, col := range cols {
				if v, ok := row[col]; ok {
					rowData[colIdx] = v
				}
			}
			cell := fmt.Sprintf("A%d", rowIdx+2)
			sw.SetRow(cell, rowData)
		}
		sw.Flush()

		// 生成文件名并保存
		fileName := input.FileName
		if fileName == "" {
			fileName = fmt.Sprintf("export_%s", time.Now().Format("20060102_150405"))
		}
		fileName = strings.TrimSuffix(fileName, ".csv")
		fileName = strings.TrimSuffix(fileName, ".xlsx")
		fileName = strings.TrimSuffix(fileName, ".xls")

		os.MkdirAll("exports", 0755)
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		if err := excel.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Excel 文件失败：%w", err)
		}

		downloadURL := fmt.Sprintf("/exports/%s.xlsx", fileName)

		return &ExportExcelOutput{
			Message:     fmt.Sprintf("已导出 %d 条数据到 Excel 文件，[点击下载](%s)", len(data), downloadURL),
			RowCount:    len(data),
			DownloadURL: downloadURL,
			FileType:    "excel",
		}, nil
	}
}

// NewExportExcelWithChartFunc 创建导出带图表的 Excel 的工具函数
func NewExportExcelWithChartFunc(connID string) func(ctx context.Context, input *ExportExcelWithChartInput) (*ExportExcelWithChartOutput, error) {
	return func(ctx context.Context, input *ExportExcelWithChartInput) (*ExportExcelWithChartOutput, error) {
		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在")
		}

		sql := strings.TrimSpace(input.SQL)
		rows, err := conn.Queryx(sql)
		if err != nil {
			return nil, fmt.Errorf("导出查询失败：%w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("获取列信息失败：%w", err)
		}

		// 创建 Excel 文件
		excel := excelize.NewFile()
		defer excel.Close()

		sw, err := excel.NewStreamWriter("Sheet1")
		if err != nil {
			return nil, fmt.Errorf("创建 Excel 写入器失败：%w", err)
		}

		// 写表头
		header := make([]interface{}, len(cols))
		for i, colName := range cols {
			header[i] = colName
		}
		sw.SetRow("A1", header)

		// 写数据
		data := dbutils.GetResultRowsForExport(conn.DriverName(), rows)
		for rowIdx, row := range data {
			rowData := make([]interface{}, len(cols))
			for colIdx, col := range cols {
				if v, ok := row[col]; ok {
					rowData[colIdx] = v
				}
			}
			cell := fmt.Sprintf("A%d", rowIdx+2)
			sw.SetRow(cell, rowData)
		}
		sw.Flush()

		// 添加图表（简化版）
		chartType := input.ChartType
		if chartType == "" {
			chartType = "line"
		}

		// 查找 X 轴和 Y 轴字段的索引
		xIndex := -1
		yIndex := -1
		for i, col := range cols {
			if col == input.XAxisField {
				xIndex = i
			}
			if col == input.YAxisField {
				yIndex = i
			}
		}

		if xIndex == -1 || yIndex == -1 {
			return nil, fmt.Errorf("未找到指定的 X 轴或 Y 轴字段")
		}

		// 创建图表（使用 excelize 内置图表功能）
		chart := &excelize.Chart{
			Type: getChartType(chartType),
			Series: []excelize.ChartSeries{
				{
					Name:       "Sheet1!$B$1",
					Categories: "Sheet1!$" + string(rune('A'+xIndex)) + "$2:$" + string(rune('A'+xIndex)) + fmt.Sprintf("$%d", len(data)+1),
					Values:     "Sheet1!$" + string(rune('A'+yIndex)) + "$2:$" + string(rune('A'+yIndex)) + fmt.Sprintf("$%d", len(data)+1),
				},
			},
			Title: []excelize.RichTextRun{
				{Text: input.ChartTitle},
			},
		}

		// 在第二个 sheet 中创建图表
		excel.NewSheet("Chart")
		if err := excel.AddChart("Chart", "A1", chart); err != nil {
			return nil, fmt.Errorf("添加图表失败：%w", err)
		}

		// 生成文件名并保存
		fileName := input.FileName
		if fileName == "" {
			fileName = fmt.Sprintf("chart_%s", time.Now().Format("20060102_150405"))
		}
		fileName = strings.TrimSuffix(fileName, ".xlsx")

		os.MkdirAll("exports", 0755)
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		if err := excel.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Excel 文件失败：%w", err)
		}

		downloadURL := fmt.Sprintf("/exports/%s.xlsx", fileName)

		return &ExportExcelWithChartOutput{
			Message:     fmt.Sprintf("已生成带图表的 Excel 文件（%d 条数据），[点击下载](%s)", len(data), downloadURL),
			RowCount:    len(data),
			DownloadURL: downloadURL,
			FileType:    "excel_with_chart",
		}, nil
	}
}

// getChartType 将字符串转换为 excelize 图表类型
func getChartType(chartType string) excelize.ChartType {
	switch chartType {
	case "bar":
		return excelize.Col
	case "pie":
		return excelize.Pie
	case "scatter":
		return excelize.Scatter
	default:
		return excelize.Line
	}
}

// NewExportPPTFunc 创建导出 PPT 的工具函数
// TODO: 需要安装 OfficeForge 并创建模板文件
func NewExportPPTFunc(connID string) func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
	return func(ctx context.Context, input *ExportPPTInput) (*ExportPPTOutput, error) {
		// 暂时返回提示信息
		return &ExportPPTOutput{
			Message:     "PPT 生成功能待实现（需要使用 OfficeForge 库并创建模板文件）",
			SlideCount:  0,
			DownloadURL: "",
			FileType:    "ppt",
		}, nil
	}
}

// NewExportAnalysisImageFunc 创建导出分析图表的工具函数
func NewExportAnalysisImageFunc(connID string) func(ctx context.Context, input *ExportAnalysisImageInput) (*ExportAnalysisImageOutput, error) {
	return func(ctx context.Context, input *ExportAnalysisImageInput) (*ExportAnalysisImageOutput, error) {
		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在")
		}

		sql := strings.TrimSpace(input.SQL)
		rows, err := conn.Queryx(sql)
		if err != nil {
			return nil, fmt.Errorf("导出查询失败：%w", err)
		}
		defer rows.Close()

		data := dbutils.GetResultRowsForExport(conn.DriverName(), rows)
		_ = data

		// TODO: 实现图表生成逻辑（需要深入研究 go-chart API）
		fileName := input.FileName
		if fileName == "" {
			fileName = fmt.Sprintf("chart_%s", time.Now().Format("20060102_150405"))
		}
		fileName = strings.TrimSuffix(fileName, ".png")

		return &ExportAnalysisImageOutput{
			Message:     "图表生成功能开发中（框架已完成，待实现具体生成逻辑）",
			DownloadURL: "",
			FileType:    "image",
			Format:      "png",
		}, nil
	}
}

// NewExportAnalysisDocxFunc 创建导出 Word 报告的工具函数
func NewExportAnalysisDocxFunc(connID string) func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
	return func(ctx context.Context, input *ExportAnalysisDocxInput) (*ExportAnalysisDocxOutput, error) {
		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在")
		}

		sql := strings.TrimSpace(input.SQL)
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

		// 使用 docx 创建 Word 文档
		builder := docx.NewDocumentBuilder(
			docx.WithTitle(input.Title),
			docx.WithDefaultFont("Arial"),
			docx.WithDefaultFontSize(22), // 11pt
		)

		// 添加标题
		builder.AddParagraph().
			Text(input.Title).
			Bold().
			FontSize(32). // 16pt
			Alignment(docx.AlignmentCenter).
			End()

		// 添加空行
		builder.AddParagraph().Text("").End()

		// 添加生成时间
		builder.AddParagraph().
			Text(fmt.Sprintf("生成时间：%s", time.Now().Format("2006-01-02 15:04:05"))).
			FontSize(18). // 9pt
			Italic().
			End()

		// 添加空行
		builder.AddParagraph().Text("").End()

		// 添加数据表格
		if len(data) > 0 {
			builder.AddParagraph().
				Text("数据明细").
				Bold().
				FontSize(24). // 12pt
				End()

			builder.AddParagraph().Text("").End()

			// 创建表格（数据 + 表头）
			table := builder.AddTable(len(data)+1, len(cols))

			// 填充表头
			headerRow := table.Row(0)
			for colIdx, colName := range cols {
				cell := headerRow.Cell(colIdx)
				cell.Text(colName).Bold().End()
			}

			// 填充数据
			for rowIdx, row := range data {
				tableRow := table.Row(rowIdx + 1)
				for colIdx, col := range cols {
					cell := tableRow.Cell(colIdx)
					if v, ok := row[col]; ok {
						cell.Text(fmt.Sprintf("%v", v)).End()
					} else {
						cell.Text("").End()
					}
				}
			}

			// 添加空行
			builder.AddParagraph().Text("").End()
		}

		// 添加统计信息
		builder.AddParagraph().
			Text("统计信息").
			Bold().
			FontSize(24).
			End()

		builder.AddParagraph().Text("").End()
		builder.AddParagraph().Text(fmt.Sprintf("总记录数：%d", len(data))).End()
		builder.AddParagraph().Text(fmt.Sprintf("列数：%d", len(cols))).End()

		// 构建文档
		doc, err := builder.Build()
		if err != nil {
			return nil, fmt.Errorf("构建 Word 文档失败：%w", err)
		}

		// 生成文件名并保存
		fileName := input.FileName
		if fileName == "" {
			fileName = fmt.Sprintf("report_%s", time.Now().Format("20060102_150405"))
		}
		fileName = strings.TrimSuffix(fileName, ".docx")

		os.MkdirAll("exports", 0755)
		filePath := fmt.Sprintf("exports/%s.docx", fileName)
		if err := doc.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Word 文件失败：%w", err)
		}

		downloadURL := fmt.Sprintf("/exports/%s.docx", fileName)

		return &ExportAnalysisDocxOutput{
			Message:     fmt.Sprintf("已生成 Word 报告（%d 条数据），[点击下载](%s)", len(data), downloadURL),
			DownloadURL: downloadURL,
			FileType:    "docx",
		}, nil
	}
}
