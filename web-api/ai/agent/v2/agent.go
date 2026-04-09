// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	admin "go-web/web-api/admin"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
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
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	SQL     string `json:"sql,omitempty"`
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
	PendingSQL   string     `json:"pendingSQL,omitempty"`
	ExcelData    *ExcelData `json:"excelData,omitempty"`
}

// ExcelData 前端上传的 Excel 文件信息（不含原始数据，数据在后端暂存区）
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
// SQLAgent
// ──────────────────────────────────────────────

// SQLAgent SQL 智能体
type SQLAgent struct {
	agent    *adk.ChatModelAgent
	sessions *SessionStore
	dbType   string
	dbSchema string
	scope    *PermissionScope
}

// 最大保留的消息轮数
const maxHistoryRounds = 20

// NewSQLAgent 创建 SQL 智能体
func NewSQLAgent(ctx context.Context, cfg *admin.AIConfig, connID, dbType, dbSchema, dbVersion string, sessions *SessionStore, scope *PermissionScope) (*SQLAgent, error) {
	cm, err := buildChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建模型失败：%w", err)
	}

	tools, err := buildTools(ctx, connID, dbType, dbSchema)
	if err != nil {
		return nil, fmt.Errorf("创建工具失败：%w", err)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SQLAgent",
		Description: "专业 SQL 助手，支持查询、分析和数据导出",
		Instruction: buildSystemPrompt(dbType, dbSchema, dbVersion, nil, scope),
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
		Handlers: []adk.ChatModelAgentMiddleware{
			&PermissionMiddleware{Scope: scope},
			&ToolErrorRecoveryMiddleware{},
			&SQLSecurityMiddleware{},
		},
		// ChatModel API 临时错误（503、超时等）自动重试
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries: 5,
		},
		// 工具调用错误由 ReAct 循环内部自动处理（错误反馈给模型重新思考），
		// MaxIterations 控制最大循环次数，防止无限重试
		MaxIterations: 25,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Agent 失败：%w", err)
	}

	if sessions == nil {
		sessions, err = NewSessionStore("./data/sessions")
		if err != nil {
			return nil, fmt.Errorf("创建会话存储失败：%w", err)
		}
	}

	return &SQLAgent{
		agent:    agent,
		sessions: sessions,
		dbType:   dbType,
		dbSchema: dbSchema,
		scope:    scope,
	}, nil
}

// RunStream 流式执行，返回 (实际使用的 sessionID, 错误)
// 工具调用错误由 Eino ReAct 循环内部自动处理，ChatModel 临时错误由 ModelRetryConfig 处理。
// 只有不可恢复的错误（如超过 MaxIterations）才会返回 error。
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

	// 保存用户消息
	userMsg := schema.UserMessage(req.Question)
	if err := sess.Append(userMsg); err != nil {
		return sessionID, err
	}

	// 获取并截断历史
	history := sess.GetMessages()
	truncated := truncateHistory(history)
	log.Printf("[Agent] 历史消息 - total=%d, truncated=%d\n", len(history), len(truncated))

	// 权限检查
	if !a.scope.HasAnyAccess() {
		flush(StreamChunk{
			Type:    "error",
			Content: "您暂时没有可访问的数据表权限，请联系管理员开通。",
		})
		flush(StreamChunk{Type: "done"})
		return sessionID, nil
	}

	// 构建消息
	sysPrompt := buildSystemPrompt(a.dbType, a.dbSchema, "", req.TableContext, a.scope)

	// 导出请求自动注入历史 SQL
	if isExportRequest(req.Question) {
		if lastSQL := extractLastSQLFromHistory(truncated); lastSQL != "" {
			sysPrompt += fmt.Sprintf("\n\n⚠️ 用户正在请求导出操作，历史 SQL：\n```sql\n%s\n```\n请直接使用此 SQL 调用导出工具，不要重新生成。", lastSQL)
		}
	}

	// 导入请求注入 Excel 文件上下文（只传 fileId 和列名，不传数据）
	if req.ExcelData != nil && req.ExcelData.FileID != "" {
		sysPrompt += fmt.Sprintf("\n\n📎 用户上传了 Excel 文件（fileId=%s）：\n- 列名：%s\n- 总行数：%d\n",
			req.ExcelData.FileID, strings.Join(req.ExcelData.Columns, ", "), req.ExcelData.TotalRows)
		sysPrompt += "请先用 get_table_schema 获取目标表结构，将 Excel 列名与表字段做严格匹配，展示映射结果给用户确认后，调用 import_data 工具（传入 fileId、tableName、mapping）执行导入。\n"
	}

	messages := []adk.Message{
		&schema.Message{Role: schema.System, Content: sysPrompt},
	}
	messages = append(messages, truncated...)

	// 运行 Agent
	var fullResponse strings.Builder
	var dangerousSQLs []string // 收集所有拦截到的危险 SQL

	iter := a.agent.Run(ctx, &adk.AgentInput{
		Messages:        messages,
		EnableStreaming: true,
	})

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			if dangerousErr, ok := errors.AsType[*DangerousSQLError](event.Err); ok {
				// 收集危险 SQL（不中断流程）
				if len(dangerousErr.SQLList) > 0 {
					dangerousSQLs = append(dangerousSQLs, dangerousErr.SQLList...)
				} else if dangerousErr.SQL != "" {
					dangerousSQLs = append(dangerousSQLs, dangerousErr.SQL)
				}
				log.Printf("[Agent] 危险 SQL 拦截 - sql=%s\n", dangerousErr.SQL)
				continue
			}

			// 其他工具调用错误：返回给 handler 层处理重试
			log.Printf("[Agent] 工具调用失败 - err=%v\n", event.Err)

			// 先把已收集的危险 SQL 推出去
			for _, dsql := range dangerousSQLs {
				flush(StreamChunk{Type: "danger_confirm", Content: "检测到危险 SQL，需要用户确认", SQL: dsql})
			}

			// 保存已有的助手回复
			if fullResponse.Len() > 0 {
				assistantMsg := schema.AssistantMessage(fullResponse.String(), nil)
				_ = sess.Append(assistantMsg)
			}

			return sessionID, event.Err
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

		// 流式输出
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

	// 批量推送所有拦截到的危险 SQL
	if len(dangerousSQLs) > 0 {
		for _, dsql := range dangerousSQLs {
			flush(StreamChunk{Type: "danger_confirm", Content: "检测到危险 SQL，需要用户确认", SQL: dsql})
		}
	}

	// 保存助手消息
	if fullResponse.Len() > 0 {
		assistantMsg := schema.AssistantMessage(fullResponse.String(), nil)
		if err := sess.Append(assistantMsg); err != nil {
			log.Printf("[Agent] 保存助手消息失败 - err=%v\n", err)
		}
	}

	// 自动更新会话标题（取第一条用户消息的前60个字符）
	title := sess.Title()
	if title != "" && title != "New Session" {
		if err := UpdateSessionTitleInDB(sess.ID, title); err != nil {
			log.Printf("[Agent] 更新会话标题失败 - err=%v\n", err)
		}
	}

	flush(StreamChunk{Type: "done"})
	return sessionID, nil
}

// ──────────────────────────────────────────────
// 辅助函数
// ──────────────────────────────────────────────

func truncateHistory(history []*schema.Message) []*schema.Message {
	if len(history) <= maxHistoryRounds*2 {
		return history
	}
	return history[len(history)-maxHistoryRounds*2:]
}

func isExportRequest(question string) bool {
	q := strings.ToLower(question)
	keywords := []string{"导出", "export", "下载", "excel", "ppt", "word", "图表"}
	for _, kw := range keywords {
		if strings.Contains(q, kw) {
			return true
		}
	}
	return false
}

func extractLastSQLFromHistory(history []*schema.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role != schema.Assistant || msg.Content == "" {
			continue
		}
		startIdx := strings.LastIndex(msg.Content, "```")
		if startIdx == -1 {
			continue
		}
		endIdx := strings.Index(msg.Content[startIdx+3:], "```")
		if endIdx == -1 {
			continue
		}
		endIdx = startIdx + 3 + endIdx
		codeBlock := strings.TrimSpace(msg.Content[startIdx+3 : endIdx])
		if idx := strings.Index(codeBlock, "\n"); idx != -1 {
			firstLine := strings.TrimSpace(strings.Split(codeBlock, "\n")[0])
			if strings.EqualFold(firstLine, "sql") {
				codeBlock = strings.TrimSpace(codeBlock[idx+1:])
			}
		}
		upper := strings.ToUpper(strings.TrimSpace(codeBlock))
		if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "SHOW") ||
			strings.HasPrefix(upper, "DESCRIBE") || strings.HasPrefix(upper, "EXPLAIN") {
			return codeBlock
		}
	}
	return ""
}

// ──────────────────────────────────────────────
// 模型与工具构建
// ──────────────────────────────────────────────

func buildChatModel(ctx context.Context, cfg *admin.AIConfig) (model.ToolCallingChatModel, error) {
	switch cfg.Provider {
	case "ollama":
		ollamaCfg := &ollama.ChatModelConfig{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			Timeout: 30 * time.Minute,
		}
		if cfg.EnableThinking {
			ollamaCfg.Thinking = &ollama.ThinkValue{Value: true}
		}
		if cfg.Temperature > 0 {
			ollamaCfg.Options = &ollama.Options{Temperature: cfg.Temperature}
		}
		return ollama.NewChatModel(ctx, ollamaCfg)
	case "openai":
		openaiCfg := &openai.ChatModelConfig{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			APIKey:  cfg.ApiKey,
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

func buildTools(_ context.Context, connID, dbType, dbSchema string) ([]tool.BaseTool, error) {
	currentDateTimeTool, err := utils.InferTool("current_date_time", "获取当前日期时间", GetCurrentDateTime())
	if err != nil {
		return nil, fmt.Errorf("创建 current_date_time 工具失败：%w", err)
	}

	queryTool, err := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN 查询并返回结果", NewQueryFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建 query_data 工具失败：%w", err)
	}

	execTool, err := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建 exec_sql 工具失败：%w", err)
	}

	schemaTool, err := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息", NewSchemaFunc(connID, dbType, dbSchema))
	if err != nil {
		return nil, fmt.Errorf("创建 get_table_schema 工具失败：%w", err)
	}

	exportExcelTool, err := utils.InferTool("export_excel", "导出 Excel 表格数据", NewExportExcelFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建 export_excel 工具失败：%w", err)
	}

	exportExcelChartTool, err := utils.InferTool("export_excel_with_chart", "导出带图表的 Excel（折线图/柱状图/饼图/散点图）", NewExportExcelWithChartFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建 export_excel_with_chart 工具失败：%w", err)
	}

	exportPPTTool, err := utils.InferTool("export_ppt", "生成 PPT 演示文稿", NewExportPPTFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建 export_ppt 工具失败：%w", err)
	}

	exportImageTool, err := utils.InferTool("export_analysis_image", "生成数据分析图表（PNG）", NewExportAnalysisImageFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建 export_analysis_image 工具失败：%w", err)
	}

	exportDocxTool, err := utils.InferTool("export_analysis_docx", "生成数据分析报告（Word）", NewExportAnalysisDocxFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建 export_analysis_docx 工具失败：%w", err)
	}

	importDataTool, err := utils.InferTool("import_data", "将用户上传的 Excel 数据导入到指定数据库表中。需要提供表名、列名列表和数据行。支持 insert（仅插入）和 upsert（有主键则更新无则插入）两种模式。", NewImportDataFunc(connID, dbType, dbSchema))
	if err != nil {
		return nil, fmt.Errorf("创建 import_data 工具失败：%w", err)
	}

	return []tool.BaseTool{
		queryTool, execTool, schemaTool, currentDateTimeTool,
		exportExcelTool, exportExcelChartTool, exportPPTTool,
		exportImageTool, exportDocxTool, importDataTool,
	}, nil
}

// ──────────────────────────────────────────────
// 系统提示词
// ──────────────────────────────────────────────

func buildSystemPrompt(dbType, dbSchema, dbVersion string, tableContext []string, scope *PermissionScope) string {
	var sb strings.Builder

	sb.WriteString("你是企业的首席数据架构师兼资深数据分析师。你精通标准 SQL (ANSI SQL) 以及MySQL、MariaDB、Oracle等多种方言特性。你不仅能够写出极致优化、安全高效的 SQL 代码，还具备将数据转化为可执行商业洞察的强大分析能力。善于帮助用户查询和分析数据，也总能用客观严谨、严肃又不失幽默的语言向用户表达。\n\n")
	fmt.Fprintf(&sb, "数据库类型：%s，版本：%s，Schema：%s\n", dbType, dbVersion, dbSchema)

	// 表上下文
	if len(tableContext) > 0 {
		fmt.Fprintf(&sb, "\n用户指定的数据表：%s\n", strings.Join(tableContext, ", "))
		sb.WriteString("只能使用这些表，不允许查询其他表。如果无法仅用这些表回答，请说明需要哪些额外的表。\n")
	} else {
		sb.WriteString("\n用户未指定数据表，可使用 get_table_schema 查询已授权表的结构。\n")
	}

	// 权限描述
	sb.WriteString(scope.DescribeForPrompt())

	sb.WriteString(`

## 核心原则
1. 数据准确性最高优先级，执行前必须验证表结构和字段名
2. 禁止使用 SELECT *，必须明确指定字段
3. 大表查询必须添加 WHERE 条件和 LIMIT
4. 回复中必须明确告知数据来源表名

## 工作流程
1. 理解需求 → 2. 调用 get_table_schema 验证表结构 → 3. 生成 SQL → 4. 执行 → 5. 回复（含表名）

## 写操作处理（红线）
- 所有写操作（INSERT/UPDATE/DELETE/DROP/ALTER/CREATE/TRUNCATE）必须调用 exec_sql 工具
- 系统会自动拦截并推送到前端由用户确认，用户确认后才会执行
- AI 不得绕过此机制，这是必须遵守的安全红线
- 写操作执行后会自动记入 SQL 审计日志

## 数据洞察与叙事（核心价值）
- **结果解读**：用自然语言概括数据结果。例如：“共返回 315 行记录，总金额 108 万元。”
- **异常发现**：主动指出数据中的离群值、空值占比或违反常识的波动（例如：“同比下滑 30% 是一个显著风险信号”）。
- **行动建议**：基于数据提出 1-2 条具体的、低成本的改进建议。

## 导出操作
当用户说"导出""下载""Excel""PPT""Word"等，从对话历史中找到最近的 SQL 直接调用导出工具，不要重新生成 SQL。

## 数据导入操作（重要）
当用户上传了 Excel 文件并要求导入数据时：
1. 用户消息中会包含 fileId 和 Excel 列名信息（由系统自动注入）
2. 你需要先调用 get_table_schema 获取目标表结构
3. 将 Excel 列名与表字段进行严格匹配（必须完全相等，大小写不敏感）
4. 如果有任何 Excel 列名在表中找不到对应字段，必须立即反馈给用户，要求确认或重新上传
5. 匹配成功后，向用户展示完整的字段映射表（Excel列名 → 数据库字段名），请求用户确认
6. 用户确认后，调用 import_data 工具执行导入，传入 fileId、tableName 和 mapping
7. mapping 格式：{"Excel列名": "数据库字段名", ...}，必须包含所有 Excel 列
8. 导入模式：有主键的表支持 upsert（自动更新或插入），无主键表仅支持 insert
9. 对于 upsert 模式，必须明确告知用户将会更新已有数据，等待用户确认
10. 字段匹配是数据安全红线，绝不允许将数据导入到错误的字段

## 多轮对话
你拥有完整的对话历史记忆。"刚才的""上面的""这个结果"等都指上一次查询。

## 错误处理
当工具调用失败时，系统会自动将错误信息反馈给你。请根据错误信息重新分析：
- 如果是字段名错误，先用 get_table_schema 验证正确的字段名
- 如果是 SQL 语法错误，根据数据库类型调整语法
- 不要重复使用相同的错误参数调用工具
`)

	return sb.String()
}

// ──────────────────────────────────────────────
// Session 管理
// ──────────────────────────────────────────────

// Session 持有单个对话的内存状态
type Session struct {
	ID        string
	CreatedAt time.Time
	filePath  string
	mu        sync.Mutex
	messages  []*schema.Message
}

func (s *Session) Append(msg *schema.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, msg)

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

func (s *Session) GetMessages() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*schema.Message, len(s.messages))
	copy(result, s.messages)
	return result
}

func (s *Session) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, msg := range s.messages {
		if msg.Role == schema.User && msg.Content != "" {
			title := msg.Content
			if len([]rune(title)) > 60 {
				title = string([]rune(title)[:60]) + "..."
			}
			return title
		}
	}
	return "New Session"
}

func (s *Session) GetDetail() SessionDetail {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := make([]SessionDetailMessage, 0, len(s.messages))
	for _, msg := range s.messages {
		msgs = append(msgs, SessionDetailMessage{Role: string(msg.Role), Content: msg.Content})
	}
	return SessionDetail{ID: s.ID, Title: s.Title(), CreatedAt: s.CreatedAt, Messages: msgs}
}

// SessionStore 管理会话存储
type SessionStore struct {
	dir   string
	mu    sync.Mutex
	cache map[string]*Session
}

func NewSessionStore(dir string) (*SessionStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建会话目录失败：%w", err)
	}
	return &SessionStore{dir: dir, cache: make(map[string]*Session)}, nil
}

func (s *SessionStore) GetOrCreate(id, userID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.cache[id]; ok {
		return sess, nil
	}

	filePath := filepath.Join(s.dir, id+".jsonl")
	var (
		sess *Session
		err  error
	)
	if _, statErr := os.Stat(filePath); os.IsNotExist(statErr) {
		sess, err = createSession(id, userID, filePath)
	} else {
		sess, err = loadSession(filePath)
	}
	if err != nil {
		return nil, err
	}
	s.cache[id] = sess
	return sess, nil
}

func (s *SessionStore) ListByUserID(userID string) ([]SessionMeta, error) {
	sessionsDB, err := ListSessionsByUserID(userID)
	if err != nil {
		return nil, err
	}
	metas := make([]SessionMeta, 0, len(sessionsDB))
	for _, sessDB := range sessionsDB {
		title := sessDB.Title
		// 如果数据库中标题为空，尝试从文件加载
		if title == "" {
			if sess, err := s.GetOrCreate(sessDB.ID, sessDB.UserID); err == nil {
				title = sess.Title()
				// 回写到数据库
				if title != "" && title != "New Session" {
					_ = UpdateSessionTitleInDB(sessDB.ID, title)
				}
			}
		}
		if title == "" {
			title = "未命名会话"
		}
		metas = append(metas, SessionMeta{ID: sessDB.ID, Title: title, CreatedAt: sessDB.CreatedAt})
	}
	return metas, nil
}

func (s *SessionStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filePath := filepath.Join(s.dir, id+".jsonl")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(s.cache, id)
	// 同步删除数据库记录
	_ = DeleteSessionInDB(id)
	return nil
}

func (s *SessionStore) GetDetail(id string) (*SessionDetail, error) {
	sessDB, err := GetSessionByID(id)
	if err != nil {
		return nil, err
	}
	if sessDB == nil {
		return nil, fmt.Errorf("会话不存在：%s", id)
	}
	sess, err := s.GetOrCreate(id, sessDB.UserID)
	if err != nil {
		return nil, err
	}
	detail := sess.GetDetail()
	return &detail, nil
}

// ──────────────────────────────────────────────
// Session 文件操作
// ──────────────────────────────────────────────

type sessionHeader struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

func createSession(id, userID, filePath string) (*Session, error) {
	header := sessionHeader{Type: "session", ID: id, UserID: userID, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filePath, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	if err := CreateSessionInDB(id, userID, "", filePath); err != nil {
		log.Printf("[createSession] 创建数据库记录失败 - id=%s, err=%v\n", id, err)
	}
	return &Session{ID: id, CreatedAt: header.CreatedAt, filePath: filePath, messages: make([]*schema.Message, 0)}, nil
}

func loadSession(filePath string) (*Session, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return nil, fmt.Errorf("空的会话文件：%s", filePath)
	}
	var header sessionHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, fmt.Errorf("会话头部损坏：%w", err)
	}

	sess := &Session{ID: header.ID, CreatedAt: header.CreatedAt, filePath: filePath, messages: make([]*schema.Message, 0)}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg schema.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		sess.messages = append(sess.messages, &msg)
	}
	return sess, scanner.Err()
}
