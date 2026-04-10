// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
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
// SQLAgent
// ──────────────────────────────────────────────

type SQLAgent struct {
	agent    *adk.ChatModelAgent
	sessions *SessionStore
	dbType   string
	dbSchema string
	scope    *PermissionScope
}

const maxHistoryRounds = 20

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
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		Handlers: []adk.ChatModelAgentMiddleware{
			&PermissionMiddleware{Scope: scope},
			&ToolErrorRecoveryMiddleware{},
			&SQLSecurityMiddleware{},
		},
		ModelRetryConfig: &adk.ModelRetryConfig{MaxRetries: 5},
		MaxIterations:    25,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Agent 失败：%w", err)
	}
	if sessions == nil {
		sessions, _ = NewSessionStore()
	}
	return &SQLAgent{agent: agent, sessions: sessions, dbType: dbType, dbSchema: dbSchema, scope: scope}, nil
}

// RunStream 流式执行
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
	if err := sess.Append("user", req.Question); err != nil {
		return sessionID, err
	}

	// 获取并截断历史，转为 schema.Message
	allMsgs := sess.GetMessages()
	truncated := truncateSessionMessages(allMsgs)
	log.Printf("[Agent] 历史消息 - total=%d, truncated=%d\n", len(allMsgs), len(truncated))

	if !a.scope.HasAnyAccess() {
		flush(StreamChunk{Type: "error", Content: "您暂时没有可访问的数据表权限，请联系管理员开通。"})
		flush(StreamChunk{Type: "done"})
		return sessionID, nil
	}

	// 构建系统提示词
	sysPrompt := buildSystemPrompt(a.dbType, a.dbSchema, "", req.TableContext, a.scope)

	if isExportRequest(req.Question) {
		if lastSQL := extractLastSQLFromSessionMessages(truncated); lastSQL != "" {
			sysPrompt += fmt.Sprintf("\n\n⚠️ 用户正在请求导出操作，历史 SQL：\n```sql\n%s\n```\n请直接使用此 SQL 调用导出工具，不要重新生成。", lastSQL)
		}
	}

	if req.ExcelData != nil && req.ExcelData.FileID != "" {
		sysPrompt += fmt.Sprintf("\n\n📎 用户上传了 Excel 文件（fileId=%s）：\n- 列名：%s\n- 总行数：%d\n",
			req.ExcelData.FileID, strings.Join(req.ExcelData.Columns, ", "), req.ExcelData.TotalRows)
		sysPrompt += "请先用 get_table_schema 获取目标表结构，将 Excel 列名与表字段做严格匹配，展示映射结果给用户确认后，调用 import_data 工具（传入 fileId、tableName、mapping）执行导入。\n"
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

	// 运行 Agent
	var fullResponse strings.Builder
	var dangerousSQLs []string

	iter := a.agent.Run(ctx, &adk.AgentInput{Messages: messages, EnableStreaming: true})

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			var dangerousErr *DangerousSQLError
			if errors.As(event.Err, &dangerousErr) {
				if len(dangerousErr.SQLList) > 0 {
					dangerousSQLs = append(dangerousSQLs, dangerousErr.SQLList...)
				} else if dangerousErr.SQL != "" {
					dangerousSQLs = append(dangerousSQLs, dangerousErr.SQL)
				}
				log.Printf("[Agent] 危险 SQL 拦截 - sql=%s\n", dangerousErr.SQL)
				continue
			}
			log.Printf("[Agent] 执行失败 - err=%v\n", event.Err)
			for _, dsql := range dangerousSQLs {
				flush(StreamChunk{Type: "danger_confirm", Content: "检测到危险 SQL，需要用户确认", SQL: dsql})
			}
			if fullResponse.Len() > 0 {
				_ = sess.Append("assistant", fullResponse.String())
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

	// 推送危险 SQL
	for _, dsql := range dangerousSQLs {
		flush(StreamChunk{Type: "danger_confirm", Content: "检测到危险 SQL，需要用户确认", SQL: dsql})
	}

	// 保存助手消息
	if fullResponse.Len() > 0 {
		if err := sess.Append("assistant", fullResponse.String()); err != nil {
			log.Printf("[Agent] 保存助手消息失败 - err=%v\n", err)
		}
	}

	flush(StreamChunk{Type: "done"})
	return sessionID, nil
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

// authTransport 实现了 http.RoundTripper 接口
type authTransport struct {
	token     string
	transport http.RoundTripper
}

// RoundTrip 在请求发出前自动添加 Authorization 头
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 1. 克隆请求，避免并发修改原始请求导致的数据竞争
	clonedReq := req.Clone(req.Context())
	if clonedReq.Header == nil {
		clonedReq.Header = make(http.Header)
	}

	clonedReq.Header.Set("Authorization", "Bearer "+t.token)

	// 3. 交由底层 Transport 执行真实网络请求
	return t.transport.RoundTrip(clonedReq)
}

// NewAuthClient 创建一个默认携带 Authorization 头的 HTTP 客户端
func NewAuthClient(token string) *http.Client {
	return &http.Client{
		Transport: &authTransport{
			token:     token,
			transport: http.DefaultTransport,
		},
	}
}

// ──────────────────────────────────────────────
// 模型与工具构建
// ──────────────────────────────────────────────

func buildChatModel(ctx context.Context, cfg *admin.AIConfig) (model.ToolCallingChatModel, error) {
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

func buildTools(_ context.Context, connID, dbType, dbSchema string) ([]tool.BaseTool, error) {
	queryTool, _ := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN 查询并返回结果", NewQueryFunc(connID))
	execTool, _ := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connID))
	schemaTool, _ := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息", NewSchemaFunc(connID, dbType, dbSchema))
	exportExcelTool, _ := utils.InferTool("export_excel", "导出 Excel 表格数据", NewExportExcelFunc(connID))
	exportExcelChartTool, _ := utils.InferTool("export_excel_with_chart", "导出带图表的 Excel（折线图/柱状图/饼图/散点图）", NewExportExcelWithChartFunc(connID))
	exportPPTTool, _ := utils.InferTool("export_ppt", "生成 PPT 演示文稿", NewExportPPTFunc(connID))
	exportDocxTool, _ := utils.InferTool("export_analysis_docx", "生成数据分析报告（Word）", NewExportAnalysisDocxFunc(connID))
	importDataTool, _ := utils.InferTool("import_data", "将用户上传的 Excel 数据导入到指定数据库表中。需要提供 fileId、tableName 和 mapping。", NewImportDataFunc(connID, dbType, dbSchema))

	return []tool.BaseTool{
		queryTool, execTool, schemaTool,
		exportExcelTool, exportExcelChartTool, exportPPTTool,
		exportDocxTool, importDataTool,
	}, nil
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
当用户说"导出""下载""Excel""PPT""Word"等，从对话历史中找到最近的 SQL 直接调用导出工具。

## 数据导入操作
当用户上传了 Excel 文件并要求导入数据时：
1. 用户消息中会包含 fileId 和 Excel 列名信息
2. 先调用 get_table_schema 获取目标表结构
3. 将 Excel 列名与表字段严格匹配（必须完全相等，大小写不敏感）
4. 匹配失败的列必须立即反馈给用户
5. 匹配成功后展示完整映射表，请求用户确认
6. 确认后调用 import_data 工具（传入 fileId、tableName、mapping）
7. 字段匹配是数据安全红线

## 多轮对话
你拥有完整的对话历史记忆。"刚才的""上面的""这个结果"等都指上一次查询。

## 错误处理
当工具调用失败时，系统会自动将错误信息反馈给你。请根据错误信息重新分析，不要重复使用相同的错误参数。
`)
	return sb.String()
}
