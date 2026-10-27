package fileparse

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"websql/internal/ai/agent/export"
)

// Python 解析链路的集成用例。
//
// 依赖本机 Python 解释器与三方库（python-docx / pdfplumber）：
// 缺失时跳过而非失败，保证精简容器（无 Python）下 CI 依然稳定；
// 无 Python 的降级行为由 Go 兜底用例（parsers_test.go）覆盖。

// probeFor 返回模块对应的探测缓存（测试内复用生产探测逻辑）
func probeFor(module string) *moduleProbe {
	if module == pythonModuleDocx {
		return &docxModuleProbe
	}
	return &pdfModuleProbe
}

// requireModule 依赖缺失时跳过用例
func requireModule(t *testing.T, module string) {
	t.Helper()
	if !PythonAvailable() {
		t.Skip("未检测到 Python 运行环境，跳过该用例")
	}
	if !pythonModuleAvailable(module, probeFor(module)) {
		t.Skipf("缺少 Python 模块 %s，跳过该用例", module)
	}
}

// writeRealDocx 用 python-docx 生成一份真实 Word 文档（含段落与表格）。
//
// 之所以用三方库生成夹具：python-docx 要求完整的 OOXML 包结构
// （[Content_Types].xml 等），手写夹具难以代表真实文件。
func writeRealDocx(t *testing.T, path string) {
	t.Helper()
	const script = `import sys
import docx
d = docx.Document()
d.add_paragraph('季度经营分析报告')
d.add_paragraph('第二段：华东区域增长显著')
table = d.add_table(rows=2, cols=2)
table.cell(0, 0).text = '地区'
table.cell(0, 1).text = '销售额'
table.cell(1, 0).text = '华东'
table.cell(1, 1).text = '120'
d.save(sys.argv[1])
`
	cmd := exec.Command(export.GetPythonPath(), "-c", script, path)
	cmd.Env = pythonEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成 docx 夹具失败：%v，输出：%s", err, string(out))
	}
}

// ─── 注册表：优先级与降级 ────────────────────────────────────

func TestDescribe_DocxPrefersPythonWhenAvailable(t *testing.T) {
	_, impl, ok := Describe(".docx")
	if !ok {
		t.Fatal(".docx 必须有可用实现（Python 或 Go 兜底）")
	}
	if pythonModuleAvailable(pythonModuleDocx, &docxModuleProbe) {
		if impl != "python-docx" {
			t.Errorf("依赖可用时应优先 Python 实现，实际 %q", impl)
		}
	} else if impl != "go-docx" {
		t.Errorf("依赖不可用时应降级到 Go 兜底，实际 %q", impl)
	}
}

func TestDescribe_PDFFollowsDependency(t *testing.T) {
	_, impl, ok := Describe(".pdf")
	wantOK := pythonModuleAvailable(pythonModulePDF, &pdfModuleProbe)
	if ok != wantOK {
		t.Errorf(".pdf 可用性应与 pdfplumber 一致：ok=%v want=%v", ok, wantOK)
	}
	if wantOK && impl != "python-pdf" {
		t.Errorf("pdfplumber 可用时应使用 Python 实现，实际 %q", impl)
	}
	if !wantOK && impl != "none" {
		t.Errorf("pdfplumber 不可用时应报告 none，实际 %q", impl)
	}
}

// ─── DOCX ───────────────────────────────────────────────────

func TestParseDOCX_ViaPython(t *testing.T) {
	requireModule(t, pythonModuleDocx)

	path := filepath.Join(t.TempDir(), "report.docx")
	writeRealDocx(t, path)

	res, err := Parse(context.Background(), Source{Path: path, Ext: ".docx", FileName: "report.docx"})
	if err != nil {
		t.Fatalf("Python 解析 docx 失败：%v", err)
	}
	if res.Impl != "python-docx" {
		t.Errorf("实现名应为 python-docx，实际 %q", res.Impl)
	}
	if res.Degraded {
		t.Error("Python 主路径不应标记为降级")
	}
	if res.Kind != KindDoc {
		t.Errorf("类别应为 doc，实际 %s", res.Kind)
	}
	for _, want := range []string{"季度经营分析报告", "华东", "销售额"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("正文应包含 %q，实际：%q", want, res.Text)
		}
	}
	if res.TotalChars == 0 {
		t.Error("应返回字符数")
	}
	if res.Meta == nil {
		t.Error("应返回结构元信息（段落数/表格数）")
	}
}

func TestParseDOCX_GoFallbackOnRealDocx(t *testing.T) {
	requireModule(t, pythonModuleDocx)

	path := filepath.Join(t.TempDir(), "report.docx")
	writeRealDocx(t, path)

	// 直接调用 Go 兜底实现（绕过注册表优先级），验证其能处理真实 Word 文件
	res, err := parseDOCX(context.Background(), Source{Path: path, Ext: ".docx", FileName: "report.docx"})
	if err != nil {
		t.Fatalf("Go 兜底解析失败：%v", err)
	}
	if !res.Degraded || res.Impl != "go-docx" {
		t.Errorf("应标记为降级实现：degraded=%v impl=%s", res.Degraded, res.Impl)
	}
	for _, want := range []string{"季度经营分析报告", "华东"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("兜底解析正文应包含 %q，实际：%q", want, res.Text)
		}
	}
}

// ─── PDF ────────────────────────────────────────────────────

func TestParsePDF_ViaPython(t *testing.T) {
	requireModule(t, pythonModulePDF)

	path := writeTemp(t, "report.pdf", pdfBytes(t, "Hello PDF Report"))

	res, err := Parse(context.Background(), Source{Path: path, Ext: ".pdf", FileName: "report.pdf"})
	if err != nil {
		t.Fatalf("Python 解析 pdf 失败：%v", err)
	}
	if res.Impl != "python-pdf" {
		t.Errorf("实现名应为 python-pdf，实际 %q", res.Impl)
	}
	if !strings.Contains(res.Text, "Hello PDF Report") {
		t.Errorf("应提取到文本，实际：%q", res.Text)
	}
	if res.Pages != 1 {
		t.Errorf("页数应为 1，实际 %d", res.Pages)
	}
	if res.Notice != "" {
		t.Errorf("正常 PDF 不应有提示，实际：%q", res.Notice)
	}
}

func TestParsePDF_InvalidContent(t *testing.T) {
	requireModule(t, pythonModulePDF)

	path := writeTemp(t, "broken.pdf", []byte("this is not a real pdf at all"))

	_, err := Parse(context.Background(), Source{Path: path, Ext: ".pdf", FileName: "broken.pdf"})
	if err == nil {
		t.Fatal("损坏的 PDF 应返回错误")
	}
	if !strings.Contains(err.Error(), "解析") {
		t.Errorf("错误文案应可读，实际：%q", err.Error())
	}
	// 不得把服务器路径/堆栈透传给用户
	if strings.Contains(err.Error(), path) {
		t.Errorf("错误文案不应包含服务器路径：%q", err.Error())
	}
}

// ─── 子进程契约与探测 ────────────────────────────────────────

func TestWritePythonScripts(t *testing.T) {
	dir := t.TempDir()
	scriptPath, err := writePythonScripts(dir)
	if err != nil {
		t.Fatalf("写入脚本失败：%v", err)
	}
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("读取脚本失败：%v", err)
	}
	if !strings.Contains(string(data), "def main()") {
		t.Error("脚本内容不完整")
	}
}

func TestPythonModuleProbe_UnknownModule(t *testing.T) {
	if !PythonAvailable() {
		t.Skip("未检测到 Python 运行环境，跳过该用例")
	}
	probe := &moduleProbe{}
	if pythonModuleAvailable("websql_no_such_module_xyz", probe) {
		t.Error("不存在的模块应判定为不可用")
	}
	// 第二次调用走缓存且结果一致
	if pythonModuleAvailable("websql_no_such_module_xyz", probe) {
		t.Error("缓存结果应与首次一致")
	}
}
