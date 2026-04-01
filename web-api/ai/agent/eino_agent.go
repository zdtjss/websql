package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go-web/logutils"
	admin "go-web/web-api/admin"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// EinoAgent 基于 Eino 框架的 SQL 智能体。
type EinoAgent struct {
	sessions *SessionStore
}

func NewEinoAgent() *EinoAgent {
	return &EinoAgent{sessions: NewSessionStore(2 * time.Hour)}
}

func (a *EinoAgent) Sessions() *SessionStore { return a.sessions }

// supportsNativeFunctionCalling 判断 provider 是否支持原生 function calling。
// openai 协议（包括用 openai 协议访问 Ollama）支持，原生 ollama 协议不稳定。
func supportsNativeFunctionCalling(provider string) bool {
	return provider == "openai"
}

// buildChatModel 根据配置创建 Eino ChatModel。
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
		return nil, fmt.Errorf("不支持的 AI 提供商: %s", cfg.Provider)
	}
}

// toolBundle 工具集合。
type toolBundle struct {
	infos   []*schema.ToolInfo
	runners map[string]func(ctx context.Context, args string) (string, error)
}

// buildTools 构建 Eino Tool 列表。
func buildTools(connId, dbSchema string) (*toolBundle, string, error) {
	ctx := context.Background()
	_, dbType := getConn(connId)

	queryTool, err := utils.InferTool("query_data", "执行 SELECT 查询并返回结果数据", NewQueryFunc(connId))
	if err != nil {
		return nil, dbType, fmt.Errorf("创建 query_data 工具失败: %w", err)
	}
	execTool, err := utils.InferTool("exec_sql", "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", NewExecFunc(connId))
	if err != nil {
		return nil, dbType, fmt.Errorf("创建 exec_sql 工具失败: %w", err)
	}
	schemaTool, err := utils.InferTool("get_table_schema", "获取指定表的建表语句和结构信息", NewSchemaFunc(connId, dbSchema))
	if err != nil {
		return nil, dbType, fmt.Errorf("创建 get_table_schema 工具失败: %w", err)
	}
	exportTool, err := utils.InferTool("export_data", "根据 SQL 查询导出数据", NewExportFunc(connId))
	if err != nil {
		return nil, dbType, fmt.Errorf("创建 export_data 工具失败: %w", err)
	}

	bundle := &toolBundle{runners: make(map[string]func(ctx context.Context, args string) (string, error))}
	for _, t := range []tool.InvokableTool{queryTool, execTool, schemaTool, exportTool} {
		info, e := t.Info(ctx)
		if e != nil {
			continue
		}
		bundle.infos = append(bundle.infos, info)
		bundle.runners[info.Name] = wrapInvokable(t)
	}
	return bundle, dbType, nil
}

func wrapInvokable(t any) func(ctx context.Context, args string) (string, error) {
	if r, ok := t.(tool.InvokableTool); ok {
		return func(ctx context.Context, args string) (string, error) {
			return r.InvokableRun(ctx, args)
		}
	}
	return func(ctx context.Context, args string) (string, error) {
		return "", fmt.Errorf("工具不支持调用")
	}
}

// executeToolCall 执行单个工具调用，返回结果字符串。
func executeToolCall(ctx context.Context, bundle *toolBundle, name, args string) string {
	if bundle == nil {
		return "没有可用的工具"
	}
	runner, ok := bundle.runners[name]
	if !ok {
		return fmt.Sprintf("未知工具: %s", name)
	}
	result, err := runner(ctx, args)
	if err != nil {
		return fmt.Sprintf("工具执行错误: %s", err.Error())
	}
	return result
}

// RunStream 流式执行智能体。
// openai provider → 原生 function calling（结构化 tool_calls）
// ollama provider → 文本 tool call（prompt 描述工具，JSON 文本解析）
func (a *EinoAgent) RunStream(ctx context.Context, cfg *admin.AIConfig, req ChatRequest, flush func(StreamChunk)) error {
	sess := a.sessions.GetOrCreate(req.SessionID)
	nativeFC := supportsNativeFunctionCalling(cfg.Provider)

	cm, err := buildChatModel(ctx, cfg)
	if err != nil {
		return fmt.Errorf("创建模型失败: %w", err)
	}

	var bundle *toolBundle
	var dbType string
	if req.ConnID != "" {
		bundle, dbType, err = buildTools(req.ConnID, req.Schema)
		if err != nil {
			return fmt.Errorf("创建工具失败: %w", err)
		}
		// 原生 FC 模式：绑定工具到模型
		if nativeFC && len(bundle.infos) > 0 {
			cm, err = cm.WithTools(bundle.infos)
			if err != nil {
				return fmt.Errorf("绑定工具失败: %w", err)
			}
		}
	}

	messages := buildMessages(sess, req, dbType, bundle, nativeFC)
	a.sessions.Append(sess.ID, Message{Role: "user", Content: req.Question})
	flush(StreamChunk{Type: "session", Content: sess.ID})

	var fullResponse strings.Builder
	maxIterations := 10

	for iterIdx := range maxIterations {
		streamReader, err := cm.Stream(ctx, messages)
		if err != nil {
			flush(StreamChunk{Type: "error", Content: fmt.Sprintf("模型调用失败: %s", err.Error())})
			return nil
		}

		var assistantContent strings.Builder
		var toolCalls []schema.ToolCall
		var streamErr error

		for {
			msg, err := streamReader.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					streamErr = err
					logutils.PrintErr(fmt.Errorf("流式读取错误 (迭代%d): %w", iterIdx, err))
				}
				break
			}
			if msg.ReasoningContent != "" {
				flush(StreamChunk{Type: "thinking", Content: msg.ReasoningContent})
			}
			if msg.Content != "" {
				assistantContent.WriteString(msg.Content)
				// 实时 flush content，实现真正的流式输出
				flush(StreamChunk{Type: "content", Content: msg.Content})
			}
			if nativeFC && len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					toolCalls = mergeToolCall(toolCalls, tc)
				}
			}
		}

		content := assistantContent.String()

		// 如果流读取出错且没有内容，报告错误
		if streamErr != nil && content == "" && len(toolCalls) == 0 {
			flush(StreamChunk{Type: "error", Content: fmt.Sprintf("模型响应异常：%s", streamErr.Error())})
			break
		}

		// ===== 原生 Function Calling 路径 =====
		if nativeFC && len(toolCalls) > 0 {
			// content 已经在上面实时 flush 了，这里不需要再 flush
			fullResponse.WriteString(content)
			assistantMsg := schema.AssistantMessage(content, toolCalls)
			messages = append(messages, assistantMsg)

			for _, tc := range toolCalls {
				flush(StreamChunk{Type: "tool_call", Content: fmt.Sprintf("调用工具：%s", tc.Function.Name)})
				result := executeToolCall(ctx, bundle, tc.Function.Name, tc.Function.Arguments)

				// 解析工具执行结果，如果是导出操作，发送下载链接给前端
				if tc.Function.Name == "export_data" {
					var exportResult ExportOutput
					if err := json.Unmarshal([]byte(result), &exportResult); err == nil && exportResult.DownloadURL != "" {
						flush(StreamChunk{
							Type:    "tool_result",
							Content: fmt.Sprintf("导出数据成功：%d 条记录", exportResult.RowCount),
							ToolResult: map[string]interface{}{
								"name":        "export_data",
								"downloadUrl": exportResult.DownloadURL,
								"rowCount":    exportResult.RowCount,
								"message":     exportResult.Message,
							},
						})
					}
				}

				messages = append(messages, &schema.Message{
					Role:       schema.Tool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			continue
		}

		// ===== 文本 Tool Call 路径（ollama 等不支持原生 FC 的模型） =====
		if !nativeFC && ContainsToolCallPattern(content) {
			parsedCalls, cleanContent := ParseToolCallsFromText(content)
			if len(parsedCalls) > 0 {
				// cleanContent 已经在上面实时 flush 了，这里不需要再 flush
				fullResponse.WriteString(cleanContent)
				messages = append(messages, schema.AssistantMessage(content, nil))

				var toolResults strings.Builder
				for _, pc := range parsedCalls {
					flush(StreamChunk{Type: "tool_call", Content: fmt.Sprintf("调用工具：%s", pc.Name)})
					result := executeToolCall(ctx, bundle, pc.Name, pc.Arguments)

					// 解析工具执行结果，如果是导出操作，发送下载链接给前端
					if pc.Name == "export_data" {
						var exportResult ExportOutput
						if err := json.Unmarshal([]byte(result), &exportResult); err == nil && exportResult.DownloadURL != "" {
							flush(StreamChunk{
								Type:    "tool_result",
								Content: fmt.Sprintf("导出数据成功：%d 条记录", exportResult.RowCount),
								ToolResult: map[string]interface{}{
									"name":        "export_data",
									"downloadUrl": exportResult.DownloadURL,
									"rowCount":    exportResult.RowCount,
									"message":     exportResult.Message,
								},
							})
						}
					}

					fmt.Fprintf(&toolResults, "工具 %s 的执行结果:\n%s\n\n", pc.Name, result)
				}
				messages = append(messages, schema.UserMessage(
					fmt.Sprintf("以下是工具执行结果，请根据结果回答用户的问题：\n\n%s", toolResults.String()),
				))
				continue
			}
		}

		// ===== 纯文本回复（没有 tool call 的情况） =====
		// content 已经在上面循环中实时 flush 了，这里只需要处理危险 SQL 检测
		if content != "" {
			fullResponse.WriteString(content)
			extracted := ExtractSQLFromResponse(content)
			if extracted != "" && ClassifySQL(extracted) == DangerConfirm {
				flush(StreamChunk{Type: "danger_confirm", Content: "检测到危险操作，请确认是否执行", SQL: extracted})
			}
		} else if streamErr == nil {
			// content 为空但没有错误，可能是模型返回了空响应
			// 在 tool call 之后的轮次这种情况需要提示
			flush(StreamChunk{Type: "content", Content: "（AI 未返回有效回复，请尝试重新提问）"})
			content = "（AI 未返回有效回复）"
		}
		break
	}

	a.sessions.Append(sess.ID, Message{Role: "assistant", Content: fullResponse.String()})
	flush(StreamChunk{Type: "done"})
	return nil
}

// Run 非流式执行。
func (a *EinoAgent) Run(ctx context.Context, cfg *admin.AIConfig, req ChatRequest) (string, error) {
	sess := a.sessions.GetOrCreate(req.SessionID)
	nativeFC := supportsNativeFunctionCalling(cfg.Provider)

	cm, err := buildChatModel(ctx, cfg)
	if err != nil {
		return "", err
	}

	var bundle *toolBundle
	var dbType string
	if req.ConnID != "" {
		bundle, dbType, err = buildTools(req.ConnID, req.Schema)
		if err != nil {
			return "", err
		}
		if nativeFC && len(bundle.infos) > 0 {
			cm, err = cm.WithTools(bundle.infos)
			if err != nil {
				return "", err
			}
		}
	}

	messages := buildMessages(sess, req, dbType, bundle, nativeFC)
	a.sessions.Append(sess.ID, Message{Role: "user", Content: req.Question})

	resp, err := cm.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	content := resp.Content
	a.sessions.Append(sess.ID, Message{Role: "assistant", Content: content})
	return content, nil
}

// buildMessages 从会话历史构建 Eino 消息列表。
// nativeFC=true 时不在 prompt 中描述工具（由 WithTools 处理）。
// nativeFC=false 时在 system prompt 中描述工具供模型以文本方式调用。
func buildMessages(sess *Session, req ChatRequest, dbType string, bundle *toolBundle, nativeFC bool) []*schema.Message {
	messages := make([]*schema.Message, 0, len(sess.Messages)+2)

	dialect := "MySQL"
	switch dbType {
	case "mysql", "mariadb":
		dialect = "MySQL"
	case "sqlite":
		dialect = "SQLite"
	case "oracle":
		dialect = "Oracle"
	case "":
		dialect = "SQL"
	default:
		dialect = dbType
	}

	// 仅在非原生 FC 模式下，把工具描述写入 system prompt
	var toolDescs []*ToolDesc
	if !nativeFC && bundle != nil {
		toolDescs = []*ToolDesc{
			{Name: "get_table_schema", Desc: "获取指定表的建表语句和结构信息", ParamsDesc: `  tables (array): 要查询结构的表名列表，必填。如 ["t_user", "t_order"]`},
			{Name: "query_data", Desc: "执行 SELECT 查询并返回结果数据", ParamsDesc: `  sql (string): 要执行的 SELECT SQL 语句，必填`},
			{Name: "exec_sql", Desc: "执行 INSERT/UPDATE/DELETE/ALTER 等写操作 SQL", ParamsDesc: `  sql (string): 要执行的写操作 SQL 语句，必填`},
			{Name: "export_data", Desc: "根据 SQL 查询导出数据", ParamsDesc: `  sql (string): 用于导出数据的 SELECT SQL，必填\n  fileName (string): 导出文件名，可选`},
		}
	}

	messages = append(messages, schema.SystemMessage(SystemInstruction(req.Schema, dialect, toolDescs)))

	history := sess.Messages
	maxHistory := 40
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
	for _, msg := range history {
		switch msg.Role {
		case "user":
			messages = append(messages, schema.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(msg.Content, nil))
		}
	}

	userContent := req.Question
	if len(req.TableContext) > 0 {
		userContent = BuildContextPrompt(req.Question, req.TableContext, "")
	}
	messages = append(messages, schema.UserMessage(userContent))
	return messages
}

// mergeToolCall 合并流式 tool call 增量（原生 FC 模式使用）。
func mergeToolCall(existing []schema.ToolCall, delta schema.ToolCall) []schema.ToolCall {
	for i := range existing {
		if existing[i].Index != nil && delta.Index != nil && *existing[i].Index == *delta.Index {
			existing[i].Function.Arguments += delta.Function.Arguments
			if delta.Function.Name != "" {
				existing[i].Function.Name = delta.Function.Name
			}
			if delta.ID != "" {
				existing[i].ID = delta.ID
			}
			return existing
		}
	}
	return append(existing, delta)
}
