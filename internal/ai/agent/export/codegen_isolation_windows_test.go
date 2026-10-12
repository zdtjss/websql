//go:build windows

package export

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCodegenIsolationWriteConfinement 验证低完整性隔离的核心保证：
// 子进程可写 runDir，但无法写入 runDir 之外（中完整性）的目录。
// 环境不支持低完整性隔离时跳过。
func TestCodegenIsolationWriteConfinement(t *testing.T) {
	if !CodegenIsolationAvailable() {
		t.Skip("当前环境无可用低完整性隔离")
	}
	python := GetPythonPath()
	if python == "" {
		t.Skip("未检测到 Python")
	}

	base := t.TempDir()
	runDir := filepath.Join(base, "run")
	otherDir := filepath.Join(base, "other")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("创建 runDir 失败：%v", err)
	}
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatalf("创建 otherDir 失败：%v", err)
	}

	const code = `import sys
try:
    open(sys.argv[1], "w").write("x")
    print("RUNDIR_OK")
except Exception as e:
    print("RUNDIR_FAIL:", e)
try:
    open(sys.argv[2], "w").write("x")
    print("LEAK")
except Exception as e:
    print("NO_LEAK:", e)
`
	script := filepath.Join(runDir, "probe.py")
	if err := os.WriteFile(script, []byte(code), 0o600); err != nil {
		t.Fatalf("写入脚本失败：%v", err)
	}
	runOut := filepath.Join(runDir, "out.txt")
	otherOut := filepath.Join(otherDir, "evil.txt")

	cmd := exec.Command(python, "-B", script, runOut, otherOut)
	cmd.Dir = runDir
	cleanup, err := applyCodegenIsolation(cmd, runDir)
	if err != nil {
		t.Fatalf("应用低完整性隔离失败：%v", err)
	}
	defer cleanup()

	out, err := cmd.CombinedOutput()
	t.Logf("子进程输出：%s", out)
	if err != nil {
		t.Fatalf("子进程执行失败：%v", err)
	}

	if _, statErr := os.Stat(runOut); statErr != nil {
		t.Errorf("runDir 内写入应成功，却失败：%v", statErr)
	}
	if _, statErr := os.Stat(otherOut); statErr == nil {
		t.Errorf("runDir 外写入应被低完整性拦截，却成功了（隔离失效）")
	}
}