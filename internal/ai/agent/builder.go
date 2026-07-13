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
	"github.com/cloudwego/eino/adk/middlewares/agentsmd"
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
		// 不 return error，Agent 仍可运行（无 skill/execute 工具，使用 Go 原生导出兜底）
		// 系统提示词会根据 skillEnv 是否为 nil 动态调整
	}

	// 初始化 Skill 元信息注册中心（版本管理、依赖检测、错误提示、命令黑名单）
	// 即使 SkillEnv 初始化失败，也尝试加载元信息（用于全局 errorHints 匹配）
	InitSkillMetaRegistry(skillsDir)

	// 检查所有 Skill 的版本兼容性（降级模式：仅记录警告，不阻止运行）
	if warnings := CheckVersionCompatibility(AgentVersion, globalSkillMetaRegistry.AllMetas()); len(warnings) > 0 {
		for _, w := range warnings {
			log.Printf("[Agent] Skill 版本兼容性警告 - %s\n", w)
		}
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
		// UseModelToolSearch=false：使用客户端工具搜索模式。
		// 原因：当前 eino-ext 的 OpenAI/Ollama 模型实现（v0.1.13/v0.1.9）不支持
		// model.WithDeferredTools 和 model.WithToolSearchTool option，底层 go-openai
		// 库的 Tool 结构也无 defer_loading 字段。若设为 true，动态工具会被移入
		// DeferredToolInfos 后被模型静默丢弃，导致导出/写操作工具完全不可用。
		// 客户端搜索模式通过每次模型调用前过滤 ToolInfos 管理工具可见性，
		// 唯一缺点是 KV-cache 失效，但对当前工具数量影响有限。
		// 当未来模型实现支持 WithDeferredTools 时，可改为 true 以提升缓存命中率。
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
		&SkillGuardMiddleware{
			Scope:    scope,
			Schemas:  schemas,
			ConnID:    connID,
			DBType:   dbType,
			DBSchema: dbSchema,
		},
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

	// AgentsMD 中间件：在每次模型调用前注入 Agents.md 补充指令。
	// 必须在 summarization 之后注册，使注入内容不被摘要压缩。
	// 内容是瞬态的（不进入会话状态），每次模型调用都看到完整最新规范。
	agentsmdMW := buildAgentsMDMiddleware(ctx)
	if agentsmdMW != nil {
		handlers = append(handlers, agentsmdMW)
	}

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
		Instruction: buildSystemPrompt(connID, dbType, dbSchema, dbVersion, nil, scope, schemas, skillEnv != nil),
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

// buildAgentsMDMiddleware 创建 AgentsMD 中间件，在每次模型调用前注入 Agents.md 补充指令。
// Agents.md 位于项目根目录，包含数据安全规范、SQL 性能规范、方言兼容性提醒等运行时指南。
// 内容是瞬态的（不进入会话状态），不会被 summarization 压缩。
// 返回 nil 表示创建失败或文件不存在（非致命，Agent 仍可正常运行）。
func buildAgentsMDMiddleware(ctx context.Context) adk.ChatModelAgentMiddleware {
	// 解析 Agents.md 的绝对路径（相对于工作目录）
	agentsMDPath := "Agents.md"
	if absPath, err := filepath.Abs(agentsMDPath); err == nil {
		agentsMDPath = absPath
	}

	// 使用 export.OSFilesystemBackend 作为 agentsmd.Backend（只需 Read 方法）
	backend := export.NewOSFilesystemBackend()

	mw, err := agentsmd.New(ctx, &agentsmd.Config{
		Backend:        backend,
		AgentsMDFiles:  []string{agentsMDPath},
		AllAgentsMDMaxBytes: 100_000, // 100KB 上限
		OnLoadWarning: func(filePath string, err error) {
			log.Printf("[AgentsMD] 加载警告 - file=%s, err=%v\n", filePath, err)
		},
	})
	if err != nil {
		log.Printf("[Agent] 创建 AgentsMD 中间件失败 - err=%v\n", err)
		return nil
	}

	log.Printf("[Agent] AgentsMD 中间件已启用 - file=%s\n", agentsMDPath)
	return mw
}
