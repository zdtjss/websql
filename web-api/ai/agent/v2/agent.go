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
	SessionID    string   `json:"sessionId"`
	UserID       string   `json:"userId"`
	ConnID       string   `json:"connId"`
	Schema       string   `json:"schema"`
	Question     string   `json:"question"`
	TableContext []string `json:"tableContext"`
	Confirmed    bool     `json:"confirmed,omitempty"`
	PendingSQL   string   `json:"pendingSQL,omitempty"`
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
			&SQLSecurityMiddleware{},
		},
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

// RunStream 流式执行
func (a *SQLAgent) RunStream(ctx context.Context, req ChatRequest, flush func(StreamChunk)) error {
	log.Printf("[Agent] 开始执行 - sessionID=%s, userID=%s, connID=%s\n", req.SessionID, req.UserID, req.ConnID)

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("%s_%d_%d", req.UserID, time.Now().UnixNano(), time.Now().UnixMilli())
		log.Printf("[Agent] 新建会话 - sessionID=%s\n", sessionID)
	}
	if req.UserID == "" {
		return fmt.Errorf("userId 不能为空")
	}

	sess, err := a.sessions.GetOrCreate(sessionID, req.UserID)
	if err != nil {
		return err
	}
	flush(StreamChunk{Type: "session", Content: sess.ID})

	// 保存用户消息
	userMsg := schema.UserMessage(req.Question)
	if err := sess.Append(userMsg); err != nil {
		return err
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
		return nil
	}

	// 构建消息
	sysPrompt := buildSystemPrompt(a.dbType, a.dbSchema, "", req.TableContext, a.scope)

	// 导出请求自动注入历史 SQL
	if isExportRequest(req.Question) {
		if lastSQL := extractLastSQLFromHistory(truncated); lastSQL != "" {
			sysPrompt += fmt.Sprintf("\n\n⚠️ 用户正在请求导出操作，历史 SQL：\n```sql\n%s\n```\n请直接使用此 SQL 调用导出工具，不要重新生成。", lastSQL)
		}
	}

	messages := []adk.Message{
		&schema.Message{Role: schema.System, Content: sysPrompt},
	}
	messages = append(messages, truncated...)

	// 运行 Agent
	iter := a.agent.Run(ctx, &adk.AgentInput{
		Messages:        messages,
		EnableStreaming: true,
	})

	var fullResponse strings.Builder

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			var dangerousErr *DangerousSQLError
			if errors.As(event.Err, &dangerousErr) {
				log.Printf("[Agent] 危险 SQL 拦截 - sql=%s\n", dangerousErr.SQL)
				flush(StreamChunk{Type: "danger_confirm", Content: "检测到危险 SQL，需要用户确认", SQL: dangerousErr.SQL})
				continue
			}
			flush(StreamChunk{Type: "error", Content: event.Err.Error()})
			return event.Err
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

	// 保存助手消息
	if fullResponse.Len() > 0 {
		assistantMsg := schema.AssistantMessage(fullResponse.String(), nil)
		if err := sess.Append(assistantMsg); err != nil {
			log.Printf("[Agent] 保存助手消息失败 - err=%v\n", err)
		}
	}

	flush(StreamChunk{Type: "done"})
	return nil
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

	return []tool.BaseTool{
		queryTool, execTool, schemaTool,
		exportExcelTool, exportExcelChartTool, exportPPTTool,
		exportImageTool, exportDocxTool,
	}, nil
}

// ──────────────────────────────────────────────
// 系统提示词
// ──────────────────────────────────────────────

func buildSystemPrompt(dbType, dbSchema, dbVersion string, tableContext []string, scope *PermissionScope) string {
	var sb strings.Builder

	sb.WriteString("你是一个专业的 SQL 助手，帮助用户查询和分析数据库。\n\n")
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
- AI 不得绕过此机制，这是安全红线
- 写操作执行后会自动记入 SQL 审计日志

## 导出操作
当用户说"导出""下载""Excel""PPT""Word"等，从对话历史中找到最近的 SQL 直接调用导出工具，不要重新生成 SQL。

## 多轮对话
你拥有完整的对话历史记忆。"刚才的""上面的""这个结果"等都指上一次查询。
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
		metas = append(metas, SessionMeta{ID: sessDB.ID, Title: sessDB.Title, CreatedAt: sessDB.CreatedAt})
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
