//go:build windows

package export

import (
	"os/exec"
	"syscall"
)

// hideWindow 隐藏 exec.Command 在 Windows 上弹出的控制台窗口。
// CREATE_NO_WINDOW 阻止创建控制台，比 HideWindow 更彻底——
// 后者仍会创建控制台进程再隐藏窗口，可能短暂闪现。
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
