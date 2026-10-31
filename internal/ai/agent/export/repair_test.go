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
)

// TestArtifactRegistry_SessionIsolation 元数据只能被当前用户、当前会话读取；
// 其他用户/会话必须 fail-closed，不允许猜测或跨会话修复。
func TestArtifactRegistry_SessionIsolation(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}

	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "隔离测试", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}

	got, err := NewGetExportArtifactFunc(audit)(ctx, &ExportArtifactInput{File: out.DownloadURL})
	if err != nil || !got.Found {
		t.Fatalf("当前会话应能读取产物源信息：err=%v out=%+v", err, got)
	}
	if got.Tool != "export_html" || got.Version != 1 || got.Content == "" {
		t.Errorf("产物元数据不完整：%+v", got)
	}

	otherSession, err := NewGetExportArtifactFunc(&AuditCtx{UserID: "u1", SessionID: "s2"})(ctx, &ExportArtifactInput{File: out.DownloadURL})
	if err != nil {
		t.Fatalf("跨会话查询不应返回系统错误：%v", err)
	}
	if otherSession.Found {
		t.Error("其他会话不应读取本次会话产物的源信息")
	}

	otherUser, err := NewGetExportArtifactFunc(&AuditCtx{UserID: "u2", SessionID: "s1"})(ctx, &ExportArtifactInput{File: out.DownloadURL})
	if err != nil {
		t.Fatalf("跨用户查询不应返回系统错误：%v", err)
	}
	if otherUser.Found {
		t.Error("其他用户不应读取该产物的源信息")
	}
}

// TestRepairExport_HTMLMinimalFix 修复应生成新版本文件、保留原文件，并记录父版本关系。
func TestRepairExport_HTMLMinimalFix(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}
	content := richContent()

	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: content,
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}

	fixedContent := strings.Replace(content, "870万", "880万", 1)
	repaired, err := NewRepairExportFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportRepairInput{
		File: out.DownloadURL, Issue: "指标卡数值错误", Changes: "修正总销售额为 880万",
		Content: fixedContent,
	})
	if err != nil {
		t.Fatalf("HTML 修复失败：%v", err)
	}
	if repaired.Version != 2 {
		t.Errorf("修复版本号应为 2：%d", repaired.Version)
	}
	if repaired.DownloadURL == out.DownloadURL {
		t.Error("修复必须生成新文件，不能静默覆盖原文件")
	}
	assertExportFile(t, out.DownloadURL, 3000)
	assertExportFile(t, repaired.DownloadURL, 3000)

	newFile := strings.TrimPrefix(repaired.DownloadURL, "/exports/")
	data, err := os.ReadFile(filepath.Join("exports", newFile))
	if err != nil {
		t.Fatalf("读取修复文件失败：%v", err)
	}
	if !strings.Contains(string(data), "880万") {
		t.Error("修复文件应包含修改后的内容")
	}
	meta, err := loadArtifactMetaByFileName(newFile)
	if err != nil {
		t.Fatalf("读取修复元数据失败：%v", err)
	}
	if meta.ParentURL != out.DownloadURL || meta.Version != 2 {
		t.Errorf("修复元数据父版本关系异常：%+v", meta)
	}
}

// TestWriteExportFile_ResolvesRunSession Agent 实例缓存复用时，
// 元数据必须记录当前运行会话，而不是构建期会话。
func TestWriteExportFile_ResolvesRunSession(t *testing.T) {
	t.Chdir(t.TempDir())
	audit := &AuditCtx{
		UserID: "u1", SessionID: "stale",
		SessionIDFn: func(context.Context) string { return "current" },
	}
	url, err := writeExportFile(context.Background(), "session-test", "html", []byte("<html><body></body></html>"), audit, &ArtifactMeta{Tool: "test"})
	if err != nil {
		t.Fatalf("写入导出文件失败：%v", err)
	}
	meta, err := loadArtifactMetaByFileName(strings.TrimPrefix(url, "/exports/"))
	if err != nil {
		t.Fatalf("读取元数据失败：%v", err)
	}
	if meta.SessionID != "current" {
		t.Errorf("元数据应记录当前运行会话，实际为 %q", meta.SessionID)
	}
}

// TestRepairExport_RejectsUnknownFile 无法定位文件时必须失败，不得猜测。
func TestRepairExport_RejectsUnknownFile(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}
	if _, err := NewRepairExportFunc(conn, audit, nil, newTestRunner(conn))(context.Background(), &ExportRepairInput{
		File: "不存在的报告.html", Content: "## 摘要\n\n内容",
	}); err == nil {
		t.Fatal("未知文件应返回错误，不能猜测目标")
	}
}

// TestValidateExportArtifact_RealOutputs 验证器必须接受现有渲染器的真实产物，
// 避免修复链路因校验过严而误杀正常文件。
func TestValidateExportArtifact_RealOutputs(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}

	docxOut, err := NewExportAnalysisDocxFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("Word 导出失败：%v", err)
	}
	assertValidExportFile(t, docxOut.DownloadURL, docxOut.ChartCount)

	pptOut, err := NewExportPPTFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("PPT 导出失败：%v", err)
	}
	assertValidExportFile(t, pptOut.DownloadURL, pptOut.ChartCount)

	xlsxOut, err := NewExportExcelWithChartFunc(conn, audit, newTestRunner(conn))(ctx, &ExportExcelWithChartInput{
		SQL:      "SELECT 月份, 金额, 成本 FROM sales ORDER BY 月份",
		FileName: "销售图表",
		Charts: []ExcelChart{{
			ChartType: "line", XAxisField: "月份",
			Series: []ExcelChartSeriesDef{{YAxisField: "金额"}},
		}},
		Summary: "## 结论\n\n保持增长。",
	})
	if err != nil {
		t.Fatalf("Excel 导出失败：%v", err)
	}
	assertValidExportFile(t, xlsxOut.DownloadURL, xlsxOut.ChartCount)
}

func assertValidExportFile(t *testing.T, url string, expectedCharts int) {
	t.Helper()
	fileName := strings.TrimPrefix(url, "/exports/")
	data, err := os.ReadFile(filepath.Join("exports", fileName))
	if err != nil {
		t.Fatalf("读取导出文件失败：%v", err)
	}
	ext := strings.TrimPrefix(filepath.Ext(fileName), ".")
	if err := validateExportArtifact(ext, data, expectedCharts); err != nil {
		t.Errorf("现有渲染器产物未通过校验（%s）：%v", fileName, err)
	}
}

// TestRepairExport_ExcelSummary Excel 修复应沿用原 SQL/图表定义，
// 仅替换 summary，并生成新版本文件。
func TestRepairExport_ExcelSummary(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}

	out, err := NewExportExcelWithChartFunc(conn, audit, newTestRunner(conn))(ctx, &ExportExcelWithChartInput{
		SQL:      "SELECT 月份, 金额, 成本 FROM sales ORDER BY 月份",
		FileName: "销售图表",
		Charts: []ExcelChart{{
			ChartType: "line", XAxisField: "月份",
			Series: []ExcelChartSeriesDef{{YAxisField: "金额"}},
		}},
		Summary: "## 结论\n\n旧结论",
	})
	if err != nil {
		t.Fatalf("Excel 导出失败：%v", err)
	}

	repaired, err := NewRepairExportFunc(conn, audit, nil, newTestRunner(conn))(ctx, &ExportRepairInput{
		File: out.DownloadURL, Issue: "结论文字错误", Changes: "更新结论",
		Summary: "## 结论\n\n修正后的结论",
	})
	if err != nil {
		t.Fatalf("Excel 修复失败：%v", err)
	}
	if repaired.Version != 2 || repaired.ChartCount == 0 || repaired.RowCount != 3 {
		t.Errorf("Excel 修复回执异常：%+v", repaired)
	}
	if !xlsxContains(t, repaired.DownloadURL, "修正后的结论") {
		t.Error("Excel 修复文件应包含新的 summary 文本")
	}
	assertExportFile(t, out.DownloadURL, 3000)
}

func xlsxContains(t *testing.T, url, want string) bool {
	t.Helper()
	fileName := strings.TrimPrefix(url, "/exports/")
	data, err := os.ReadFile(filepath.Join("exports", fileName))
	if err != nil {
		t.Fatalf("读取 Excel 失败：%v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("解析 Excel 失败：%v", err)
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, _ := io.ReadAll(rc)
		_ = rc.Close()
		if strings.Contains(string(content), want) {
			return true
		}
	}
	return false
}

// TestCleanupOrphanArtifacts 原导出文件被清理后，元数据应一并回收。
func TestCleanupOrphanArtifacts(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}
	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(context.Background(), &ExportReportInput{
		Title: "清理测试", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	fileName := strings.TrimPrefix(out.DownloadURL, "/exports/")
	if _, err := os.Stat(artifactFilePath(fileName)); err != nil {
		t.Fatalf("元数据应存在：%v", err)
	}
	if err := os.Remove(filepath.Join("exports", fileName)); err != nil {
		t.Fatalf("删除导出文件失败：%v", err)
	}
	CleanupOrphanArtifacts()
	if _, err := os.Stat(artifactFilePath(fileName)); !os.IsNotExist(err) {
		t.Error("导出文件已删除，元数据应被清理")
	}
}

// TestRepairExport_Patches 长文档应支持局部替换，不需要模型回传整份正文。
func TestRepairExport_Patches(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}

	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	repaired, err := NewRepairExportFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportRepairInput{
		File: out.DownloadURL, Issue: "指标卡数值错误", Changes: "局部修正指标卡",
		Patches: []ExportPatch{{Find: "870万", Replace: "880万"}},
	})
	if err != nil {
		t.Fatalf("patches 修复失败：%v", err)
	}
	if repaired.Version != 2 {
		t.Errorf("修复版本应为 2：%d", repaired.Version)
	}
	if !fileContains(t, repaired.DownloadURL, "880万") {
		t.Error("修复文件应包含 patch 后的内容")
	}
	if fileContains(t, repaired.DownloadURL, "870万") {
		t.Error("修复文件不应保留被替换的旧值")
	}
}

// TestRepairExport_NoopRejected 参数与原导出完全一致时必须报错，不能产出假修复版本。
func TestRepairExport_NoopRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}
	content := richContent()
	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: content,
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	if _, err := NewRepairExportFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportRepairInput{
		File: out.DownloadURL, Issue: "无实际修改", Content: content,
	}); err == nil {
		t.Fatal("参数与原导出一致时应报错，不能生成假修复版本")
	}
}

// TestRepairExport_ExplicitSQLOverridesContent 只传 sql 时应切换到 SQL 模式，
// 不能被旧 content 回填后忽略。
func TestRepairExport_ExplicitSQLOverridesContent(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}
	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "销售分析", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	repaired, err := NewRepairExportFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportRepairInput{
		File: out.DownloadURL, SQL: "SELECT 月份, 金额 FROM sales ORDER BY 月份",
	})
	if err != nil {
		t.Fatalf("SQL 模式修复失败：%v", err)
	}
	if fileContains(t, repaired.DownloadURL, "结论与建议") {
		t.Error("显式传 sql 时应按 SQL 模式重建，不应沿用旧 content")
	}
	if !fileContains(t, repaired.DownloadURL, "数据概览") {
		t.Error("SQL 模式重建应包含数据概览章节")
	}
}

func fileContains(t *testing.T, url, want string) bool {
	t.Helper()
	fileName := strings.TrimPrefix(url, "/exports/")
	data, err := os.ReadFile(filepath.Join("exports", fileName))
	if err != nil {
		t.Fatalf("读取导出文件失败：%v", err)
	}
	return strings.Contains(string(data), want)
}

// TestGetExportArtifact_IncludeFile 文本产物可附带本体预览，用于诊断渲染问题。
func TestGetExportArtifact_IncludeFile(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}
	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "预览测试", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	got, err := NewGetExportArtifactFunc(audit)(ctx, &ExportArtifactInput{File: out.DownloadURL, IncludeFile: true})
	if err != nil || !got.Found {
		t.Fatalf("读取产物失败：err=%v out=%+v", err, got)
	}
	if !strings.Contains(got.FilePreview, "<html") {
		t.Error("HTML 产物应返回文本预览")
	}
	if got.FilePreviewTruncated {
		t.Error("小型预览不应被截断")
	}
}

// TestArtifactVersionNameKeepsSuffix 长文件名截断后仍必须保留 _vN 后缀。
func TestArtifactVersionNameKeepsSuffix(t *testing.T) {
	meta := &ArtifactMeta{FileName: strings.Repeat("很长的报告名称", 20) + ".html", Version: 1}
	name := artifactVersionNameFor(meta, 2)
	if !strings.HasSuffix(name, "_v2") {
		t.Fatalf("版本后缀被截断：%s", name)
	}
	if len([]rune(name)) > 53 {
		t.Errorf("版本名过长（%d rune），可能再次被 SanitizeFileName 截断：%s", len([]rune(name)), name)
	}
}

// TestRepairExport_VersionIncrements 同一原文件的多次独立修复应递增版本，
// 不能重复 v2 或累积 _v2_v3。
func TestRepairExport_VersionIncrements(t *testing.T) {
	t.Chdir(t.TempDir())
	conn := newTestConn(t)
	ctx := context.Background()
	audit := &AuditCtx{UserID: "u1", SessionID: "s1"}
	out, err := NewExportHTMLFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportReportInput{
		Title: "版本测试", Content: richContent(),
	})
	if err != nil {
		t.Fatalf("HTML 导出失败：%v", err)
	}
	repair := func(find, replace string) *ExportRepairOutput {
		t.Helper()
		res, err := NewRepairExportFunc(conn, audit, newFakeDataSource(), newTestRunner(conn))(ctx, &ExportRepairInput{
			File: out.DownloadURL, Patches: []ExportPatch{{Find: find, Replace: replace}},
		})
		if err != nil {
			t.Fatalf("修复失败：%v", err)
		}
		return res
	}
	v2 := repair("870万", "880万")
	v3 := repair("870万", "890万")
	if v2.Version != 2 || v3.Version != 3 {
		t.Errorf("版本号应递增：v2=%d v3=%d", v2.Version, v3.Version)
	}
	if v2.DownloadURL == v3.DownloadURL {
		t.Error("多次修复必须产生不同文件")
	}
	if !strings.Contains(v3.DownloadURL, "_v3_") {
		t.Errorf("v3 文件名应包含 _v3：%s", v3.DownloadURL)
	}
}

// TestValidateExportArtifact 基本校验应拦截损坏的 HTML/OOXML/PDF。
func TestValidateExportArtifact(t *testing.T) {
	goodHTML := []byte("<!DOCTYPE html><html><body><div></div></body></html>")
	if err := validateExportArtifact("html", goodHTML, 0); err != nil {
		t.Fatalf("合法 HTML 不应失败：%v", err)
	}
	badHTML := []byte("<html><body><script>alert(1)</body></html>")
	if err := validateExportArtifact("html", badHTML, 0); err == nil {
		t.Fatal("脚本标签未配对应被拦截")
	}
	if err := validateExportArtifact("html", goodHTML, 1); err == nil {
		t.Fatal("有图表但缺少 SVG 时应被拦截")
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types></Types>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body></w:body></w:document>`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateExportArtifact("docx", buf.Bytes(), 0); err != nil {
		t.Fatalf("结构完整的 DOCX 不应失败：%v", err)
	}
	if err := validateExportArtifact("pdf", []byte("%PDF-1.7\nbody"), 0); err == nil {
		t.Fatal("PDF 缺少 EOF 尾标记应被拦截")
	}
}
