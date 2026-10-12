//go:build !windows && !linux

package export

import (
	"fmt"
	"log"
	"os/exec"
	"sync"
)

// 其他平台（如 macOS）暂未提供 OS 级隔离实现，视为「无可用隔离」，
// 调用方 fail-closed 不注册高级导出工具。

var isolationOnce sync.Once

func codegenIsolationName() string { return "none" }

func codegenIsolationAvailable() bool {
	isolationOnce.Do(func() {
		log.Printf("[Codegen] 当前平台无 OS 级隔离实现，高级导出将不予注册（fail-closed）\n")
	})
	return false
}

func applyCodegenIsolation(_ *exec.Cmd, _ string) (func(), error) {
	return nil, fmt.Errorf("当前平台无可用 OS 级隔离")
}