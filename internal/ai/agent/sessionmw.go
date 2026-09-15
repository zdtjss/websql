// sessionmw.go — SessionSyncMiddleware：把 Eino ADK 的内部消息状态同步回 SessionStore。
//
// 用途：当 summarization 中间件压缩了对话历史后，本中间件确保持久化层也反映压缩后的状态，
// 避免下次加载时历史重新膨胀。
//
// 并发安全设计（重要）：
// per-run 状态（当前 Session、初始消息数、是否发生压缩）通过 **context 传递**，
// 不再存放于中间件实例。历史实现用 SetSession 把 Session 绑定到中间件字段上，
// 而 Agent 实例被工厂按 (connID, dbSchema, userID, schemas) 缓存共享，
// 同用户多标签并发时会把 A 会话的压缩结果写到 B 会话，本次修复改为：
//   - RunStream 用 WithRunSessionState(ctx, newRunSessionState(sess, len(allMsgs))) 注入
//   - AfterAgent / AfterModelRewriteState 从 ctx 读取 per-run 状态
// 中间件实例因此变为无状态，可被任意并发 Run 安全共享。
package agent

import (
	"context"
	"log"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type runSessionCtxKey struct{}

// runSessionState 单次 Run 的会话同步状态（每个 Run 独立一份，由 context 持有）。
type runSessionState struct {
	mu               sync.Mutex
	sess             *Session
	initialMsgCount  int
	summarizationHit bool
}

func newRunSessionState(sess *Session, initialMsgCount int) *runSessionState {
	return &runSessionState{sess: sess, initialMsgCount: initialMsgCount}
}

// WithRunSessionState 把本次运行的会话同步状态放入 context。
func WithRunSessionState(ctx context.Context, st *runSessionState) context.Context {
	if st == nil {
		return ctx
	}
	return context.WithValue(ctx, runSessionCtxKey{}, st)
}

func runSessionStateFrom(ctx context.Context) *runSessionState {
	if st, ok := ctx.Value(runSessionCtxKey{}).(*runSessionState); ok {
		return st
	}
	return nil
}

// SessionSyncMiddleware 将 Eino Agent 的内部消息状态与 SessionStore 同步。
// 无状态：per-run 绑定信息全部来自 context。
type SessionSyncMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// AfterAgent 在 Agent 成功结束后，检查消息是否被 summarization 压缩，
// 如果是则同步压缩后的消息到会话（仅写回本 Run 绑定的会话，不会串号）。
func (m *SessionSyncMiddleware) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
	st := runSessionStateFrom(ctx)
	if st == nil || state == nil {
		return ctx, nil
	}

	st.mu.Lock()
	sess := st.sess
	hit := st.summarizationHit
	st.mu.Unlock()

	if sess == nil || !hit || len(state.Messages) == 0 {
		return ctx, nil
	}

	log.Printf("[SessionSync] 检测到 summarization 压缩 - session=%s, 当前消息=%d\n", sess.ID, len(state.Messages))

	// 将 Eino 内部的压缩后消息同步回 Session（跳过 system 消息）
	compressed := make([]SessionMessage, 0, len(state.Messages))
	for _, msg := range state.Messages {
		if msg.Role == schema.System {
			continue // system prompt 不持久化
		}
		sm := schemaMessageToSession(msg)
		if sm != nil {
			compressed = append(compressed, *sm)
		}
	}

	if len(compressed) > 0 {
		sess.ReplaceMessages(compressed)
		log.Printf("[SessionSync] 已同步压缩后的消息到 Session - session=%s, count=%d\n", sess.ID, len(compressed))
	}
	return ctx, nil
}

// AfterModelRewriteState 在每次模型调用后检查消息数量变化，用于检测 summarization 是否发生。
func (m *SessionSyncMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	st := runSessionStateFrom(ctx)
	if st == nil || state == nil {
		return ctx, state, nil
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	currentMsgCount := len(state.Messages)
	if st.initialMsgCount > 0 && currentMsgCount < st.initialMsgCount {
		st.summarizationHit = true
	}
	// 更新跟踪计数
	st.initialMsgCount = currentMsgCount
	return ctx, state, nil
}

// schemaMessageToSession 将 Eino schema.Message 转换为 SessionMessage
func schemaMessageToSession(msg *schema.Message) *SessionMessage {
	if msg == nil {
		return nil
	}

	sm := &SessionMessage{
		Role:    string(msg.Role),
		Content: msg.Content,
	}

	if len(msg.ToolCalls) > 0 {
		sm.ToolCalls = sessionToolCallsFromSchema(msg.ToolCalls)
	}
	if msg.ToolCallID != "" {
		sm.ToolCallID = msg.ToolCallID
	}
	if msg.ToolName != "" {
		sm.ToolName = msg.ToolName
	}

	return sm
}