//go:build desktop

package bindings

// registerFileio 注册 fileio 模块的 binding。
// 对应 service: DesktopApp 自身已实现 OpenFileDialog/SaveFileDialog/ReadFile/WriteFile
// 注册类型: 无 (文件操作通过 DesktopApp 直接暴露给前端,无需经过 Registry 分发)
// 对应 HTTP 路由: 无 (桌面版独有功能)
// 本占位函数保留以保持 registerAll 完整性,实际无操作。
func registerFileio(r *Registry) {
	// 桌面版文件操作直接通过 DesktopApp 暴露,无需在 Registry 注册。
}
