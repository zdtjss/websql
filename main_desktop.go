//go:build desktop

package main

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Fatalf("[DesktopPanic] %v", r)
		}
	}()

	container := initDesktopContainer()
	defer container.Close()

	ns := notifications.New()
	desktopApp := NewDesktopApp(container, ns)

	app := application.New(application.Options{
		Name:        "WebSql",
		Description: "数据库管理工具桌面版",
		Icon:        trayIconBytes,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontendAssets),
		},
		Services: []application.Service{
			application.NewService(desktopApp),
			application.NewService(ns),
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "WebSql",
		Width:     1280,
		Height:    800,
		MinWidth:  1024,
		MinHeight: 700,
	})

	setupTray(app)

	if err := app.Run(); err != nil {
		log.Fatalf("[Desktop] Wails v3 启动失败: %v", err)
	}
}
