package export

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/ai/agent/export/report"
	"websql/internal/audit"

	"github.com/jmoiron/sqlx"
)

// AuditCtx 导出工具所需的最小审计上下文。
// export 包不能反向依赖 internal/ai/agent（会形成循环导入），
// 因此这里定义本地最小结构，由 agent 包在构造导出工具时注入。
type AuditCtx struct {
	ConnID    string
	UserID    string
	UserName  string
	SessionID string
}

// recordExportAudit 记录一次导出工具调用（成功/失败各一条），
// SQLType 固定为 EXPORT、RiskLevel 固定为 low。
// auditCtx 为 nil 时仍记录（不含用户上下文），保证导出行为始终可追溯。
func recordExportAudit(auditCtx *AuditCtx, toolName string, start time.Time, err error) {
	status := "success"
	errorMsg := ""
	if err != nil {
		status = "failed"
		errorMsg = err.Error()
	}

	entry := &audit.AuditEntry{
		Source:     "agent",
		ToolName:   toolName,
		SQLType:    "EXPORT",
		RiskLevel:  "low",
		Status:     status,
		ExecTimeMs: int(time.Since(start).Milliseconds()),
		ErrorMsg:   errorMsg,
	}
	if auditCtx != nil {
		entry.ConnID = auditCtx.ConnID
		entry.UserID = auditCtx.UserID
		entry.UserName = auditCtx.UserName
		entry.SessionID = auditCtx.SessionID
	}
	audit.GetAuditService().Record(entry)
}

func ownerOf(auditCtx *AuditCtx) string {
	if auditCtx == nil {
		return ""
	}
	return auditCtx.UserID
}

// writeExportFile 把渲染产物写入 exports 目录并登记归属，返回下载 URL。
func writeExportFile(fileName, ext string, data []byte, auditCtx *AuditCtx) (string, error) {
	EnsureExportsDir()
	filePath := filepath.Join("exports", fileName+"."+ext)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("保存导出文件失败：%w", err)
	}
	url := "/exports/" + fileName + "." + ext
	RecordExportOwnerByURL(url, ownerOf(auditCtx))
	return url, nil
}

// queryRowsAny 把查询结果转为按列顺序的原始值二维表（保留数值类型供 Excel 数字格式识别）
func queryRowsAny(qr *QueryResult) [][]any {
	rows := make([][]any, 0, len(qr.Data))
	for _, row := range qr.Data {
		cells := make([]any, len(qr.Columns))
		for i, col := range qr.Columns {
			cells[i] = row[col]
		}
		rows = append(rows, cells)
	}
	return rows
}

// buildExportDocument 构建报告文档：
//   - content 模式：解析扩展 Markdown（```kpi/```chart/```table 等），并回填内嵌 SQL 数据；
//   - sql 模式：由查询结果自动生成「指标卡 + 图表 + 明细表」图文报告。
func buildExportDocument(ctx context.Context, conn *sqlx.DB, ds DataSource, title, subtitle, content, sql string) (*report.Document, error) {
	content = strings.TrimSpace(content)
	sql = strings.TrimSpace(sql)
	switch {
	case content != "":
		doc := report.BuildDocument(title, subtitle, content)
		BindDocument(ctx, doc, ds)
		return doc, nil
	case sql != "":
		qr, err := QueryForExport(conn, sql)
		if err != nil {
			return nil, err
		}
		return BuildDataDocument(title, subtitle, qr), nil
	default:
		return nil, fmt.Errorf("必须提供 content（扩展 Markdown 报告正文）或 sql 参数")
	}
}

// NewExportExcelFunc 创建 Excel 数据导出工具（Go 原生渲染：表头样式/冻结/筛选/数字格式）。
func NewExportExcelFunc(conn *sqlx.DB, auditCtx *AuditCtx) func(ctx context.Context, input *ExportExcelInput) (*ExportExcelOutput, error) {
	return func(ctx context.Context, input *ExportExcelInput) (out *ExportExcelOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(auditCtx, "export_excel", start, err) }()

		qr, err := QueryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}
		title := strings.TrimSpace(input.Title)
		if title == "" {
			title = "数据导出"
		}

		buf, err := report.RenderXLSX(report.XLSXOptions{
			Title: title,
			Theme: report.ResolveTheme("", "business"),
			Data:  &report.SheetData{Name: "数据明细", Headers: qr.Columns, Rows: queryRowsAny(qr)},
			SQL:   input.SQL,
		})
		if err != nil {
			return nil, fmt.Errorf("生成 Excel 失败：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "export")
		url, err := writeExportFile(fileName, "xlsx", buf, auditCtx)
		if err != nil {
			return nil, err
		}
		log.Printf("[Tool:export_excel] 成功 - rows=%d, url=%s\n", len(qr.Data), url)

		return &ExportExcelOutput{
			Message:     fmt.Sprintf("已导出 %d 条数据，[点击下载](%s)", len(qr.Data), url),
			RowCount:    len(qr.Data),
			DownloadURL: url,
			FileType:    "excel",
		}, nil
	}
}

// NewExportExcelWithChartFunc 创建带图表的 Excel 导出工具。
// 产出：分析摘要（指标卡/统计）+ 数据明细 + 原生可编辑图表（工作表）+ 说明（取数 SQL）。
func NewExportExcelWithChartFunc(conn *sqlx.DB, auditCtx *AuditCtx) func(ctx context.Context, input *ExportExcelWithChartInput) (*ExportExcelWithChartOutput, error) {
	return func(ctx context.Context, input *ExportExcelWithChartInput) (out *ExportExcelWithChartOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(auditCtx, "export_excel_with_chart", start, err) }()

		qr, err := QueryForExport(conn, input.SQL)
		if err != nil {
			return nil, err
		}

		charts := NormalizeCharts(input)
		dataCharts := DataChartsFromExcelCharts(qr, charts)
		title := strings.TrimSpace(input.ChartTitle)
		if title == "" {
			title = "数据分析报告"
		}

		buf, err := report.RenderXLSX(report.XLSXOptions{
			Title:      title,
			Theme:      report.ResolveTheme("", "business"),
			Summary:    BuildExcelSummary(title, qr),
			Data:       &report.SheetData{Name: "数据明细", Headers: qr.Columns, Rows: queryRowsAny(qr)},
			DataCharts: dataCharts,
			SQL:        input.SQL,
		})
		if err != nil {
			return nil, fmt.Errorf("生成 Excel 失败：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "chart")
		url, err := writeExportFile(fileName, "xlsx", buf, auditCtx)
		if err != nil {
			return nil, err
		}
		log.Printf("[Tool:export_excel_chart] 成功 - rows=%d, charts=%d, url=%s\n", len(qr.Data), len(dataCharts), url)

		msg := fmt.Sprintf("已生成含 %d 个图表的 Excel（%d 条数据），[点击下载](%s)", len(dataCharts), len(qr.Data), url)
		if len(dataCharts) == 0 && len(charts) > 0 {
			msg = fmt.Sprintf("已生成 Excel（%d 条数据）。注意：图表字段名未匹配查询结果列，未生成图表；可用列为 %v，[点击下载](%s)",
				len(qr.Data), qr.Columns, url)
		}

		return &ExportExcelWithChartOutput{
			Message:     msg,
			RowCount:    len(qr.Data),
			ChartCount:  len(dataCharts),
			DownloadURL: url,
			FileType:    "excel_with_chart",
		}, nil
	}
}

// NewExportAnalysisDocxFunc 创建 Word 分析报告导出工具（Go 原生渲染：
// 封面/目录/页眉页脚/指标卡/原生可编辑图表/分页表格）。
//
// content 模式支持扩展 Markdown（```kpi/```chart/```table/提示框/公式/分页），
// 内嵌 SQL 由注入的 DataSource 经权限校验后取数；仅给 sql 时自动生成图文报告。
func NewExportAnalysisDocxFunc(conn *sqlx.DB, auditCtx *AuditCtx, ds DataSource) func(ctx context.Context, input *ExportReportInput) (*ExportReportOutput, error) {
	return func(ctx context.Context, input *ExportReportInput) (out *ExportReportOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(auditCtx, "export_analysis_docx", start, err) }()

		doc, err := buildExportDocument(ctx, conn, ds, input.Title, input.Subtitle, input.Content, input.SQL)
		if err != nil {
			return nil, err
		}
		data, err := report.RenderDOCX(doc, report.ResolveTheme(input.Theme, "business"))
		if err != nil {
			return nil, fmt.Errorf("生成 Word 文档失败：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "report")
		url, err := writeExportFile(fileName, "docx", data, auditCtx)
		if err != nil {
			return nil, err
		}
		log.Printf("[Tool:export_docx] 成功 - blocks=%d, url=%s\n", len(doc.Blocks), url)

		return &ExportReportOutput{
			Message:     fmt.Sprintf("已生成 Word 分析报告（%s），[点击下载](%s)", doc.Title, url),
			DownloadURL: url,
			FileType:    "docx",
		}, nil
	}
}

// NewExportPPTFunc 创建 PPT 导出工具（Go 原生渲染：封面/目录/章节页/指标卡/
// 原生可编辑图表/分页表格/洞察面板）。
func NewExportPPTFunc(conn *sqlx.DB, auditCtx *AuditCtx, ds DataSource) func(ctx context.Context, input *ExportReportInput) (*ExportReportOutput, error) {
	return func(ctx context.Context, input *ExportReportInput) (out *ExportReportOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(auditCtx, "export_ppt", start, err) }()

		doc, err := buildExportDocument(ctx, conn, ds, input.Title, input.Subtitle, input.Content, input.SQL)
		if err != nil {
			return nil, err
		}
		data, err := report.RenderPPTX(doc, report.ResolveTheme(input.Theme, "business"))
		if err != nil {
			return nil, fmt.Errorf("生成 PPT 失败：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "slides")
		url, err := writeExportFile(fileName, "pptx", data, auditCtx)
		if err != nil {
			return nil, err
		}
		slideCount := countPPTXSlides(data)
		log.Printf("[Tool:export_ppt] 成功 - slides=%d, url=%s\n", slideCount, url)

		return &ExportReportOutput{
			Message:     fmt.Sprintf("已生成 PPT（%d 页），[点击下载](%s)", slideCount, url),
			SlideCount:  slideCount,
			DownloadURL: url,
			FileType:    "ppt",
		}, nil
	}
}

// NewExportHTMLFunc 创建 HTML 报告导出工具。
// 服务端渲染、全文转义、图表内联 SVG，断网可用；含 mermaid 图示/公式时才引入 CDN 渐进增强。
func NewExportHTMLFunc(conn *sqlx.DB, auditCtx *AuditCtx, ds DataSource) func(ctx context.Context, input *ExportReportInput) (*ExportReportOutput, error) {
	return func(ctx context.Context, input *ExportReportInput) (out *ExportReportOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(auditCtx, "export_html", start, err) }()

		doc, err := buildExportDocument(ctx, conn, ds, input.Title, input.Subtitle, input.Content, input.SQL)
		if err != nil {
			return nil, err
		}
		data := report.RenderHTML(doc, report.ResolveTheme(input.Theme, "business"))

		fileName := SanitizeFileName(input.FileName, "report")
		url, err := writeExportFile(fileName, "html", data, auditCtx)
		if err != nil {
			return nil, err
		}
		log.Printf("[Tool:export_html] 成功 - url=%s\n", url)

		return &ExportReportOutput{
			Message:     fmt.Sprintf("已生成 HTML 报告（%s），[点击下载](%s)", doc.Title, url),
			DownloadURL: url,
			FileType:    "html",
		}, nil
	}
}

// countPPTXSlides 统计 PPTX 中的幻灯片数量（ppt/slides/slideN.xml）。
func countPPTXSlides(data []byte) int {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0
	}
	n := 0
	for _, f := range zr.File {
		name := f.Name
		rest, ok := strings.CutPrefix(name, "ppt/slides/")
		if !ok || !strings.HasPrefix(rest, "slide") || !strings.HasSuffix(rest, ".xml") {
			continue
		}
		if !strings.Contains(strings.TrimSuffix(rest, ".xml"), "/") {
			n++
		}
	}
	return n
}
