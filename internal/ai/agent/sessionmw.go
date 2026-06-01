// package agent 提供 SessionSyncMiddleware，将 Eino ADK 的内部消息状态
// 与系统的 SessionStore 持久化层同步。
//
// 这是"会话管理重构 → 对接 Eino Memory/Session"的核心实现：
// 利用 Eino v0.9 的 AfterAgent 和 AfterModelRewriteState 钩子，
// 在 Agent 运行过程中自动将状态变化（如 summarization 压缩后的消息）
// 同步回 SessionStore，确保持久化层始终反映 Eino 内部的最新状态。
package agent

import (
	"context"
	"log"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// SessionSyncMiddleware 将 Eino Agent 的内部消息状态与 SessionStore 同步。
// 当 summarization middleware 压缩了对话历史后，本 middleware 确保
// 持久化层也反映压缩后的状态，避免下次加载时重新膨胀。
//
// 使用方式：在 Agent 创建时注册，通过 SetSession 在每次 Run 前绑定当前 Session。
type SessionSyncMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	mu               sync.Mutex
	session          *Session
	initialMsgCount  int
	summarizationHit bool
}

// SetSession 在每次 Agent Run 前绑定当前 Session
func (m *SessionSyncMiddleware) SetSession(sess *Session, msgCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.session = sess
	m.initialMsgCount = msgCount
	m.summarizationHit = false
}

// ClearSession 在 Agent Run 结束后清除 Session 引用
func (m *SessionSyncMiddleware) ClearSession() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.session = nil
	m.initialMsgCount = 0
	m.summarizationHit = false
}

// AfterAgent 在 Agent 成功结束后，检查消息是否被 summarization 压缩，
// 如果是则同步压缩后的消息到 SessionStore。
func (m *SessionSyncMiddleware) AfterAgent(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, error) {
	m.mu.Lock()
	sess := m.session
	hit := m.summarizationHit
	m.mu.Unlock()

	if sess == nil || state == nil {
		return ctx, nil
	}

	// 检测 summarization 是否发生
	currentMsgCount := len(state.Messages)
	if hit && currentMsgCount > 0 {
		log.Printf("[SessionSync] 检测到 summarization 压缩 - 当前消息=%d\n", currentMsgCount)

		// 将 Eino 内部的压缩后消息同步回 Session（跳过 system 消息）
		compressed := make([]SessionMessage, 0, currentMsgCount)
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
			log.Printf("[SessionSync] 已同步压缩后的消息到 Session - count=%d\n", len(compressed))
		}
	}

	return ctx, nil
}

// AfterModelRewriteState 在每次模型调用后检查消息数量变化，
// 用于检测 summarization 是否发生。
func (m *SessionSyncMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session == nil || state == nil {
		return ctx, state, nil
	}

	currentMsgCount := len(state.Messages)
	if m.initialMsgCount > 0 && currentMsgCount < m.initialMsgCount {
		m.summarizationHit = true
	}
	// 更新跟踪计数
	m.initialMsgCount = currentMsgCount

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
