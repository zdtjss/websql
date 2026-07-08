//go:build desktop

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed web-src/static/favicon.ico
var trayIconBytes []byte

// setupTray 设置系统托盘:图标常驻,右键菜单(显示主窗口 / 退出)。
func setupTray(app *application.App) {
	tray := app.SystemTray.New()
	tray.SetIcon(trayIconBytes)
	tray.SetLabel("WebSql")

	menu := application.NewMenu()
	menu.Add("显示主窗口").
		OnClick(func(ctx *application.Context) { app.Show() })
	menu.Add("新建连接…").
		OnClick(func(ctx *application.Context) { app.Event.Emit("menu:file:new-conn") })
	menu.AddSeparator()
	menu.Add("退出").
		OnClick(func(ctx *application.Context) { app.Quit() })

	tray.SetMenu(menu)
}
