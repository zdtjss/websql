package export

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websql/internal/ai/agent/export/report"

	"github.com/jmoiron/sqlx"
)

// 端到端：五个导出工具在新 report 渲染引擎下的完整链路
// （content 模式含指标卡/图表/数据表；sql 模式自动生成图文报告）。

func newTestConn(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Skipf("sqlite 驱动不可用：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE sales (月份 TEXT, 金额 REAL, 成本 REAL)`); err != nil {
		t.Fatalf("建表失败：%v", err)
	}
	if _, err := db.Exec(`INSERT INTO sales (月份, 金额, 成本) VALUES ('1月', 120, 80), ('2月', 300, 150), ('3月', 450, 220)`); err != nil {
		t.Fatalf("插入失败：%v", err)
	}
	return db
}

// testDirectRunner 仅用于测试：导出包不再允许无 runner 直查，
// 测试用该 runner 保留原有端到端行为。
type testDirectRunner struct{ conn *sqlx.DB }

func (r testDirectRunner) QueryForExport(_ context.Context, sql string, _ int) (*QueryResult, error) {
	return QueryForExport(r.conn, sql)
}

func newTestRunner(conn *sqlx.DB) QueryRunner { return testDirectRunner{conn: conn} }

func newFakeDataSource() DataSource {
	return func(_ context.Context, _ *report.DataRef) (*report.Table, error) {
		return &report.Table{
			Headers: []string{"月份", "金额"},
			Rows:    [][]string{{"1月", "120"}, {"2月", "300"}},
		}, nil
	}
}

func richContent() string {
	return "# 销售分析\n\n" +
		"## 摘要\n\n上半年销售稳步增长。\n\n" +
		fence("kpi", `[{"label":"总销售额","value":"870万","change":"+12%","trend":"up"},{"label":"毛利率","value":"48%","change":"-2%","trend":"down"}]`) +
		fence("chart", `{"type":"bar","title":"月度销售额","sql":"SELECT 月份, 金额 FROM sales","x":"月份","y":["金额"]}`) +
		fence("table", `{"sql":"SELECT 1 AS x","title":"明细","limit":10}`) +
		"\n> [!NOTE] 数据来源\n> 销售主表\n\n" +
		"## 结论与建议\n\n1. 保持增长势头。\n"
}

func assertExportFile(t *testing.T, url string, minSize int) string {
	t.Helper()
	if !strings.HasPrefix(url, "/exports/") {
		t.Fatalf("下载 URL 异常：%s", url)
	}
	fileName := strings.TrimPrefix(url, "/exports/")
	path := filepath.Join("exports", fileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("导出文件不存在：%v", err)
	}
	if info.Size() < int64(minSize) {
		t.Fatalf("导出文件过小（%d 字节），可能渲染失败：%s", info.Size(), path)
	}
	return fileName
}

func TestExportTools_ContentMode(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	auditCtx := &AuditCtx{UserID: "u1"}
	in := &ExportReportInput{Title: "销售分析", Subtitle: "2026 上半年", Content: richContent()}

	// Word
	docxOut, err := NewExportAnalysisDocxFunc(conn, auditCtx, newFakeDataSource(), newTestRunner(conn))(ctx, in)
	if err != nil {
		t.Fatalf("Word 导出失败：%v", err)
	}
	docxFile := assertExportFile(t, docxOut.DownloadURL, 5000)
	if allowed, known := CheckExportAccess(docxFile, "u1"); !known || !allowed {
		t.Error("导出后应登记归属用户")
	}
	if allowed, _ := CheckExportAccess(docxFile, "u2"); allowed {
		t.Error("非归属用户不应有权下载")
	}

	// PPT
	pptOut, err := NewExportPPTFunc(conn, auditCtx, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: richContent(), Theme: "tech",
	})
	if err != nil {
		t.Fatalf("PPT 导出失败：%v", err)
	}
	assertExportFile(t, pptOut.DownloadURL, 8000)
	if pptOut.SlideCount < 3 {
		t.Errorf("PPT 页数异常：%d", pptOut.SlideCount)
	}

	// HTML：内联 SVG 图表、无 CDN 依赖（无 mermaid/公式时）
	htmlOut, err := NewExportHTMLFunc(conn, auditCtx, newFakeDataSource(), newTestRunner(conn))(ctx, in)
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	htmlFile := assertExportFile(t, htmlOut.DownloadURL, 3000)
	data, err := os.ReadFile(filepath.Join("exports", htmlFile))
	if err != nil {
		t.Fatalf("读取 HTML 失败：%v", err)
	}
	html := string(data)
	if !strings.Contains(html, "<svg") {
		t.Error("HTML 应内联 SVG 图表")
	}
	if strings.Contains(html, "cdn.jsdelivr.net") {
		t.Error("无 mermaid/公式时不应引入 CDN")
	}
	if !strings.Contains(html, "870万") {
		t.Error("HTML 应包含指标卡内容")
	}
}

func TestExportTools_SQLMode(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	auditCtx := &AuditCtx{UserID: "u1"}

	// export_excel：纯数据导出
	excelOut, err := NewExportExcelFunc(conn, auditCtx, newTestRunner(conn))(ctx, &ExportExcelInput{
		SQL: "SELECT 月份, 金额, 成本 FROM sales ORDER BY 月份", FileName: "销售数据",
	})
	if err != nil {
		t.Fatalf("Excel 导出失败：%v", err)
	}
	if excelOut.RowCount != 3 {
		t.Errorf("行数异常：%d", excelOut.RowCount)
	}
	assertExportFile(t, excelOut.DownloadURL, 3000)

	// export_excel_with_chart：原生图表 + 分析摘要
	chartOut, err := NewExportExcelWithChartFunc(conn, auditCtx, newTestRunner(conn))(ctx, &ExportExcelWithChartInput{
		SQL:      "SELECT 月份, 金额, 成本 FROM sales ORDER BY 月份",
		FileName: "销售图表",
		Charts: []ExcelChart{{
			ChartType: "line", XAxisField: "月份", ChartTitle: "月度金额与成本",
			Series: []ExcelChartSeriesDef{{YAxisField: "金额"}, {YAxisField: "成本"}},
		}},
	})
	if err != nil {
		t.Fatalf("Excel 图表导出失败：%v", err)
	}
	if chartOut.ChartCount != 1 {
		t.Errorf("图表数量异常：%d", chartOut.ChartCount)
	}
	assertExportFile(t, chartOut.DownloadURL, 5000)

	// sql 模式报告：自动生成 指标卡 + 图表 + 明细
	docxOut, err := NewExportAnalysisDocxFunc(conn, auditCtx, nil, newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "自动报告", SQL: "SELECT 月份, 金额, 成本 FROM sales ORDER BY 月份",
	})
	if err != nil {
		t.Fatalf("sql 模式 Word 导出失败：%v", err)
	}
	assertExportFile(t, docxOut.DownloadURL, 5000)
}

// TestExportTools_ReportReceipt 导出回执需说明文件类型与结构要素；
// 结构不达标（缺指标卡/图表/表格）时给出修复提示，但不阻断导出。
func TestExportTools_ReportReceipt(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	auditCtx := &AuditCtx{UserID: "u1"}
	ctx := context.Background()

	docxOut, err := NewExportAnalysisDocxFunc(conn, auditCtx, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("Word 导出失败：%v", err)
	}
	if docxOut.SectionCount == 0 || docxOut.ChartCount == 0 || docxOut.TableCount == 0 {
		t.Errorf("回执结构统计异常：sections=%d charts=%d tables=%d",
			docxOut.SectionCount, docxOut.ChartCount, docxOut.TableCount)
	}
	if !strings.Contains(docxOut.Message, ".docx") || !strings.Contains(docxOut.Message, "章节") {
		t.Errorf("回执应包含文件类型与结构信息：%s", docxOut.Message)
	}
	if strings.Contains(docxOut.Message, "未达到可直接汇报的标准") {
		t.Error("结构完整的报告不应出现质量提示")
	}

	htmlOut, err := NewExportHTMLFunc(conn, auditCtx, nil, newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "纯文字", Content: "## 摘要\n\n只有一段文字。",
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	if !strings.Contains(htmlOut.Message, "未达到可直接汇报的标准") {
		t.Errorf("缺少指标卡/图表/表格时应给出修复提示：%s", htmlOut.Message)
	}
}

// TestExportTools_ExcelSummaryMarkdown Excel 报告可通过 summary 传入结论与建议，
// 渲染到「分析摘要」工作表，使数据明细类交付也具备可汇报结构。
func TestExportTools_ExcelSummaryMarkdown(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	auditCtx := &AuditCtx{UserID: "u1"}

	out, err := NewExportExcelWithChartFunc(conn, auditCtx, newTestRunner(conn))(context.Background(), &ExportExcelWithChartInput{
		SQL:      "SELECT 月份, 金额, 成本 FROM sales ORDER BY 月份",
		FileName: "销售分析",
		Charts: []ExcelChart{{
			ChartType: "line", XAxisField: "月份", ChartTitle: "月度金额",
			Series: []ExcelChartSeriesDef{{YAxisField: "金额"}},
		}},
		Summary: "## 结论与建议\n\n1. 保持增长势头。",
	})
	if err != nil {
		t.Fatalf("Excel 导出失败：%v", err)
	}
	fileName := strings.TrimPrefix(out.DownloadURL, "/exports/")
	data, err := os.ReadFile(filepath.Join("exports", fileName))
	if err != nil {
		t.Fatalf("读取 Excel 失败：%v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("解析 Excel 失败：%v", err)
	}
	found := false
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, _ := io.ReadAll(rc)
		_ = rc.Close()
		if strings.Contains(string(content), "保持增长势头") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Excel 分析摘要工作表应包含 summary 文本")
	}
}

func TestExportTools_RequiresContentOrSQL(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	auditCtx := &AuditCtx{UserID: "u1"}

	if _, err := NewExportHTMLFunc(conn, auditCtx, nil, newTestRunner(conn))(context.Background(), &ExportReportInput{Title: "空报告"}); err == nil {
		t.Error("content 与 sql 均为空时应报错")
	}
}

// TestExportTools_BindFailureRendersWarning 绑定失败（无权/连接不可用）时
// 报告仍能生成，失败块渲染为警告提示框，不中断整篇导出。
func TestExportTools_BindFailureRendersWarning(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	auditCtx := &AuditCtx{UserID: "u1"}
	ds := func(context.Context, *report.DataRef) (*report.Table, error) {
		return nil, os.ErrPermission
	}

	out, err := NewExportHTMLFunc(conn, auditCtx, ds, newTestRunner(conn))(context.Background(), &ExportReportInput{
		Title:   "部分失败",
		Content: "## 摘要\n\n" + fence("chart", `{"type":"bar","sql":"SELECT 1 AS x","x":"x","y":["x"]}`),
	})
	if err != nil {
		t.Fatalf("绑定失败不应中断导出：%v", err)
	}
	fileName := strings.TrimPrefix(out.DownloadURL, "/exports/")
	data, err := os.ReadFile(filepath.Join("exports", fileName))
	if err != nil {
		t.Fatalf("读取 HTML 失败：%v", err)
	}
	if !strings.Contains(string(data), "图表未能生成") {
		t.Error("绑定失败应渲染为警告提示框")
	}
}
