// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"context"
	"fmt"
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

// StreamChunk 流式输出块
type StreamChunk struct {
	Type       string                 `json:"type"`
	Content    string                 `json:"content,omitempty"`
	SQL        string                 `json:"sql,omitempty"`
	ToolResult map[string]interface{} `json:"toolResult,omitempty"`
}

// SQLAgent SQL 智能体
type SQLAgent struct {
	agent    *adk.ChatModelAgent
	sessions *SessionStore
	dbType   string
	dbName   string
}

// SessionStore 会话存储（简化版本）
type SessionStore struct {
	sessions map[string]*Session
	timeout  time.Duration
}

// Session 会话
type Session struct {
	ID        string    `json:"id"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message 消息
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NewSessionStore 创建会话存储
func NewSessionStore(timeout time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		timeout:  timeout,
	}
}

// GetOrCreate 获取或创建会话
func (s *SessionStore) GetOrCreate(sessionID string) *Session {
	if sess, ok := s.sessions[sessionID]; ok {
		return sess
	}
	sess := &Session{
		ID:        sessionID,
		Messages:  make([]Message, 0),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.sessions[sessionID] = sess
	return sess
}

// Append 添加消息到会话
func (s *SessionStore) Append(sessionID string, msg Message) {
	if sess, ok := s.sessions[sessionID]; ok {
		sess.Messages = append(sess.Messages, msg)
		sess.UpdatedAt = time.Now()
	}
}

// NewSQLAgent 创建 SQL 智能体
func NewSQLAgent(ctx context.Context, cfg *admin.AIConfig, connID, dbType, dbName string) (*SQLAgent, error) {
	// 1. 创建 ChatModel
	cm, err := buildChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建模型失败：%w", err)
	}

	// 2. 创建工具
	tools, err := buildTools(ctx, connID, dbType, dbName)
	if err != nil {
		return nil, fmt.Errorf("创建工具失败：%w", err)
	}

	// 3. 创建 Agent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SQLAgent",
		Description: "一个专业的 SQL 助手，可以执行查询、数据导出和分析",
		Instruction: buildSystemPrompt(dbType, dbName, nil),
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Agent 失败：%w", err)
	}

	// 4. 设置系统提示词（通过消息传递）
	// ChatModelAgent 的系统提示词需要通过消息传递，而不是在配置中

	return &SQLAgent{
		agent:    agent,
		sessions: NewSessionStore(2 * time.Hour),
		dbType:   dbType,
		dbName:   dbName,
	}, nil
}

// RunStream 流式执行 - 完全参考官方 streamer.go 和 server.go 的实现
func (a *SQLAgent) RunStream(ctx context.Context, req ChatRequest, flush func(StreamChunk)) error {
	sess := a.sessions.GetOrCreate(req.SessionID)
	flush(StreamChunk{Type: "session", Content: sess.ID})

	// 保存用户消息
	a.sessions.Append(sess.ID, Message{Role: "user", Content: req.Question})

	// 构建消息（包含系统提示词）
	messages := []adk.Message{
		{
			Role:    schema.System,
			Content: buildSystemPrompt(a.dbType, a.dbName, req.TableContext),
		},
		{
			Role:    schema.User,
			Content: req.Question,
		},
	}

	// 运行 Agent（使用 Run 方法，它返回 AsyncIterator）
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

		// 处理错误 - 参考官方 streamer.go:99-103
		if event.Err != nil {
			flush(StreamChunk{Type: "error", Content: event.Err.Error()})
			return event.Err
		}

		// 关键：hasOutput 和 hasExit 判断 - 参考官方 streamer.go:131-141
		hasOutput := event.Output != nil && event.Output.MessageOutput != nil
		hasExit := event.Action != nil && event.Action.Exit

		if !hasOutput {
			if hasExit {
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
				}

			} else if mo.Message != nil {
				// 非流式模式 - 参考官方 streamer.go:253-270
				msg := mo.Message

				if msg.ReasoningContent != "" {
					flush(StreamChunk{Type: "thinking", Content: msg.ReasoningContent})
				}

				if msg.Content != "" {
					flush(StreamChunk{Type: "content", Content: msg.Content})
					fullResponse.WriteString(msg.Content)
				}
			}
		}

		// 关键：处理完输出后检查 hasExit - 参考官方 streamer.go:276-279
		if hasExit {
			break
		}
	}

	// 保存 assistant 消息
	if fullResponse.Len() > 0 {
		a.sessions.Append(sess.ID, Message{Role: "assistant", Content: fullResponse.String()})
	}

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
func buildTools(ctx context.Context, connID, dbType, dbName string) ([]tool.BaseTool, error) {
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
		NewSchemaFunc(connID, dbType, dbName),
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
func buildSystemPrompt(dbType, dbName string, tableContext []string) string {
	dbInfo := fmt.Sprintf("当前数据库类型：%s，数据库名：%s", dbType, dbName)

	// 构建表上下文信息
	var tableContextInfo string
	if len(tableContext) > 0 {
		tableContextInfo = fmt.Sprintf("\n\n📋 **用户指定的数据表**：%s\n**重要约束**：\n1. 用户已经明确指定了要查询的数据表，**只能使用这些表**，绝对不允许查询其他表！\n2. 请在回复中**明确告知用户**数据来源表名\n3. 如果用户的问题无法仅用这些表回答，请说明需要哪些额外的表", strings.Join(tableContext, ", "))
	} else {
		tableContextInfo = "\n\n📋 **用户未指定数据表**：\n1. 你可以使用 `get_table_schema` 工具查询整库表信息\n2. **在回复中要明确告知用户**数据来源表名\n3. 例如：'我已从表 xxx 中查询到...'"
	}

	return fmt.Sprintf(`你是一个专业的 SQL 助手，专门帮助用户查询和分析数据库。

%s%s

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

请根据用户的需求，选择合适的工具来完成任务。始终将**数据准确性**放在第一位！`, dbInfo, tableContextInfo)
}

// ChatRequest 聊天请求（与旧代码兼容）
type ChatRequest struct {
	SessionID    string   `json:"sessionId"`
	ConnID       string   `json:"connId"`
	Schema       string   `json:"schema"`
	Question     string   `json:"question"`
	TableContext []string `json:"tableContext"`
	Confirmed    bool     `json:"confirmed,omitempty"`
	PendingSQL   string   `json:"pendingSQL,omitempty"`
}
