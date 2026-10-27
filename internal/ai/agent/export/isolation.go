package export

import (
	"log"
	"os/exec"
)

// 面向「第一方受信子进程」的隔离辅助。
//
// 背景：codegen（高级导出）执行的是**模型生成的临时代码**，因此采用
// fail-closed 语义——无 OS 级隔离就不注册工具（见 codegen_isolation.go）。
//
// 而文件内容解析（fileparse 包）执行的是仓库内自带的第一方只读脚本，
// 风险面与 codegen 不同：脚本行为固定、只读单个目标文件、不访问数据库、
// 不需要联网。这里提供「尽力而为」的隔离入口：可用则启用（纵深防御，
// 限制越界写与网络），不可用则返回空 cleanup 由调用方继续执行——
// 若同样 fail-closed，绝大多数部署将无法解析 docx/pdf。

// HideCommandWindow 隐藏子进程控制台窗口（Windows 下阻止黑窗闪现）。
func HideCommandWindow(cmd *exec.Cmd) {
	hideWindow(cmd)
}

// ApplyIsolationBestEffort 尽力为受信子进程应用 OS 级隔离。
//
// 返回值 cleanup 一定非 nil（不可用时为空操作），调用方可直接 defer。
// 返回的 bool 表示隔离是否实际生效，用于日志与审计。
func ApplyIsolationBestEffort(cmd *exec.Cmd, runDir string) (cleanup func(), applied bool) {
	if !CodegenIsolationAvailable() {
		return func() {}, false
	}
	fn, err := applyCodegenIsolation(cmd, runDir)
	if err != nil {
		log.Printf("[Isolation] 应用隔离失败，降级为无隔离执行 - mechanism=%s, err=%v\n", CodegenIsolationName(), err)
		return func() {}, false
	}
	if fn == nil {
		return func() {}, true
	}
	return fn, true
}
