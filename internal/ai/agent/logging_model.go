// logging_model.go — 包装 ChatModel，在调用 LLM 时输出更丰富的日志。
//
// 现状：BuildChatModel 返回的 model.ToolCallingChatModel 仅被 toolCallIndexFixerModel
// 包装用于修复 ToolCall Index 冲突，模型调用本身没有任何日志，调试时只能依赖
// events.go 中的事件流日志，难以快速定位"模型到底收到了什么、返回了什么、耗时多久"。
//
// 本文件新增 loggingToolCallingChatModel，在 Generate / Stream / WithTools 三个入口
// 注入日志，覆盖以下场景：
//  1. 每次模型调用的输入消息概要（角色、长度、工具调用数）
//  2. 每次模型调用的输出概要（内容长度、工具调用数、reasoning 长度）
//  3. 每次模型调用的耗时
//  4. Stream 模式下 chunk 数量与首 token 耗时
//  5. WithTools 时记录工具列表，便于排查"模型看到了哪些工具"
package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// loggingToolCallingChatModel 包装 model.ToolCallingChatModel，输出调用日志。
type loggingToolCallingChatModel struct {
	model.ToolCallingChatModel
}

func newLoggingModel(inner model.ToolCallingChatModel) model.ToolCallingChatModel {
	if inner == nil {
		return nil
	}
	return &loggingToolCallingChatModel{ToolCallingChatModel: inner}
}

func (m *loggingToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := m.ToolCallingChatModel.WithTools(tools)
	if err != nil {
		log.Printf("[Model:WithTools] 失败 - toolCount=%d, err=%v\n", len(tools), err)
		return nil, err
	}
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		if t != nil {
			toolNames = append(toolNames, t.Name)
		}
	}
	log.Printf("[Model:WithTools] 绑定工具 - count=%d, tools=%v\n", len(tools), toolNames)
	return &loggingToolCallingChatModel{ToolCallingChatModel: inner}, nil
}

func (m *loggingToolCallingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	start := time.Now()
	inputSummary := summarizeInputMessages(input)
	log.Printf("[Model:Generate] 开始 - input=%s\n", inputSummary)

	resp, err := m.ToolCallingChatModel.Generate(ctx, input, opts...)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("[Model:Generate] 失败 - elapsed=%v, err=%v\n", elapsed, err)
		return nil, err
	}
	outputSummary := summarizeOutputMessage(resp)
	log.Printf("[Model:Generate] 完成 - elapsed=%v, output=%s\n", elapsed, outputSummary)
	return resp, nil
}

func (m *loggingToolCallingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	start := time.Now()
	inputSummary := summarizeInputMessages(input)
	log.Printf("[Model:Stream] 开始 - input=%s\n", inputSummary)

	sr, err := m.ToolCallingChatModel.Stream(ctx, input, opts...)
	if err != nil {
		log.Printf("[Model:Stream] 失败 - elapsed=%v, err=%v\n", time.Since(start), err)
		return nil, err
	}

	// 使用 StreamReaderWithConvert 包装流，在 convert 函数中统计 chunk 数量、
	// 首 token 耗时、累计内容长度、工具调用数，并周期性输出进度日志。
	// 流结束的汇总日志由 events.go 的 MessageStream.Recv 结束日志覆盖，
	// 这里不重复输出，避免 goroutine 泄漏。
	chunkIdx := 0
	totalContentLen := 0
	totalReasoningLen := 0
	totalToolCalls := 0
	firstChunkLogged := false
	seenToolCallIDs := make(map[string]bool)

	wrapped := schema.StreamReaderWithConvert(sr, func(msg *schema.Message) (*schema.Message, error) {
		chunkIdx++
		if !firstChunkLogged {
			firstChunkLogged = true
			log.Printf("[Model:Stream] 首 chunk - firstTokenLatency=%v\n", time.Since(start))
		}
		totalContentLen += len(msg.Content)
		totalReasoningLen += len(msg.ReasoningContent)
		tcCount := len(msg.ToolCalls)
		if tcCount > 0 {
			totalToolCalls += tcCount
			// 记录新出现的工具调用 ID（仅前 10 个）
			if len(seenToolCallIDs) < 10 {
				for _, tc := range msg.ToolCalls {
					if tc.ID != "" && !seenToolCallIDs[tc.ID] {
						seenToolCallIDs[tc.ID] = true
						argsPreview := tc.Function.Arguments
						if len(argsPreview) > 120 {
							argsPreview = argsPreview[:120] + "..."
						}
						log.Printf("[Model:Stream] 工具调用 - callID=%s, func=%s, args=%s\n",
							tc.ID, tc.Function.Name, argsPreview)
					}
				}
			}
		}
		// 周期性输出进度（前 5 个 chunk + 每 50 个 chunk）
		if chunkIdx <= 5 || chunkIdx%50 == 0 {
			log.Printf("[Model:Stream] chunk[%d] - contentLen=%d, reasoningLen=%d, toolCalls=%d\n",
				chunkIdx, len(msg.Content), len(msg.ReasoningContent), tcCount)
		}
		return msg, nil
	})

	return wrapped, nil
}

// summarizeInputMessages 生成输入消息的概要字符串，用于日志输出。
func summarizeInputMessages(msgs []*schema.Message) string {
	if len(msgs) == 0 {
		return "empty"
	}
	var parts []string
	for i, msg := range msgs {
		if i >= 10 {
			parts = append(parts, fmt.Sprintf("...(%d more)", len(msgs)-i))
			break
		}
		role := string(msg.Role)
		contentLen := len(msg.Content)
		tcCount := len(msg.ToolCalls)
		if msg.ToolCallID != "" {
			parts = append(parts, fmt.Sprintf("%s#%d[tool:%s,len=%d]", role, i, msg.ToolName, contentLen))
		} else if tcCount > 0 {
			parts = append(parts, fmt.Sprintf("%s#%d[len=%d,tc=%d]", role, i, contentLen, tcCount))
		} else {
			parts = append(parts, fmt.Sprintf("%s#%d[len=%d]", role, i, contentLen))
		}
	}
	return fmt.Sprintf("count=%d, %s", len(msgs), strings.Join(parts, ", "))
}

// summarizeOutputMessage 生成输出消息的概要字符串，用于日志输出。
func summarizeOutputMessage(msg *schema.Message) string {
	if msg == nil {
		return "nil"
	}
	contentLen := len(msg.Content)
	reasoningLen := len(msg.ReasoningContent)
	tcCount := len(msg.ToolCalls)
	tcNames := make([]string, 0, tcCount)
	for _, tc := range msg.ToolCalls {
		tcNames = append(tcNames, tc.Function.Name)
	}
	preview := msg.Content
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	preview = strings.ReplaceAll(preview, "\n", " ")
	return fmt.Sprintf("contentLen=%d, reasoningLen=%d, toolCalls=%d(%v), preview=%q",
		contentLen, reasoningLen, tcCount, tcNames, preview)
}
