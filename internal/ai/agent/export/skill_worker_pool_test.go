package export

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testWorkerScript 返回仓库内 worker 脚本的绝对路径。
func testWorkerScript(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "skills", "lib", "skill_worker.py"))
	if err != nil {
		t.Fatalf("解析 worker 脚本路径失败：%v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("worker 脚本不存在，跳过：%v", err)
	}
	return p
}

// newTestPool 构造不启动回收协程的小池。
func newTestPool(t *testing.T, max int) *skillWorkerPool {
	t.Helper()
	python := GetPythonPath()
	if python == "" {
		t.Skip("未检测到 Python，跳过 worker 池测试")
	}
	envDir := ""
	if filepath.IsAbs(python) {
		envDir = filepath.Dir(python)
	}
	return &skillWorkerPool{
		python:       python,
		pythonEnvDir: envDir,
		workerScript: testWorkerScript(t),
		max:          max,
		minWarm:      0,
		idleTTL:      time.Minute,
		maxJobs:      50,
		wake:         make(chan struct{}),
	}
}

// writeScript 在临时目录写入测试脚本。
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("写入脚本失败：%v", err)
	}
	return path
}

// TestWorkerPoolModuleReuse 同一 worker 复用进程内已导入模块（load_count 递增）。
func TestWorkerPoolModuleReuse(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "reuse_probe.py", "load_count = 0\n")
	script := writeScript(t, dir, "probe.py", `
import json
import os
import sys
import tempfile

import reuse_probe

reuse_probe.load_count += 1
raw = sys.stdin.read()
print(json.dumps({
    "n": reuse_probe.load_count,
    "cwd": os.getcwd(),
    "temp": os.environ.get("TEMP", ""),
    "tempdir": tempfile.gettempdir(),
    "argv": sys.argv[1:],
    "stdin": raw,
}, ensure_ascii=False))
`)

	pool := newTestPool(t, 1)
	spec := skillExecSpec{
		Script:  script,
		Args:    []string{"first.json"},
		Cwd:     dir,
		Env:     map[string]string{"TEMP": filepath.Join(dir, "t1")},
		Timeout: 30 * time.Second,
	}

	res1, err := pool.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("首次执行失败：%v", err)
	}
	if res1.ExitCode != 0 {
		t.Fatalf("首次执行应成功，exit=%d output=%s", res1.ExitCode, res1.Output)
	}
	if !strings.Contains(res1.Output, `"n": 1`) {
		t.Fatalf("首次应输出 n=1：%s", res1.Output)
	}
	if !strings.Contains(res1.Output, `"argv": ["first.json"]`) {
		t.Fatalf("argv 未正确传递：%s", res1.Output)
	}

	// 第二次执行：同一 worker，模块缓存命中 → n=2
	spec.Env = map[string]string{"TEMP": filepath.Join(dir, "t2")}
	res2, err := pool.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("二次执行失败：%v", err)
	}
	if !strings.Contains(res2.Output, `"n": 2`) {
		t.Fatalf("二次执行应复用同一进程（n=2）：%s", res2.Output)
	}
	if !strings.Contains(res2.Output, filepath.Join("t2")) {
		t.Fatalf("二次执行的 TEMP 未按请求切换：%s", res2.Output)
	}
	// tempfile 缓存必须随请求重置，否则第二个用户会写进上一个用户的临时目录
	if !strings.Contains(res2.Output, `"tempdir": "`) || !strings.Contains(res2.Output, filepath.Join("t2")) {
		t.Fatalf("tempfile 临时目录未随请求切换：%s", res2.Output)
	}
}

// TestWorkerPoolStdinFile 位置参数与 stdin 重定向两种输入方式都要可用。
func TestWorkerPoolStdinFile(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "echo_stdin.py", `
import json
import sys

raw = sys.stdin.read()
print(json.dumps({"stdin": raw, "argv": sys.argv[1:]}, ensure_ascii=False))
`)
	inputPath := filepath.Join(dir, "input.json")
	if err := os.WriteFile(inputPath, []byte(`{"hello":"世界"}`), 0o644); err != nil {
		t.Fatalf("写入输入文件失败：%v", err)
	}

	pool := newTestPool(t, 1)
	res, err := pool.Run(context.Background(), skillExecSpec{
		Script:    script,
		Cwd:       dir,
		StdinFile: inputPath,
		Timeout:   30 * time.Second,
	})
	if err != nil {
		t.Fatalf("执行失败：%v", err)
	}
	if !strings.Contains(res.Output, "世界") {
		t.Fatalf("stdin 内容未传入脚本：%s", res.Output)
	}
}

// TestWorkerPoolExitCodeAndTraceback SystemExit 退出码与未捕获异常分别映射为 exitCode/traceback。
func TestWorkerPoolExitCodeAndTraceback(t *testing.T) {
	dir := t.TempDir()
	pool := newTestPool(t, 1)

	exit3 := writeScript(t, dir, "exit3.py", "import sys\nsys.exit(3)\n")
	res, err := pool.Run(context.Background(), skillExecSpec{Script: exit3, Cwd: dir, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("执行失败：%v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("SystemExit(3) 应映射为 exitCode=3，实际 %d", res.ExitCode)
	}

	boom := writeScript(t, dir, "boom.py", "raise RuntimeError('boom')\n")
	res, err = pool.Run(context.Background(), skillExecSpec{Script: boom, Cwd: dir, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("执行失败：%v", err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.Output, "RuntimeError") {
		t.Fatalf("异常应映射为 exitCode=1 且带 traceback，实际 exit=%d output=%s", res.ExitCode, res.Output)
	}

	// 异常之后 worker 仍应可用（同一个池继续执行）
	res, err = pool.Run(context.Background(), skillExecSpec{Script: exit3, Cwd: dir, Timeout: 30 * time.Second})
	if err != nil || res.ExitCode != 3 {
		t.Fatalf("异常后 worker 应继续可用：err=%v exit=%d", err, res.ExitCode)
	}
}

// TestWorkerPoolTimeout 超时杀进程并返回可读提示，随后池能重新拉起新 worker。
func TestWorkerPoolTimeout(t *testing.T) {
	dir := t.TempDir()
	sleep := writeScript(t, dir, "sleep.py", "import time\nprint('start', flush=True)\ntime.sleep(10)\n")
	pool := newTestPool(t, 1)

	start := time.Now()
	res, err := pool.Run(context.Background(), skillExecSpec{Script: sleep, Cwd: dir, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("超时应返回结果而非错误：%v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("超时未及时生效：%v", elapsed)
	}
	if res.ExitCode != -1 || !strings.Contains(res.Output, "已被终止") {
		t.Fatalf("超时结果异常：exit=%d output=%s", res.ExitCode, res.Output)
	}

	quick := writeScript(t, dir, "quick.py", "print('ok')\n")
	res, err = pool.Run(context.Background(), skillExecSpec{Script: quick, Cwd: dir, Timeout: 30 * time.Second})
	if err != nil || res.ExitCode != 0 || !strings.Contains(res.Output, "ok") {
		t.Fatalf("超时后应能重新拉起 worker：err=%v exit=%d output=%s", err, res.ExitCode, res.Output)
	}
}

// TestWorkerPoolRetireByMaxJobs 达到 maxJobs 后回收旧 worker（模块计数重置）。
func TestWorkerPoolRetireByMaxJobs(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "reuse_probe.py", "load_count = 0\n")
	script := writeScript(t, dir, "probe.py", `
import json

import reuse_probe

reuse_probe.load_count += 1
print(json.dumps({"n": reuse_probe.load_count}))
`)
	pool := newTestPool(t, 1)
	pool.maxJobs = 1

	spec := skillExecSpec{Script: script, Cwd: dir, Timeout: 30 * time.Second}
	res, err := pool.Run(context.Background(), spec)
	if err != nil || !strings.Contains(res.Output, `"n": 1`) {
		t.Fatalf("首次执行异常：err=%v output=%s", err, res.Output)
	}
	res, err = pool.Run(context.Background(), spec)
	if err != nil || !strings.Contains(res.Output, `"n": 1`) {
		t.Fatalf("maxJobs=1 应回收旧进程（计数重置为 1）：err=%v output=%s", err, res.Output)
	}
}

// TestWorkerPoolRealPPTWorker 用真实的 export-ppt 技能脚本验证 runpy 与相对导入兼容性：
// worker 与 one-shot 两条路径应产出相同的 success/slideCount。
func TestWorkerPoolRealPPTWorker(t *testing.T) {
	python := GetPythonPath()
	if python == "" {
		t.Skip("未检测到 Python，跳过")
	}
	if err := exec.Command(python, "-c", "import pptx, matplotlib").Run(); err != nil {
		t.Skipf("缺少 python-pptx/matplotlib，跳过：%v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "skills", "export-ppt", "scripts", "export_ppt.py"))
	if err != nil {
		t.Fatalf("解析脚本路径失败：%v", err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Skipf("技能脚本不存在：%v", err)
	}

	dir := t.TempDir()
	input := map[string]any{
		"mode":       "content",
		"title":      "并发闸门验证",
		"outputPath": filepath.Join(dir, "out.pptx"),
		"markdown":   "## 结论\n\n1. 第一项\n2. 第二项\n\n| 列A | 列B |\n| --- | --- |\n| 1 | 2 |\n",
	}
	raw, _ := json.Marshal(input)
	inputPath := filepath.Join(dir, "input.json")
	if err := os.WriteFile(inputPath, raw, 0o644); err != nil {
		t.Fatalf("写入输入失败：%v", err)
	}

	pythonEnvDir := ""
	if filepath.IsAbs(python) {
		pythonEnvDir = filepath.Dir(python)
	}
	spec := skillExecSpec{
		Script:       script,
		Python:       python,
		PythonEnvDir: pythonEnvDir,
		Cwd:          dir,
		Env:          map[string]string{"WEBSQL_EXPORT_DIR": dir, "TEMP": dir, "TMP": dir, "TMPDIR": dir},
		StdinFile:    inputPath,
		Timeout:      120 * time.Second,
	}

	oneShot, err := runOneShotSkill(context.Background(), spec)
	if err != nil {
		t.Fatalf("one-shot 执行失败：%v", err)
	}
	pool := newTestPool(t, 1)
	viaWorker, err := pool.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("worker 执行失败：%v", err)
	}

	for name, res := range map[string]*skillExecResult{"one-shot": oneShot, "worker": viaWorker} {
		if res.ExitCode != 0 || !strings.Contains(res.Output, `"success": true`) {
			t.Fatalf("%s 执行失败：exit=%d output=%s", name, res.ExitCode, res.Output)
		}
	}
	if oneShot.Output != viaWorker.Output {
		t.Fatalf("两条路径的产物信息不一致：\none-shot=%s\nworker=%s", oneShot.Output, viaWorker.Output)
	}
}

// benchPPTspec 构造 PPT 基准测试的固定输入（缺依赖时跳过）。
func benchPPTspec(b *testing.B) (skillExecSpec, *skillWorkerPool) {
	b.Helper()
	python := GetPythonPath()
	if python == "" {
		b.Skip("未检测到 Python")
	}
	if err := exec.Command(python, "-c", "import pptx, matplotlib").Run(); err != nil {
		b.Skipf("缺少依赖：%v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "skills", "export-ppt", "scripts", "export_ppt.py"))
	if err != nil {
		b.Fatalf("解析脚本路径失败：%v", err)
	}
	dir := b.TempDir()
	input := map[string]any{
		"mode":       "content",
		"title":      "基准测试",
		"outputPath": filepath.Join(dir, "bench.pptx"),
		"markdown":   "## 结论\n\n1. 第一项\n2. 第二项\n",
	}
	raw, _ := json.Marshal(input)
	inputPath := filepath.Join(dir, "input.json")
	if err := os.WriteFile(inputPath, raw, 0o644); err != nil {
		b.Fatalf("写入输入失败：%v", err)
	}
	envDir := ""
	if filepath.IsAbs(python) {
		envDir = filepath.Dir(python)
	}
	spec := skillExecSpec{
		Script:       script,
		Python:       python,
		PythonEnvDir: envDir,
		Cwd:          dir,
		Env:          map[string]string{"WEBSQL_EXPORT_DIR": dir, "TEMP": dir, "TMP": dir, "TMPDIR": dir},
		StdinFile:    inputPath,
		Timeout:      120 * time.Second,
	}
	pool := &skillWorkerPool{
		python:       python,
		pythonEnvDir: envDir,
		workerScript: testWorkerScriptB(b),
		max:          1,
		minWarm:      1,
		idleTTL:      time.Minute,
		maxJobs:      1000,
		wake:         make(chan struct{}),
	}
	return spec, pool
}

func testWorkerScriptB(b *testing.B) string {
	b.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "skills", "lib", "skill_worker.py"))
	if err != nil {
		b.Fatalf("解析 worker 脚本路径失败：%v", err)
	}
	return p
}

// BenchmarkSkillScriptOneShot 现状：每次新起解释器（含 import 开销）。
func BenchmarkSkillScriptOneShot(b *testing.B) {
	spec, _ := benchPPTspec(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runOneShotSkill(context.Background(), spec); err != nil {
			b.Fatalf("执行失败：%v", err)
		}
	}
}

// BenchmarkSkillScriptWorker 常驻池：仅首次支付 import 开销。
func BenchmarkSkillScriptWorker(b *testing.B) {
	spec, pool := benchPPTspec(b)
	// 预热一个 worker，避免把冷启动算进基准
	if _, err := pool.Run(context.Background(), spec); err != nil {
		b.Fatalf("预热失败：%v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pool.Run(context.Background(), spec); err != nil {
			b.Fatalf("执行失败：%v", err)
		}
	}
}
