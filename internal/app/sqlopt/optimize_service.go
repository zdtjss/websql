package sqlopt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	agentv2 "websql/internal/ai/agent"
	admin "websql/internal/app/admin"
	"websql/internal/app/system"
	"websql/internal/logger"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// OptimizeRequest 是 SQL 优化的入参，gin handler 和 Wails binding 共用。
type OptimizeRequest struct {
	ConnID        string
	Schema        string
	SQL           string
	ExplainResult *ExplainResult // 可选,前端传入
	Authorization string
}

// StreamChunk 与 agentv2.StreamChunk 一致，作为 emit 回调的数据类型。
type StreamChunk = agentv2.StreamChunk

// OptimizeService 封装 SQL 优化业务逻辑，与 gin.Context 解耦。
// 通过 emit 回调输出流式数据：
//   - HTTP 模式: emit = func(chunk) { writeSSE(c.Writer, chunk) }
//   - Wails 模式: emit = func(chunk) { runtime.EventsEmit(ctx, "sse:<sid>:data", chunk) }
type OptimizeService struct{}

var (
	defaultOptimizeService *OptimizeService
	defaultOptimizeOnce    sync.Once
)

// ensureDefaultOptimize 返回单例 OptimizeService。
func ensureDefaultOptimize() *OptimizeService {
	defaultOptimizeOnce.Do(func() {
		defaultOptimizeService = &OptimizeService{}
	})
	return defaultOptimizeService
}

// Optimize 执行 SQL 优化流式输出。
// 业务逻辑来自原 optimize.go 的 OptimizeSQLStream handler（line 176-361）。
//
// 错误约定:
//   - 校验类错误（SQL 为空、AI 未配置）: 返回 error,由调用方决定是否再 emit error
//   - 流式错误: 直接通过 emit 推送 {Type:"error"},service 内部已处理
//   - 完成时: 调用方负责 emit {Type:"done"}
func (s *OptimizeService) Optimize(ctx context.Context, req *OptimizeRequest, emit func(StreamChunk)) error {
	sqlStr := strings.TrimSpace(req.SQL)
	connId := req.ConnID
	dbSchema := req.Schema

	dbType, cfgSchema, dbVersion := agentv2.GetDBInfo(connId)
	if dbSchema == "" {
		dbSchema = cfgSchema
	}

	log.Printf("[OptAgent] 开始优化 - connID=%s, dbType=%s, schema=%s, sqlLen=%d\n", connId, dbType, dbSchema, len(sqlStr))

	if sqlStr == "" {
		return errors.New("SQL不能为空")
	}

	aiCfg := system.GetSelectedModelConfig("")
	if aiCfg == nil || aiCfg.ApiKey == "" || aiCfg.BaseURL == "" {
		return errors.New("AI 模型未配置，请先在系统设置中配置 AI 模型")
	}

	cm, err := agentv2.BuildChatModel(ctx, aiCfg)
	if err != nil {
		logger.PrintErrf("创建优化Agent模型失败", err)
		emit(StreamChunk{Type: "error", Content: "AI 模型初始化失败: " + err.Error()})
		return nil
	}
	log.Printf("[OptAgent] 模型初始化成功 - provider=%s, model=%s\n", aiCfg.Provider, aiCfg.Model)

	schemas := []agentv2.SchemaRef{{ConnID: connId, Schema: dbSchema}}
	authorization := req.Authorization
	var optUserId string
	if authorization != "" {
		if user := admin.GetUser(authorization); user != nil {
			optUserId = user.Id
		}
	}
	optTools, err := buildOptTools(connId, dbType, dbSchema, schemas, optUserId)
	if err != nil {
		logger.PrintErrf("创建优化Agent工具失败", err)
		emit(StreamChunk{Type: "error", Content: "工具初始化失败: " + err.Error()})
		return nil
	}
	log.Printf("[OptAgent] 工具初始化成功 - toolCount=%d\n", len(optTools))

	sysPrompt := buildOptSystemPrompt(dbType, dbVersion, req.ExplainResult)

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SQLOptimizer",
		Description: "SQL 优化专家，分析 SQL 性能问题并给出优化建议",
		Instruction: sysPrompt,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: optTools},
		},
		MaxIterations: 10,
	})
	if err != nil {
		logger.PrintErrf("创建优化Agent失败", err)
		emit(StreamChunk{Type: "error", Content: "Agent 创建失败: " + err.Error()})
		return nil
	}
	log.Printf("[OptAgent] Agent 创建成功 - maxIterations=10\n")

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	userPrompt := fmt.Sprintf("请分析并优化以下 SQL：\n\n```sql\n%s\n```", sqlStr)
	messages := []adk.Message{
		&schema.Message{Role: schema.System, Content: sysPrompt},
		&schema.Message{Role: schema.User, Content: userPrompt},
	}

	log.Printf("[OptAgent] 开始执行 - sqlLen=%d\n", len(sqlStr))
	iter := runner.Run(ctx, messages)

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			log.Printf("[OptAgent] 事件错误 - err=%+v\n", event.Err)
			logger.PrintErrf("优化Agent事件错误", event.Err)
			emit(StreamChunk{Type: "error", Content: "AI 处理出错: " + event.Err.Error()})
			break
		}
		if event.Action != nil && event.Action.Exit {
			log.Printf("[OptAgent] Agent 执行完毕\n")
			break
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			log.Printf("[OptAgent] Agent 被中断\n")
			emit(StreamChunk{Type: "error", Content: "AI 处理被中断，请重试"})
			break
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mo := event.Output.MessageOutput
		if mo.IsStreaming && mo.MessageStream != nil {
			for {
				chunk, recvErr := mo.MessageStream.Recv()
				if recvErr != nil {
					break
				}
				if chunk.ReasoningContent != "" {
					emit(StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
				}
				if chunk.Content != "" {
					emit(StreamChunk{Type: "content", Content: chunk.Content})
				}
			}
		}
	}

	log.Printf("[OptAgent] 优化流程结束 - connID=%s\n", connId)
	return nil
}

// OptimizeByService 是包级便捷函数，供 Wails binding 直接调用。
// 与 conn/snippet 包级委托函数模式一致。
func OptimizeByService(ctx context.Context, req *OptimizeRequest, emit func(StreamChunk)) error {
	return ensureDefaultOptimize().Optimize(ctx, req, emit)
}

// decodeExplainResult 从 JSON 字符串反序列化 ExplainResult。
// 与原 OptimizeSQLStream 中 c.PostForm("explainResult") 后的 json.Unmarshal 等价。
func decodeExplainResult(jsonStr string) *ExplainResult {
	if jsonStr == "" {
		return nil
	}
	var er ExplainResult
	if err := json.Unmarshal([]byte(jsonStr), &er); err != nil {
		return nil
	}
	return &er
}
