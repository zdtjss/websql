package export

// 端到端冒烟测试：验证 Go 薄封装 → Python 渲染器的完整链路。
//
// 依赖本机 Python 环境（python-pptx/python-docx/matplotlib/docxtpl），
// 无 Python 时自动跳过（CI 环境安全）。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func initSkillsDirForTest(t *testing.T) {
	t.Helper()
	if os.Getenv("SKILLS_DIR") != "" {
		return
	}
	// export 包位于 <root>/internal/ai/agent/export，项目根向上 4 级
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Skipf("解析项目根目录失败: %v", err)
	}
	skillsDir := filepath.Join(root, "skills")
	if _, err := os.Stat(skillsDir); err != nil {
		t.Skipf("skills 目录不存在: %v", err)
	}
	t.Setenv("SKILLS_DIR", skillsDir)
}

func skipIfNoPython(t *testing.T) {
	t.Helper()
	if !IsPythonAvailable() {
		t.Skip("Python 不可用，跳过薄封装冒烟测试")
	}
}

func TestRunPythonRendererPPT(t *testing.T) {
	initSkillsDirForTest(t)
	skipIfNoPython(t)

	qr := &QueryResult{
		Columns: []string{"month", "revenue", "cost"},
		Data: []map[string]any{
			{"month": "2026-01", "revenue": 12000.0, "cost": 8000.0},
			{"month": "2026-02", "revenue": 15000.0, "cost": 9000.0},
			{"month": "2026-03", "revenue": 11000.0, "cost": 7500.0},
		},
	}
	outFile := filepath.Join(os.TempDir(), "websql_e2e_ppt_test.pptx")
	defer os.Remove(outFile)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req := buildPPTDataJSON(qr, "端到端测试", "/"+filepath.ToSlash(outFile))
	result, err := runPythonRenderer(ctx,
		filepath.Join("export-ppt", "scripts", "export_ppt.py"), req)
	if err != nil {
		t.Fatalf("PPT 渲染失败: %v", err)
	}
	if result.SlideCount <= 0 {
		t.Fatalf("slideCount 异常: %d", result.SlideCount)
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("PPT 输出文件不存在: %v", err)
	}
	t.Logf("PPT 冒烟通过: slides=%d, output=%s", result.SlideCount, outFile)
}

func TestRunPythonRendererDocx(t *testing.T) {
	initSkillsDirForTest(t)
	skipIfNoPython(t)

	qr := &QueryResult{
		Columns: []string{"month", "revenue", "cost"},
		Data: []map[string]any{
			{"month": "2026-01", "revenue": 12000.0, "cost": 8000.0},
			{"month": "2026-02", "revenue": 15000.0, "cost": 9000.0},
			{"month": "2026-03", "revenue": 11000.0, "cost": 7500.0},
		},
	}
	outFile := filepath.Join(os.TempDir(), "websql_e2e_docx_test.docx")
	defer os.Remove(outFile)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req := buildDocxDataJSON(qr, "端到端测试", "/"+filepath.ToSlash(outFile))
	result, err := runPythonRenderer(ctx,
		filepath.Join("export-word", "scripts", "word_generator.py"), req)
	if err != nil {
		t.Fatalf("Word 渲染失败: %v", err)
	}
	if result.OutputPath == "" {
		t.Fatalf("outputPath 为空")
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("Word 输出文件不存在: %v", err)
	}
	t.Logf("Word 冒烟通过: output=%s", outFile)
}

func TestRunPythonRendererContentMode(t *testing.T) {
	initSkillsDirForTest(t)
	skipIfNoPython(t)

	markdown := "# 摘要\n\n本季度经营状况良好。\n\n## 关键发现\n\n- 收入同比增长 15%\n- 成本控制良好\n\n## 数据明细\n\n| 区域 | 收入 |\n|---|---|\n| 华东 | 4200 |\n| 华北 | 3100 |\n"
	outFile := filepath.Join(os.TempDir(), "websql_e2e_content_test.pptx")
	defer os.Remove(outFile)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req := buildPPTContentJSON("内容模式测试", markdown, "/"+filepath.ToSlash(outFile))
	result, err := runPythonRenderer(ctx,
		filepath.Join("export-ppt", "scripts", "export_ppt.py"), req)
	if err != nil {
		t.Fatalf("content 模式渲染失败: %v", err)
	}
	if result.SlideCount <= 0 {
		t.Fatalf("slideCount 异常: %d", result.SlideCount)
	}
	t.Logf("content 模式冒烟通过: slides=%d", result.SlideCount)
}

// TestGoFallbackRenderers 验证无 Python 时 Go 基础版渲染器可用
// （“导出不因缺失 Python 而失败”的保证链路）。
func TestGoFallbackRenderers(t *testing.T) {
	qr := &QueryResult{
		Columns: []string{"month", "revenue", "cost"},
		Data: []map[string]any{
			{"month": "2026-01", "revenue": 12000.0, "cost": 8000.0},
			{"month": "2026-02", "revenue": 15000.0, "cost": 9000.0},
			{"month": "2026-03", "revenue": 11000.0, "cost": 7500.0},
		},
	}
	// 与生产链路一致：工具函数在生成前先确保 exports/ 目录存在
	// （Go 兜底图表写到 exports/ 相对目录，依赖服务 cwd）
	EnsureExportsDir()

	// PPT data 模式兜底
	pptPath := filepath.Join(os.TempDir(), "websql_fallback_test.pptx")
	defer os.Remove(pptPath)
	chartPaths := GeneratePptCharts(qr, "兜底测试", "fallback")
	count, err := GeneratePptx(qr, "兜底测试", chartPaths, pptPath)
	cleanupFiles(chartPaths)
	if err != nil {
		t.Fatalf("Go 兜底 PPT 生成失败: %v", err)
	}
	if count <= 0 {
		t.Fatalf("slideCount 异常: %d", count)
	}
	if _, err := os.Stat(pptPath); err != nil {
		t.Fatalf("PPT 输出文件不存在: %v", err)
	}
	t.Logf("Go 兜底 PPT 通过: slides=%d", count)

	// Word data 模式兜底
	docxPath := filepath.Join(os.TempDir(), "websql_fallback_test.docx")
	defer os.Remove(docxPath)
	docxCharts := GenerateDocxCharts(qr, "兜底测试", "fallback")
	if err := GenerateDocx(qr, "兜底测试", docxCharts, docxPath); err != nil {
		cleanupFiles(docxCharts)
		t.Fatalf("Go 兜底 Word 生成失败: %v", err)
	}
	cleanupFiles(docxCharts)
	if _, err := os.Stat(docxPath); err != nil {
		t.Fatalf("Word 输出文件不存在: %v", err)
	}
	t.Logf("Go 兜底 Word 通过")

	// content 模式兜底
	markdown := "## 摘要\n\n本季度经营状况良好。\n\n- 要点一\n- 要点二\n"
	pptContent := filepath.Join(os.TempDir(), "websql_fallback_content_test.pptx")
	defer os.Remove(pptContent)
	if _, err := GeneratePptxFromContent(markdown, "内容兜底测试", pptContent); err != nil {
		t.Fatalf("Go 兜底 PPT(content) 生成失败: %v", err)
	}
	docxContent := filepath.Join(os.TempDir(), "websql_fallback_content_test.docx")
	defer os.Remove(docxContent)
	if err := GenerateDocxFromContent(markdown, "内容兜底测试", docxContent); err != nil {
		t.Fatalf("Go 兜底 Word(content) 生成失败: %v", err)
	}
	t.Logf("Go 兜底 content 模式通过")
}
