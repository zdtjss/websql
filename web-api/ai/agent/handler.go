package agent

import (
	"encoding/json"
	"fmt"
	"net/http"

	admin "go-web/web-api/admin"

	"github.com/gin-gonic/gin"
)

// Handler HTTP 处理器，持有智能体实例。
type Handler struct {
	agent *EinoAgent
}

// NewHandler 创建 Handler。
func NewHandler() *Handler {
	return &Handler{
		agent: NewEinoAgent(),
	}
}

// getAIConfig 获取原始 AI 配置。
func getAIConfig() (*admin.AIConfig, error) {
	cfg := admin.GetAIConfigFromDB()
	if cfg == nil {
		return nil, fmt.Errorf("请先配置 AI 服务")
	}
	return cfg, nil
}

// HandleChatStream 流式对话（SSE）。
func (h *Handler) HandleChatStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败：" + err.Error()})
		return
	}

	cfg, err := getAIConfig()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	// 如果是确认执行危险 SQL
	if req.Confirmed && req.PendingSQL != "" {
		h.handleConfirmedExec(c, cfg, req)
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(200, gin.H{"code": 500, "msg": "不支持流式响应"})
		return
	}

	err = h.agent.RunStream(c.Request.Context(), cfg, req, func(chunk StreamChunk) {
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	})

	if err != nil {
		data, _ := json.Marshal(StreamChunk{Type: "error", Content: err.Error()})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// HandleChat 非流式对话。
func (h *Handler) HandleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "参数解析失败：" + err.Error()})
		return
	}

	cfg, err := getAIConfig()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	resp, err := h.agent.Run(c.Request.Context(), cfg, req)
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "AI 响应失败：" + err.Error()})
		return
	}

	c.JSON(200, gin.H{"code": 200, "data": resp, "sessionId": req.SessionID})
}

// handleConfirmedExec 处理用户确认后的危险 SQL 执行。
func (h *Handler) handleConfirmedExec(c *gin.Context, cfg *admin.AIConfig, req ChatRequest) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(200, gin.H{"code": 500, "msg": "不支持流式响应"})
		return
	}

	// 直接执行已确认的 SQL
	execFn := NewExecFunc(req.ConnID)
	result, err := execFn(c.Request.Context(), &ExecInput{SQL: req.PendingSQL})

	var chunk StreamChunk
	if err != nil {
		chunk = StreamChunk{Type: "error", Content: "执行失败: " + err.Error()}
	} else {
		chunk = StreamChunk{Type: "content", Content: result.Message}
		// 记录到会话
		h.agent.Sessions().Append(req.SessionID, Message{
			Role:    "assistant",
			Content: fmt.Sprintf("已执行: %s\n结果: %s", req.PendingSQL, result.Message),
		})
	}

	data, _ := json.Marshal(chunk)
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	flusher.Flush()

	doneData, _ := json.Marshal(StreamChunk{Type: "done"})
	fmt.Fprintf(c.Writer, "data: %s\n\n", doneData)
	flusher.Flush()
}

// HandleGetSessions 获取会话列表（可选）。
func (h *Handler) HandleGetSessions(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		c.JSON(200, gin.H{"code": 200, "data": nil})
		return
	}
	msgs := h.agent.Sessions().GetMessages(sessionID)
	c.JSON(200, gin.H{"code": 200, "data": msgs})
}

// HandleDeleteSession 删除会话。
func (h *Handler) HandleDeleteSession(c *gin.Context) {
	sessionID := c.Query("sessionId")
	h.agent.Sessions().Delete(sessionID)
	c.JSON(200, gin.H{"code": 200, "msg": "已删除"})
}
