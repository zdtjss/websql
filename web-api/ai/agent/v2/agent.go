// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	admin "go-web/web-api/admin"
	"go-web/web-api/ai/agent/v2/export"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
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
	export.InitSkillEnv()

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
			ContextTokens: 10000000,
		},
	})
	if err != nil {
		log.Printf("[Agent] 创建摘要中间件失败 - err=%v\n", err)
		return nil, fmt.Errorf("创建摘要中间件失败：%w", err)
	}

	osfsBackend := export.NewOSFilesystemBackend()
	skillsDir := filepath.Join(export.GetSkillScriptsBaseDir(), "skills")

	fsm, err := filesystem.New(ctx, &filesystem.MiddlewareConfig{
		Backend:        osfsBackend,
		StreamingShell: osfsBackend,
	})
	if err != nil {
		log.Printf("[Agent] 创建 Filesystem 中间件失败 - err=%v\n", err)
		return nil, fmt.Errorf("创建文件系统中间件失败：%w", err)
	}

	skillBackend, err := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{
		Backend: osfsBackend,
		BaseDir: skillsDir,
	})
	if err != nil {
		log.Printf("[Agent] 创建 Skill Backend 失败 - err=%v\n", err)
		return nil, fmt.Errorf("创建 Skill Backend 失败：%w", err)
	}

	sm, err := skill.NewMiddleware(ctx, &skill.Config{
		Backend: skillBackend,
	})
	if err != nil {
		log.Printf("[Agent] 创建 Skill 中间件失败 - err=%v\n", err)
		return nil, fmt.Errorf("创建 Skill 中间件失败：%w", err)
	}

	var permAgentTool tool.BaseTool
	if scope.IsRemote && !scope.HasFullConnAccess {
		permAgent, err := NewPermissionAgent(ctx, cfg, connID, dbType, dbSchema, scope.UserID)
		if err != nil {
			log.Printf("[Agent] 创建权限审核 Agent 失败，回退到程序化检查 - err=%v\n", err)
		} else {
			permAgentTool = adk.NewAgentTool(ctx, permAgent)
		}
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SQLAgent",
		Description: "专业 SQL 助手，支持跨库查询、多 Schema 数据组合分析、数据导入导出和报告生成",
		Instruction: buildSystemPrompt(dbType, dbSchema, dbVersion, nil, scope, nil),
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		Handlers: []adk.ChatModelAgentMiddleware{
			&ToolCallLoggingMiddleware{},
			&PermissionMiddleware{Scope: scope, PermAgent: permAgentTool},
			&DangerousSQLApprovalMiddleware{},
			&ToolErrorRecoveryMiddleware{},
			summarizationMW,
			fsm,
			sm,
		},
		MaxIterations: 200,
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

	sysPrompt := buildSystemPrompt(a.dbType, a.dbSchema, "", req.TableContext, a.scope, req.Schemas)

	if isExportRequest(req.Question) {
		if lastSQL := extractLastSQLFromSessionMessages(truncated); lastSQL != "" {
			sysPrompt += fmt.Sprintf("\n\n⚠️ 用户正在请求导出操作，历史 SQL：\n```sql\n%s\n```\n如果用户要求导出 Excel，请直接使用此 SQL 调用导出工具；如果用户要求导出 Word/PPT 报告，优先使用 content 模式将分析结果传入。", lastSQL)
		}
	}

	if req.ExcelData != nil && req.ExcelData.FileID != "" {
		sysPrompt += fmt.Sprintf("\n\n📎 用户上传了 Excel 文件（fileId=%s）：\n- 列名：%s\n- 总行数：%d\n",
			req.ExcelData.FileID, strings.Join(req.ExcelData.Columns, ", "), req.ExcelData.TotalRows)
		sysPrompt += "请先用 get_table_schema 确认目标表存在并获取表结构，然后向用户明确说明：1）目标表名 2）操作模式（插入/更新/插入+更新）3）字段映射关系 4）预计影响行数。等用户确认后再调用 import_data 工具。如果用户没有指定目标表，请询问用户。\n"
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

	fullResponse, _ := a.processEvents(iter, flush, sess, checkPointID)

	if fullResponse.Len() > 0 {
		if err := sess.Append("assistant", fullResponse.String()); err != nil {
			log.Printf("[Agent] 保存助手消息失败 - err=%v\n", err)
		}
	}

	log.Printf("[Agent] 执行完毕 - sessionID=%s\n", sessionID)

	return sessionID, nil
}

// ResumeStream 恢复被中断的执行（用户确认/取消后）
// 当再次被中断时（如 LLM 生成了新的危险 SQL），不发送 done，让前端继续等待用户确认
func (a *SQLAgent) ResumeStream(ctx context.Context, checkPointID string, targets map[string]bool, flush func(StreamChunk), sess *Session) error {
	log.Printf("[Agent] resume - cpID=%s, targets=%v\n", checkPointID, targets)

	// 将所有 interruptID 放入 Targets map，一次性恢复
	// 使用 SQLApprovalResult 传递审批结果，支持拒绝原因
	targetsAny := make(map[string]any, len(targets))
	for id, approved := range targets {
		targetsAny[id] = SQLApprovalResult{Approved: approved}
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
			if errors.Is(event.Err, context.Canceled) || errors.Is(event.Err, context.DeadlineExceeded) {
				break
			}
			if errors.Is(event.Err, adk.ErrExceedMaxIterations) || strings.Contains(event.Err.Error(), "exceeds max iterations") {
				flush(StreamChunk{Type: "error", Content: "AI 处理步骤过多，部分查询尝试未完成。已执行的操作可能已生效，请检查数据或精简问题后重试。"})
				break
			}
			flush(StreamChunk{Type: "error", Content: "AI 处理出错，请稍后重试"})
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
	conn, _ := GetConn(connID)
	queryTool, _ := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 查询并返回结果", NewQueryFunc(connID))
	execTool, _ := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connID, auditCtx))
	schemaTool, _ := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息", NewSchemaFunc(connID, dbType, dbSchema))
	exportExcelTool, _ := utils.InferTool("export_excel", "导出 Excel 表格数据", export.NewExportExcelFunc(conn))
	exportExcelChartTool, _ := utils.InferTool("export_excel_with_chart", "导出带图表的 Excel", export.NewExportExcelWithChartFunc(conn))
	exportPPTTool, _ := utils.InferTool("export_ppt", "生成 PPT 演示文稿", export.NewExportPPTFunc(conn))
	exportDocxTool, _ := utils.InferTool("export_analysis_docx", "生成数据分析报告（Word）", export.NewExportAnalysisDocxFunc(conn))
	importDataTool, _ := utils.InferTool("import_data", "将用户上传的 Excel 数据导入到指定数据库表中", NewImportDataFunc(connID, dbType, dbSchema))
	// 获取当前日期、星期几和时间  不是所有模型都支持正确使用SQL获取当前日期信息
	currentDateInfoTool, _ := utils.InferTool("get_current_date_info", "获取当前日期、星期几和时间", GetCurrentDateInfo())

	allTools := []tool.BaseTool{queryTool, execTool, schemaTool, exportExcelTool, exportExcelChartTool, exportPPTTool, exportDocxTool, importDataTool, currentDateInfoTool}
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

func buildSystemPrompt(dbType, dbSchema, dbVersion string, tableContext []string, scope *PermissionScope, schemas []SchemaRef) string {
	var sb strings.Builder

	sb.WriteString("你是企业的首席数据架构师兼资深数据分析师。")
	sb.WriteString("你精通标准 SQL（SQL-92/99/2003），以及 ")
	fmt.Fprintf(&sb, "%s 的方言特性、索引策略和查询优化技巧。", dbType)
	sb.WriteString("你不仅写出极致优化、安全高效的 SQL，还擅长将查询结果转化为富有洞察且具有中国特色的分析结论。")
	sb.WriteString("\n\n")

	if len(schemas) > 1 {
		fmt.Fprintf(&sb, "当前环境 — 数据库：%s，版本：%s\n", dbType, dbVersion)
		sb.WriteString("**多 Schema 上下文**：用户选择了以下 schema，你可以进行跨库查询与数据组合：\n")
		for i, s := range schemas {
			fmt.Fprintf(&sb, "  [%d] 连接ID=%s, Schema=%s\n", i+1, s.ConnID, s.Schema)
		}
	} else {
		fmt.Fprintf(&sb, "当前环境 — 数据库：%s，版本：%s，Schema：%s\n", dbType, dbVersion, dbSchema)
	}

	if len(tableContext) > 0 {
		fmt.Fprintf(&sb, "\n用户指定表范围：%s\n", strings.Join(tableContext, ", "))
		sb.WriteString("只能在这些表上操作。若需求无法仅用这些表满足，请明确告知需要哪些额外表。\n")
	} else {
		sb.WriteString("\n用户未限定表范围，你可以调用 get_table_schema 探索已授权表的结构。\n")
	}

	sb.WriteString(scope.DescribeForPrompt())

	sb.WriteString(`

## 行为准则（必须遵守）
1. 准确性第一：生成 SQL 前必须通过 get_table_schema 验证表名和字段名，禁止臆测
2. 禁止 SELECT *：必须显式列出所需字段，除非用户明确要求导出全部列
3. 控制查询量：对大表查询必须添加合理的 WHERE 条件并配合 LIMIT
4. 透明可追溯：每次查询/操作后必须在回复中明确说明来源表名和影响范围
5. **禁止假执行**：当用户要求导出/生成文件时，必须通过调用 export_excel / export_ppt / export_analysis_docx 工具实际执行，绝不能只输出文本描述"已完成导出"，更不能凭空编造下载链接或文件名。如果你没有调用工具，就绝不能声称文件已生成。
`)

	if len(schemas) > 1 {
		sb.WriteString(`
## 跨库查询与数据组合
你被授权访问多个 schema，可以进行跨库数据关联和分析。请注意：

1. **跨库 JOIN / UNION**：使用 ` + dbType + ` 支持的跨库语法链接不同 schema 的表数据
2. **大数据量防范**：跨库组合可能导致结果集非常大，**务必使用 LIMIT 或聚合函数控制返回行数**
3. **上下文溢出保护**：如果一次查询返回几万行数据，会超出大模型的上下文窗口，导致分析中断
   - 优先使用聚合查询（SUM、COUNT、AVG 等）返回统计结果
   - 对明细数据，如果需要导出完整数据集，请调用 export_excel 工具
   - 对多表关联产生的大结果集，先分析数据量（COUNT），再分页查询
4. **Python 脚本分析**：当需要进行复杂的跨库大数据量统计分析时，系统已预置 ` + "`cross-db-analysis`" + ` Python 脚本工具
   - 脚本可直接连接各数据库，在数据库中完成聚合计算，只返回分析结论
   - 适用于跨库数据量大于 10 万行或需要进行复杂统计模型计算的场景
   - 触发时机：用户要求"分析"、"对比"、"统计"多个库的数据时，优先考虑使用脚本

## 标准工作流程`)
	} else {
		sb.WriteString(`
## 标准工作流程`)
	}

	sb.WriteString(`
执行每个数据分析任务时，按以下步骤推进：
1. 理解需求 — 澄清模棱两可的表达、确认统计口径（去重？含空值？）、明确时间范围
2. 探索结构 — 调用 get_table_schema 获取相关表的字段、类型、索引信息
3. 编写 SQL — 基于真实字段名和数据类型编写优化 SQL，确保与 ` + dbType + ` 方言兼容
4. 执行查询 — 调用 query_data（读）或 exec_sql（写）
5. 解读结果 — 不仅返回数据，还要给出 2-5 行的分析小结（趋势、异常、业务建议）
6. 当涉及写操作时，在步骤4之前先向用户说明将要执行的操作，等待系统推送确认

## 工具使用指南
| 工具 | 用途与约束 |
|------|-----------|
| get_table_schema | 获取表结构（建表 DDL）。每次查询新表前必调。支持一次传入多个表名 |
| query_data | 执行只读 SQL（SELECT / SHOW / DESCRIBE / EXPLAIN / WITH） |
| exec_sql | 执行写操作 SQL（INSERT / UPDATE / DELETE / ALTER 等）。系统会拦截并推送前端确认，你无需额外处理 |
| export_excel | 导出 Excel，必须传入 sql 参数。必须实际调用此工具，禁止仅文字描述 |
| export_excel_with_chart | 导出带图表的 Excel，传入 sql。必须实际调用此工具。图表类型根据数据特征自动选择 |
| export_ppt | 生成 PPT 报告，优先使用 content 模式（直接传入分析文本）避免重复查询。必须实际调用此工具才能生成文件 |
| export_analysis_docx | 生成 Word 分析报告，优先使用 content 模式。必须实际调用此工具才能生成文件 |
| import_data | 导入 Excel 数据到指定表。使用前须确认目标表名、操作模式、字段映射、影响行数 |

## SQL 编写规范（` + dbType + `）
` + getSQLDialectRules(dbType) + `

## 写操作安全
- 所有写操作必须通过 exec_sql 工具执行，系统会自动拦截并推送前端由用户确认
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

## 迭代次数限制（重要）
你的每次思考与工具调用都会消耗 1 次迭代，你有 20 次迭代上限。请高效利用：
- 当 query_data 连续 2 次返回空结果（如不同 schema 下都查不到表），立即停止尝试，直接告知用户该表不存在
- 不要在同一个问题上反复尝试不同变体（如猜不同的 schema 名、表名变体），这会在几次内耗尽迭代上限
- 优先使用一次聚合查询获取概况，而非多次明细查询
- 如果发现在重复踩同一个错误，停下来思考替代方案，而不是继续硬试
`)
	return sb.String()
}

func getSQLDialectRules(dbType string) string {
	base := "- 字段名和表名若含特殊字符或关键字，使用反引号包裹\n"
	base += "- 字符串比较注意字符集和排序规则\n"

	switch strings.ToLower(dbType) {
	case "mysql", "mariadb":
		return base +
			"- 优先使用 EXPLAIN 分析执行计划，检查是否走索引\n" +
			"- 字符串模糊匹配优先 LIKE 'prefix%'（可利用索引），避免 LIKE '%middle%'\n" +
			"- 日期函数使用 DATE_FORMAT、DATE_ADD、DATEDIFF 等\n" +
			"- 分页优先使用 LIMIT offset, count\n" +
			"- 注意 ONLY_FULL_GROUP_BY 模式，GROUP BY 的字段必须在 SELECT 中出现或使用聚合函数\n" +
			"- 多表 JOIN 时注意驱动表选择，小表驱动大表\n"
	case "oracle":
		return base +
			"- 使用 EXPLAIN PLAN FOR 分析执行计划\n" +
			"- 分页使用 ROWNUM 或 OFFSET/FETCH（12c+），注意 ROWNUM 是在排序前计算的\n" +
			"- 日期函数使用 TO_DATE、TO_CHAR、ADD_MONTHS 等\n" +
			"- 字符串连接使用 || 而非 CONCAT\n" +
			"- 注意空字符串在 Oracle 中等价于 NULL\n" +
			"- Dual 表用于无表查询，如 SELECT SYSDATE FROM DUAL\n"
	case "postgresql", "postgres":
		return base +
			"- 使用 EXPLAIN ANALYZE 分析实际执行计划\n" +
			"- 字段名和表名使用双引号包裹（若含大写或特殊字符）\n" +
			"- 分页使用 LIMIT count OFFSET offset\n" +
			"- 日期函数使用 TO_CHAR、DATE_TRUNC、AGE 等\n" +
			"- 字符串拼接使用 || 或 CONCAT\n" +
			"- 注意 PostgreSQL 的 MVCC 特性，大量更新后建议 VACUUM\n"
	case "sqlite":
		return base +
			"- 使用 EXPLAIN QUERY PLAN 分析查询计划\n" +
			"- 日期函数使用 strftime、date、time、datetime\n" +
			"- 字符串拼接使用 ||\n" +
			"- AUTOINCREMENT 仅用于 INTEGER PRIMARY KEY\n" +
			"- 写操作会锁定整个数据库，避免长事务\n"
	case "sqlserver", "mssql":
		return base +
			"- 使用 SET STATISTICS IO ON 查看 IO 统计\n" +
			"- 分页使用 OFFSET/FETCH（2012+）或 ROW_NUMBER() OVER()\n" +
			"- 日期函数使用 FORMAT、DATEADD、DATEDIFF 等\n" +
			"- 使用 TOP 限制返回行数（旧版本），新版本用 OFFSET/FETCH\n" +
			"- 字符串拼接使用 + 或 CONCAT（2012+）\n"
	default:
		return base +
			"- 使用 EXPLAIN 分析执行计划\n" +
			"- 遵循标准 SQL 语法，避免数据库特有的非标准扩展\n"
	}
}
