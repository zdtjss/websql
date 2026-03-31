package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	admin "go-web/web-api/admin"
)

// GenerateSqlRequest is the request body for /ai/generateSql.
type GenerateSqlRequest struct {
	ConnId       string   `json:"connId"`
	Schema       string   `json:"schema"`
	Question     string   `json:"question"`
	TableContext []string `json:"tableContext"`
}

// ChatRequest is the request body for /ai/chat.
type ChatRequest struct {
	ConnId     string           `json:"connId"`
	Schema     string           `json:"schema"`
	TableName  string           `json:"tableName"`
	Messages   []ChatMessage    `json:"messages"`
	DataSample []map[string]any `json:"dataSample"`
}

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChunk is the SSE payload sent to the client.
type StreamChunk struct {
	Type    string `json:"type"` // "thinking" | "content" | "done" | "error"
	Content string `json:"content"`
}

var httpClient = &http.Client{Timeout: 30 * time.Minute}

// CallAI dispatches to the appropriate AI provider based on cfg.Provider.
func CallAI(cfg *admin.AIConfig, messages []ChatMessage) (string, error) {
	switch cfg.Provider {
	case "ollama":
		return callOllama(cfg, messages)
	case "openai":
		return callOpenAI(cfg, messages)
	default:
		return "", fmt.Errorf("未知的 AI 提供商: %s", cfg.Provider)
	}
}

// StreamAI streams tokens from the AI provider via SSE, writing to flush-capable writer.
// Each SSE event is: "data: <json>\n\n"
func StreamAI(cfg *admin.AIConfig, messages []ChatMessage, flush func(StreamChunk)) error {
	switch cfg.Provider {
	case "ollama":
		return streamOllama(cfg, messages, flush)
	case "openai":
		return streamOpenAI(cfg, messages, flush)
	default:
		return fmt.Errorf("未知的 AI 提供商: %s", cfg.Provider)
	}
}

// callOllama calls the Ollama cloud /api/chat endpoint (non-streaming, OpenAI-compatible).
func callOllama(cfg *admin.AIConfig, messages []ChatMessage) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    cfg.Model,
		"messages": messages,
		"stream":   false,
	})
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.ApiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用 Ollama 失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取完整响应以便调试
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败：%w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama 返回非 200 状态码 %d: %s", resp.StatusCode, string(raw))
	}

	// 尝试解析为 OpenAI 兼容格式
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Message *struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("解析 Ollama 响应失败：%w, 原始响应：%s", err, string(raw))
	}

	// 优先使用 choices 格式（OpenAI 兼容）
	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content, nil
	}

	// 尝试使用 message 格式（Ollama 原生格式）
	if result.Message != nil && result.Message.Content != "" {
		return result.Message.Content, nil
	}

	return "", fmt.Errorf("Ollama 返回空响应，原始数据：%s", string(raw))
}

// streamOllama streams from Ollama cloud /api/chat (NDJSON format, one JSON object per line).
func streamOllama(cfg *admin.AIConfig, messages []ChatMessage, flush func(StreamChunk)) error {
	body, _ := json.Marshal(map[string]any{
		"model":    cfg.Model,
		"messages": messages,
		"stream":   true,
	})
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.ApiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("调用 Ollama 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Ollama 返回非 200 状态码 %d: %s", resp.StatusCode, string(raw))
	}

	var chunk struct {
		Message struct {
			Thinking string `json:"thinking"`
			Content  string `json:"content"`
		} `json:"message"`
		Done bool `json:"done"`
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		if chunk.Message.Thinking != "" {
			flush(StreamChunk{Type: "thinking", Content: chunk.Message.Thinking})
		}
		if chunk.Message.Content != "" {
			flush(StreamChunk{Type: "content", Content: chunk.Message.Content})
		}
		if chunk.Done {
			break
		}
	}
	return scanner.Err()
}

// callOpenAI calls the OpenAI /v1/chat/completions endpoint (non-streaming).
func callOpenAI(cfg *admin.AIConfig, messages []ChatMessage) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": cfg.Model, "messages": messages})
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.ApiKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用 OpenAI 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OpenAI 返回非 200 状态码 %d: %s", resp.StatusCode, string(raw))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析 OpenAI 响应失败: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("OpenAI 返回空 choices")
	}
	return result.Choices[0].Message.Content, nil
}

// streamOpenAI streams from OpenAI-compatible /v1/chat/completions with stream:true.
// Handles both reasoning_content (thinking) and content tokens.
func streamOpenAI(cfg *admin.AIConfig, messages []ChatMessage, flush func(StreamChunk)) error {
	body, _ := json.Marshal(map[string]any{
		"model":    cfg.Model,
		"messages": messages,
		"stream":   true,
	})
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.ApiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("调用 OpenAI 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenAI 返回非 200 状态码 %d: %s", resp.StatusCode, string(raw))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					ReasoningContent string `json:"reasoning_content"`
					Content          string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.ReasoningContent != "" {
			flush(StreamChunk{Type: "thinking", Content: delta.ReasoningContent})
		}
		if delta.Content != "" {
			flush(StreamChunk{Type: "content", Content: delta.Content})
		}
	}
	return scanner.Err()
}

func BuildGenerateSqlPrompt(req GenerateSqlRequest, tableSchema string) string {
	var sb strings.Builder
	sb.WriteString("You are a SQL expert. Given the following database schema and user question, generate a precise SQL query.\n\n")
	if tableSchema != "" {
		sb.WriteString("Table Schema:\n")
		sb.WriteString(tableSchema)
		sb.WriteString("\n\n")
	}
	if len(req.TableContext) > 0 {
		sb.WriteString("Available tables: ")
		sb.WriteString(strings.Join(req.TableContext, ", "))
		sb.WriteString("\n\n")
	}
	sb.WriteString("Database: ")
	sb.WriteString(req.Schema)
	sb.WriteString("\n\n")
	sb.WriteString("Question: ")
	sb.WriteString(req.Question)
	sb.WriteString("\n\n")
	sb.WriteString("Please respond with only the SQL query, without any explanation or markdown formatting.")
	return sb.String()
}

func BuildChatPrompt(req ChatRequest) string {
	var sb strings.Builder
	sb.WriteString("You are a data analysis assistant. Help the user analyze data and answer questions.\n\n")
	sb.WriteString("Database: ")
	sb.WriteString(req.Schema)
	sb.WriteString("\n\n")
	sb.WriteString("Table: ")
	sb.WriteString(req.TableName)
	sb.WriteString("\n\n")
	if len(req.DataSample) > 0 {
		sb.WriteString("Sample data:\n")
		for _, row := range req.DataSample {
			sb.WriteString(fmt.Sprintf("%v\n", row))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Question: ")
	for _, msg := range req.Messages {
		sb.WriteString(msg.Role + ": " + msg.Content + "\n")
	}
	return sb.String()
}
