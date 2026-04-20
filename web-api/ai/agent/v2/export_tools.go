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

type ExportExcelWithChartInput struct {
	SQL        string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出的 SELECT SQL"`
	FileName   string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	ChartType  string `json:"chartType" jsonschema_description:"图表类型: line, bar, pie, scatter"`
	XAxisField string `json:"xAxisField" jsonschema:"required" jsonschema_description:"X 轴字段名"`
	YAxisField string `json:"yAxisField" jsonschema:"required" jsonschema_description:"Y 轴字段名"`
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

		f := excelize.NewFile()
		defer f.Close()

		// 写数据到 Sheet1（使用普通模式以支持图表数据引用）
		writeExcelSheetMode(f, "Sheet1", qr, false)

		// 查找 X/Y 轴列索引
		xIdx := colIndex(qr.Columns, input.XAxisField)
		yIdx := colIndex(qr.Columns, input.YAxisField)
		if xIdx == -1 || yIdx == -1 {
			// 如果找不到精确匹配，尝试模糊匹配
			for i, c := range qr.Columns {
				cl := strings.ToLower(c)
				if xIdx == -1 && strings.Contains(cl, strings.ToLower(input.XAxisField)) {
					xIdx = i
				}
				if yIdx == -1 && strings.Contains(cl, strings.ToLower(input.YAxisField)) {
					yIdx = i
				}
			}
		}
		if xIdx == -1 || yIdx == -1 {
			return nil, fmt.Errorf("未找到 X 轴字段 '%s' 或 Y 轴字段 '%s'，可用列：%s",
				input.XAxisField, input.YAxisField, strings.Join(qr.Columns, ", "))
		}

		rowCount := len(qr.Data)
		xCol := colLetter(xIdx)
		yCol := colLetter(yIdx)

		chartTitle := input.ChartTitle
		if chartTitle == "" {
			chartTitle = fmt.Sprintf("%s vs %s", input.XAxisField, input.YAxisField)
		}

		chart := &excelize.Chart{
			Type: getChartType(input.ChartType),
			Series: []excelize.ChartSeries{
				{
					Name:       fmt.Sprintf("Sheet1!$%s$1", yCol),
					Categories: fmt.Sprintf("Sheet1!$%s$2:$%s$%d", xCol, xCol, rowCount+1),
					Values:     fmt.Sprintf("Sheet1!$%s$2:$%s$%d", yCol, yCol, rowCount+1),
				},
			},
			Title: []excelize.RichTextRun{{Text: chartTitle}},
			PlotArea: excelize.ChartPlotArea{
				ShowVal: true,
			},
		}

		// 图表放在数据右侧
		chartCell := fmt.Sprintf("%s1", colLetter(len(qr.Columns)+1))
		if err := f.AddChart("Sheet1", chartCell, chart); err != nil {
			log.Printf("[Tool:export_excel_chart] 添加图表失败 - err=%v\n", err)
			// 图表失败不影响数据导出
		}

		fileName := sanitizeFileName(input.FileName, "chart")
		ensureExportsDir()
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		if err := f.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Excel 失败：%w", err)
		}

		url := fmt.Sprintf("/exports/%s.xlsx", fileName)
		log.Printf("[Tool:export_excel_chart] 成功 - rows=%d, url=%s\n", rowCount, url)

		return &ExportExcelWithChartOutput{
			Message:     fmt.Sprintf("已生成带 %s 图表的 Excel（%d 条数据），[点击下载](%s)", input.ChartType, rowCount, url),
			RowCount:    rowCount,
			DownloadURL: url,
			FileType:    "excel_with_chart",
		}, nil
	}
}

func getChartType(t string) excelize.ChartType {
	switch strings.ToLower(t) {
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

		var chartImagePath string
		if input.IncludeChart && len(qr.Columns) >= 2 && len(qr.Data) > 0 {
			chartImagePath = fmt.Sprintf("exports/%s_chart.png", fileName)
			if err := renderChartPNG(qr, 0, 1, title, "bar", chartImagePath); err != nil {
				log.Printf("[Tool:export_docx] 生成图表失败 - err=%v\n", err)
				chartImagePath = ""
			}
			if chartImagePath != "" {
				if _, statErr := os.Stat(chartImagePath); os.IsNotExist(statErr) {
					chartImagePath = ""
				}
			}
		}

		if err := generateDocx(qr, title, chartImagePath, filePath); err != nil {
			return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
		}

		if chartImagePath != "" {
			os.Remove(chartImagePath)
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

		slideCount, err := generatePptx(qr, title, filePath)
		if err != nil {
			return nil, fmt.Errorf("生成 PPT 失败：%w", err)
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
	trimmed := strings.Trim(strings.TrimSpace(line), "|")
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
