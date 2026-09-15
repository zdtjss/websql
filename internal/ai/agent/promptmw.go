// promptmw.go — per-run 动态系统提示词中间件
//
// 背景：SQLAgent 是工厂缓存中的共享实例（同一 connID+dbSchema+userID+schemas 复用），
// 而系统提示词需要按每次请求动态构建（tableContext、上传文件、历史执行引导等均为 per-run 上下文）。
// eino ADK 的正统做法是通过 ChatModelAgentMiddleware.BeforeAgent 改写
// ChatModelAgentContext.Instruction，框架会在 defaultGenModelInput 中将其转换为本次 Run 的系统消息。
//
// 并发安全设计（重要）：
// per-run 数据通过 **context 传递**，不再存放在中间件实例字段里。
// 历史实现用 SetPrompt 把提示词存在中间件字段上，同一 Agent 被多标签/多请求并发使用时
// 会互相覆盖（A 的 tableContext 被 B 的提示词覆盖），本次修复改为 context 穿透：
//   - RunStream 用 WithRunPrompt(ctx, prompt) 注入
//   - BeforeAgent 从 ctx 读取（eino 的 applyBeforeAgent/getRunFunc 使用调用方 ctx，见
//     eino@v0.9.19/adk/chatmodel.go:812、:1421）
// 中间件实例因此变为无状态，可被任意并发 Run 安全共享。
package agent

import (
	"context"

	"github.com/cloudwego/eino/adk"
)

type runPromptCtxKey struct{}

// WithRunPrompt 返回携带「本次运行系统提示词」的 context。
// prompt 为空时原样返回，保证 Resume 等路径不受影响。
func WithRunPrompt(ctx context.Context, prompt string) context.Context {
	if prompt == "" {
		return ctx
	}
	return context.WithValue(ctx, runPromptCtxKey{}, prompt)
}

// runPromptFrom 从 context 取出本次运行的系统提示词。
func runPromptFrom(ctx context.Context) string {
	if prompt, ok := ctx.Value(runPromptCtxKey{}).(string); ok {
		return prompt
	}
	return ""
}

// DynamicPromptMiddleware 在 BeforeAgent 阶段用本次运行的系统提示词覆盖 Instruction。
// 无状态：提示词来自 context；context 未携带时保持原 Instruction 不变
// （Resume 恢复路径依赖 checkpoint 中已有的系统消息，无需重新注入）。
type DynamicPromptMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func NewDynamicPromptMiddleware() *DynamicPromptMiddleware {
	return &DynamicPromptMiddleware{}
}

func (m *DynamicPromptMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	if prompt := runPromptFrom(ctx); prompt != "" {
		runCtx.Instruction = prompt
	}
	return ctx, runCtx, nil
}