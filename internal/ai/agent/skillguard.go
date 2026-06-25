// skillguard.go — Skill 安全守卫中间件
//
// 在 Skill 执行链中集成三项检查（提升 AI 分析的可靠性和强大性）：
//   1. 依赖检测：skill 工具被调用时，先检查 dependencies（skill/context/permission）
//   2. 命令黑名单：exec_sql / query_data 工具被调用时，先检查 command_blacklist
//   3. 活跃 Skill 追踪：记录当前加载的 Skill 名，供 ToolErrorRecoveryMiddleware 匹配 errorHints
//
// 设计原则：
//   - 检查失败时返回 JSON 结果（而非 error），让 LLM 能看到提示并调整策略
//   - 中断错误（InterruptError）不被拦截，确保 Eino 中断/恢复机制正常工作
//   - 向后兼容：SkillMeta 为 nil 时（旧 SKILL.md）跳过所有检查
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ──────────────────────────────────────────────
// 活跃 Skill 追踪器
// ──────────────────────────────────────────────

// skillContextTracker 追踪当前 Agent Run 期间的活跃 Skill 名。
// 使用可变指针以便在 context 中传递并可修改（context.Value 本身不可变）。
type skillContextTracker struct {
	mu          sync.Mutex
	activeSkill string
}

func (t *skillContextTracker) set(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.activeSkill = name
}

func (t *skillContextTracker) get() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activeSkill
}

type contextKeySkillTracker struct{}

// ──────────────────────────────────────────────
// SkillGuardMiddleware
// ──────────────────────────────────────────────

// SkillGuardMiddleware Skill 安全守卫中间件
//
// 拦截以下工具调用：
//   - skill 工具：检查依赖（dependencies），通过后记录活跃 Skill
//   - exec_sql 工具：检查命令黑名单（command_blacklist）
//   - query_data 工具：检查命令黑名单（command_blacklist）
type SkillGuardMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	Scope    *PermissionScope
	Schemas  []SchemaRef
	ConnID   string
	DBType   string
	DBSchema string
}

// BeforeAgent 在 Agent 开始执行前初始化活跃 Skill 追踪器到 context 中
func (m *SkillGuardMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	ctx = context.WithValue(ctx, contextKeySkillTracker{}, &skillContextTracker{})
	return ctx, runCtx, nil
}

// WrapInvokableToolCall 包装同步工具调用
func (m *SkillGuardMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		// 1. skill 工具：依赖检查 + 活跃 Skill 追踪
		if tCtx.Name == "skill" {
			if blocked := m.checkSkillDependencies(ctx, argumentsInJSON); blocked != "" {
				log.Printf("[SkillGuard] 依赖检查未通过 - args=%s, reason=%s\n",
					truncateArgsForLog(argumentsInJSON), blocked)
				return blocked, nil
			}
			m.trackActiveSkill(ctx, argumentsInJSON)
		}

		// 2. exec_sql / query_data 工具：命令黑名单检查
		if tCtx.Name == "exec_sql" || tCtx.Name == "query_data" {
			if blocked := m.checkBlacklist(argumentsInJSON); blocked != "" {
				log.Printf("[SkillGuard] 命令黑名单拦截 - tool=%s, args=%s\n",
					tCtx.Name, truncateArgsForLog(argumentsInJSON))
				return blocked, nil
			}
		}

		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

// WrapStreamableToolCall 包装流式工具调用
func (m *SkillGuardMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		// 1. skill 工具：依赖检查 + 活跃 Skill 追踪
		if tCtx.Name == "skill" {
			if blocked := m.checkSkillDependencies(ctx, argumentsInJSON); blocked != "" {
				log.Printf("[SkillGuard:Stream] 依赖检查未通过 - args=%s, reason=%s\n",
					truncateArgsForLog(argumentsInJSON), blocked)
				return schema.StreamReaderFromArray([]string{blocked}), nil
			}
			m.trackActiveSkill(ctx, argumentsInJSON)
		}

		// 2. exec_sql / query_data 工具：命令黑名单检查
		if tCtx.Name == "exec_sql" || tCtx.Name == "query_data" {
			if blocked := m.checkBlacklist(argumentsInJSON); blocked != "" {
				log.Printf("[SkillGuard:Stream] 命令黑名单拦截 - tool=%s, args=%s\n",
					tCtx.Name, truncateArgsForLog(argumentsInJSON))
				return schema.StreamReaderFromArray([]string{blocked}), nil
			}
		}

		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

// checkSkillDependencies 检查 skill 工具调用的依赖。
// 返回空字符串表示依赖满足；返回非空字符串表示依赖不满足（JSON 错误结果，直接返回给 LLM）。
func (m *SkillGuardMiddleware) checkSkillDependencies(ctx context.Context, argumentsInJSON string) string {
	skillName := extractSkillNameFromArgs(argumentsInJSON)
	if skillName == "" {
		return ""
	}
	meta := globalSkillMetaRegistry.GetSkillMeta(skillName)
	if meta == nil {
		// 未注册元信息（旧 SKILL.md），跳过依赖检查
		return ""
	}

	checkCtx := &SkillCheckContext{
		ConnID:          m.ConnID,
		Scope:           m.Scope,
		Schemas:         m.Schemas,
		DBType:          m.DBType,
		DBSchema:        m.DBSchema,
		AvailableSkills: globalSkillMetaRegistry.AvailableSkillNames(),
	}

	if err := CheckDependencies(meta, checkCtx); err != nil {
		return formatSkillGuardBlock("dependency_check", skillName, err.Error())
	}
	return ""
}

// checkBlacklist 检查 SQL 是否命中命令黑名单。
// 返回空字符串表示未命中；返回非空字符串表示命中（JSON 错误结果，直接返回给 LLM）。
func (m *SkillGuardMiddleware) checkBlacklist(argumentsInJSON string) string {
	sql := extractSQLFromAnyArgs(argumentsInJSON)
	if sql == "" {
		return ""
	}
	blacklist := globalSkillMetaRegistry.GlobalCommandBlacklist()
	if err := CheckCommandBlacklist(sql, blacklist); err != nil {
		return formatSkillGuardBlock("command_blacklist", "", err.Error())
	}
	return ""
}

// trackActiveSkill 从 skill 工具参数中提取 Skill 名并记录到追踪器
func (m *SkillGuardMiddleware) trackActiveSkill(ctx context.Context, argumentsInJSON string) {
	skillName := extractSkillNameFromArgs(argumentsInJSON)
	if skillName == "" {
		return
	}
	if tracker, ok := ctx.Value(contextKeySkillTracker{}).(*skillContextTracker); ok {
		tracker.set(skillName)
		log.Printf("[SkillGuard] 活跃 Skill 已记录 - name=%s\n", skillName)
	}
}

// getActiveSkillFromContext 从 context 中获取当前活跃的 Skill 名
func getActiveSkillFromContext(ctx context.Context) string {
	if tracker, ok := ctx.Value(contextKeySkillTracker{}).(*skillContextTracker); ok {
		return tracker.get()
	}
	return ""
}

// ──────────────────────────────────────────────
// 参数提取辅助函数
// ──────────────────────────────────────────────

// extractSkillNameFromArgs 从 skill 工具的 JSON 参数中提取 Skill 名。
// Eino skill 中间件的 skill 工具接受 {"name": "<skill_name>"} 或类似参数。
func extractSkillNameFromArgs(argumentsInJSON string) string {
	if argumentsInJSON == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &parsed); err != nil {
		return ""
	}
	// 尝试常见字段名
	for _, key := range []string{"name", "skill", "skill_name", "skillName"} {
		if v, ok := parsed[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// extractSQLFromAnyArgs 从工具参数中提取 SQL 语句。
// 支持 exec_sql（ExecInput）和 query_data（QueryInput）的参数格式。
func extractSQLFromAnyArgs(argumentsInJSON string) string {
	if argumentsInJSON == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &parsed); err != nil {
		return ""
	}
	if v, ok := parsed["sql"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// formatSkillGuardBlock 格式化 SkillGuard 拦截结果为 JSON，供 LLM 阅读
func formatSkillGuardBlock(checkType, skillName, message string) string {
	detail := ""
	if skillName != "" {
		detail = fmt.Sprintf(`,"skill":"%s"`, escapeJSONString(skillName))
	}
	return fmt.Sprintf(
		`{"error":true,"blocked_by":"skill_guard","check_type":"%s"%s,"message":"%s","recovery_hint":"请根据上述提示调整策略：修正依赖条件或换用其他工具/Skill。命令黑名单拦截的 SQL 无法通过确认绕过。"}`,
		checkType, detail, escapeJSONString(message),
	)
}
