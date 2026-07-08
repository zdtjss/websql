//go:build desktop

package main

import (
	"fmt"
	"time"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Notify 发送一条原生桌面通知。
// title/body 为基本文案,前端通过 Call.ByName('main.DesktopApp.Notify', title, body) 调用。
func (a *DesktopApp) Notify(title, body string) error {
	if a.notifications == nil {
		return fmt.Errorf("通知服务未初始化")
	}
	return a.notifications.SendNotification(notifications.NotificationOptions{
		ID:    fmt.Sprintf("websql-%d", time.Now().UnixNano()),
		Title: title,
		Body:  body,
	})
}

// registerNotificationCallback 在 ServiceStartup 中调用,把用户对通知的响应转发为前端事件。
// 前端可监听 "notification:response" 事件做相应处理(目前仅日志,前端可按需订阅)。
func (a *DesktopApp) registerNotificationCallback() {
	if a.notifications == nil || a.app == nil {
		return
	}
	a.notifications.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			a.app.Event.Emit("notification:error", result.Error.Error())
			return
		}
		a.app.Event.Emit("notification:response", result.Response)
	})
}
