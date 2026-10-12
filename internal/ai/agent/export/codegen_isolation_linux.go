//go:build linux

package export

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"sync"
)

// P2 OS 级隔离（Linux）：基于 bubblewrap(bwrap) 的文件系统 + 网络隔离。
//
// 约束：整机只读挂载（--ro-bind / /），仅 runDir 可写（--bind），
// 并 --unshare-all（含 --unshare-net）切断网络；--die-with-parent 随父进程退出。
// bwrap 不可用时视为「无可用隔离」，调用方 fail-closed。
//
// 说明：本实现未在 Linux 上实机验证；若 bwrap 参数或内核不支持（未开启
// 非特权 user namespace），子进程会直接启动失败（安全方向失败），不会静默降级。

var (
	isolationOnce   sync.Once
	isolationUsable bool
	bwrapPath       string
)

func codegenIsolationName() string { return "linux-bwrap" }

func codegenIsolationAvailable() bool {
	isolationOnce.Do(func() {
		p, err := exec.LookPath("bwrap")
		if err != nil {
			log.Printf("[Codegen] 未找到 bubblewrap(bwrap)，高级导出将不予注册（fail-closed）\n")
			return
		}
		bwrapPath = p
		isolationUsable = true
	})
	return isolationUsable
}

// applyCodegenIsolation 将原始命令包装为 bwrap 沙箱调用（原地改写 cmd.Path/Args）。
func applyCodegenIsolation(cmd *exec.Cmd, runDir string) (func(), error) {
	if bwrapPath == "" {
		p, err := exec.LookPath("bwrap")
		if err != nil {
			return nil, fmt.Errorf("未找到 bubblewrap(bwrap)：%w", err)
		}
		bwrapPath = p
	}
	absRun, err := filepath.Abs(runDir)
	if err != nil {
		return nil, fmt.Errorf("解析运行目录绝对路径失败：%w", err)
	}

	origPath, origArgs := cmd.Path, cmd.Args
	wrapped := []string{
		bwrapPath,
		"--die-with-parent",
		"--unshare-all",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", absRun, absRun,
		"--chdir", absRun,
		origPath,
	}
	if len(origArgs) > 1 {
		wrapped = append(wrapped, origArgs[1:]...)
	}
	cmd.Path = bwrapPath
	cmd.Args = wrapped
	return func() {}, nil
}