// promptmw.go — per-run 动态系统提示词中间件
//
// 背景：SQLAgent 是工厂缓存中的共享实例，而系统提示词需要按每次请求动态构建
// （tableContext、上传文件、历史执行引导等均为 per-run 上下文）。
// eino ADK 提供的正统做法是通过 ChatModelAgentMiddleware.BeforeAgent 改写
// ChatModelAgentContext.Instruction，框架会在 defaultGenModelInput 中将其转换为
// 本次 Run 的系统消息（拼接在输入消息之前），而非在输入消息里手动携带 System 消息。
//
// 好处：
//   - 系统提示词只注入一次，不会出现「Instruction 一份 + 手动 System 消息一份」的双份冗余
//   - per-run 上下文（如 tableContext）不会在构造期与运行期产生不一致的矛盾描述
//
// 并发说明：与 SessionSyncMiddleware 相同的模式——RunStream 在调用 runner.Run 前
// SetPrompt，BeforeAgent 在 Run 内同步执行（getRunFunc → applyBeforeAgent 为同步调用，
// Run 返回前已完成），SetPrompt 使用互斥锁保护读写。
package agent

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/adk"
)

// DynamicPromptMiddleware 持有 per-run 系统提示词，在 BeforeAgent 阶段覆盖 Agent 的
// Instruction。未 SetPrompt 时（如 Resume 恢复路径）不修改原 Instruction。
type DynamicPromptMiddleware struct {
	*adk.BaseChatModelAgentMiddleware

	mu     sync.Mutex
	prompt string
}

// NewDynamicPromptMiddleware 创建动态提示词中间件
func NewDynamicPromptMiddleware() *DynamicPromptMiddleware {
	return &DynamicPromptMiddleware{}
}

// SetPrompt 设置下一次 Run 使用的系统提示词（每次 RunStream 调用前设置，覆盖旧值）
func (m *DynamicPromptMiddleware) SetPrompt(prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompt = prompt
}

// GetPrompt 返回当前持有的提示词（用于诊断日志）
func (m *DynamicPromptMiddleware) GetPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prompt
}

// BeforeAgent 在每次 Run 前将持有的提示词覆盖到 Instruction。
// 提示词为空时保持原 Instruction 不变（Resume 恢复路径依赖 checkpoint 中
// 已有的系统消息，无需重新注入）。
func (m *DynamicPromptMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	m.mu.Lock()
	prompt := m.prompt
	m.mu.Unlock()

	if prompt != "" {
		runCtx.Instruction = prompt
	}
	return ctx, runCtx, nil
}
