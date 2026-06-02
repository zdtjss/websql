package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	agent "websql/internal/ai/agent"
	system "websql/internal/app/system"

	"github.com/gin-gonic/gin"
)

var httpClient = &http.Client{Timeout: 30 * time.Minute}

func SaveAIConfig(cfg system.AIConfig) error {
	system.SaveAIConfigToDB(cfg)
	// 配置变更后立即失效 Agent 与 Permission 缓存，避免 admin 改了 AI 配置
	// 但前端还是用旧的 ChatModel / Agent / Permission Agent 跑业务（EINO_DEEP_ANALYSIS §11.1）。
	// 失效是异步的：当前正在跑的 Run 不受影响（它们持有自己的 ChatModel 引用），
	// 但下一次 GetOrCreate 会用新配置重建。
	if factory := agent.GetAgentFactory(); factory != nil {
		factory.InvalidateAll()
	}
	agent.InvalidatePermissionAgentCache()
	return nil
}

func GetAIConfig() (*system.AIConfig, error) {
	cfg := system.GetAIConfigFromDB()
	if cfg == nil {
		return nil, nil
	}
	return cfg, nil
}

func HandleSaveConfig(c *gin.Context) {
	var cfg system.AIConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		log.Printf("[AI] 参数解析失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败"})
		return
	}
	if err := SaveAIConfig(cfg); err != nil {
		log.Printf("[AI] 保存配置失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "保存配置失败"})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": "保存成功"})
}

func HandleGetConfig(c *gin.Context) {
	cfg, err := GetAIConfig()
	if err != nil {
		log.Printf("[AI] 获取配置失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "获取配置失败"})
		return
	}
	if cfg == nil {
		c.JSON(200, gin.H{"code": 200, "data": nil})
		return
	}
	c.JSON(200, gin.H{"code": 200, "data": cfg})
}

func HandleTestConfig(c *gin.Context) {
	var cfg system.AIConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		log.Printf("[AI] 参数解析失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败"})
		return
	}
	if cfg.Provider == "" || cfg.BaseURL == "" || cfg.Model == "" {
		c.JSON(200, gin.H{"code": 500, "msg": "请填写完整的 AI 配置（provider、baseUrl、model）"})
		return
	}

	_, err := CallAI(&cfg, []ChatMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		log.Printf("[AI] 连接测试失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 200, "msg": "连接成功"})
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func CallAI(cfg *system.AIConfig, messages []ChatMessage) (string, error) {
	switch cfg.Provider {
	case "ollama":
		return callOllama(cfg, messages)
	case "openai":
		return callOpenAI(cfg, messages)
	default:
		return "", fmt.Errorf("未知的 AI 提供商 %s", cfg.Provider)
	}
}

func callOllama(cfg *system.AIConfig, messages []ChatMessage) (string, error) {
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

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama 返回 %d: %s", resp.StatusCode, string(raw))
	}

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
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content, nil
	}
	if result.Message != nil && result.Message.Content != "" {
		return result.Message.Content, nil
	}
	return "", errors.New("Ollama 返回空响应")
}

func callOpenAI(cfg *system.AIConfig, messages []ChatMessage) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": cfg.Model, "messages": messages})
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
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
		return "", fmt.Errorf("OpenAI 返回 %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", errors.New("OpenAI 返回空 choices")
	}
	return result.Choices[0].Message.Content, nil
}

func streamOllama(cfg *system.AIConfig, messages []ChatMessage, flush func(StreamChunk)) error {
	body, _ := json.Marshal(map[string]any{"model": cfg.Model, "messages": messages, "stream": true})
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.ApiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Ollama 返回 %d: %s", resp.StatusCode, string(raw))
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

func streamOpenAI(cfg *system.AIConfig, messages []ChatMessage, flush func(StreamChunk)) error {
	body, _ := json.Marshal(map[string]any{"model": cfg.Model, "messages": messages, "stream": true})
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.ApiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenAI 返回 %d: %s", resp.StatusCode, string(raw))
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

type StreamChunk struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func StreamAI(cfg *system.AIConfig, messages []ChatMessage, flush func(StreamChunk)) error {
	switch cfg.Provider {
	case "ollama":
		return streamOllama(cfg, messages, flush)
	case "openai":
		return streamOpenAI(cfg, messages, flush)
	default:
		return fmt.Errorf("未知的 AI 提供商 %s", cfg.Provider)
	}
}