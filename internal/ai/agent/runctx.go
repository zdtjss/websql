package agent

import "context"

// RunIdentity 单次 Run 的身份信息，经 context 传给工具。
//
// 背景：SQLAgent 按 (连接, schema, 用户, 模型) 缓存复用，工具闭包在构建时捕获的
// ExecAuditCtx.SessionID 是「第一次构建时」的会话 ID（新会话时甚至为空），
// 同一 Agent 服务后续其他会话时审计记录会写错会话。工具执行时应以本次 Run 的身份为准。
type RunIdentity struct {
	SessionID string
	UserID    string
}

type runIdentityKey struct{}

// WithRunIdentity 把本次运行身份放入 context
func WithRunIdentity(ctx context.Context, id RunIdentity) context.Context {
	return context.WithValue(ctx, runIdentityKey{}, id)
}

func runIdentityFrom(ctx context.Context) (RunIdentity, bool) {
	if ctx == nil {
		return RunIdentity{}, false
	}
	id, ok := ctx.Value(runIdentityKey{}).(RunIdentity)
	return id, ok
}

// ForRun 返回绑定本次 Run 会话 ID 的审计上下文副本（不修改共享实例）
func (a *ExecAuditCtx) ForRun(ctx context.Context) *ExecAuditCtx {
	if a == nil {
		return nil
	}
	id, ok := runIdentityFrom(ctx)
	if !ok || id.SessionID == "" || id.SessionID == a.SessionID {
		return a
	}
	cp := *a
	cp.SessionID = id.SessionID
	return &cp
}
