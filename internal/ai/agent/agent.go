// package agent 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/ai/agent/export"
	system "websql/internal/app/system"
	idgen "websql/internal/pkg/idgen"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch"
	"github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// ──────────────────────────────────────────────
// 数据结构
// ──────────────────────────────────────────────

// StreamChunk 流式输出块
type StreamChunk struct {
	Type         string `json:"type"`
	Content      string `json:"content,omitempty"`
	SQL          string `json:"sql,omitempty"`          // 展示用
	InterruptID  string `json:"interruptId,omitempty"`  // Eino 中断地址 ID
	CheckPointID string `json:"checkPointId,omitempty"` // Runner CheckPoint ID
}

// ChatRequest 聊天请求
type ChatRequest struct {
	SessionID    string      `json:"sessionId"`
	UserID       string      `json:"userId"`
	ConnID       string      `json:"connId"`
	Schema       string      `json:"schema"`
	Schemas      []SchemaRef `json:"schemas,omitempty"` // 多 schema 模式
	Question     string      `json:"question"`
	TableContext []string    `json:"tableContext"`
	Confirmed    bool        `json:"confirmed,omitempty"`
	InterruptIDs []string    `json:"interruptIds,omitempty"` // 确认时回传（支持多条）
	CheckPointID string      `json:"checkPointId,omitempty"` // 确认时回传
	ExcelData    *ExcelData  `json:"excelData,omitempty"`
	ModelId      string      `json:"modelId,omitempty"` // 选中的模型 ID
}

// SchemaRef 单个 schema 引用
type SchemaRef struct {
	ConnID string `json:"connId"`
	Schema string `json:"schema"`
}

// ExcelData 前端上传的 Excel 文件信息
type ExcelData struct {
	FileID    string   `json:"fileId"`
	Columns   []string `json:"columns"`
	TotalRows int      `json:"totalRows"`
}

// SessionMeta 会话列表摘要
type SessionMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

// SessionContext 会话上下文（保存当时选择的 schema 和表）
type SessionContext struct {
	Schemas []SchemaRef `json:"schemas,omitempty"`
	Tables  []string    `json:"tables,omitempty"`
}

// SessionDetail 会话详情
type SessionDetail struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	CreatedAt time.Time              `json:"createdAt"`
	Messages  []SessionDetailMessage `json:"messages"`
	Context   *SessionContext        `json:"context,omitempty"`
}

// SessionDetailMessage 会话消息
type SessionDetailMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ──────────────────────────────────────────────
// SQLAgent + Runner
// ──────────────────────────────────────────────

const (
	maxIterations = 50

	defaultContextTokens = 128000
)

func computeSummarizationTrigger(cfg *system.AIConfig) int {
	ctxTokens := cfg.MaxContextTokens
	if ctxTokens <= 0 {
		ctxTokens = defaultContextTokens
	}
	return ctxTokens * 85 / 100
}

// 全局 CheckPointStore（单实例共享）
var globalCheckPointStore = newAutoCheckPointStore()

type SQLAgent struct {
	runner           *adk.Runner
	agent            *adk.ChatModelAgent
	sessions         *SessionStore
	dbType           string
	dbSchema         string
	scope            *PermissionScope
	schemas          []SchemaRef
	maxContextTokens int
	cancelFunc       adk.AgentCancelFunc    // Eino v0.9 Agent Cancel 支持
	sessionSync      *SessionSyncMiddleware // Eino v0.9 会话同步中间件
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
		&ToolCallLoggingMiddleware{},
	}
	if tsMiddleware != nil {
		handlers = append(handlers, tsMiddleware)
	}
	handlers = append(handlers,
		&PermissionMiddleware{Scope: scope, PermAgent: permAgentTool},
		&ReductionMiddleware{},
		&DangerousSQLApprovalMiddleware{},
		&ToolErrorRecoveryMiddleware{},
		summarizationMW,
	)

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

	return &SQLAgent{runner: runner, agent: agent, sessions: sessions, dbType: dbType, dbSchema: dbSchema, scope: scope, schemas: schemas, maxContextTokens: resolvedCtxTokens, sessionSync: sessionSyncMW}, nil
}

// RunStream 流式执行（首次查询）
func (a *SQLAgent) RunStream(ctx context.Context, req ChatRequest, flush func(StreamChunk)) (string, error) {
	log.Printf("[Agent] 开始执行 - sessionID=%s, userID=%s, connID=%s\n", req.SessionID, req.UserID, req.ConnID)

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = idgen.RandomStr()
		log.Printf("[Agent] 新建会话 - sessionID=%s\n", sessionID)
	}
	if req.UserID == "" {
		return "", errors.New("userId 不能为空")
	}

	sess, err := a.sessions.GetOrCreate(sessionID, req.UserID)
	if err != nil {
		return "", err
	}
	flush(StreamChunk{Type: "session", Content: sess.ID})

	if err := sess.Append("user", req.Question); err != nil {
		return sessionID, err
	}

	allMsgs := sess.GetMessages()
	log.Printf("[Agent] 历史消息 - total=%d, maxContextTokens=%d\n", len(allMsgs), a.maxContextTokens)

	if !a.scope.HasAnyAccess() {
		flush(StreamChunk{Type: "error", Content: "您暂时没有可访问的数据表权限，请联系管理员开通。"})
		flush(StreamChunk{Type: "done"})
		return sessionID, nil
	}

	defaultConnID := req.ConnID
	if defaultConnID == "" && len(req.Schemas) > 0 {
		defaultConnID = req.Schemas[0].ConnID
	}
	sysPrompt := buildSystemPrompt(defaultConnID, a.dbType, a.dbSchema, "", req.TableContext, a.scope, req.Schemas)

	if detectPreviousExecution(allMsgs) {
		sysPrompt += "\n\n## 📌 上一轮有查询或写入操作。当用户追问、要求重新操作、要求导出时，" +
			"你必须基于对话历史中实际执行的 tool_calls 参数和 tool 返回结果来回答，" +
			"禁止凭记忆编造。如果历史中没有相关信息，直接告知用户，禁止猜测。\n"
		log.Printf("[Agent] 检测到历史执行记录，追加历史引导\n")
	}

	if req.ExcelData != nil && req.ExcelData.FileID != "" {
		sysPrompt += fmt.Sprintf("\n\n📎 用户上传了 Excel 文件（fileId=%s）：\n- 列名：%s\n- 总行数：%d\n",
			req.ExcelData.FileID, strings.Join(req.ExcelData.Columns, ", "), req.ExcelData.TotalRows)
		sysPrompt += "请按「数据导入流程」操作：先确认目标表，向用户说明操作模式、字段映射和影响行数，等用户确认后再调用 import_data。\n"
	}

	// 构建 Eino 消息列表
	messages := []adk.Message{
		&schema.Message{Role: schema.System, Content: sysPrompt},
	}
	for _, msg := range allMsgs {
		switch msg.Role {
		case "user":
			messages = append(messages, &schema.Message{Role: schema.User, Content: msg.Content})
		case "assistant":
			sm := &schema.Message{Role: schema.Assistant, Content: msg.Content}
			if len(msg.ToolCalls) > 0 {
				sm.ToolCalls = sessionToolCallsToSchema(msg.ToolCalls)
			}
			messages = append(messages, sm)
		case "tool":
			messages = append(messages, &schema.Message{
				Role:       schema.Tool,
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
				ToolName:   msg.ToolName,
			})
		}
	}

	log.Printf("[Agent] LLM 输入消息 - total=%d\n", len(messages))
	for i, msg := range messages {
		role := msg.Role
		contentLen := len(msg.Content)
		tcCount := len(msg.ToolCalls)
		toolCallID := msg.ToolCallID
		toolName := msg.ToolName
		if role == schema.System {
			log.Printf("[Agent]   msg[%d] - role=%s, contentLen=%d\n", i, role, contentLen)
		} else {
			log.Printf("[Agent]   msg[%d] - role=%s, contentLen=%d, toolCalls=%d, toolCallID=%s, toolName=%s\n",
				i, role, contentLen, tcCount, toolCallID, toolName)
		}
	}

	checkPointID := fmt.Sprintf("cp_%s_%d", sessionID, time.Now().UnixMilli())

	// 使用 Eino v0.9 Agent Cancel：通过 WithCancel 获取 AgentRunOption 和 CancelFunc，
	// 支持安全点取消（等待当前工具调用完成后再取消）
	cancelOpt, cancelFn := adk.WithCancel()
	a.cancelFunc = cancelFn

	// 绑定 SessionSyncMiddleware，使 summarization 压缩后的消息能同步到 Session
	if a.sessionSync != nil {
		a.sessionSync.SetSession(sess, len(allMsgs))
	}

	iter := a.runner.Run(ctx, messages, adk.WithCheckPointID(checkPointID), cancelOpt)

	_, _ = a.processEvents(iter, flush, sess, checkPointID)

	// 清理 cancelFunc 和 session 绑定
	a.cancelFunc = nil
	if a.sessionSync != nil {
		a.sessionSync.ClearSession()
	}

	if err := sess.SaveToDB(); err != nil {
		log.Printf("[Agent] 保存会话失败 - err=%v\n", err)
	}

	log.Printf("[Agent] 执行完毕 - sessionID=%s\n", sessionID)

	return sessionID, nil
}

// Cancel 主动取消正在运行的 Agent（Eino v0.9 安全点取消）
func (a *SQLAgent) Cancel() {
	if a.cancelFunc != nil {
		log.Printf("[Agent] 触发 Agent Cancel\n")
		a.cancelFunc()
	}
}

// ResumeStream 恢复被中断的执行（用户确认/取消后）
// 当再次被中断时（如 LLM 生成了新的危险 SQL），不发送 done，让前端继续等待用户确认
func (a *SQLAgent) ResumeStream(ctx context.Context, checkPointID string, targets map[string]bool, flush func(StreamChunk), sess *Session) error {
	log.Printf("[Agent] resume 开始 - cpID=%s, targets=%v, sessionMsgs=%d\n", checkPointID, targets, len(sess.messages))

	targetsAny := make(map[string]any, len(targets))
	for id, approved := range targets {
		targetsAny[id] = SQLApprovalResult{Approved: approved}
	}

	resumeStart := time.Now()
	iter, err := a.runner.ResumeWithParams(ctx, checkPointID, &adk.ResumeParams{
		Targets: targetsAny,
	})
	if err != nil {
		return fmt.Errorf("resume failed: %w", err)
	}
	log.Printf("[Agent] resume ResumeWithParams 返回 - elapsed=%v\n", time.Since(resumeStart))

	_, _ = a.processEvents(iter, flush, sess, checkPointID)

	log.Printf("[Agent] resume processEvents 完成 - totalElapsed=%v, sessionMsgs=%d\n", time.Since(resumeStart), len(sess.messages))

	if err := sess.SaveToDB(); err != nil {
		log.Printf("[Agent] save assistant msg failed - err=%v\n", err)
	}

	return nil
}

// extractRootErrorMessage 从错误链中提取根错误消息
func extractRootErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err.Error()
		}
		err = unwrapped
	}
}

// logUnwrappedError 逐层解包错误并记录日志
func logUnwrappedError(err error) {
	if err == nil {
		return
	}
	log.Printf("[Agent] 错误详情 - err=%v, type=%T\n", err, err)
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			break
		}
		log.Printf("[Agent] 错误原因 - err=%v, type=%T\n", unwrapped, unwrapped)
		err = unwrapped
	}
	log.Printf("[Agent] 根错误 - err=%q, type=%T\n", err, err)

	// 尝试提取 APIError 的详细信息
	type apiError interface {
		GetCode() any
		GetMessage() string
		GetType() string
		GetHTTPStatusCode() int
	}
	if ae, ok := err.(apiError); ok {
		log.Printf("[Agent] APIError - code=%v, message=%q, type=%q, httpStatus=%d\n",
			ae.GetCode(), ae.GetMessage(), ae.GetType(), ae.GetHTTPStatusCode())
	}

	// 使用 fmt.Sprintf("%#v") 打印完整结构
	log.Printf("[Agent] 根错误结构 - %#v\n", err)
}

// processEvents 处理 Agent 事件流
func (a *SQLAgent) processEvents(iter *adk.AsyncIterator[*adk.AgentEvent], flush func(StreamChunk), sess *Session, checkPointID string) (strings.Builder, bool) {
	var fullResponse strings.Builder
	interrupted := false
	eventIdx := 0

	for {
		eventStart := time.Now()
		event, ok := iter.Next()
		if !ok {
			log.Printf("[Agent] 事件迭代结束 - totalEvents=%d\n", eventIdx)
			break
		}
		eventIdx++
		if event.Err != nil {
			log.Printf("[Agent] 事件错误 - err=%+v\n", event.Err)
			logUnwrappedError(event.Err)
			if errors.Is(event.Err, context.Canceled) || errors.Is(event.Err, context.DeadlineExceeded) {
				sess.RemoveTrailingIncompleteToolCalls()
				if fullResponse.Len() > 0 {
					_ = sess.SaveToDB()
				}
				if errors.Is(event.Err, context.DeadlineExceeded) {
					flush(StreamChunk{Type: "error", Content: "AI 处理超时，部分操作可能未完成。你可以在对话框中继续提问，AI 会基于已有的对话历史继续处理。"})
				}
				break
			}
			// Eino v0.9: 区分主动取消（CancelError）与普通业务失败
			var cancelErr *adk.CancelError
			if errors.As(event.Err, &cancelErr) {
				sess.RemoveTrailingIncompleteToolCalls()
				if fullResponse.Len() > 0 {
					_ = sess.SaveToDB()
				}
				log.Printf("[Agent] Agent 被主动取消\n")
				flush(StreamChunk{Type: "cancelled", Content: "已停止生成"})
				break
			}
			if errors.Is(event.Err, adk.ErrExceedMaxIterations) || strings.Contains(event.Err.Error(), "exceeds max iterations") {
				sess.RemoveTrailingIncompleteToolCalls()
				if fullResponse.Len() > 0 {
					_ = sess.SaveToDB()
				}
				flush(StreamChunk{Type: "error", Content: "AI 处理步骤过多，部分查询尝试未完成。已执行的操作可能已生效。你可以在对话框中继续提问，AI 会基于已有的对话历史继续处理。"})
				break
			}
			if strings.Contains(event.Err.Error(), "stream reader is empty") || strings.Contains(event.Err.Error(), "concat stream reader fail") {
				sess.RemoveTrailingIncompleteToolCalls()
				if fullResponse.Len() > 0 {
					_ = sess.SaveToDB()
				}
				flush(StreamChunk{Type: "error", Content: "AI 处理遇到内部错误，前置工具调用可能未成功。你可以重新提问或提供更具体的指令，AI 会重新尝试处理。"})
				break
			}
			errMsg := extractRootErrorMessage(event.Err)
			flush(StreamChunk{Type: "error", Content: "AI 处理出错：" + errMsg})
			break
		}

		// 检查是否被中断
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = true
			hasDangerConfirm := false
			for _, ictx := range event.Action.Interrupted.InterruptContexts {
				if !ictx.IsRootCause {
					continue
				}
				if sqlInfo, ok := ictx.Info.(*DangerousSQLInfo); ok {
					hasDangerConfirm = true
					log.Printf("[Agent] 危险 SQL 中断 - id=%s, sql=%s\n", ictx.ID, sqlInfo.SQL)
					flush(StreamChunk{
						Type:         "danger_confirm",
						Content:      "检测到危险 SQL，需要用户确认",
						SQL:          sqlInfo.SQL,
						InterruptID:  ictx.ID,
						CheckPointID: checkPointID,
					})
				} else {
					log.Printf("[Agent] 未知类型中断 - id=%s, info=%T\n", ictx.ID, ictx.Info)
				}
			}
			if !hasDangerConfirm {
				// 中断事件中没有 DangerousSQLInfo，属于异常情况
				// 标记为非中断，让调用方发送 done，避免前端永远卡住
				interrupted = false
				log.Printf("[Agent] 中断事件无 DangerousSQLInfo，视为非中断\n")
				flush(StreamChunk{Type: "error", Content: "AI 处理出现异常中断，请重试"})
			}
			if fullResponse.Len() > 0 {
				_ = sess.SaveToDB()
			}
			break
		}

		hasOutput := event.Output != nil && event.Output.MessageOutput != nil
		hasExit := event.Action != nil && event.Action.Exit

		if !hasOutput {
			if hasExit {
				break
			}
			continue
		}

		mo := event.Output.MessageOutput
		role := mo.Role
		if role == "" && mo.Message != nil {
			role = mo.Message.Role
		}

		log.Printf("[Agent] 事件输出 [#%d] - role=%s, isStreaming=%v, hasStream=%v, hasMsg=%v, toolCalls=%d, exit=%v, waitTime=%v\n",
			eventIdx, role, mo.IsStreaming, mo.MessageStream != nil, mo.Message != nil, func() int {
				if mo.Message != nil {
					return len(mo.Message.ToolCalls)
				}
				return 0
			}(), hasExit, time.Since(eventStart))

		if mo.IsStreaming && mo.MessageStream != nil {
			var accContent strings.Builder
			var accToolCalls []schema.ToolCall
			chunkIdx := 0
			streamStart := time.Now()
			repeatCount := 0
			lastChunkContent := ""
			const maxRepeats = 10
			for {
				chunk, recvErr := mo.MessageStream.Recv()
				if recvErr != nil {
					elapsed := time.Since(streamStart)
					log.Printf("[Agent] MessageStream.Recv 结束 - role=%s, accLen=%d, chunks=%d, toolCalls=%d, elapsed=%v, err=%v\n",
						role, accContent.Len(), chunkIdx, len(accToolCalls), elapsed, recvErr)
					if accContent.Len() > 0 && accContent.Len() < 10 {
						log.Printf("[Agent] MessageStream 异常短内容 - accContent=%q\n", accContent.String())
					}
					break
				}
				chunkIdx++
				contentLen := len(chunk.Content)
				reasoningLen := len(chunk.ReasoningContent)
				tcCount := len(chunk.ToolCalls)
				if chunkIdx <= 5 || contentLen > 0 || reasoningLen > 0 || tcCount > 0 || chunkIdx%20 == 0 {
					contentPreview := chunk.Content
					if len(contentPreview) > 80 {
						contentPreview = contentPreview[:80] + "..."
					}
					log.Printf("[Agent] MessageStream chunk[%d] - contentLen=%d, reasoningLen=%d, toolCalls=%d, content=%q\n",
						chunkIdx, contentLen, reasoningLen, tcCount, contentPreview)
				}
				if chunk.ReasoningContent != "" {
					flush(StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
				}
				if chunk.Content != "" {
					if chunk.Content == lastChunkContent && len(chunk.Content) > 0 {
						repeatCount++
						if repeatCount >= maxRepeats {
							log.Printf("[Agent] MessageStream 检测到重复输出，中断流 - repeatCount=%d, content=%q, accLen=%d\n",
								repeatCount, chunk.Content, accContent.Len())
							flush(StreamChunk{Type: "error", Content: "模型输出异常（重复内容），已自动中断"})
							break
						}
					} else {
						repeatCount = 0
					}
					lastChunkContent = chunk.Content
					accContent.WriteString(chunk.Content)
					flush(StreamChunk{Type: "content", Content: chunk.Content})
				}
				if len(chunk.ToolCalls) > 0 {
					accToolCalls = append(accToolCalls, chunk.ToolCalls...)
				}
			}
			if accContent.Len() > 0 || len(accToolCalls) > 0 {
				fullResponse.WriteString(accContent.String())
				sm := SessionMessage{Role: string(role), Content: accContent.String()}
				if len(accToolCalls) > 0 {
					sm.ToolCalls = sessionToolCallsFromSchema(mergeToolCalls(accToolCalls))
				}
				sess.AppendMessageNoSave(sm)
			}
		} else if role == schema.Tool {
			msg := mo.Message
			if msg != nil {
				sess.AppendMessageNoSave(SessionMessage{
					Role:       "tool",
					Content:    msg.Content,
					ToolCallID: msg.ToolCallID,
					ToolName:   msg.ToolName,
				})
			}
		} else if role == schema.Assistant && mo.Message != nil {
			msg := mo.Message
			if len(msg.ToolCalls) > 0 {
				sess.AppendMessageNoSave(SessionMessage{
					Role:      "assistant",
					Content:   msg.Content,
					ToolCalls: sessionToolCallsFromSchema(msg.ToolCalls),
				})
			} else if msg.Content != "" {
				fullResponse.WriteString(msg.Content)
				flush(StreamChunk{Type: "content", Content: msg.Content})
				sess.AppendMessageNoSave(SessionMessage{Role: string(role), Content: msg.Content})
			}
		}

		if hasExit {
			break
		}
	}

	return fullResponse, interrupted
}

// ──────────────────────────────────────────────
// 辅助函数
// ──────────────────────────────────────────────

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
func detectPreviousExecution(msgs []SessionMessage) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// authTransport 实现了 http.RoundTripper 接口
type authTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clonedReq := req.Clone(req.Context())
	if clonedReq.Header == nil {
		clonedReq.Header = make(http.Header)
	}
	clonedReq.Header.Set("Authorization", "Bearer "+t.token)
	return t.transport.RoundTrip(clonedReq)
}

func NewAuthClient(token string) *http.Client {
	return &http.Client{
		Transport: &authTransport{token: token, transport: http.DefaultTransport},
	}
}

// ──────────────────────────────────────────────
// 模型与工具构建
// ──────────────────────────────────────────────

var ollamaURLPrefix = "https://ollama.com"

func isOllamaURL(baseURL string) bool {
	if strings.HasPrefix(strings.ToLower(baseURL), ollamaURLPrefix) {
		return true
	}
	return false
}

func BuildChatModel(ctx context.Context, cfg *system.AIConfig) (model.ToolCallingChatModel, error) {
	log.Printf("[ChatModel] 初始化 - provider=%s, model=%s\n", cfg.Provider, cfg.Model)

	switch cfg.Provider {
	case "ollama":
		ollamaCfg := &ollama.ChatModelConfig{
			BaseURL: cfg.BaseURL, Model: cfg.Model, Timeout: 30 * time.Minute,
		}
		if cfg.EnableThinking {
			ollamaCfg.Thinking = &ollama.ThinkValue{Value: true}
		}
		if cfg.Temperature > 0 {
			ollamaCfg.Options = &ollama.Options{Temperature: cfg.Temperature}
		}
		ollamaCfg.HTTPClient = NewAuthClient(cfg.ApiKey)
		return ollama.NewChatModel(ctx, ollamaCfg)
	case "openai":
		openaiCfg := &openai.ChatModelConfig{
			BaseURL: cfg.BaseURL, Model: cfg.Model, APIKey: cfg.ApiKey,
			Timeout: 30 * time.Minute,
		}
		if cfg.Temperature > 0 {
			openaiCfg.Temperature = new(cfg.Temperature)
		}
		return openai.NewChatModel(ctx, openaiCfg)
	default:
		return nil, fmt.Errorf("不支持的 AI 提供商：%s", cfg.Provider)
	}
}

func buildTools(_ context.Context, connID, dbType, dbSchema string, schemas []SchemaRef, auditCtx *ExecAuditCtx, scope *PermissionScope) (coreTools []tool.BaseTool, deferredTools []tool.BaseTool, err error) {
	conn, _ := GetConn(connID, scope.UserID)
	queryTool, qErr := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 查询并返回结果", NewQueryFunc(connID, schemas, auditCtx, scope.UserID))
	schemaTool, sErr := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息，支持一次传入多个表名", NewSchemaFunc(connID, dbType, dbSchema, schemas, scope.UserID))
	listTablesTool, lErr := utils.InferTool("list_tables", "获取当前数据库的所有表名及表注释", NewListTablesFunc(connID, dbType, dbSchema, schemas, scope.UserID))
	currentDateInfoTool, dErr := utils.InferTool("get_current_date_info", "获取当前日期、星期几和时间", GetCurrentDateInfo())

	for _, t := range []tool.BaseTool{queryTool, schemaTool, listTablesTool, currentDateInfoTool} {
		if t != nil {
			coreTools = append(coreTools, t)
		}
	}
	if qErr != nil || sErr != nil || lErr != nil || dErr != nil {
		return nil, nil, fmt.Errorf("创建核心工具失败：query=%v schema=%v list=%v date=%v", qErr, sErr, lErr, dErr)
	}

	exportExcelTool, _ := utils.InferTool("export_excel", "导出 Excel 表格数据，须传入 sql 参数", export.NewExportExcelFunc(conn))
	exportExcelChartTool, _ := utils.InferTool("export_excel_with_chart", "导出带图表的 Excel，图表类型根据数据特征自动选择", export.NewExportExcelWithChartFunc(conn))
	exportPPTTool, _ := utils.InferTool("export_ppt", "生成 PPT 演示文稿，优先使用 content 模式（直接传入分析文本）避免重复查询", export.NewExportPPTFunc(conn))
	exportDocxTool, _ := utils.InferTool("export_analysis_docx", "生成数据分析报告（Word），优先使用 content 模式（直接传入分析文本）", export.NewExportAnalysisDocxFunc(conn))

	for _, t := range []tool.BaseTool{exportExcelTool, exportExcelChartTool, exportPPTTool, exportDocxTool} {
		if t != nil {
			deferredTools = append(deferredTools, t)
		}
	}

	if scope.AllowModify {
		execTool, _ := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connID, schemas, auditCtx, scope.UserID))
		importDataTool, _ := utils.InferTool("import_data", "将用户上传的 Excel 数据导入到指定数据库表中", NewImportDataFunc(connID, dbType, dbSchema, auditCtx, scope.UserID))
		for _, t := range []tool.BaseTool{execTool, importDataTool} {
			if t != nil {
				deferredTools = append(deferredTools, t)
			}
		}
	}

	return coreTools, deferredTools, nil
}

// ──────────────────────────────────────────────
// 系统提示词
// ──────────────────────────────────────────────

// estimateTokenCount 估算文本的 token 数量。
// 提供比默认 4 chars/token 更精确的估算，针对中英文混合文本优化。
func estimateTokenCount(_ context.Context, input *summarization.TokenCounterInput) (int, error) {
	total := 0
	for _, msg := range input.Messages {
		total += estimateTextTokens(msg.Content)
		for _, tc := range msg.ToolCalls {
			total += estimateTextTokens(tc.Function.Name)
			total += estimateTextTokens(tc.Function.Arguments)
			total += 4
		}
		if msg.ToolCallID != "" {
			total += estimateTextTokens(msg.ToolCallID) + 3
		}
		if msg.ToolName != "" {
			total += estimateTextTokens(msg.ToolName) + 3
		}
		if msg.Name != "" {
			total += estimateTextTokens(msg.Name) + 3
		}
		total += 6
	}
	for _, tool := range input.Tools {
		total += estimateTextTokens(tool.Desc)
	}
	total = total * 115 / 100
	return total, nil
}

func estimateTextTokens(text string) int {
	total := 0
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		ch := runes[i]
		if ch >= 0x4E00 && ch <= 0x9FFF {
			total += 2
			i++
		} else if ch >= 0x3000 && ch <= 0x303F {
			total += 2
			i++
		} else if ch >= 0xFF00 && ch <= 0xFFEF {
			total += 2
			i++
		} else if ch >= 0x2000 && ch <= 0x206F {
			total += 1
			i++
		} else if ch >= 0x0080 {
			total += 2
			i++
		} else if ch == ' ' || ch == '\n' || ch == '\t' {
			total += 1
			i++
		} else {
			segLen := 1
			for j := i + 1; j < len(runes) && runes[j] < 0x0080 && runes[j] != ' ' && runes[j] != '\n' && runes[j] != '\t'; j++ {
				segLen++
			}
			total += (segLen + 3) / 4
			i += segLen
		}
	}
	if total == 0 && len(text) > 0 {
		total = (len(text) + 3) / 4
	}
	return total
}

func buildSystemPrompt(connID, dbType, dbSchema, dbVersion string, tableContext []string, scope *PermissionScope, schemas []SchemaRef) string {
	var sb strings.Builder

	sb.WriteString(buildStaticPromptPart(dbType))

	sb.WriteString(buildDynamicPromptPart(connID, dbType, dbSchema, dbVersion, tableContext, scope, schemas))

	return sb.String()
}

func buildStaticPromptPart(dbType string) string {
	var sb strings.Builder

	sb.WriteString("你是企业的首席数据架构师兼资深数据分析师。")
	sb.WriteString("你精通标准 SQL（SQL-92/99/2003），以及 ")
	fmt.Fprintf(&sb, "%s 的方言特性、索引策略和查询优化技巧。", dbType)
	sb.WriteString("你不仅写出极致优化、安全高效的 SQL，还擅长将查询结果转化为富有洞察且具有中国特色的分析结论。")
	sb.WriteString("\n\n")

	sb.WriteString(`## 核心准则（必须遵守，每条只声明一次，全文以此为准）
1. **先验证再查询**：生成 SQL 前必须通过 get_table_schema 验证表名和字段名，禁止臆测
2. **禁止 SELECT ***：必须显式列出所需字段，除非用户明确要求导出全部列
3. **控制查询量**：对大表查询必须添加合理的 WHERE 条件并配合 LIMIT
4. **透明可追溯**：每次查询/操作后必须在回复中明确说明来源表名和影响范围
5. **禁止假执行**：导出/生成文件时必须实际调用 export_excel / export_ppt / export_analysis_docx 等工具，绝不能只输出文字描述"已完成导出"，更不能凭空编造下载链接或文件名
6. **优先使用 Skill 导出**：生成 Word/PPT/Excel 报告时，直接调用 export_ppt、export_analysis_docx 或 export_excel 专属工具即可。这些工具内部会优先使用 Python Skill 生成高质量文档，若 Skill 失败则自动回退到 Go 原生实现。无需手动通过 skill、read_file、write_file、execute 工具自行拼装导出流程
7. **禁止猜测表名**：用户未指定表名时，必须先调用 list_tables 获取表列表及表注释，通过注释判断目标表；注释无法判断时才可向用户确认，绝不允许凭空猜测
8. **写操作自动确认**：执行写操作时，先简要说明意图（目标表、操作类型、影响范围），然后立即调用 exec_sql，系统会自动拦截并推送前端确认弹窗，无需等待用户文字确认
`)

	sb.WriteString(`
## 标准工作流程
1. 理解需求 — 澄清模棱两可的表达、确认统计口径（去重？含空值？）、明确时间范围
2. 定位表 — 按准则#7：未指定表名时先调 list_tables，通过注释匹配目标表
3. 探索结构 — 按准则#1：调用 get_table_schema 获取字段、类型、索引信息
4. 编写 SQL — 基于真实字段名和数据类型编写优化 SQL，确保与 ` + dbType + ` 方言兼容
5. 执行查询 — 调用 query_data（读）或 exec_sql（写）
6. 解读结果 — 不仅返回数据，还要给出 2-5 行的分析小结（趋势、异常、业务建议）
7. 写操作 — 按准则#8：说明意图后立即调用 exec_sql

## SQL 编写规范（` + dbType + `）
` + getSQLDialectRules(dbType) + `

## 写操作安全
- 生成写操作 SQL 时，尽量包含精确的 WHERE 条件，避免批量误操作
- DELETE / UPDATE 无 WHERE 子句的语句将被系统标记为高风险

## 数据导入流程
用户上传 Excel 并要求导入时：
1. 调用 get_table_schema 了解目标表结构
2. 向用户明确说明并等待确认：
   - 目标表名、操作模式（insert / upsert / insert+update）
   - Excel 列 → 数据库列的映射关系
   - 预计影响行数
3. 用户确认后调用 import_data（传入 fileId、tableName、mode），后端自动按列名匹配
4. 若用户未指定目标表，必须先询问

## 多轮对话
你拥有完整对话历史。"刚才的""上一个""这个结果"均指上一轮上下文。
当用户追问时，优先基于已有结果分析，而非重复查询。

## 错误恢复
工具调用失败时系统会将错误信息反馈给你。请：
- 仔细阅读错误信息，不要重复使用相同的错误参数
- 调整 SQL 或参数后重试，最多尝试 3 次
- 若 3 次均失败，向用户解释原因并建议替代方案

## 迭代次数限制
你的每次思考与工具调用都会消耗 1 次迭代，你有 ` + fmt.Sprint(maxIterations) + ` 次迭代上限。请高效利用：

### 减少试错
1. **合并调用**：get_table_schema 支持一次传入多个表名，一次 SQL 涉及的所有表应在同一轮完成探索
2. **SQL 自检**：写完后在脑中快速检查引号是否正确、LIMIT 是否添加、JOIN 条件是否完整，确认无误后再调用工具

### 及时止损
- query_data 连续 2 次返回空结果或"表不存在"类错误 → 立即停止，告知用户数据不可用，禁止猜测其他表名变体
- 同一个错误信息连续出现 2 次 → 禁止用相同参数重试，转为向用户说明问题或切换工具/策略
- 禁止猜测表名变体：加 _bak / _old / _temp / _new 后缀的猜测不超过 2 次就应放弃
- 若迭代已消耗超过 35 次，暂停新探索，尽快整合已有结果输出给用户

### 最大化有效产出
- 能用一条 JOIN 查询完成的多表分析，不要拆成多次单表查询再手动合并
- 优先用 GROUP BY + 聚合函数一次获取多维度统计概况，而非逐维度分多次查询
- 查询结果确认正确后再导出（export_excel / export_ppt / export_analysis_docx），避免导出错误数据后重新查询浪费迭代
- 复杂任务中途向用户反馈进度，让用户感知分析在推进

## 数据可视化（Mermaid 图表）
你可以在回复中使用 Mermaid 语法绘制图表，以更直观的方式呈现数据分析结论。只需将 Mermaid 代码放在 ` + "```mermaid" + ` 代码块中即可，前端会自动渲染为 SVG 图表。

### 适用场景
- **业务流程分析**：用 flowchart 展示数据流转、审批流程、业务逻辑
- **时序/趋势分析**：用 sequenceDiagram 展示系统交互时序，用 timeline 展示时间线
- **数据关系分析**：用 erDiagram 展示表间关联关系（ER 图）
- **占比/分类分析**：用 pie 或 xychart-beta 展示比例分布、趋势对比
- **层级/分类分析**：用 mindmap 或 graph 展示分类体系、组织结构
- **甘特图/进度**：用 gantt 展示项目排期、里程碑

### 使用原则
1. **图表辅助文字**：Mermaid 图表是文字分析的补充，不能替代文字解读。先给出分析结论，再用图表直观呈现
2. **简洁优先**：每个图表聚焦一个核心观点，避免信息过载。节点数控制在 15 个以内
3. **语法正确**：确保 Mermaid 语法严格正确，否则无法渲染。避免使用实验性或冷门语法
4. **合理选择类型**：根据数据特征选择最合适的图表类型，不要用流程图展示数值趋势
5. **与导出工具配合**：Mermaid 图表用于即时可视化；如需导出带图表的 Excel，请调用 export_excel_with_chart

### 示例
` + "```mermaid" + `
pie title 各部门预算占比
  "研发部" : 40
  "市场部" : 25
  "运营部" : 20
  "行政部" : 15
` + "```" + `
`)

	return sb.String()
}

func buildDynamicPromptPart(connID, dbType, dbSchema, dbVersion string, tableContext []string, scope *PermissionScope, schemas []SchemaRef) string {
	var sb strings.Builder

	if len(schemas) > 1 {
		fmt.Fprintf(&sb, "当前环境 — 数据库：%s，版本：%s\n", dbType, dbVersion)
		type connGroup struct {
			connID  string
			schemas []string
		}
		connMap := make(map[string]*connGroup)
		var connOrder []string
		for _, s := range schemas {
			if s.ConnID == "" || s.Schema == "" {
				continue
			}
			if _, ok := connMap[s.ConnID]; !ok {
				connMap[s.ConnID] = &connGroup{connID: s.ConnID}
				connOrder = append(connOrder, s.ConnID)
			}
			connMap[s.ConnID].schemas = append(connMap[s.ConnID].schemas, s.Schema)
		}
		sb.WriteString("**多 Schema 上下文**（按数据库连接分组，相同连接内的 schema 可直接 JOIN）：\n")
		for _, connID := range connOrder {
			g := connMap[connID]
			dbConn, _ := GetConn(connID, scope.UserID)
			typeStr := ""
			if dbConn != nil {
				typeStr = dbConn.DriverName()
			}
			fmt.Fprintf(&sb, "  🔗 连接 %s (%s)：\n", connID, typeStr)
			for _, s := range g.schemas {
				fmt.Fprintf(&sb, "    - Schema: %s\n", s)
			}
		}
		if connID != "" {
			fmt.Fprintf(&sb, "  ⭐ 默认连接（query_data/exec_sql 不指定 connId 时使用）：连接ID=%s\n", connID)
		}
	} else if len(schemas) == 1 {
		fmt.Fprintf(&sb, "当前环境 — 数据库：%s，版本：%s，Schema：%s\n", dbType, dbVersion, schemas[0].Schema)
	} else {
		fmt.Fprintf(&sb, "当前环境 — 数据库：%s，版本：%s，Schema：%s\n", dbType, dbVersion, dbSchema)
	}

	if len(tableContext) > 0 {
		fmt.Fprintf(&sb, "\n用户指定表范围：%s\n", strings.Join(tableContext, ", "))
		sb.WriteString("只能在这些表上操作。若需求无法仅用这些表满足，请明确告知需要哪些额外表。\n")
	} else {
		sb.WriteString("\n用户未限定表范围，请按准则#7 先调用 list_tables 获取表列表。\n")
	}

	sb.WriteString(scope.DescribeForPrompt())

	if len(schemas) > 1 {
		sb.WriteString(`
## 跨库操作规则（重要）
你被授权访问多个 schema，可能来自同一个数据库连接或多个不同连接。遵循以下规则：

### 1. 连接分组概览
参考上方的"多 Schema 上下文"分组：
  - **同组 schema**（同一连接）→ 可在同一条 SQL 中引用，支持 JOIN / UNION / 子查询
  - **不同组 schema**（不同连接）→ 是独立的数据库实例，**绝不能**放在同一条 SQL 中

### 2. 混合场景示例
假设你有 3 个 schema：Schema_A 和 Schema_B 属于连接1，Schema_C 属于连接2：
  ✅ 正确做法：
    第1步：query_data(sql="SELECT ... FROM Schema_A.table1 JOIN Schema_B.table2 ...", connId="Schema_A")
            （连接1内可 JOIN，无需指定 connId 或传 Schema_A）
    第2步：query_data(sql="SELECT ... FROM Schema_C.table3 ...", connId="Schema_C")
            （连接2需单独查询，通过 connId="Schema_C" 路由）
    第3步：你综合分析两部分结果后回复用户

  ❌ 错误做法：
    query_data(sql="SELECT ... FROM Schema_A.table1 JOIN Schema_C.table3 ...")
    → 会报错，因为 Schema_A 和 Schema_C 不在同一数据库中

### 3. 读操作（SELECT）规则
  - **同一连接内跨 schema**：可自由 JOIN / UNION，使用 schema.table 语法
    SELECT ... FROM schemaA.table1 t1 JOIN schemaB.table2 t2 ON ...
  - **不同连接间**：必须分步查询，每步使用各自的 connId 参数
    步骤1: query_data(sql="SELECT ... FROM table1", connId="schema名")
    步骤2: query_data(sql="SELECT ... FROM table2", connId="schema名")
    然后由你综合分析两部分结果

### 4. 写操作（INSERT / UPDATE / DELETE）规则
  - **写操作同样受连接限制**：一条 SQL 只能操作一个连接
  - **同一连接内**：可 UPDATE 表A 基于 JOIN 表B（同 schema 或同连接跨 schema）
  - **不同连接间**：必须在不同 exec_sql 调用中分别执行
    ✅ 正确：
      第1步：exec_sql(sql="UPDATE Schema_A.table1 SET ...", connId="Schema_A")
      第2步：exec_sql(sql="UPDATE Schema_C.table3 SET ...", connId="Schema_C")
  - **事务隔离**：不同连接有各自的事务，无法跨连接回滚。如果某一步失败，你需要告知用户哪些操作已完成、哪些需要手动回滚
  - **写入前先说明**：执行写操作前，先向用户说明将要在哪些连接上做什么修改，等待系统推送确认

### 5. query_data / exec_sql 的 connId 参数
这两个工具现在支持可选参数 connId：
  - **不填**：在默认连接上执行（标注 ⭐ 的连接）
  - **填写 Schema 名**：自动路由到该 Schema 所在的连接
  - **填写连接ID**：直接使用该连接
参考上面的连接分组信息，选择正确的连接执行 SQL。

### 6. 数据来源标注
当从不同连接获取数据并综合分析时，请在回复中明确标注每条数据/结论的来源：
  - "来自连接1(Schema_A)的数据显示..."
  - "来自连接2(Schema_C)的数据显示..."
  - 让用户清晰了解跨库操作的完整链路

### 7. 大数据量防范
跨库组合可能导致结果集非常大，**务必使用 LIMIT 或聚合函数控制返回行数**。

### 8. 上下文溢出保护
如果一次查询返回几万行数据，会超出大模型的上下文窗口，导致分析中断
   - 优先使用聚合查询（SUM、COUNT、AVG 等）返回统计结果
   - 对明细数据，如果需要导出完整数据集，请调用 export_excel 工具
   - 对多表关联产生的大结果集，先分析数据量（COUNT），再分页查询

### 9. Python 脚本分析
当需要进行复杂的跨库大数据量统计分析时，系统已预置 ` + "`cross-db-analysis`" + ` Python 脚本工具
   - 脚本可直接连接各数据库，在数据库中完成聚合计算，只返回分析结论
   - 适用于跨库数据量大于 10 万行或需要进行复杂统计模型计算的场景
   - 触发时机：用户要求"分析"、"对比"、"统计"多个库的数据时，优先考虑使用脚本
`)
	}

	return sb.String()
}

func getSQLDialectRules(dbType string) string {
	base := "- 字符串比较注意字符集和排序规则\n"

	switch strings.ToLower(dbType) {
	case "mysql", "mariadb":
		return "- 字段名和表名若含特殊字符或关键字，使用反引号包裹\n" +
			base +
			"- 优先使用 EXPLAIN 分析执行计划，检查是否走索引\n" +
			"- 字符串模糊匹配优先 LIKE 'prefix%'（可利用索引），避免 LIKE '%middle%'\n" +
			"- 日期函数使用 DATE_FORMAT、DATE_ADD、DATEDIFF 等\n" +
			"- 分页优先使用 LIMIT offset, count\n" +
			"- 注意 ONLY_FULL_GROUP_BY 模式，GROUP BY 的字段必须在 SELECT 中出现或使用聚合函数\n" +
			"- 多表 JOIN 时注意驱动表选择，小表驱动大表\n"
	case "oracle":
		return "- 字段名和表名若含特殊字符或关键字，使用双引号包裹，禁止使用反引号\n" +
			base +
			"- 使用 EXPLAIN PLAN FOR 分析执行计划\n" +
			"- 分页使用 ROWNUM 或 OFFSET/FETCH（12c+），注意 ROWNUM 是在排序前计算的\n" +
			"- 日期函数使用 TO_DATE、TO_CHAR、ADD_MONTHS 等\n" +
			"- 字符串连接使用 || 而非 CONCAT\n" +
			"- 注意空字符串在 Oracle 中等价于 NULL\n" +
			"- Dual 表用于无表查询，如 SELECT SYSDATE FROM DUAL\n"
	case "postgresql", "postgres":
		return "- 字段名和表名若含特殊字符或关键字，使用双引号包裹，禁止使用反引号\n" +
			base +
			"- 使用 EXPLAIN ANALYZE 分析实际执行计划\n" +
			"- 分页使用 LIMIT count OFFSET offset\n" +
			"- 日期函数使用 TO_CHAR、DATE_TRUNC、AGE 等\n" +
			"- 字符串拼接使用 || 或 CONCAT\n" +
			"- 注意 PostgreSQL 的 MVCC 特性，大量更新后建议 VACUUM\n"
	case "sqlite":
		return "- 字段名和表名若含特殊字符或关键字，使用反引号或双引号包裹\n" +
			base +
			"- 使用 EXPLAIN QUERY PLAN 分析查询计划\n" +
			"- 日期函数使用 strftime、date、time、datetime\n" +
			"- 字符串拼接使用 ||\n" +
			"- AUTOINCREMENT 仅用于 INTEGER PRIMARY KEY\n" +
			"- 写操作会锁定整个数据库，避免长事务\n"
	case "sqlserver", "mssql":
		return "- 字段名和表名若含特殊字符或关键字，使用方括号 [] 包裹，禁止使用反引号\n" +
			base +
			"- 使用 SET STATISTICS IO ON 查看 IO 统计\n" +
			"- 分页使用 OFFSET/FETCH（2012+）或 ROW_NUMBER() OVER()\n" +
			"- 日期函数使用 FORMAT、DATEADD、DATEDIFF 等\n" +
			"- 使用 TOP 限制返回行数（旧版本），新版本用 OFFSET/FETCH\n" +
			"- 字符串拼接使用 + 或 CONCAT（2012+）\n"
	default:
		return "- 字段名和表名若含特殊字符或关键字，使用双引号包裹\n" +
			base +
			"- 使用 EXPLAIN 分析执行计划\n" +
			"- 遵循标准 SQL 语法，避免数据库特有的非标准扩展\n"
	}
}
