//go:build desktop

package main

import (
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Fatalf("[DesktopPanic] %v", r)
		}
	}()

	container := initDesktopContainer()
	defer container.Close()

	desktopApp := NewDesktopApp(container)

	err := wails.Run(&options.App{
		Title:     "WebSql",
		Width:     1280,
		Height:    800,
		MinWidth:  1024,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		OnStartup:  desktopApp.OnStartup,
		OnShutdown: desktopApp.OnShutdown,
		Bind: []interface{}{
			desktopApp,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.SystemDefault,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarDefault(),
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "WebSql",
				Message: "数据库管理工具桌面版",
			},
		},
	})
	if err != nil {
		log.Fatalf("[Desktop] Wails 启动失败: %v", err)
	}
}
