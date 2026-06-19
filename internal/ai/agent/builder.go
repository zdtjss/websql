// builder.go — SQLAgent 构造器与构建辅助函数（从 agent.go 拆分而来）
package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/ai/agent/export"
	system "websql/internal/app/system"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch"
	"github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

func computeSummarizationTrigger(cfg *system.AIConfig) int {
	ctxTokens := cfg.MaxContextTokens
	if ctxTokens <= 0 {
		ctxTokens = defaultContextTokens
	}
	return ctxTokens * 85 / 100
}

func NewSQLAgent(ctx context.Context, cfg *system.AIConfig, connID, dbType, dbSchema, dbVersion string, schemas []SchemaRef, sessions *SessionStore, scope *PermissionScope, auditCtx *ExecAuditCtx) (*SQLAgent, error) {
	skillsDir := os.Getenv("SKILLS_DIR")
	if skillsDir == "" {
		cwd, _ := os.Getwd()
		skillsDir = filepath.Join(cwd, "skills")
	}
	if err := export.InitSkillEnv(ctx, skillsDir); err != nil {
		log.Printf("[Agent] 初始化 Skill 环境失败 - err=%v\n", err)
	}

	cm, err := BuildChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建模型失败：%w", err)
	}
	coreTools, deferredTools, err := buildTools(ctx, connID, dbType, dbSchema, schemas, auditCtx, scope)
	if err != nil {
		return nil, fmt.Errorf("创建工具失败：%w", err)
	}

	triggerTokens := computeSummarizationTrigger(cfg)

	summarizationMW, err := summarization.New(ctx, &summarization.Config{
		Model: cm,
		Trigger: &summarization.TriggerCondition{
			ContextTokens:   triggerTokens,
			ContextMessages: 200,
		},
		TokenCounter: estimateTokenCount,
	})

	if err != nil {
		log.Printf("[Agent] 创建摘要中间件失败 - err=%v\n", err)
		return nil, fmt.Errorf("创建摘要中间件失败：%w", err)
	}

	skillEnv := export.GetSkillEnv()

	var fsm adk.ChatModelAgentMiddleware
	var sm adk.ChatModelAgentMiddleware

	if skillEnv != nil {
		fsm, err = filesystem.New(ctx, &filesystem.MiddlewareConfig{
			Backend:        skillEnv.FilesystemBackend(),
			StreamingShell: skillEnv.FilesystemBackend(),
		})
		if err != nil {
			log.Printf("[Agent] 创建 Filesystem 中间件失败 - err=%v\n", err)
			return nil, fmt.Errorf("创建文件系统中间件失败：%w", err)
		}

		sm, err = skill.NewMiddleware(ctx, &skill.Config{
			Backend: skillEnv.Backend(),
		})
		if err != nil {
			log.Printf("[Agent] 创建 Skill 中间件失败 - err=%v\n", err)
			return nil, fmt.Errorf("创建 Skill 中间件失败：%w", err)
		}
	}

	var permAgentTool tool.BaseTool
	if scope.IsRemote && !scope.HasFullConnAccess {
		permSchemaNames := buildSchemaNames(schemas)
		if len(permSchemaNames) == 0 && dbSchema != "" {
			permSchemaNames = []string{dbSchema}
		}
		permAgentTool, err = GetPermissionAgentCache().GetOrCreate(ctx, cfg, connID, dbType, dbSchema, scope.UserID, permSchemaNames)
		if err != nil {
			log.Printf("[Agent] 创建权限审核 Agent 失败，回退到程序化检查 - err=%v\n", err)
			permAgentTool = nil
		}
	}

	var tsMiddleware adk.ChatModelAgentMiddleware
	if len(deferredTools) > 0 {
		tsMiddleware, err = toolsearch.New(ctx, &toolsearch.Config{
			DynamicTools:       deferredTools,
			UseModelToolSearch: false,
		})
		if err != nil {
			log.Printf("[Agent] 创建 ToolSearch 中间件失败 - err=%v\n", err)
			coreTools = append(coreTools, deferredTools...)
			deferredTools = nil
		}
	}

	handlers := []adk.ChatModelAgentMiddleware{
		// patchtoolcalls 必须在最外层，确保每次 LLM 调用前 dangling tool_calls 被补占位
		// 防止历史会话（崩溃/cancel 后）发送 dangling tool_calls 给 LLM
		// 使用自定义 PatchedContentGenerator，把"工具未完成"信息以更友好的
		// 形式呈现给 LLM，便于下一轮模型理解上下文
		buildPatchToolCallsMiddleware(),
		&ToolCallLoggingMiddleware{},
	}
	if tsMiddleware != nil {
		handlers = append(handlers, tsMiddleware)
	}
	handlers = append(handlers,
		&PermissionMiddleware{Scope: scope, PermAgent: permAgentTool},
		&DangerousSQLApprovalMiddleware{},
		&ToolErrorRecoveryMiddleware{},
	)
	// 替换自定义 ReductionMiddleware 为 eino 官方的 reduction 中间件
	// 官方实现支持 Offload to File + 完整的 clear 策略，既能裁剪大结果，
	// 又能保留流式语义（解决自实现 ReductionMiddleware "Recv 全部再切片"
	// 破坏流式 UX 的问题）。
	if reductionMW := buildEinoReductionMiddleware(); reductionMW != nil {
		handlers = append(handlers, reductionMW)
	}
	handlers = append(handlers, summarizationMW)

	// SessionSyncMiddleware：对接 Eino Memory/Session，
	// 在 summarization 压缩消息后自动同步到 SessionStore
	sessionSyncMW := &SessionSyncMiddleware{}
	handlers = append(handlers, sessionSyncMW)

	if fsm != nil {
		handlers = append(handlers, fsm)
	}
	if sm != nil {
		handlers = append(handlers, sm)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SQLAgent",
		Description: "专业 SQL 助手，支持跨库查询、多 Schema 数据组合分析、数据导入导出和报告生成",
		Instruction: buildSystemPrompt(connID, dbType, dbSchema, dbVersion, nil, scope, schemas),
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: coreTools},
		},
		Handlers:      handlers,
		MaxIterations: maxIterations,
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries:  3,
			ShouldRetry: buildShouldRetryFunc(),
		},
	})

	if err != nil {
		return nil, fmt.Errorf("创建 Agent 失败：%w", err)
	}

	// 创建 Runner，配置 CheckPointStore 用于中断状态持久化
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
		CheckPointStore: globalCheckPointStore,
	})

	if sessions == nil {
		sessions, _ = NewSessionStore()
	}

	resolvedCtxTokens := cfg.MaxContextTokens
	if resolvedCtxTokens <= 0 {
		resolvedCtxTokens = defaultContextTokens
	}

	return &SQLAgent{runner: runner, agent: agent, sessions: sessions, dbType: dbType, dbSchema: dbSchema, dbVersion: dbVersion, scope: scope, schemas: schemas, maxContextTokens: resolvedCtxTokens, sessionSync: sessionSyncMW}, nil
}

// buildShouldRetryFunc 构建 v0.9 ShouldRetry 决策函数。
// 相比旧的 IsRetryAble，ShouldRetry 可以读取模型输出、拒绝不满足条件的输出、
// 修改下一次输入、追加模型 option，并覆盖 backoff。
func buildShouldRetryFunc() func(ctx context.Context, retryCtx *adk.RetryContext) *adk.RetryDecision {
	return func(ctx context.Context, retryCtx *adk.RetryContext) *adk.RetryDecision {
		// 有错误时：根据错误类型决定是否重试
		if retryCtx.Err != nil {
			s := retryCtx.Err.Error()

			// 不可重试的错误：内容安全过滤、认证失败
			if strings.Contains(s, "content_filter") ||
				strings.Contains(s, "content_policy") ||
				strings.Contains(s, "safety") ||
				strings.Contains(s, "401") ||
				strings.Contains(s, "403") ||
				strings.Contains(s, "invalid_api_key") {
				log.Printf("[ShouldRetry] 不可重试错误 - err=%s\n", s)
				return &adk.RetryDecision{Retry: false}
			}

			// 可重试的错误：速率限制、服务端错误、网络问题
			isRetryable := strings.Contains(s, "429") ||
				strings.Contains(s, "500") ||
				strings.Contains(s, "502") ||
				strings.Contains(s, "503") ||
				strings.Contains(s, "504") ||
				strings.Contains(s, "timeout") ||
				strings.Contains(s, "connection") ||
				strings.Contains(s, "rate limit") ||
				strings.Contains(s, "too many requests") ||
				strings.Contains(s, "stream") ||
				strings.Contains(s, "EOF")

			if isRetryable {
				// 对 429 使用更长的退避时间
				if strings.Contains(s, "429") || strings.Contains(s, "rate limit") || strings.Contains(s, "too many requests") {
					backoff := time.Duration(retryCtx.RetryAttempt+1) * 3 * time.Second
					log.Printf("[ShouldRetry] 速率限制，退避 %v - attempt=%d\n", backoff, retryCtx.RetryAttempt)
					return &adk.RetryDecision{Retry: true, Backoff: backoff}
				}
				backoff := time.Duration(retryCtx.RetryAttempt+1) * time.Second
				log.Printf("[ShouldRetry] 可重试错误，退避 %v - attempt=%d, err=%s\n", backoff, retryCtx.RetryAttempt, s)
				return &adk.RetryDecision{Retry: true, Backoff: backoff}
			}

			log.Printf("[ShouldRetry] 未知错误，不重试 - err=%s\n", s)
			return &adk.RetryDecision{Retry: false}
		}

		// 无错误但有输出时：检查模型输出质量
		if retryCtx.OutputMessage != nil && retryCtx.OutputMessage.Content == "" && len(retryCtx.OutputMessage.ToolCalls) == 0 {
			// 模型返回了空响应（无内容也无工具调用），重试
			log.Printf("[ShouldRetry] 模型返回空响应，重试 - attempt=%d\n", retryCtx.RetryAttempt)
			return &adk.RetryDecision{Retry: true, Backoff: time.Second}
		}

		return &adk.RetryDecision{Retry: false}
	}
}
