//go:build !windows

package export

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
