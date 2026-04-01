package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ParsedToolCall 从文本中解析出的工具调用。
type ParsedToolCall struct {
	Name      string
	Arguments string // JSON 格式
}

// --- JSON 格式解析 ---
// {"tool_call": {"name": "xxx", "arguments": {...}}}

type jsonToolCallWrapper struct {
	ToolCall *jsonToolCall `json:"tool_call"`
}

type jsonToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// --- XML 格式解析（兼容旧模型） ---

var (
	reInvoke    = regexp.MustCompile(`(?s)<invoke\s+name="([^"]+)">(.*?)</invoke>`)
	reParam     = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)"[^>]*>(.*?)</parameter>`)
	reFuncBlock = regexp.MustCompile(`(?s)<function_calls?>(.*?)</function_calls?>`)
)

// ParseToolCallsFromText 尝试从模型输出的文本中解析 tool call。
// 支持 JSON 格式和 XML 格式。
// 返回解析出的 tool calls 和去除 tool call 后的纯文本内容。
func ParseToolCallsFromText(content string) ([]ParsedToolCall, string) {
	// 先尝试 JSON 格式
	if calls, clean := parseJSONToolCalls(content); len(calls) > 0 {
		return calls, clean
	}
	// 再尝试 XML 格式
	if calls, clean := parseXMLToolCalls(content); len(calls) > 0 {
		return calls, clean
	}
	return nil, content
}

// parseJSONToolCalls 解析 JSON 格式的 tool call。
func parseJSONToolCalls(content string) ([]ParsedToolCall, string) {
	trimmed := strings.TrimSpace(content)

	// 尝试直接解析整个内容
	var wrapper jsonToolCallWrapper
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil && wrapper.ToolCall != nil && wrapper.ToolCall.Name != "" {
		return []ParsedToolCall{{
			Name:      wrapper.ToolCall.Name,
			Arguments: string(wrapper.ToolCall.Arguments),
		}}, ""
	}

	// 尝试从文本中提取 JSON 块（模型可能在 JSON 前后加了文字）
	start := strings.Index(content, `{"tool_call"`)
	if start == -1 {
		return nil, content
	}

	// 找到匹配的闭合大括号
	depth := 0
	end := -1
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
		if end != -1 {
			break
		}
	}

	if end == -1 {
		return nil, content
	}

	jsonStr := content[start:end]
	if err := json.Unmarshal([]byte(jsonStr), &wrapper); err == nil && wrapper.ToolCall != nil && wrapper.ToolCall.Name != "" {
		before := strings.TrimSpace(content[:start])
		after := strings.TrimSpace(content[end:])
		clean := strings.TrimSpace(before + " " + after)
		return []ParsedToolCall{{
			Name:      wrapper.ToolCall.Name,
			Arguments: string(wrapper.ToolCall.Arguments),
		}}, clean
	}

	return nil, content
}

// parseXMLToolCalls 解析 XML 格式的 tool call。
func parseXMLToolCalls(content string) ([]ParsedToolCall, string) {
	var calls []ParsedToolCall

	// 查找 <function_calls>...</function_calls> 块
	funcBlocks := reFuncBlock.FindAllStringSubmatchIndex(content, -1)
	if len(funcBlocks) == 0 {
		if !reInvoke.MatchString(content) {
			return nil, content
		}
		calls = parseInvokes(content)
		if len(calls) > 0 {
			cleaned := reInvoke.ReplaceAllString(content, "")
			return calls, strings.TrimSpace(cleaned)
		}
		return nil, content
	}

	cleaned := content
	for _, block := range funcBlocks {
		blockText := content[block[2]:block[3]]
		calls = append(calls, parseInvokes(blockText)...)
	}

	if len(calls) > 0 {
		cleaned = reFuncBlock.ReplaceAllString(content, "")
		cleaned = strings.TrimSpace(cleaned)
	}

	return calls, cleaned
}

// parseInvokes 从文本中解析所有 <invoke> 标签。
func parseInvokes(text string) []ParsedToolCall {
	var calls []ParsedToolCall

	invokeMatches := reInvoke.FindAllStringSubmatch(text, -1)
	for _, m := range invokeMatches {
		if len(m) < 3 {
			continue
		}
		name := m[1]
		body := m[2]

		args := make(map[string]interface{})
		paramMatches := reParam.FindAllStringSubmatch(body, -1)
		for _, pm := range paramMatches {
			if len(pm) < 3 {
				continue
			}
			paramName := pm[1]
			paramValue := strings.TrimSpace(pm[2])

			var jsonVal interface{}
			if err := json.Unmarshal([]byte(paramValue), &jsonVal); err == nil {
				args[paramName] = jsonVal
			} else {
				args[paramName] = paramValue
			}
		}

		argsJSON, err := json.Marshal(args)
		if err != nil {
			argsJSON = []byte(fmt.Sprintf(`{"error":"参数序列化失败: %s"}`, err.Error()))
		}

		calls = append(calls, ParsedToolCall{
			Name:      name,
			Arguments: string(argsJSON),
		})
	}

	return calls
}

// ContainsToolCallPattern 快速检测文本是否可能包含文本格式的 tool call。
func ContainsToolCallPattern(content string) bool {
	return strings.Contains(content, `"tool_call"`) ||
		strings.Contains(content, "<invoke") ||
		strings.Contains(content, "<function_call")
}
