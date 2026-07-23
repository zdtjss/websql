// tokencounter.go — 统一 token 计数，供 reduction 和 summarization 中间件共用。
//
// 提供比默认 4 chars/token 更精确的估算，针对中英文混合文本优化。
// 之前 reduction 用 chars/4 粗估、summarization 用中文感知精估，两者触发阈值不可比。
// 现统一为同一套计数逻辑，确保阈值一致。
package agent

import (
	"context"

	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"
)

// estimateTextTokens 估算文本的 token 数量（中英文混合优化）。
// 中文及 CJK 标点按 2 token/字，ASCII 连续段按 4 chars/token，其他 Unicode 按 2 token/字。
func estimateTextTokens(text string) int {
	total := 0
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		ch := runes[i]
		if ch >= 0x4E00 && ch <= 0x9FFF {
			total += 2
			i++
		} else if ch >= 0x3000 && ch <= 0x303F {
			total += 2
			i++
		} else if ch >= 0xFF00 && ch <= 0xFFEF {
			total += 2
			i++
		} else if ch >= 0x2000 && ch <= 0x206F {
			total += 1
			i++
		} else if ch >= 0x0080 {
			total += 2
			i++
		} else if ch == ' ' || ch == '\n' || ch == '\t' {
			total += 1
			i++
		} else {
			segLen := 1
			for j := i + 1; j < len(runes) && runes[j] < 0x0080 && runes[j] != ' ' && runes[j] != '\n' && runes[j] != '\t'; j++ {
				segLen++
			}
			total += (segLen + 3) / 4
			i += segLen
		}
	}
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
		if tool != nil {
			total += estimateTextTokens(tool.Desc)
		}
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
