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
	// 设置为中文
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
			&SQLSecurityMiddleware{},
			&ToolErrorRecoveryMiddleware{},
			summarizationMW,
		},
		// ModelRetryConfig: &adk.ModelRetryConfig{MaxRetries: 5},
		MaxIterations: 20,
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
		// 从后往前查找最后一个 SQL 代码块
		content := msg.Content
		for {
			endIdx := strings.LastIndex(content, "```")
			if endIdx <= 0 {
				break
			}
			// 找到这个 ``` 对应的开始 ```
			startIdx := strings.LastIndex(content[:endIdx], "```")
			if startIdx == -1 {
				break
			}
			codeBlock := strings.TrimSpace(content[startIdx+3 : endIdx])
			// 去掉语言标识行
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
			// 继续往前找
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
	log.Printf("[ChatModel] 初始化配置 - provider=%s, baseUrl=%s, model=%s, temperature=%.2f, maxTokens=%d, enableThinking=%t\n",
		cfg.Provider, cfg.BaseURL, cfg.Model, cfg.Temperature, cfg.MaxTokens, cfg.EnableThinking)

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
	queryTool, err := utils.InferTool("query_data", "执行 SELECT/SHOW/DESCRIBE/EXPLAIN 查询并返回结果", NewQueryFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建工具 query_data 失败：%w", err)
	}
	execTool, err := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建工具 exec_sql 失败：%w", err)
	}
	schemaTool, err := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息", NewSchemaFunc(connID, dbType, dbSchema))
	if err != nil {
		return nil, fmt.Errorf("创建工具 get_table_schema 失败：%w", err)
	}
	exportExcelTool, err := utils.InferTool("export_excel", "导出 Excel 表格数据", NewExportExcelFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建工具 export_excel 失败：%w", err)
	}
	exportExcelChartTool, err := utils.InferTool("export_excel_with_chart", "导出带图表的 Excel（折线图/柱状图/饼图/散点图）", NewExportExcelWithChartFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建工具 export_excel_with_chart 失败：%w", err)
	}
	exportPPTTool, err := utils.InferTool("export_ppt", "生成 PPT 演示文稿。支持两种模式：1) content 模式（推荐）— 传入 Markdown 分析内容自动分页生成；2) sql 模式 — 基于 SQL 查询数据生成数据演示", NewExportPPTFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建工具 export_ppt 失败：%w", err)
	}
	exportDocxTool, err := utils.InferTool("export_analysis_docx", "生成数据分析报告（Word）。支持两种模式：1) content 模式（推荐）— 传入 Markdown 分析内容生成报告；2) sql 模式 — 基于 SQL 查询数据生成数据报告", NewExportAnalysisDocxFunc(connID))
	if err != nil {
		return nil, fmt.Errorf("创建工具 export_analysis_docx 失败：%w", err)
	}
	importDataTool, err := utils.InferTool("import_data", "将用户上传的 Excel 数据导入到指定数据库表中。需要提供 fileId、tableName 和 mapping。", NewImportDataFunc(connID, dbType, dbSchema))
	if err != nil {
		return nil, fmt.Errorf("创建工具 import_data 失败：%w", err)
	}

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

## 输出格式规范（重要）
你善于使用 Markdown 和 Mermaid 来呈现分析结果，让输出结构清晰、可视化强：

### Markdown 使用规范
- 使用标题层级组织内容结构
- 使用粗体强调关键指标和结论
- 使用行内代码标注字段名、表名、SQL 关键词
- 使用列表罗列要点
- 使用表格展示对比数据
- 使用代码块展示 SQL、配置等

### Mermaid 图表使用规范
在分析数据关系、流程、趋势时，优先使用 Mermaid 图表增强可读性：
- 流程/架构：使用 graph TD / graph LR 展示数据流向和系统架构
- 时序关系：使用 sequenceDiagram 展示交互流程
- 数据对比：使用 pie 饼图展示占比分布
- 状态变迁：使用 stateDiagram-v2 展示状态流转
- ER 关系：使用 erDiagram 展示表间关联

示例：
` + "```" + `mermaid
pie title 销售占比
    "华东" : 35
    "华南" : 28
    "华北" : 22
    "其他" : 15
` + "```" + `

## 写操作处理（红线）
- 所有写操作必须调用 exec_sql 工具，系统会自动拦截并推送到前端由用户确认
- AI 不得绕过此机制，这是必须遵守的安全红线

## 导出操作
导出工具分两种模式，根据场景选择：

### Excel 导出（必须依赖 SQL）
- export_excel / export_excel_with_chart：必须提供 sql 参数，用于导出原始数据

### Word/PPT 导出（支持内容模式）
- export_analysis_docx / export_ppt：支持两种方式：
  1. **内容模式（推荐）**：提供 content 参数（Markdown 格式），将你的分析结论直接生成文档。适用于分析报告、洞察总结等场景
  2. **SQL 模式**：提供 sql 参数，基于查询数据生成文档。适用于需要展示原始数据的场景
- 优先使用内容模式，将你已完成的分析结果（含 Markdown 格式、Mermaid 图表）传入 content 参数
- 内容模式无需数据库连接，即使 SQL 不可用也能生成报告

### 导出决策指南
- 用户说"导出 Excel" → 必须使用 SQL 模式
- 用户说"导出报告/Word/PPT"或"导出上述内容"，或者简短的说“导出” → 优先使用 content 模式，传入你的分析内容
- 用户说"把数据导出为 Word" → 使用 SQL 模式展示原始数据

## 数据导入操作
当用户上传了 Excel 文件并要求导入数据时：
1. 用户消息中会包含 fileId 和 Excel 列名信息
2. 先调用 get_table_schema 获取目标表结构，确认目标表存在
3. 直接调用 import_data 工具，只需传入 fileId 和 tableName 即可
4. 后端会自动按列名匹配 Excel 列与数据库字段（大小写不敏感、忽略空格和下划线差异）
5. 如果你非常确定映射关系，也可以传入 mapping 参数，但这不是必须的
6. 导入结果会包含实际使用的字段映射，请展示给用户确认

## 多轮对话
你拥有完整的对话历史记忆。"刚才的""上面的""这个结果"等都指上一次查询。

## 错误处理
当工具调用失败时，系统会自动将错误信息反馈给你。请根据错误信息重新分析，不要重复使用相同的错误参数。
`)
	return sb.String()
}
