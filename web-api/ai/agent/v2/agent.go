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

// StreamChunk 流式输出块
type StreamChunk struct {
	Type       string                 `json:"type"`
	Content    string                 `json:"content,omitempty"`
	SQL        string                 `json:"sql,omitempty"`
	ToolResult map[string]interface{} `json:"toolResult,omitempty"`
}

// SQLAgent SQL 智能体
type SQLAgent struct {
	agent     *adk.ChatModelAgent
	sessions  *SessionStore
	dbType    string
	dbSchema  string
	dbVersion string
	scope     *PermissionScope
}

// SessionMeta 提供会话列表的摘要信息
type SessionMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
}

// SessionDetail 提供单个会话的详细信息（包含所有消息）
type SessionDetail struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	CreatedAt time.Time              `json:"createdAt"`
	Messages  []SessionDetailMessage `json:"messages"`
}

// SessionDetailMessage 会话详细信息中的消息
type SessionDetailMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Session 持有单个对话的内存状态
type Session struct {
	ID        string
	CreatedAt time.Time

	filePath string
	mu       sync.Mutex
	messages []*schema.Message
}

// Append 向内存添加消息并持久化到磁盘
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

// GetMessages 返回所有消息的快照
func (s *Session) GetMessages() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]*schema.Message, len(s.messages))
	copy(result, s.messages)
	return result
}

// Title 从第一条用户消息派生显示标题
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

// GetDetail 返回会话的详细信息（包含所有消息）
func (s *Session) GetDetail() SessionDetail {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := make([]SessionDetailMessage, 0, len(s.messages))
	for _, msg := range s.messages {
		msgs = append(msgs, SessionDetailMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}

	return SessionDetail{
		ID:        s.ID,
		Title:     s.Title(),
		CreatedAt: s.CreatedAt,
		Messages:  msgs,
	}
}

// SessionStore 管理以 JSONL 文件支持的持久化会话存储
type SessionStore struct {
	dir   string
	mu    sync.Mutex
	cache map[string]*Session
}

// NewSessionStore 创建一个由给定目录支持的会话存储（如果目录不存在则创建）
func NewSessionStore(dir string) (*SessionStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建会话目录失败：%w", err)
	}
	return &SessionStore{
		dir:   dir,
		cache: make(map[string]*Session),
	}, nil
}

// GetOrCreate 返回指定 id 的会话，如果不存在则创建
func (s *SessionStore) GetOrCreate(id string) (*Session, error) {
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
		sess, err = createSession(id, filePath)
	} else {
		sess, err = loadSession(filePath)
	}
	if err != nil {
		return nil, err
	}

	s.cache[id] = sess
	return sess, nil
}

// List 返回所有已知会话的元数据
func (s *SessionStore) List() ([]SessionMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	var metas []SessionMeta
	for _, e := range entries {
		// 跳过目录和隐藏文件
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// 只处理 .jsonl 后缀的文件
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		// 跳过空 ID（可能是隐藏文件残留）
		if id == "" {
			continue
		}

		if sess, ok := s.cache[id]; ok {
			metas = append(metas, SessionMeta{ID: id, Title: sess.Title(), CreatedAt: sess.CreatedAt})
			continue
		}

		sess, loadErr := loadSession(filepath.Join(s.dir, e.Name()))
		if loadErr != nil {
			continue
		}
		metas = append(metas, SessionMeta{ID: id, Title: sess.Title(), CreatedAt: sess.CreatedAt})
	}
	return metas, nil
}

// ListByUserID 返回指定用户的所有会话元数据
func (s *SessionStore) ListByUserID(userID string) ([]SessionMeta, error) {
	// 现在会话 ID 就是 userId，直接获取该用户的会话
	sess, err := s.GetOrCreate(userID)
	if err != nil {
		return nil, err
	}

	// 返回单个会话（每个用户只有一个会话）
	return []SessionMeta{
		{
			ID:        sess.ID,
			Title:     sess.Title(),
			CreatedAt: sess.CreatedAt,
		},
	}, nil
}

// Delete 删除会话文件并从缓存中移除
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

// GetDetail 获取指定会话的详细信息
func (s *SessionStore) GetDetail(id string) (*SessionDetail, error) {
	sess, err := s.GetOrCreate(id)
	if err != nil {
		return nil, err
	}
	detail := sess.GetDetail()
	return &detail, nil
}

// sessionHeader 是每个会话文件的第一行 JSONL 行
type sessionHeader struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

func createSession(id, filePath string) (*Session, error) {
	header := sessionHeader{
		Type:      "session",
		ID:        id,
		CreatedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filePath, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	return &Session{
		ID:        id,
		CreatedAt: header.CreatedAt,
		filePath:  filePath,
		messages:  make([]*schema.Message, 0),
	}, nil
}

func loadSession(filePath string) (*Session, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// 第一行：头部
	if !scanner.Scan() {
		return nil, fmt.Errorf("空的会话文件：%s", filePath)
	}
	var header sessionHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, fmt.Errorf("%s 中的会话头部损坏：%w", filePath, err)
	}

	sess := &Session{
		ID:        header.ID,
		CreatedAt: header.CreatedAt,
		filePath:  filePath,
		messages:  make([]*schema.Message, 0),
	}

	// 剩余行：消息
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg schema.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue // 跳过格式错误的行
		}
		sess.messages = append(sess.messages, &msg)
	}

	return sess, scanner.Err()
}

// NewSQLAgent 创建 SQL 智能体
func NewSQLAgent(ctx context.Context, cfg *admin.AIConfig, connID, dbType, dbSchema, dbVersion string, sessions *SessionStore, scope *PermissionScope) (*SQLAgent, error) {
	// 1. 创建 ChatModel
	cm, err := buildChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建模型失败：%w", err)
	}

	// 2. 创建工具
	tools, err := buildTools(ctx, connID, dbType, dbSchema)
	if err != nil {
		return nil, fmt.Errorf("创建工具失败：%w", err)
	}

	// 3. 创建中间件
	permMiddleware := &PermissionMiddleware{Scope: scope}
	sqlSecurityMiddleware := &SQLSecurityMiddleware{}

	// 4. 创建 Agent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SQLAgent",
		Description: "一个专业的 SQL 助手，可以执行查询、数据导出和分析",
		Instruction: buildSystemPrompt(dbType, dbSchema, dbVersion, nil, scope),
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
		Handlers: []adk.ChatModelAgentMiddleware{
			permMiddleware,
			sqlSecurityMiddleware,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Agent 失败：%w", err)
	}

	// 如果没有提供会话存储，创建一个新的
	if sessions == nil {
		sessions, err = NewSessionStore("./data/sessions")
		if err != nil {
			return nil, fmt.Errorf("创建会话存储失败：%w", err)
		}
	}

	return &SQLAgent{
		agent:     agent,
		sessions:  sessions,
		dbType:    dbType,
		dbSchema:  dbSchema,
		dbVersion: dbVersion,
		scope:     scope,
	}, nil
}

// 最大保留的消息轮数（用于防止上下文过长）
const maxHistoryRounds = 20

// truncateHistory 截断历史消息，防止上下文过长
func truncateHistory(history []*schema.Message) []*schema.Message {
	if len(history) <= maxHistoryRounds*2 {
		return history
	}
	// 保留最近的 maxHistoryRounds 轮对话（每轮 2 条消息）
	startIdx := len(history) - maxHistoryRounds*2
	return history[startIdx:]
}

// extractLastSQLFromHistory 从历史消息中提取最近一次成功的 SQL 查询
func extractLastSQLFromHistory(history []*schema.Message) string {
	// 从后向前查找，找到最近的 assistant 消息中的 SQL
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role == schema.Assistant && msg.Content != "" {
			// 尝试从消息内容中提取 SQL 代码块
			content := msg.Content
			// 查找 ```sql ... ``` 或 ``` ... ``` 代码块
			startIdx := strings.LastIndex(content, "```")
			if startIdx == -1 {
				continue
			}
			// 查找结束标记
			endIdx := strings.Index(content[startIdx+3:], "```")
			if endIdx == -1 {
				continue
			}
			endIdx = startIdx + 3 + endIdx

			// 提取代码块内容
			codeBlock := strings.TrimSpace(content[startIdx+3 : endIdx])
			// 去除可能的语言标识（如 sql）
			if idx := strings.Index(codeBlock, "\n"); idx != -1 {
				firstLine := strings.TrimSpace(strings.Split(codeBlock, "\n")[0])
				if strings.ToLower(firstLine) == "sql" {
					codeBlock = strings.TrimSpace(codeBlock[idx+1:])
				}
			}

			// 检查是否是 SELECT 语句
			upperSQL := strings.ToUpper(strings.TrimSpace(codeBlock))
			if strings.HasPrefix(upperSQL, "SELECT") ||
				strings.HasPrefix(upperSQL, "SHOW") ||
				strings.HasPrefix(upperSQL, "DESCRIBE") ||
				strings.HasPrefix(upperSQL, "EXPLAIN") {
				return codeBlock
			}
		}
	}
	return ""
}

// RunStream 流式执行 - 完全参考官方 streamer.go 和 server.go 的实现
func (a *SQLAgent) RunStream(ctx context.Context, req ChatRequest, flush func(StreamChunk)) error {
	// 日志：Agent 开始执行
	log.Printf("[Agent] 开始执行 - sessionID=%s, userID=%s, connID=%s, question=%s\n", req.SessionID, req.UserID, req.ConnID, req.Question)

	// 使用 userId 作为会话 ID：如果传了 sessionId 则使用 sessionId，否则使用 userId
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = req.UserID
	}
	if sessionID == "" {
		log.Printf("[Agent] 错误 - sessionId 和 userId 都为空\n")
		return fmt.Errorf("sessionId 和 userId 都不能为空")
	}

	sess, err := a.sessions.GetOrCreate(sessionID)
	if err != nil {
		log.Printf("[Agent] 获取会话失败 - sessionID=%s, err=%v\n", sessionID, err)
		return err
	}
	log.Printf("[Agent] 会话已加载 - sessionID=%s, title=%s\n", sessionID, sess.Title())
	flush(StreamChunk{Type: "session", Content: sess.ID})

	// 添加当前用户消息到会话
	userMsg := schema.UserMessage(req.Question)
	if err := sess.Append(userMsg); err != nil {
		log.Printf("[Agent] 保存用户消息失败 - err=%v\n", err)
		return err
	}
	log.Printf("[Agent] 用户消息已保存 - sessionID=%s\n", sessionID)

	// 获取历史消息并截断防止上下文过长
	history := sess.GetMessages()
	truncatedHistory := truncateHistory(history)
	log.Printf("[Agent] 历史消息 - total=%d, truncated=%d\n", len(history), len(truncatedHistory))

	// 关键改进：检测用户是否在请求导出操作，如果是，自动从历史中提取 SQL
	// 这样可以避免 AI 因为上下文理解错误而编造不存在的字段
	userQuestionLower := strings.ToLower(req.Question)
	isExportRequest := strings.Contains(userQuestionLower, "导出") ||
		strings.Contains(userQuestionLower, "export") ||
		strings.Contains(userQuestionLower, "下载") ||
		strings.Contains(userQuestionLower, "excel") ||
		strings.Contains(userQuestionLower, "ppt") ||
		strings.Contains(userQuestionLower, "word") ||
		strings.Contains(userQuestionLower, "图表")

	// 如果是导出请求，在系统提示词前添加特殊的上下文信息
	var exportContextPrompt string
	if isExportRequest {
		lastSQL := extractLastSQLFromHistory(truncatedHistory)
		if lastSQL != "" {
			exportContextPrompt = fmt.Sprintf("\n\n⚠️ **用户正在请求导出操作**：系统已自动从历史对话中提取到最近的成功查询 SQL 为：\n```sql\n%s\n```\n**重要**：请直接使用此 SQL 调用导出工具，不要重新生成或修改 SQL！", lastSQL)
			log.Printf("[Agent] 检测到导出请求，已提取历史 SQL - sql=%s\n", lastSQL)
		}
	}

	// 构建消息（包含系统提示词和历史对话）
	messages := []adk.Message{
		&schema.Message{
			Role:    schema.System,
			Content: buildSystemPrompt(a.dbType, a.dbSchema, a.dbVersion, req.TableContext, a.scope) + exportContextPrompt,
		},
	}
	messages = append(messages, truncatedHistory...)
	if len(messages) > 0 {
		log.Printf("[Agent] 消息构建完成 - history_count=%d\n", len(truncatedHistory))
	}

	// 关键改进：在运行 Agent 前，先检查用户是否有任何权限
	// 如果用户没有任何权限，直接返回友好的提示
	if !a.scope.HasAnyAccess() {
		log.Printf("[Agent] 用户无任何权限 - userID=%s\n", req.UserID)
		flush(StreamChunk{
			Type:    "error",
			Content: "您好！看起来您暂时还没有可访问的数据表权限呢~\n\n建议您联系管理员为您开通相关权限，开通后就可以愉快地使用数据查询功能啦！😊",
		})
		return nil
	}
	log.Printf("[Agent] 权限检查通过 - userID=%s\n", req.UserID)

	// 运行 Agent（使用 Run 方法，它返回 AsyncIterator）
	log.Printf("[Agent] 开始调用 Agent.Run - enable_streaming=true\n")
	iter := a.agent.Run(ctx, &adk.AgentInput{
		Messages:        messages,
		EnableStreaming: true,
	})

	var fullResponse strings.Builder

	for {
		event, ok := iter.Next()
		if !ok {
			log.Printf("[Agent] 事件迭代器结束\n")
			break
		}

		// 处理错误 - 参考官方 streamer.go:99-103
		if event.Err != nil {
			// 检查是否为危险 SQL 错误
			var dangerousErr *DangerousSQLError
			if errors.As(event.Err, &dangerousErr) {
				log.Printf("[Agent] 检测到危险 SQL - sql=%s\n", dangerousErr.SQL)
				// 发送危险 SQL 确认事件
				flush(StreamChunk{
					Type:    "danger_confirm",
					Content: "检测到危险 SQL 操作，需要用户确认",
					SQL:     dangerousErr.SQL,
				})
				// 不返回错误，让 Agent 继续生成回复
				continue
			}
			// 其他错误正常返回
			log.Printf("[Agent] 事件错误 - err=%v\n", event.Err)
			flush(StreamChunk{Type: "error", Content: event.Err.Error()})
			return event.Err
		}

		// 关键：hasOutput 和 hasExit 判断 - 参考官方 streamer.go:131-141
		hasOutput := event.Output != nil && event.Output.MessageOutput != nil
		hasExit := event.Action != nil && event.Action.Exit

		if !hasOutput {
			if hasExit {
				log.Printf("[Agent] 事件退出 - action=%+v\n", event.Action)
				break
			}
			continue // 关键！没有输出但也没有退出，继续下一个事件
		}

		mo := event.Output.MessageOutput
		role := mo.Role
		if role == "" && mo.Message != nil {
			role = mo.Message.Role
		}

		switch role {
		case schema.Tool:
			// Tool 消息 - 我们可以选择是否展示给用户
			// 这里我们不展示工具结果给用户，保持简洁
			log.Printf("[Agent] 收到 Tool 消息 - 已跳过\n")
			continue

		default:
			// Assistant (or unknown role) — 可能包含文本内容和/或工具调用
			if mo.IsStreaming && mo.MessageStream != nil {
				// 流式模式 - 参考官方 streamer.go:160-251
				var accContent strings.Builder
				var contentEmitted bool

				for {
					chunk, recvErr := mo.MessageStream.Recv()
					if recvErr != nil {
						break
					}

					// 处理 reasoning content
					if chunk.ReasoningContent != "" {
						log.Printf("[Agent] 思考中 - content_length=%d\n", len(chunk.ReasoningContent))
						flush(StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
					}

					// 实时输出文本内容
					if chunk.Content != "" {
						accContent.WriteString(chunk.Content)
						flush(StreamChunk{Type: "content", Content: chunk.Content})
						contentEmitted = true
					}

					// ToolCalls 由 eino 框架自动处理，我们不需要在这里处理
				}

				// 保存完整响应
				if contentEmitted {
					fullResponse.WriteString(accContent.String())
					log.Printf("[Agent] 流式响应完成 - content_length=%d\n", accContent.Len())
				}

			} else if mo.Message != nil {
				// 非流式模式 - 参考官方 streamer.go:253-270
				msg := mo.Message

				if msg.ReasoningContent != "" {
					log.Printf("[Agent] 思考内容 - content_length=%d\n", len(msg.ReasoningContent))
					flush(StreamChunk{Type: "thinking", Content: msg.ReasoningContent})
				}

				if msg.Content != "" {
					flush(StreamChunk{Type: "content", Content: msg.Content})
					fullResponse.WriteString(msg.Content)
					log.Printf("[Agent] 非流式响应完成 - content_length=%d\n", len(msg.Content))
				}
			}
		}

		// 关键：处理完输出后检查 hasExit - 参考官方 streamer.go:276-279
		if hasExit {
			log.Printf("[Agent] 事件退出标志 - hasOutput=true\n")
			break
		}
	}

	// 保存 assistant 消息
	if fullResponse.Len() > 0 {
		assistantMsg := schema.AssistantMessage(fullResponse.String(), nil)
		if err := sess.Append(assistantMsg); err != nil {
			log.Printf("[Agent] 保存助手消息失败 - err=%v\n", err)
			return err
		}
		log.Printf("[Agent] 助手消息已保存 - sessionID=%s, content_length=%d\n", sessionID, fullResponse.Len())
	}

	log.Printf("[Agent] 执行完成 - sessionID=%s, response_length=%d\n", sessionID, fullResponse.Len())
	flush(StreamChunk{Type: "done"})
	return nil
}

// buildChatModel 创建 ChatModel
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

// buildTools 创建工具列表
func buildTools(ctx context.Context, connID, dbType, dbSchema string) ([]tool.BaseTool, error) {
	// 1. 查询工具
	queryTool, err := utils.InferTool(
		"query_data",
		"执行 SELECT 查询并返回结果数据",
		NewQueryFunc(connID),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 query_data 工具失败：%w", err)
	}

	// 2. 执行工具
	execTool, err := utils.InferTool(
		"exec_sql",
		"执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL",
		NewExecFunc(connID),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 exec_sql 工具失败：%w", err)
	}

	// 3. 表结构工具
	schemaTool, err := utils.InferTool(
		"get_table_schema",
		"获取指定表的建表语句和结构信息",
		NewSchemaFunc(connID, dbType, dbSchema),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 get_table_schema 工具失败：%w", err)
	}

	// 4. 导出工具组
	// 4.1 基础 Excel 导出
	exportExcelTool, err := utils.InferTool(
		"export_excel",
		"导出 Excel 表格数据",
		NewExportExcelFunc(connID),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 export_excel 工具失败：%w", err)
	}

	// 4.2 带图表的 Excel 导出
	exportExcelChartTool, err := utils.InferTool(
		"export_excel_with_chart",
		"导出带图表的 Excel（支持折线图、柱状图、饼图、散点图）",
		NewExportExcelWithChartFunc(connID),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 export_excel_with_chart 工具失败：%w", err)
	}

	// 4.3 PPT 导出
	exportPPTTool, err := utils.InferTool(
		"export_ppt",
		"生成 PPT 演示文稿",
		NewExportPPTFunc(connID),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 export_ppt 工具失败：%w", err)
	}

	// 4.4 分析图表导出
	exportImageTool, err := utils.InferTool(
		"export_analysis_image",
		"生成数据分析图表（PNG 格式）",
		NewExportAnalysisImageFunc(connID),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 export_analysis_image 工具失败：%w", err)
	}

	// 4.5 Word 报告导出
	exportDocxTool, err := utils.InferTool(
		"export_analysis_docx",
		"生成数据分析报告（Word 文档）",
		NewExportAnalysisDocxFunc(connID),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 export_analysis_docx 工具失败：%w", err)
	}

	return []tool.BaseTool{
		queryTool,
		execTool,
		schemaTool,
		exportExcelTool,
		exportExcelChartTool,
		exportPPTTool,
		exportImageTool,
		exportDocxTool,
	}, nil
}

// buildSystemPrompt 构建系统提示词
func buildSystemPrompt(dbType, dbSchema, dbVersion string, tableContext []string, scope *PermissionScope) string {
	dbInfo := fmt.Sprintf("当前数据库类型：%s，数据库版本：%s，当前 Schema：%s", dbType, dbVersion, dbSchema)

	// 构建表上下文信息
	var tableContextInfo string
	if len(tableContext) > 0 {
		tableContextInfo = fmt.Sprintf("\n\n📋 **用户指定的数据表**：%s\n**重要约束**：\n1. 用户已经明确指定了要查询的数据表，**只能使用这些表**，绝对不允许查询其他表！\n2. 请在回复中**明确告知用户**数据来源表名\n3. 如果用户的问题无法仅用这些表回答，请说明需要哪些额外的表", strings.Join(tableContext, ", "))
	} else {
		tableContextInfo = "\n\n📋 **用户未指定数据表**：\n1. 你可以使用 `get_table_schema` 工具查询**已授权表**的结构信息\n2. **在回复中要明确告知用户**数据来源表名\n3. 例如：'我已从表 xxx 中查询到...'"
	}

	permissionDesc := scope.DescribeForPrompt()

	return fmt.Sprintf(`你是一个专业的 SQL 助手，专门帮助用户查询和分析数据库。

%s%s%s

## 🎯 核心原则（准确性优先）

**数据准确性是最高优先级**，所有操作必须遵循以下原则：

1. **零容忍错误**：查询、导出、分析等操作必须百分之百准确
2. **验证优先**：执行任何操作前，必须先验证表结构、字段名、数据类型
3. **安全第一**：禁止执行任何可能导致数据丢失或损坏的操作
4. **精确匹配**：表名、字段名必须与数据库 schema 完全一致（区分大小写）
5. **透明沟通**：**必须明确告知用户数据来源表名**

## 📊 工作流程

### 步骤 1：理解需求
- 仔细分析用户的查询需求
- 识别需要的字段、表、过滤条件
- 如果需求不明确，**必须**向用户确认

### 步骤 2：验证表结构（必需）
- **在生成 SQL 前，必须先调用 get_table_schema 获取表结构**
- 验证表名是否存在
- 验证字段名是否正确
- 理解字段含义和数据类型

### 步骤 3：生成 SQL
- 使用**完全限定的表名和字段名**（使用反引号包裹）
- 明确指定需要的字段，**禁止使用 SELECT ***
- 添加适当的 WHERE 条件过滤数据
- 考虑性能，避免全表扫描

### 步骤 4：验证 SQL
- 检查表名是否在已知的表列表中
- 检查字段名是否在表结构中存在
- 检查 SQL 语法是否正确
- 检查是否有潜在的性能问题

### 步骤 5：执行并验证结果
- 执行查询
- 验证返回结果是否符合预期
- 如果结果为空或异常，分析原因并告知用户

### 步骤 6：回复用户（重要）
- **必须明确告知数据来源表名**
- 示例回复：
  - ✅ "我已从表 eccp_bpm_instance 中查询到 2025 年共有 150 个流程，其中 120 个已完成"
  - ❌ "2025 年共有 150 个流程"（未说明数据来源）

## ⚠️ 安全规则（必须遵守）

### 禁止的操作（需要用户确认）
- ❌ **DROP / TRUNCATE / DELETE** - **必须**由用户在页面操作确认后执行
- ❌ **UPDATE / INSERT / ALTER** - **必须**由用户在页面操作确认后执行
- ❌ **CREATE / REPLACE / MERGE** - **必须**由用户在页面操作确认后执行
- ❌ **SELECT *** - 必须明确指定需要的字段
- ❌ **无 WHERE 条件的大表查询** - 必须添加适当的过滤条件
- ❌ **未经验证的表名或字段名** - 必须先查询表结构

### AI 的职责边界
- ✅ **查询操作（SELECT）**：AI 可以直接执行
- ✅ **只读操作（SHOW/DESCRIBE/EXPLAIN）**：AI 可以直接执行
- ✅ **生成写操作 SQL**：AI **可以生成** DELETE/UPDATE/INSERT SQL **用于展示给用户**
- ❌ **执行写操作**：AI **不能调用 exec_sql 执行**写操作
- ❌ **DDL 操作**：AI **只能生成 SQL**，**不能执行**
- ⚠️ **重要**：当用户要求执行写操作时，AI 应该：
  1. 生成正确的 SQL
  2. 告知用户该操作的风险
  3. **明确说明需要用户在页面确认后执行**
  4. **不要尝试调用 exec_sql 工具执行**
  5. **使用以下格式回复**：
     - 在 SQL 代码块前添加 [CONFIRM_REQUIRED] 标记
     - 说明风险等级、操作类型、注意事项

### 🔒 自动危险 SQL 检测机制
系统已内置自动危险 SQL 检测中间件，当 AI 尝试调用 exec_sql 工具时：
- **自动拦截**：所有写操作（INSERT/UPDATE/DELETE/DROP/TRUNCATE/ALTER/CREATE）都会被自动拦截
- **前端确认**：拦截后会立即触发前端的确认对话框，展示 SQL 内容和风险等级
- **用户确认后执行**：只有用户在页面点击"确认执行"后，SQL 才会真正执行
- **AI 无需手动标记**：AI 不需要再添加 [CONFIRM_REQUIRED] 标记，系统会自动处理

### 推荐的做法
- ✅ **使用 LIMIT** - 大表查询时限制返回行数
- ✅ **添加注释** - 复杂 SQL 添加注释说明逻辑
- ✅ **分步验证** - 复杂查询先验证子查询
- ✅ **错误处理** - 捕获错误并提供友好的错误信息
- ✅ **风险评估** - 写操作前评估影响范围

## 🔧 工具使用说明

### query_data（查询数据）
- **用途**：执行 SELECT 查询
- **验证**：执行前自动验证 SQL 语法
- **限制**：只允许 SELECT/SHOW/DESCRIBE/EXPLAIN 语句
- **示例**：SELECT id, name FROM user WHERE status = 'active'
- **AI 权限**：✅ 可以直接执行

### exec_sql（执行写操作）
- **用途**：执行 INSERT/UPDATE/DELETE 等操作
- **验证**：**必须**经过用户确认
- **风险**：高风险操作，谨慎使用
- **示例**：UPDATE user SET status = 'inactive' WHERE id = 123
- **AI 权限**：❌ **AI 不应该执行此工具**
- **重要说明**：
  - 当用户要求执行写操作时，AI 应该**生成 SQL 并告知用户风险**
  - **建议用户在页面手动执行**，而不是让 AI 调用 exec_sql
  - 只有在**用户明确要求 AI 执行**且**经过二次确认**后，才能调用此工具
  - 对于**DROP/TRUNCATE**等高危操作，**绝对不能执行**，只能生成 SQL

### get_table_schema（获取表结构）
- **用途**：获取表的 DDL 和字段信息
- **时机**：**生成 SQL 前必须调用**
- **参数**：tables - 表名列表
- **示例**：["user", "order"]
- **AI 权限**：✅ 可以直接执行

### export_data（导出数据）
- **用途**：导出数据到 Excel
- **验证**：先执行查询验证数据
- **限制**：大数据量时分批导出
- **AI 权限**：✅ 可以直接执行

## 📝 SQL 编写规范

### 表名和字段名
- 使用反引号包裹：table_name, field_name
- 区分大小写：MySQL 在 Linux 上表名区分大小写
- 使用别名：复杂查询使用表别名简化

### 查询优化
- 避免全表扫描：添加 WHERE 条件
- 使用索引字段：优先使用有索引的字段过滤
- 限制返回行数：大表使用 LIMIT
- 避免子查询：使用 JOIN 替代

### 数据准确性
- 验证数据类型：字符串用引号，数字不用
- 处理 NULL 值：使用 IS NULL 或 IS NOT NULL
- 日期格式：使用正确的日期格式
- 字符编码：注意特殊字符的转义

## 💡 最佳实践

1. **先查询表结构** - 永远不要假设表结构
2. **小步验证** - 复杂查询分步验证
3. **错误分析** - 遇到错误先分析原因
4. **用户沟通** - 需求不明确时主动询问
5. **性能意识** - 考虑查询对数据库的影响

## 多轮对话与上下文理解

重要：你拥有完整的对话历史记忆，能够理解上下文！

### 导出操作的处理方式
当用户提出以下类型的请求时，必须从历史对话中获取上一次的 SQL：
- "导出为 Excel"
- "导出为图表"
- "生成 PPT"
- "生成 Word 报告"
- "把刚才的查询结果导出"
- "以上一次查询的数据导出"

正确处理流程：
1. 识别意图：用户要导出/可视化数据，而不是重新查询
2. 查找历史：从对话历史中找到最近一次成功执行的 SQL 查询
3. 复用 SQL：直接使用该 SQL，不要重新生成或执行查询
4. 调用导出工具：
   - 导出 Excel：调用 export_excel，传入历史 SQL
   - 导出带图表的 Excel：调用 export_excel_with_chart，传入历史 SQL + 图表参数
   - 导出 PPT：调用 export_ppt，传入历史 SQL
   - 导出 Word：调用 export_analysis_docx，传入历史 SQL
   - 导出分析图表：调用 export_analysis_image，传入历史 SQL
5. 明确告知用户："我将基于刚才的查询结果为您导出..."

示例对话：
用户：查询 2025 年的订单数据
AI：[执行 SQL: SELECT * FROM orders WHERE year=2025] 已查询到 1500 条记录
用户：导出为 Excel
AI：好的，我将基于刚才的查询结果（SELECT * FROM orders WHERE year=2025）为您导出 Excel 文件...
   [调用 export_excel，sql="SELECT * FROM orders WHERE year=2025"]
   导出成功！共 1500 行数据，下载地址：...

绝对禁止：
- 用户说"导出"时，重新执行 SQL 查询
- 忽略历史对话，当作新查询处理
- 要求用户重新提供 SQL

### 上下文关联词理解
以下表达都指的是上一次查询：
- "刚才的查询"
- "上面的数据"
- "这个结果"
- "这些数据"
- "查询结果"

请根据用户的需求，选择合适的工具来完成任务。始终将数据准确性放在第一位！`, dbInfo, tableContextInfo, permissionDesc)
}

// ChatRequest 聊天请求
type ChatRequest struct {
	SessionID    string   `json:"sessionId"` // 可选，如果不传则使用 userId 作为会话 ID
	UserID       string   `json:"userId"`    // 用户 ID，用于标识会话
	ConnID       string   `json:"connId"`
	Schema       string   `json:"schema"`
	Question     string   `json:"question"`
	TableContext []string `json:"tableContext"`
	Confirmed    bool     `json:"confirmed,omitempty"`
	PendingSQL   string   `json:"pendingSQL,omitempty"`
}
