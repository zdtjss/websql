// tokencounter.go — 统一 token 计数，供 reduction 和 summarization 中间件共用。
//
// 提供比默认 4 chars/token 更精确的估算，针对中英文混合文本优化。
// 之前 reduction 用 chars/4 粗估、summarization 用中文感知精估，两者触发阈值不可比。
// 现统一为同一套计数逻辑，确保阈值一致。
package agent

import (
	"context"
	"encoding/json"
	"os"
	"strconv"

	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// cjkWeightTenths 中文/全角字符的 token 权重（×10）。
// 默认 20（2 token/字，偏保守）；可用 AGENT_TOKEN_CJK_WEIGHT 在 10~30 之间校准
// （例如 13 表示 1.3 token/字）。reduction 与 summarization 共用同一权重。
var cjkWeightTenths = func() int {
	if v, err := strconv.Atoi(os.Getenv("AGENT_TOKEN_CJK_WEIGHT")); err == nil && v >= 10 && v <= 30 {
		return v
	}
	return 20
}()

// estimateTextTokens 估算文本的 token 数量（中英文混合优化）。
// 中文及 CJK 标点按 2 token/字，ASCII 连续段按 4 chars/token，其他 Unicode 按 2 token/字。
func estimateTextTokens(text string) int {
	totalTenths := 0
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		ch := runes[i]
		switch {
		case ch >= 0x4E00 && ch <= 0x9FFF,
			ch >= 0x3000 && ch <= 0x303F,
			ch >= 0xFF00 && ch <= 0xFFEF:
			totalTenths += cjkWeightTenths
			i++
		case ch >= 0x2000 && ch <= 0x206F:
			totalTenths += 10
			i++
		case ch >= 0x0080:
			totalTenths += cjkWeightTenths
			i++
		case ch == ' ' || ch == '\n' || ch == '\t':
			totalTenths += 10
			i++
		default:
			segLen := 1
			for j := i + 1; j < len(runes) && runes[j] < 0x0080 && runes[j] != ' ' && runes[j] != '\n' && runes[j] != '\t'; j++ {
				segLen++
			}
			totalTenths += ((segLen + 3) / 4) * 10
			i += segLen
		}
	}
	total := (totalTenths + 9) / 10
	if total == 0 && len(text) > 0 {
		total = (len(text) + 3) / 4
	}
	return total
}

// countMessageTokens 统计消息列表和工具信息的 token 数（内部共用逻辑）。
func countMessageTokens(msgs []*schema.Message, tools []*schema.ToolInfo) int {
	total := 0
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		total += estimateTextTokens(msg.Content)
		for _, tc := range msg.ToolCalls {
			total += estimateTextTokens(tc.Function.Name)
			total += estimateTextTokens(tc.Function.Arguments)
			total += 4
		}
		if msg.ToolCallID != "" {
			total += estimateTextTokens(msg.ToolCallID) + 3
		}
		if msg.ToolName != "" {
			total += estimateTextTokens(msg.ToolName) + 3
		}
		if msg.Name != "" {
			total += estimateTextTokens(msg.Name) + 3
		}
		total += 6
	}
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		total += estimateTextTokens(tool.Name)
		total += estimateTextTokens(tool.Desc)
		// 工具参数 schema 同样占用模型上下文；旧实现只统计 Desc，
		// 会低估固定开销，导致 reduction/summarization 触发过晚。
		if tool.ParamsOneOf != nil {
			if data, err := json.Marshal(tool.ParamsOneOf); err == nil {
				total += estimateTextTokens(string(data))
			}
		}
		total += 8
	}
	// 加 15% 余量，与原 estimateTokenCount 行为一致
	total = total * 115 / 100
	return total
}

// CountMessages 适配 reduction 中间件的 TokenCounter 签名。
func CountMessages(_ context.Context, msgs []*schema.Message, tools []*schema.ToolInfo) (int64, error) {
	return int64(countMessageTokens(msgs, tools)), nil
}

// CountForSummarization 适配 summarization 中间件的 TokenCounter 签名。
func CountForSummarization(_ context.Context, input *summarization.TokenCounterInput) (int, error) {
	return countMessageTokens(input.Messages, input.Tools), nil
}

// countToolSchemaTokens 只统计工具参数 schema 的 token 量（不含名称/描述）。
func countToolSchemaTokens(tools []*schema.ToolInfo) int {
	total := 0
	for _, info := range tools {
		if info == nil || info.ParamsOneOf == nil {
			continue
		}
		if data, err := json.Marshal(info.ParamsOneOf); err == nil {
			total += estimateTextTokens(string(data))
		}
	}
	return total
}

// countToolSchemaTokensFromBaseTools 统计已注册工具的参数 schema token 量。
func countToolSchemaTokensFromBaseTools(ctx context.Context, tools []tool.BaseTool) int {
	infos := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		if info, err := t.Info(ctx); err == nil && info != nil {
			infos = append(infos, info)
		}
	}
	return countToolSchemaTokens(infos)
}
