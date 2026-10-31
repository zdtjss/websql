// queue_control.go — 运行级「技能执行排队」控制：把排队进度推给前端，
// 并提供「用户选择改用 Go 原生导出」的反向信号。
//
// 设计要点：
//   - 同一会话同一时刻只允许一次运行（Session.TryBeginRun 互斥），因此按 sessionID
//     登记控制项是安全的；
//   - 控制项随运行创建、运行结束注销（比较后删除，避免误删后续运行的登记）；
//   - 降级是「一次性信号」：用户点击后关闭通道，仅让当前排队中的 execute 放弃等待，
//     不影响本轮后续工具调用（用户可能只是不想等这一次）。
package agent

import (
	"context"
	"strings"
	"sync"

	"websql/internal/ai/agent/export"
)

type runQueueControl struct {
	sessionID string
	degradeCh chan struct{}
	once      sync.Once
}

func (c *runQueueControl) requestDegrade() {
	if c == nil {
		return
	}
	c.once.Do(func() { close(c.degradeCh) })
}

var runQueueControls sync.Map // sessionID -> *runQueueControl

// registerRunQueueControl 登记本次运行的控制项，返回注销函数。
func registerRunQueueControl(sessionID string) (*runQueueControl, func()) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, func() {}
	}
	c := &runQueueControl{sessionID: sessionID, degradeCh: make(chan struct{})}
	runQueueControls.Store(sessionID, c)
	return c, func() {
		if v, ok := runQueueControls.Load(sessionID); ok && v == c {
			runQueueControls.Delete(sessionID)
		}
	}
}

// RequestSkillDegrade 请求把该会话当前排队中的技能执行降级为 Go 原生渲染。
// 返回 false 表示当前没有等待中的运行（或已结束）。
func RequestSkillDegrade(sessionID string) bool {
	v, ok := runQueueControls.Load(strings.TrimSpace(sessionID))
	if !ok {
		return false
	}
	v.(*runQueueControl).requestDegrade()
	return true
}

// withRunQueueControl 把排队通知与降级信号注入本次运行的 context。
//
// 通知只走 SSE（不进 messages）：排队位次属于运行态信息，写入历史会污染模型上下文。
func withRunQueueControl(ctx context.Context, c *runQueueControl, flush func(StreamChunk)) context.Context {
	if c == nil || flush == nil {
		return ctx
	}
	ctx = export.WithDegradeSignal(ctx, c.degradeCh)
	ctx = export.WithQueueNotifier(ctx, func(ev export.QueueEvent) {
		chunk := StreamChunk{
			Type:         "queue_notice",
			Phase:        ev.Phase,
			Position:     ev.Ahead,
			WaitMs:       ev.Wait.Milliseconds(),
			Limit:        ev.Limit,
			PerUserLimit: ev.PerUserLimit,
		}
		switch ev.Phase {
		case "waiting":
			chunk.Content = "技能导出排队中"
		case "acquired":
			chunk.Content = "已获得执行资源，开始导出"
		case "degraded":
			chunk.Content = "已改用 Go 原生导出"
		case "timeout":
			chunk.Content = "排队超时，已改用 Go 原生导出"
		}
		flush(chunk)
	})
	return ctx
}
