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
	adkfilesystem "github.com/cloudwego/eino/adk/filesystem"
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
		// 压缩后保留完整 transcript 路径，细节可按需回读。
		Finalize: buildTranscriptFinalize(),
	})

	if err != nil {
		log.Printf("[Agent] 创建摘要中间件失败 - err=%v\n", err)
		return nil, fmt.Errorf("创建摘要中间件失败：%w", err)
	}

	skillEnv := export.GetSkillEnv()

	var fsm adk.ChatModelAgentMiddleware
	var sm adk.ChatModelAgentMiddleware

	if skillEnv != nil {
		// 按用户克隆后端：Agent 按用户缓存，文件沙箱根目录随用户隔离。
		osBackend := skillEnv.FilesystemBackend().CloneForUser(scope.UserID)
		// 上传文件以 /uploads/<fileId>/... 虚拟路径暴露给 read_file/grep/glob；
		// 其余路径仍由 OSFilesystemBackend 处理（skills/exports/workspace）。
		var fsBackend adkfilesystem.Backend = osBackend
		if os.Getenv("AGENT_UPLOAD_FS") != "0" {
			fsBackend = newUploadFSBackend(osBackend, scope.UserID)
		}
		fsm, err = filesystem.New(ctx, &filesystem.MiddlewareConfig{
			Backend:        fsBackend,
			StreamingShell: osBackend,
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

	// 工具直接静态暴露：核心工具 + 导出/写工具全部进入 ToolInfos。
	//
	// 不再使用 toolsearch 客户端搜索模式：
	//   - 该模式要求模型先调 tool_search 才能看到延迟工具，而 tool_search 未登记
	//     ToolPolicy 会被执行层拒绝，导致导出/写工具不可发现；
	//   - 延迟工具的参数 schema 不进入模型，模型只能猜参数；
	//   - 当前工具总量（≤16）远低于需要动态发现的程度，静态暴露的 token 成本
	//     低于 tool_search 自身 2.2k 字符的描述。
	coreTools = append(coreTools, deferredTools...)
	deferredTools = nil
	log.Printf("[Agent] 工具注册完成 - visible=%d\n", len(coreTools))
	recordAgentMetric("ai_tool_registered_total", fmt.Sprint(len(coreTools)))
	addAgentMetricValue("ai_tool_schema_tokens", int64(countToolSchemaTokensFromBaseTools(ctx, coreTools)), "visible")

	// promptMW 从 context 读取 per-run 系统提示词，在 BeforeAgent 阶段覆盖 Instruction。
	promptMW := NewDynamicPromptMiddleware()

	resolvedCtxTokens := cfg.MaxContextTokens
	if resolvedCtxTokens <= 0 {
		resolvedCtxTokens = defaultContextTokens
	}
	// reduction 必须在 PermissionMiddleware 外层（handlers[0] 最外层）：
	// permission 先调用内层工具并完成脱敏，reduction 再对脱敏后的结果截断/落盘。
	// 反过来会导致 offload 文件保存原始未脱敏结果。
	reductionMW := buildEinoReductionMiddleware(resolvedCtxTokens)
	var datasetMW adk.ChatModelAgentMiddleware
	if os.Getenv("AGENT_DATASET") != "0" {
		datasetMW = NewDatasetMiddleware()
	}

	// analyze_data 子 agent：在隔离上下文中完成多步数据分析，只返回结论/证据。
	// 失败时降级为不注册，主 agent 仍可直接使用原有工具。
	// 纯文件分析（无连接）不注册：子 agent 的取数/权限链路依赖数据库连接。
	if connID != "" && os.Getenv("AGENT_ANALYZE_DATA") != "0" {
		if analyzeTool, analyzeErr := buildAnalyzeDataTool(ctx, cm, connID, dbType, dbSchema, schemas, auditCtx, scope, permAgentTool, reductionMW, datasetMW); analyzeErr != nil {
			recordAgentMetric("ai_analyze_data_registered", "failed")
			log.Printf("[Agent] 创建 analyze_data 子 agent 失败，已跳过 - err=%v\n", analyzeErr)
		} else if analyzeTool != nil {
			recordAgentMetric("ai_analyze_data_registered", "ok")
			coreTools = append(coreTools, analyzeTool)
		}
	}

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
	handlers = append(handlers,
		// ToolErrorRecovery 必须包在权限/审批/Skill 守卫之外：
		// 工具返回 error 时 eino ToolsNode 会让整次 Run 以错误结束（compose/tool_node.go），
		// 权限拒绝（PermissionError）若不转换为工具结果，用户看到的是「AI 处理出错」，
		// 模型也无法向用户解释缺少哪张表/哪个字段的权限。中断信号（审批）会原样透传。
		&ToolErrorRecoveryMiddleware{},
	)
	if reductionMW != nil {
		handlers = append(handlers, reductionMW)
	}
	if datasetMW != nil {
		handlers = append(handlers, datasetMW)
	}
	handlers = append(handlers,
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
	handlers = append(handlers, &InjectedContextFilterMiddleware{}, summarizationMW)

	// Agents.md 已并入系统提示词（buildAgentsMDSection），不再作为 User 消息注入：
	//   - 规范以 system 优先级生效，不会被后续用户消息覆盖；
	//   - 不会进入 summarization 输入/transcript/会话历史；
	//   - 每次 Run 重新读取磁盘文件，仍支持运行时热更新。

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

	return &SQLAgent{runner: runner, sessions: sessions, dbType: dbType, dbSchema: dbSchema, dbVersion: dbVersion, scope: scope, schemas: schemas, maxContextTokens: resolvedCtxTokens, sessionSync: sessionSyncMW}, nil
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
