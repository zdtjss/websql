package export

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
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
	// SessionIDFn 可选：按运行上下文解析当前会话。
	// Agent 实例按连接/用户/权限缓存，构建期 SessionID 可能来自首个会话；
	// 导出元数据必须落在当前会话下，因此每次工具调用需重新解析。
	SessionIDFn func(ctx context.Context) string
	// Guard 可选：按运行上下文校验数据出域安全（脱敏/修订号）。
	// 对话式修复会读取已导出源内容，必须与数据库数据访问使用同一安全门槛。
	Guard func(ctx context.Context) error
	// Mask 可选：按当前运行策略对导出源文本应用脱敏（元数据回填时必须重新应用）。
	Mask func(ctx context.Context, text string) string
}

// forRun 返回当前运行下的审计上下文副本（仅解析会话 ID，不修改共享实例）。
func (a *AuditCtx) forRun(ctx context.Context) *AuditCtx {
	if a == nil || a.SessionIDFn == nil {
		return a
	}
	cp := *a
	// 以运行上下文为准：解析不到会话时置空（宁可本次产物不可修复，也不写入错误的会话归属）。
	cp.SessionID = strings.TrimSpace(a.SessionIDFn(ctx))
	cp.SessionIDFn = nil
	return &cp
}

// sessionOfCtx 返回当前运行下的会话 ID。
func sessionOfCtx(ctx context.Context, a *AuditCtx) string {
	a = a.forRun(ctx)
	if a == nil {
		return ""
	}
	return a.SessionID
}

// guardExportSource 校验本次运行是否允许读取/重渲染导出源内容。
func guardExportSource(ctx context.Context, a *AuditCtx) error {
	if a == nil || a.Guard == nil {
		return nil
	}
	return a.Guard(ctx)
}

// maskArtifactText 按当前运行策略脱敏源文本；未注入脱敏钩子时原样返回。
func maskArtifactText(ctx context.Context, a *AuditCtx, text string) string {
	if a == nil || a.Mask == nil || text == "" {
		return text
	}
	return a.Mask(ctx, text)
}

// recordExportAudit 记录一次导出工具调用（成功/失败各一条），
// SQLType 固定为 EXPORT、RiskLevel 固定为 low。
// auditCtx 为 nil 时仍记录（不含用户上下文），保证导出行为始终可追溯。
func recordExportAudit(ctx context.Context, auditCtx *AuditCtx, toolName string, start time.Time, err error) {
	auditCtx = auditCtx.forRun(ctx)
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
// 可选传入 ArtifactMeta：记录原导出参数/源内容，供后续对话式修复做增量重渲染。
func writeExportFile(ctx context.Context, fileName, ext string, data []byte, auditCtx *AuditCtx, metas ...*ArtifactMeta) (string, error) {
	auditCtx = auditCtx.forRun(ctx)
	EnsureExportsDir()
	filePath := Path(fileName + "." + ext)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("保存导出文件失败：%w", err)
	}
	url := "/exports/" + fileName + "." + ext
	RecordExportOwnerByURL(url, ownerOf(auditCtx))

	if len(metas) > 0 && metas[0] != nil {
		meta := metas[0]
		meta.URL = url
		meta.FileName = fileName + "." + ext
		if meta.BaseName == "" {
			meta.BaseName = artifactBaseNameOf(meta.FileName)
		}
		meta.FileType = ext
		if meta.Tool == "" {
			meta.Tool = "unknown"
		}
		if meta.SourceKind == "" {
			meta.SourceKind = artifactSourceKindOf(meta)
		}
		if meta.CreatedAt.IsZero() {
			meta.CreatedAt = time.Now()
		}
		if meta.Version <= 0 {
			meta.Version = 1
		}
		if auditCtx != nil {
			meta.UserID = auditCtx.UserID
			meta.SessionID = auditCtx.SessionID
			meta.ConnID = auditCtx.ConnID
		}
		if err := saveArtifactMeta(meta); err != nil {
			// 元数据只影响“后续修复”，不影响文件本身可用性。
			log.Printf("[ExportArtifact] 写入元数据失败 - file=%s, err=%v\n", meta.FileName, err)
		}
	}
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

// documentStats 统计报告结构要素：一级标题数（无一级标题时统计全部标题）、
// 有数据的图表数、有数据的表格数、指标卡数量。用于导出回执说明关键信息。
func documentStats(doc *report.Document) (sections, charts, tables, kpis int) {
	if doc == nil {
		return
	}
	headings := 0
	for _, b := range doc.Blocks {
		switch b.Kind {
		case report.KindHeading:
			headings++
			if b.Level == 1 {
				sections++
			}
		case report.KindChart:
			if b.Chart != nil && b.Chart.HasData() {
				charts++
			}
		case report.KindTable:
			if b.Table != nil && len(b.Table.Rows) > 0 {
				tables++
			}
		case report.KindKPI:
			kpis += len(b.KPIs)
		}
	}
	if sections == 0 {
		sections = headings
	}
	return
}

// reportQualityHint 对不符合「可直接对外汇报」结构的产出给出修复提示（不阻断导出）。
func reportQualityHint(sections, charts, tables, kpis int) string {
	var missing []string
	if sections == 0 {
		missing = append(missing, "章节标题")
	}
	if kpis == 0 {
		missing = append(missing, "关键指标卡（kpi）")
	}
	if charts == 0 {
		missing = append(missing, "原生图表（chart）")
	}
	if tables == 0 {
		missing = append(missing, "数据表（table）")
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("。提示：报告缺少 %s，未达到可直接汇报的标准，建议补充后重新导出", strings.Join(missing, "、"))
}

// firstNonEmptyStr 返回第一个非空白字符串。
func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// renderReportExport 内置报告渲染核心（导出工具与对话式修复共用）。
func renderReportExport(ctx context.Context, conn *sqlx.DB, ds DataSource, runner QueryRunner, tool string, in *ExportReportInput) ([]byte, *report.Document, error) {
	doc, err := buildExportDocument(ctx, conn, ds, runner, in.Title, in.Subtitle, in.Content, in.SQL)
	if err != nil {
		return nil, nil, err
	}
	theme := report.ResolveTheme(in.Theme, "business")
	switch tool {
	case "export_analysis_docx":
		data, err := report.RenderDOCX(doc, theme)
		if err != nil {
			return nil, nil, fmt.Errorf("生成 Word 文档失败：%w", err)
		}
		return data, doc, nil
	case "export_ppt":
		data, err := report.RenderPPTX(doc, theme)
		if err != nil {
			return nil, nil, fmt.Errorf("生成 PPT 失败：%w", err)
		}
		return data, doc, nil
	case "export_html":
		return report.RenderHTML(doc, theme), doc, nil
	default:
		return nil, nil, fmt.Errorf("不支持的报告导出工具：%s", tool)
	}
}

// renderExcelData 纯数据表渲染（export_excel 与修复共用）。
func renderExcelData(title, sql string, qr *QueryResult) ([]byte, error) {
	return report.RenderXLSX(report.XLSXOptions{
		Title: title,
		Theme: report.ResolveTheme("", "business"),
		Data:  &report.SheetData{Name: "数据明细", Headers: qr.Columns, Rows: queryRowsAny(qr)},
		SQL:   sql,
	})
}

// renderExcelAnalysis 图文 Excel 渲染（export_excel_with_chart 与修复共用）。
func renderExcelAnalysis(title, sql string, qr *QueryResult, dataCharts []report.DataChart, summary *report.Document) ([]byte, error) {
	return report.RenderXLSX(report.XLSXOptions{
		Title:      title,
		Theme:      report.ResolveTheme("", "business"),
		Summary:    summary,
		Data:       &report.SheetData{Name: "数据明细", Headers: qr.Columns, Rows: queryRowsAny(qr)},
		DataCharts: dataCharts,
		SQL:        sql,
	})
}

// buildExcelSummary 合并自动统计指标与模型提供的结论/建议（扩展 Markdown）。
func buildExcelSummary(title string, qr *QueryResult, extraMarkdown string) *report.Document {
	summary := BuildExcelSummary(title, qr)
	if extra := strings.TrimSpace(extraMarkdown); extra != "" {
		summary.Blocks = append(summary.Blocks, report.ParseMarkdown(extra)...)
	}
	return summary
}

// buildExportDocument 构建报告文档：
//   - content 模式：解析扩展 Markdown（```kpi/```chart/```table 等），并回填内嵌 SQL 数据；
//   - sql 模式：由查询结果自动生成「指标卡 + 图表 + 明细表」图文报告。
func buildExportDocument(ctx context.Context, conn *sqlx.DB, ds DataSource, runner QueryRunner, title, subtitle, content, sql string) (*report.Document, error) {
	content = strings.TrimSpace(content)
	sql = strings.TrimSpace(sql)
	switch {
	case content != "":
		doc := report.BuildDocument(title, subtitle, content)
		BindDocument(ctx, doc, ds)
		return doc, nil
	case sql != "":
		qr, err := runExportQuery(ctx, conn, runner, sql)
		if err != nil {
			return nil, err
		}
		return BuildDataDocument(title, subtitle, qr), nil
	default:
		return nil, fmt.Errorf("必须提供 content（扩展 Markdown 报告正文）或 sql 参数")
	}
}

// NewExportExcelFunc 创建 Excel 数据导出工具（Go 原生渲染：表头样式/冻结/筛选/数字格式）。
func NewExportExcelFunc(conn *sqlx.DB, auditCtx *AuditCtx, runners ...QueryRunner) func(ctx context.Context, input *ExportExcelInput) (*ExportExcelOutput, error) {
	runner := firstQueryRunner(runners)
	return func(ctx context.Context, input *ExportExcelInput) (out *ExportExcelOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(ctx, auditCtx, "export_excel", start, err) }()

		qr, err := runExportQuery(ctx, conn, runner, input.SQL)
		if err != nil {
			return nil, err
		}
		title := strings.TrimSpace(input.Title)
		if title == "" {
			title = "数据导出"
		}

		buf, err := renderExcelData(title, maskArtifactText(ctx, auditCtx, input.SQL), qr)
		if err != nil {
			return nil, fmt.Errorf("生成 Excel 失败：%w", err)
		}
		if err := validateExportArtifact("xlsx", buf, 0); err != nil {
			return nil, fmt.Errorf("导出产物校验未通过：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "export")
		url, err := writeExportFile(ctx, fileName, "xlsx", buf, auditCtx, &ArtifactMeta{
			Tool: "export_excel", Title: title, SQL: input.SQL,
		})
		if err != nil {
			return nil, err
		}
		log.Printf("[Tool:export_excel] 成功 - rows=%d, url=%s\n", len(qr.Data), url)

		return &ExportExcelOutput{
			Message:     fmt.Sprintf("已生成 Excel 数据表（.xlsx，%d 行 × %d 列），[点击下载](%s)", len(qr.Data), len(qr.Columns), url),
			RowCount:    len(qr.Data),
			DownloadURL: url,
			FileType:    "excel",
			Version:     1,
		}, nil
	}
}

// NewExportExcelWithChartFunc 创建带图表的 Excel 导出工具。
// 产出：分析摘要（指标卡/统计）+ 数据明细 + 原生可编辑图表（工作表）+ 说明（取数 SQL）。
func NewExportExcelWithChartFunc(conn *sqlx.DB, auditCtx *AuditCtx, runners ...QueryRunner) func(ctx context.Context, input *ExportExcelWithChartInput) (*ExportExcelWithChartOutput, error) {
	runner := firstQueryRunner(runners)
	return func(ctx context.Context, input *ExportExcelWithChartInput) (out *ExportExcelWithChartOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(ctx, auditCtx, "export_excel_with_chart", start, err) }()

		qr, err := runExportQuery(ctx, conn, runner, input.SQL)
		if err != nil {
			return nil, err
		}

		charts := NormalizeCharts(input)
		dataCharts := DataChartsFromExcelCharts(qr, charts)
		title := strings.TrimSpace(input.ChartTitle)
		if title == "" {
			title = "数据分析报告"
		}

		// 分析摘要工作表：自动统计指标 + 模型提供的结论与建议（扩展 Markdown），
		// 使 Excel 也能具备「可对外汇报」的结构（指标卡/图表/结论）。
		summary := buildExcelSummary(title, qr, input.Summary)

		buf, err := renderExcelAnalysis(title, maskArtifactText(ctx, auditCtx, input.SQL), qr, dataCharts, summary)
		if err != nil {
			return nil, fmt.Errorf("生成 Excel 失败：%w", err)
		}
		if err := validateExportArtifact("xlsx", buf, len(dataCharts)); err != nil {
			return nil, fmt.Errorf("导出产物校验未通过：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "chart")
		url, err := writeExportFile(ctx, fileName, "xlsx", buf, auditCtx, &ArtifactMeta{
			Tool: "export_excel_with_chart", Title: title, SQL: input.SQL,
			Charts: charts, Summary: input.Summary,
		})
		if err != nil {
			return nil, err
		}
		log.Printf("[Tool:export_excel_chart] 成功 - rows=%d, charts=%d, url=%s\n", len(qr.Data), len(dataCharts), url)

		msg := fmt.Sprintf("已生成 Excel 分析表（.xlsx，%d 个图表、%d 行 × %d 列数据），[点击下载](%s)",
			len(dataCharts), len(qr.Data), len(qr.Columns), url)
		if len(dataCharts) == 0 && len(charts) > 0 {
			msg = fmt.Sprintf("已生成 Excel 分析表（.xlsx，%d 行 × %d 列数据）。注意：图表字段名未匹配查询结果列，未生成图表；可用列为 %v，[点击下载](%s)",
				len(qr.Data), len(qr.Columns), qr.Columns, url)
		}

		return &ExportExcelWithChartOutput{
			Message:     msg,
			RowCount:    len(qr.Data),
			ChartCount:  len(dataCharts),
			DownloadURL: url,
			FileType:    "excel_with_chart",
			Version:     1,
		}, nil
	}
}

// NewExportAnalysisDocxFunc 创建 Word 分析报告导出工具（Go 原生渲染：
// 封面/目录/页眉页脚/指标卡/原生可编辑图表/分页表格）。
//
// content 模式支持扩展 Markdown（```kpi/```chart/```table/提示框/公式/分页），
// 内嵌 SQL 由注入的 DataSource 经权限校验后取数；仅给 sql 时自动生成图文报告。
func NewExportAnalysisDocxFunc(conn *sqlx.DB, auditCtx *AuditCtx, ds DataSource, runners ...QueryRunner) func(ctx context.Context, input *ExportReportInput) (*ExportReportOutput, error) {
	runner := firstQueryRunner(runners)
	return func(ctx context.Context, input *ExportReportInput) (out *ExportReportOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(ctx, auditCtx, "export_analysis_docx", start, err) }()

		data, doc, err := renderReportExport(ctx, conn, ds, runner, "export_analysis_docx", input)
		if err != nil {
			return nil, err
		}
		sections, charts, tables, kpis := documentStats(doc)
		if err := validateExportArtifact("docx", data, charts); err != nil {
			return nil, fmt.Errorf("导出产物校验未通过：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "report")
		url, err := writeExportFile(ctx, fileName, "docx", data, auditCtx, &ArtifactMeta{
			Tool: "export_analysis_docx", Title: input.Title, Subtitle: input.Subtitle,
			Theme: input.Theme, Content: input.Content, SQL: input.SQL,
		})
		if err != nil {
			return nil, err
		}
		log.Printf("[Tool:export_docx] 成功 - blocks=%d, url=%s\n", len(doc.Blocks), url)

		return &ExportReportOutput{
			Message: fmt.Sprintf("已生成 Word 报告《%s》（.docx，%d 个章节、%d 个图表、%d 张表格），[点击下载](%s)%s",
				doc.Title, sections, charts, tables, url, reportQualityHint(sections, charts, tables, kpis)),
			SectionCount: sections,
			ChartCount:   charts,
			TableCount:   tables,
			DownloadURL:  url,
			FileType:     "docx",
			Version:      1,
		}, nil
	}
}

// NewExportPPTFunc 创建 PPT 导出工具（Go 原生渲染：封面/目录/章节页/指标卡/
// 原生可编辑图表/分页表格/洞察面板）。
func NewExportPPTFunc(conn *sqlx.DB, auditCtx *AuditCtx, ds DataSource, runners ...QueryRunner) func(ctx context.Context, input *ExportReportInput) (*ExportReportOutput, error) {
	runner := firstQueryRunner(runners)
	return func(ctx context.Context, input *ExportReportInput) (out *ExportReportOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(ctx, auditCtx, "export_ppt", start, err) }()

		data, doc, err := renderReportExport(ctx, conn, ds, runner, "export_ppt", input)
		if err != nil {
			return nil, err
		}
		sections, charts, tables, kpis := documentStats(doc)
		if err := validateExportArtifact("pptx", data, charts); err != nil {
			return nil, fmt.Errorf("导出产物校验未通过：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "slides")
		url, err := writeExportFile(ctx, fileName, "pptx", data, auditCtx, &ArtifactMeta{
			Tool: "export_ppt", Title: input.Title, Subtitle: input.Subtitle,
			Theme: input.Theme, Content: input.Content, SQL: input.SQL,
		})
		if err != nil {
			return nil, err
		}
		slideCount := countPPTXSlides(data)
		log.Printf("[Tool:export_ppt] 成功 - slides=%d, url=%s\n", slideCount, url)

		return &ExportReportOutput{
			Message: fmt.Sprintf("已生成 PPT 演示文稿（.pptx，%d 页、%d 个章节、%d 个图表），[点击下载](%s)%s",
				slideCount, sections, charts, url, reportQualityHint(sections, charts, tables, kpis)),
			SlideCount:   slideCount,
			SectionCount: sections,
			ChartCount:   charts,
			TableCount:   tables,
			DownloadURL:  url,
			FileType:     "ppt",
			Version:      1,
		}, nil
	}
}

// NewExportHTMLFunc 创建 HTML 报告导出工具。
// 服务端渲染、全文转义、图表内联 SVG，断网可用；含 mermaid 图示/公式时才引入 CDN 渐进增强。
func NewExportHTMLFunc(conn *sqlx.DB, auditCtx *AuditCtx, ds DataSource, runners ...QueryRunner) func(ctx context.Context, input *ExportReportInput) (*ExportReportOutput, error) {
	runner := firstQueryRunner(runners)
	return func(ctx context.Context, input *ExportReportInput) (out *ExportReportOutput, err error) {
		start := time.Now()
		defer func() { recordExportAudit(ctx, auditCtx, "export_html", start, err) }()

		data, doc, err := renderReportExport(ctx, conn, ds, runner, "export_html", input)
		if err != nil {
			return nil, err
		}
		sections, charts, tables, kpis := documentStats(doc)
		if err := validateExportArtifact("html", data, charts); err != nil {
			return nil, fmt.Errorf("导出产物校验未通过：%w", err)
		}

		fileName := SanitizeFileName(input.FileName, "report")
		url, err := writeExportFile(ctx, fileName, "html", data, auditCtx, &ArtifactMeta{
			Tool: "export_html", Title: input.Title, Subtitle: input.Subtitle,
			Theme: input.Theme, Content: input.Content, SQL: input.SQL,
		})
		if err != nil {
			return nil, err
		}
		log.Printf("[Tool:export_html] 成功 - url=%s\n", url)

		return &ExportReportOutput{
			Message: fmt.Sprintf("已生成 HTML 报告《%s》（.html，单文件离线可读，%d 个章节、%d 个图表、%d 张表格），[点击下载](%s)%s",
				doc.Title, sections, charts, tables, url, reportQualityHint(sections, charts, tables, kpis)),
			SectionCount: sections,
			ChartCount:   charts,
			TableCount:   tables,
			DownloadURL:  url,
			FileType:     "html",
			Version:      1,
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
