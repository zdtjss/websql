// builder.go — SQLAgent 构造器与构建辅助函数（从 agent.go 拆分而来）
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/ai/agent/export"
	system "websql/internal/app/system"

	"github.com/cloudwego/eino-ext/components/model/openai"
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
		TokenCounter: CountForSummarization,
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
	// 未选择数据库连接（纯文件分析）时无需构建数据库权限审核 Agent
	if scope.IsRemote && !scope.HasFullConnAccess && connID != "" {
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

	// promptMW 持有 per-run 系统提示词，RunStream 每次调用前 SetPrompt
	promptMW := NewDynamicPromptMiddleware()

	// 中间件执行顺序（eino v0.9.21，adk/wrappers.go）：
	//   - BeforeAgent / BeforeModelRewriteState / AfterModelRewriteState：按注册顺序正序执行
	//   - 工具包裹（Wrap*ToolCall）：handlers[0] 在最外层
	handlers := []adk.ChatModelAgentMiddleware{
		// DynamicPromptMiddleware 必须最先注册：在每次 Run 前将 RunStream 按请求构建的
		// 系统提示词（含 tableContext / 上传文件 / 历史引导等 per-run 上下文）覆盖到 Instruction。
		// Agent 构造期不再设置 Instruction，避免与动态提示词双份注入。
		promptMW,
	}
	// patchtoolcalls 在每次 LLM 调用前为 dangling tool_calls 补占位，
	// 防止历史会话（崩溃/cancel 后）把不完整的 tool_calls 发给 LLM。
	// 构造失败返回 nil：nil 接口进入 handlers 会在首次调用钩子时 panic，必须判空。
	if patchMW := buildPatchToolCallsMiddleware(); patchMW != nil {
		handlers = append(handlers, patchMW)
	}
	handlers = append(handlers, &ToolCallLoggingMiddleware{})
	if tsMiddleware != nil {
		handlers = append(handlers, tsMiddleware)
	}
	handlers = append(handlers,
		// ToolErrorRecovery 必须包在权限/审批/Skill 守卫之外：
		// 工具返回 error 时 eino ToolsNode 会让整次 Run 以错误结束（compose/tool_node.go），
		// 权限拒绝（PermissionError）若不转换为工具结果，用户看到的是「AI 处理出错」，
		// 模型也无法向用户解释缺少哪张表/哪个字段的权限。中断信号（审批）会原样透传。
		&ToolErrorRecoveryMiddleware{},
		&PermissionMiddleware{Scope: scope, PermAgent: permAgentTool, ConnID: connID, Schemas: schemas},
		&DangerousSQLApprovalMiddleware{},
		&SkillGuardMiddleware{
			Scope:    scope,
			Schemas:  schemas,
			ConnID:   connID,
			DBType:   dbType,
			DBSchema: dbSchema,
		},
	)
	resolvedCtxTokens := cfg.MaxContextTokens
	if resolvedCtxTokens <= 0 {
		resolvedCtxTokens = defaultContextTokens
	}

	// 替换自定义 ReductionMiddleware 为 eino 官方的 reduction 中间件
	// 官方实现支持 Offload to File + 完整的 clear 策略，既能裁剪大结果，
	// 又能保留流式语义（解决自实现 ReductionMiddleware "Recv 全部再切片"
	// 破坏流式 UX 的问题）。
	// clear 阈值与模型上下文窗口联动（见 buildEinoReductionMiddleware）。
	if reductionMW := buildEinoReductionMiddleware(resolvedCtxTokens); reductionMW != nil {
		handlers = append(handlers, reductionMW)
	}
	handlers = append(handlers, summarizationMW)

	// AgentsMD 中间件：在每次模型调用前注入 Agents.md 补充指令。
	// 必须在 summarization 之后注册，使注入内容排在压缩之后。
	// 注入消息会进入 eino state.Messages，但会话持久化层按 Extra 标记过滤（见 sessionmw.go），
	// 不会落库累积；每次模型调用都看到完整最新规范。
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
		// Instruction 留空：系统提示词按请求动态构建，由 DynamicPromptMiddleware
		// 在 BeforeAgent 阶段注入（见 promptmw.go）。避免构造期静态 Instruction
		// 与运行期动态提示词双份发送、以及 tableContext 缺失导致的矛盾描述。
		Instruction: "",
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

	return &SQLAgent{runner: runner, agent: agent, sessions: sessions, dbType: dbType, dbSchema: dbSchema, dbVersion: dbVersion, scope: scope, schemas: schemas, maxContextTokens: resolvedCtxTokens, sessionSync: sessionSyncMW, promptMW: promptMW}, nil
}

// buildShouldRetryFunc 构建 v0.9 ShouldRetry 决策函数。
// 相比旧的 IsRetryAble，ShouldRetry 可以读取模型输出、拒绝不满足条件的输出、
// 修改下一次输入、追加模型 option，并覆盖 backoff。
func buildShouldRetryFunc() func(ctx context.Context, retryCtx *adk.RetryContext) *adk.RetryDecision {
	return func(ctx context.Context, retryCtx *adk.RetryContext) *adk.RetryDecision {
		// 有错误时：根据错误类型决定是否重试
		if retryCtx.Err != nil {
			// 优先按 provider 结构化错误判定（OpenAI 兼容 APIError 携带 HTTP 状态码），
			// 避免子串匹配（"500"/"connection"）把非状态码场景误判为可重试
			var apiErr *openai.APIError
			if errors.As(retryCtx.Err, &apiErr) && apiErr.HTTPStatusCode > 0 {
				code := apiErr.HTTPStatusCode
				switch {
				case code == 429:
					backoff := time.Duration(retryCtx.RetryAttempt+1) * 3 * time.Second
					log.Printf("[ShouldRetry] HTTP 429 速率限制，退避 %v - attempt=%d\n", backoff, retryCtx.RetryAttempt)
					return &adk.RetryDecision{Retry: true, Backoff: backoff}
				case code >= 500, code == 408, code == 409:
					backoff := time.Duration(retryCtx.RetryAttempt+1) * time.Second
					log.Printf("[ShouldRetry] HTTP %d 服务端错误，退避 %v - attempt=%d\n", code, backoff, retryCtx.RetryAttempt)
					return &adk.RetryDecision{Retry: true, Backoff: backoff}
				default:
					log.Printf("[ShouldRetry] HTTP %d 不可重试\n", code)
					return &adk.RetryDecision{Retry: false}
				}
			}

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

// AgentsMD 中间件：在每次模型调用前注入 Agents.md 补充指令。
// Agents.md 位于项目根目录，包含数据安全规范、SQL 性能规范、方言兼容性提醒等运行时指南。
// 必须在 summarization 之后注册，使注入内容排在压缩之后；若被摘要压缩丢弃，
// 下一次模型调用会重新注入，模型始终能看到完整最新规范。
//
// 事实澄清（已核对 eino v0.9.21 源码）：注入的是一条 User 消息且会写入 state.Messages
// （并非"完全不进入状态"）；会话持久化由 sessionmw.go 按 Extra["__agentsmd_content__"] 标记过滤，
// 因此不会落库、不会在下次加载时重复累积。
//
// 读取优先级：
//  1. 磁盘文件（工作目录下的 AGENTS.md）— 支持运行时热更新
//  2. 编译时嵌入的内容（_embedded_agents.md）— 确保打包后可用
//
// 返回 nil 表示创建失败或文件不存在（非致命，Agent 仍可正常运行）。
func buildAgentsMDMiddleware(ctx context.Context) adk.ChatModelAgentMiddleware {
	// 解析磁盘路径（可能不存在，backend 会回退到嵌入内容）
	diskPath := "AGENTS.md"
	if absPath, err := filepath.Abs(diskPath); err == nil {
		diskPath = absPath
	}

	// 使用嵌入式后端：优先磁盘 → 回退嵌入
	backend := &embeddedAgentsMDBackend{diskPath: diskPath}

	// agentsmd 中间件需要一个文件路径作为标识（不影响实际读取，因为 backend 会处理）
	mw, err := agentsmd.New(ctx, &agentsmd.Config{
		Backend:             backend,
		AgentsMDFiles:       []string{diskPath},
		AllAgentsMDMaxBytes: 100_000,
		OnLoadWarning: func(filePath string, err error) {
			log.Printf("[AgentsMD] 加载警告 - file=%s, err=%v\n", filePath, err)
		},
	})
	if err != nil {
		log.Printf("[Agent] 创建 AgentsMD 中间件失败 - err=%v\n", err)
		return nil
	}

	// 检测实际来源
	if _, statErr := os.Stat(diskPath); statErr == nil {
		log.Printf("[Agent] AgentsMD 已启用（来源：磁盘文件）- file=%s\n", diskPath)
	} else {
		log.Printf("[Agent] AgentsMD 已启用（来源：嵌入内容，%d 字节）\n", len(embeddedAgentsMD))
	}
	return mw
}
