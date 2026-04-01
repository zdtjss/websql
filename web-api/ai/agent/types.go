// Package agent 基于 Eino 框架重构的 AI SQL 智能体。
// 支持 SQL 生成、数据导出/导入、危险操作确认、会话记忆。
package agent

import "time"

// --- 请求/响应类型 ---

// ChatRequest 统一的聊天请求。
type ChatRequest struct {
	SessionID    string   `json:"sessionId"`
	ConnID       string   `json:"connId"`
	Schema       string   `json:"schema"`
	Question     string   `json:"question"`
	TableContext []string `json:"tableContext"`
	// Confirmed 用户确认执行危险 SQL（前端二次确认后回传）
	Confirmed bool `json:"confirmed,omitempty"`
	// PendingSQL 待确认的 SQL（前端回传）
	PendingSQL string `json:"pendingSQL,omitempty"`
}

// StreamChunk SSE 推送给前端的事件块。
type StreamChunk struct {
	Type       string                 `json:"type"`       // thinking | content | danger_confirm | tool_call | tool_result | done | error
	Content    string                 `json:"content"`    // 文本内容
	SQL        string                 `json:"sql,omitempty"`
	ToolResult map[string]interface{} `json:"toolResult,omitempty"` // 工具执行结果（如导出下载链接）
}

// DangerLevel SQL 危险等级。
type DangerLevel int

const (
	DangerNone    DangerLevel = iota // SELECT 等安全操作
	DangerConfirm                    // ALTER / UPDATE / DELETE / DROP 需确认
)

// --- 会话相关 ---

// Message 会话中的一条消息。
type Message struct {
	Role      string    `json:"role"` // user | assistant | system
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Session 一次完整的对话会话。
type Session struct {
	ID        string    `json:"id"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
