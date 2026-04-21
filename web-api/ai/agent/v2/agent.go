// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"context"
	"fmt"
	admin "go-web/web-api/admin"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
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
	SessionID    string     `json:"sessionId"`
	UserID       string     `json:"userId"`
	ConnID       string     `json:"connId"`
	Schema       string     `json:"schema"`
	Question     string     `json:"question"`
	TableContext []string   `json:"tableContext"`
	Confirmed    bool       `json:"confirmed,omitempty"`
	InterruptIDs []string   `json:"interruptIds,omitempty"` // 确认时回传（支持多条）
	CheckPointID string     `json:"checkPointId,omitempty"` // 确认时回传
	ExcelData    *ExcelData `json:"excelData,omitempty"`
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

// SessionDetail 会话详情
type SessionDetail struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	CreatedAt time.Time              `json:"createdAt"`
	Messages  []SessionDetailMessage `json:"messages"`
}

// SessionDetailMessage 会话消息
type SessionDetailMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ──────────────────────────────────────────────
// SQLAgent + Runner
// ──────────────────────────────────────────────

// 全局 CheckPointStore（单实例共享）
var globalCheckPointStore = NewInMemoryCheckPointStore()

type SQLAgent struct {
	runner   *adk.Runner
	agent    *adk.ChatModelAgent
	sessions *SessionStore
	dbType   string
	dbSchema string
	scope    *PermissionScope
}

const maxHistoryRounds = 20

func NewSQLAgent(ctx context.Context, cfg *admin.AIConfig, connID, dbType, dbSchema, dbVersion string, sessions *SessionStore, scope *PermissionScope, auditCtx *ExecAuditCtx) (*SQLAgent, error) {
	cm, err := buildChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建模型失败：%w", err)
	}
	tools, err := buildTools(ctx, connID, dbType, dbSchema, auditCtx)
	if err != nil {
		return nil, fmt.Errorf("创建工具失败：%w", err)
	}

	err = adk.SetLanguage(adk.LanguageChinese)
	if err != nil {
		log.Printf("[Agent] 设置语言失败 - err=%v\n", err)
	}

	summarizationMW, err := summarization.New(ctx, &summarization.Config{
		Model: cm,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: 100000,
		},
	})
	if err != nil {
		log.Printf("[Agent] 创建摘要中间件失败 - err=%v\n", err)
		return nil, fmt.Errorf("创建摘要中间件失败：%w", err)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SQLAgent",
		Description: "专业 SQL 助手，支持查询、分析和数据导入导出",
		Instruction: buildSystemPrompt(dbType, dbSchema, dbVersion, nil, scope),
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		Handlers: []adk.ChatModelAgentMiddleware{
			&PermissionMiddleware{Scope: scope},
			// 不再需要 SQLSecurityMiddleware — 危险 SQL 检测已移到 exec_sql 工具内部
			// 使用 tool.StatefulInterrupt 实现，由 Runner 自动 checkpoint
			&ToolErrorRecoveryMiddleware{},
			summarizationMW,
		},
		MaxIterations: 20,
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
	return &SQLAgent{runner: runner, agent: agent, sessions: sessions, dbType: dbType, dbSchema: dbSchema, scope: scope}, nil
}

// RunStream 流式执行（首次查询）
func (a *SQLAgent) RunStream(ctx context.Context, req ChatRequest, flush func(StreamChunk)) (string, error) {
	log.Printf("[Agent] 开始执行 - sessionID=%s, userID=%s, connID=%s\n", req.SessionID, req.UserID, req.ConnID)

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("%s_%d_%d", req.UserID, time.Now().UnixNano(), time.Now().UnixMilli())
		log.Printf("[Agent] 新建会话 - sessionID=%s\n", sessionID)
	}
	if req.UserID == "" {
		return "", fmt.Errorf("userId 不能为空")
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
	truncated := truncateSessionMessages(allMsgs)
	log.Printf("[Agent] 历史消息 - total=%d, truncated=%d\n", len(allMsgs), len(truncated))

	if !a.scope.HasAnyAccess() {
		flush(StreamChunk{Type: "error", Content: "您暂时没有可访问的数据表权限，请联系管理员开通。"})
		flush(StreamChunk{Type: "done"})
		return sessionID, nil
	}

	sysPrompt := buildSystemPrompt(a.dbType, a.dbSchema, "", req.TableContext, a.scope)

	if isExportRequest(req.Question) {
		if lastSQL := extractLastSQLFromSessionMessages(truncated); lastSQL != "" {
			sysPrompt += fmt.Sprintf("\n\n⚠️ 用户正在请求导出操作，历史 SQL：\n```sql\n%s\n```\n如果用户要求导出 Excel，请直接使用此 SQL 调用导出工具；如果用户要求导出 Word/PPT 报告，优先使用 content 模式将分析结果传入。", lastSQL)
		}
	}

	if req.ExcelData != nil && req.ExcelData.FileID != "" {
		sysPrompt += fmt.Sprintf("\n\n📎 用户上传了 Excel 文件（fileId=%s）：\n- 列名：%s\n- 总行数：%d\n",
			req.ExcelData.FileID, strings.Join(req.ExcelData.Columns, ", "), req.ExcelData.TotalRows)
		sysPrompt += "请先用 get_table_schema 确认目标表存在，然后直接调用 import_data 工具（传入 fileId 和 tableName），后端会自动匹配字段。如果用户没有指定目标表，请询问用户。\n"
	}

	// 构建 Eino 消息列表
	messages := []adk.Message{
		&schema.Message{Role: schema.System, Content: sysPrompt},
	}
	for _, msg := range truncated {
		switch msg.Role {
		case "user":
			messages = append(messages, &schema.Message{Role: schema.User, Content: msg.Content})
		case "assistant":
			messages = append(messages, &schema.Message{Role: schema.Assistant, Content: msg.Content})
		}
	}

	// 使用 Runner.Run 执行，传入 CheckPointID 以支持中断恢复
	// CheckPointID 使用 sessionID，确保同一会话的中断可以恢复
	checkPointID := fmt.Sprintf("cp_%s_%d", sessionID, time.Now().UnixMilli())
	iter := a.runner.Run(ctx, messages, adk.WithCheckPointID(checkPointID))

	fullResponse, interrupted := a.processEvents(iter, flush, sess, checkPointID)

	// 如果被中断，将 checkPointID 存入会话以便恢复
	if interrupted {
		// checkPointID 已通过 flush 推送给前端
		log.Printf("[Agent] 执行被中断 - checkPointID=%s\n", checkPointID)
	}

	if fullResponse.Len() > 0 {
		if err := sess.Append("assistant", fullResponse.String()); err != nil {
			log.Printf("[Agent] 保存助手消息失败 - err=%v\n", err)
		}
	}

	// 无论是否中断都发 done，让前端结束 loading 状态
	// 中断场景下前端已通过 danger_confirm 事件知道需要用户确认
	flush(StreamChunk{Type: "done"})

	return sessionID, nil
}

// ResumeStream 恢复被中断的执行（用户确认后）
// 返回 interrupted 标志，如果为 true，说明再次被中断（如新的危险 SQL），需要等待用户再次确认
func (a *SQLAgent) ResumeStream(ctx context.Context, checkPointID string, targets map[string]bool, flush func(StreamChunk), sess *Session) error {
	log.Printf("[Agent] resume - cpID=%s, targets=%v\n", checkPointID, targets)

	// 将所有 interruptID 放入 Targets map，一次性恢复
	targetsAny := make(map[string]any, len(targets))
	for id, approved := range targets {
		targetsAny[id] = approved
	}

	iter, err := a.runner.ResumeWithParams(ctx, checkPointID, &adk.ResumeParams{
		Targets: targetsAny,
	})
	if err != nil {
		return fmt.Errorf("resume failed: %w", err)
	}

	fullResponse, _ := a.processEvents(iter, flush, sess, checkPointID)

	if fullResponse.Len() > 0 {
		if err := sess.Append("assistant", fullResponse.String()); err != nil {
			log.Printf("[Agent] save assistant msg failed - err=%v\n", err)
		}
	}

	// Always send done so frontend unlocks UI
	// If a new dangerous SQL was encountered, frontend already got danger_confirm event
	flush(StreamChunk{Type: "done"})
	return nil
}

// processEvents 处理 Agent 事件流
func (a *SQLAgent) processEvents(iter *adk.AsyncIterator[*adk.AgentEvent], flush func(StreamChunk), sess *Session, checkPointID string) (strings.Builder, bool) {
	var fullResponse strings.Builder
	interrupted := false

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			log.Printf("[Agent] 事件错误 - err=%+v\n", event.Err)
			flush(StreamChunk{Type: "error", Content: "AI 处理出错，请稍后重试"})
			break
		}

		// 检查是否被中断
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = true
			for _, ictx := range event.Action.Interrupted.InterruptContexts {
				if !ictx.IsRootCause {
					continue
				}
				if sqlInfo, ok := ictx.Info.(*DangerousSQLInfo); ok {
					log.Printf("[Agent] 危险 SQL 中断 - id=%s, sql=%s\n", ictx.ID, sqlInfo.SQL)
					flush(StreamChunk{
						Type:         "danger_confirm",
						Content:      "检测到危险 SQL，需要用户确认",
						SQL:          sqlInfo.SQL,
						InterruptID:  ictx.ID,
						CheckPointID: checkPointID,
					})
				}
			}
			if fullResponse.Len() > 0 {
				_ = sess.Append("assistant", fullResponse.String())
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
		if role == schema.Tool {
			continue
		}

		if mo.IsStreaming && mo.MessageStream != nil {
			var accContent strings.Builder
			for {
				chunk, recvErr := mo.MessageStream.Recv()
				if recvErr != nil {
					break
				}
				if chunk.ReasoningContent != "" {
					flush(StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
				}
				if chunk.Content != "" {
					accContent.WriteString(chunk.Content)
					flush(StreamChunk{Type: "content", Content: chunk.Content})
				}
			}
			if accContent.Len() > 0 {
				fullResponse.WriteString(accContent.String())
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

func truncateSessionMessages(msgs []SessionMessage) []SessionMessage {
	if len(msgs) <= maxHistoryRounds*2 {
		return msgs
	}
	return msgs[len(msgs)-maxHistoryRounds*2:]
}

func isExportRequest(question string) bool {
	q := strings.ToLower(question)
	for _, kw := range []string{"导出", "export", "下载", "excel", "ppt", "word", "图表"} {
		if strings.Contains(q, kw) {
			return true
		}
	}
	return false
}

func extractLastSQLFromSessionMessages(msgs []SessionMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role != "assistant" || msg.Content == "" {
			continue
		}
		content := msg.Content
		for {
			endIdx := strings.LastIndex(content, "```")
			if endIdx <= 0 {
				break
			}
			startIdx := strings.LastIndex(content[:endIdx], "```")
			if startIdx == -1 {
				break
			}
			codeBlock := strings.TrimSpace(content[startIdx+3 : endIdx])
			if idx := strings.Index(codeBlock, "\n"); idx != -1 {
				firstLine := strings.TrimSpace(codeBlock[:idx])
				if strings.EqualFold(firstLine, "sql") {
					codeBlock = strings.TrimSpace(codeBlock[idx+1:])
				}
			}
			upper := strings.ToUpper(strings.TrimSpace(codeBlock))
			if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "SHOW") ||
				strings.HasPrefix(upper, "DESCRIBE") || strings.HasPrefix(upper, "EXPLAIN") ||
				strings.HasPrefix(upper, "WITH") {
				return codeBlock
			}
			content = content[:startIdx]
		}
	}
	return ""
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

func buildChatModel(ctx context.Context, cfg *admin.AIConfig) (model.ToolCallingChatModel, error) {
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
		}
		if cfg.Temperature > 0 {
			t := cfg.Temperature
			openaiCfg.Temperature = &t
		}
		if cfg.MaxTokens > 0 {
			openaiCfg.MaxTokens = &cfg.MaxTokens
		}
		return openai.NewChatModel(ctx, openaiCfg)
	default:
		return nil, fmt.Errorf("不支持的 AI 提供商：%s", cfg.Provider)
	}
}

func buildTools(_ context.Context, connID, dbType, dbSchema string, auditCtx *ExecAuditCtx) ([]tool.BaseTool, error) {
	queryTool, _ := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 查询并返回结果", NewQueryFunc(connID))
	execTool, _ := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connID, auditCtx))
	schemaTool, _ := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息", NewSchemaFunc(connID, dbType, dbSchema))
	exportExcelTool, _ := utils.InferTool("export_excel", "导出 Excel 表格数据", NewExportExcelFunc(connID))
	exportExcelChartTool, _ := utils.InferTool("export_excel_with_chart", "导出带图表的 Excel", NewExportExcelWithChartFunc(connID))
	exportPPTTool, _ := utils.InferTool("export_ppt", "生成 PPT 演示文稿", NewExportPPTFunc(connID))
	exportDocxTool, _ := utils.InferTool("export_analysis_docx", "生成数据分析报告（Word）", NewExportAnalysisDocxFunc(connID))
	importDataTool, _ := utils.InferTool("import_data", "将用户上传的 Excel 数据导入到指定数据库表中", NewImportDataFunc(connID, dbType, dbSchema))

	allTools := []tool.BaseTool{queryTool, execTool, schemaTool, exportExcelTool, exportExcelChartTool, exportPPTTool, exportDocxTool, importDataTool}
	// 过滤掉 nil（InferTool 失败时）
	var validTools []tool.BaseTool
	for _, t := range allTools {
		if t != nil {
			validTools = append(validTools, t)
		}
	}
	return validTools, nil
}

// ──────────────────────────────────────────────
// 系统提示词
// ──────────────────────────────────────────────

func buildSystemPrompt(dbType, dbSchema, dbVersion string, tableContext []string, scope *PermissionScope) string {
	var sb strings.Builder

	sb.WriteString("你是企业的首席数据架构师兼资深数据分析师。你精通标准 SQL 以及 MySQL、MariaDB、Oracle 等多种方言特性。你不仅能够写出极致优化、安全高效的 SQL 代码，还具备将数据转化为可执行商业洞察的强大分析能力。\n\n")
	fmt.Fprintf(&sb, "数据库类型：%s，版本：%s，Schema：%s\n", dbType, dbVersion, dbSchema)

	if len(tableContext) > 0 {
		fmt.Fprintf(&sb, "\n用户指定的数据表：%s\n", strings.Join(tableContext, ", "))
		sb.WriteString("只能使用这些表，不允许查询其他表。如果无法仅用这些表回答，请说明需要哪些额外的表。\n")
	} else {
		sb.WriteString("\n用户未指定数据表，可使用 get_table_schema 查询已授权表的结构。\n")
	}

	sb.WriteString(scope.DescribeForPrompt())

	sb.WriteString(`

## 核心原则
1. 数据准确性最高优先级，执行前必须验证表结构和字段名
2. 禁止使用 SELECT *，必须明确指定字段，除非用户明确要求导出所有列
3. 大表查询必须添加 WHERE 条件和 LIMIT
4. 回复中必须明确告知数据来源表名

## 工作流程
1. 理解需求 → 2. 调用 get_table_schema 验证表结构 → 3. 生成 SQL → 4. 执行 → 5. 回复（含表名）

## 写操作处理（红线）
- 所有写操作必须调用 exec_sql 工具，系统会自动拦截并推送到前端由用户确认
- AI 不得绕过此机制，这是必须遵守的安全红线

## 导出操作
- export_excel / export_excel_with_chart：必须提供 sql 参数
- export_analysis_docx / export_ppt：支持 content 模式（推荐）和 sql 模式

## 数据导入操作
当用户上传了 Excel 文件并要求导入数据时：
1. 先调用 get_table_schema 获取目标表结构
2. 直接调用 import_data 工具，传入 fileId 和 tableName
3. 后端会自动按列名匹配

## 多轮对话
你拥有完整的对话历史记忆。"刚才的""上面的""这个结果"等都指上一次查询。

## 错误处理
当工具调用失败时，系统会自动将错误信息反馈给你。请根据错误信息重新分析，不要重复使用相同的错误参数。
`)
	return sb.String()
}
