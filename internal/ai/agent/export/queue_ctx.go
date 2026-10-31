// queue_ctx.go — 排队事件回调与「用户降级」信号在 context 中的传递约定。
//
// 与 skillContextTracker / runPromptCtxKey 同一模式：由 Agent 在每次运行时注入，
// execute（OSFilesystemBackend）从 context 取出，避免把 HTTP 层依赖带进 export 包。
package export

import "context"

type queueNotifierCtxKey struct{}
type degradeSignalCtxKey struct{}

// WithQueueNotifier 注入本次运行的排队事件回调（通常转发为 SSE 事件）。
func WithQueueNotifier(ctx context.Context, fn QueueNotifier) context.Context {
	if ctx == nil || fn == nil {
		return ctx
	}
	return context.WithValue(ctx, queueNotifierCtxKey{}, fn)
}

// QueueNotifierFrom 取出排队事件回调；未注入时返回 nil（静默排队）。
func QueueNotifierFrom(ctx context.Context) QueueNotifier {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(queueNotifierCtxKey{}).(QueueNotifier)
	return fn
}

// WithDegradeSignal 注入「用户选择降级」信号：通道关闭即放弃排队。
func WithDegradeSignal(ctx context.Context, ch <-chan struct{}) context.Context {
	if ctx == nil || ch == nil {
		return ctx
	}
	return context.WithValue(ctx, degradeSignalCtxKey{}, ch)
}

// DegradeSignalFrom 取出降级信号通道；未注入时返回 nil（永久阻塞的 select 分支）。
func DegradeSignalFrom(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	ch, _ := ctx.Value(degradeSignalCtxKey{}).(<-chan struct{})
	return ch
}
