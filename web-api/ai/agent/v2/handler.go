// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go-web/config"
	"go-web/logutils"
	admin "go-web/web-api/admin"

	"github.com/gin-gonic/gin"
)

// Handler v2 版本的 HTTP Handler
type Handler struct {
	agent *SQLAgent
}

// getDBInfo 获取数据库类型和名称
func getDBInfo(connID string) (string, string) {
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connID)
	if err != nil || len(cfgList) == 0 {
		return "", ""
	}
	return cfgList[0].DbType, cfgList[0].Name
}

// NewHandler 创建 Handler
func NewHandler() (*Handler, error) {
	// 创建一个临时的 Agent 用于访问会话管理
	// 实际的 Agent 会在每次请求时动态创建
	agent := &SQLAgent{
		sessions: NewSessionStore(2 * time.Hour),
	}
	return &Handler{
		agent: agent,
	}, nil
}

// ChatStream 流式聊天接口
func (h *Handler) ChatStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取配置
	cfg := admin.GetAIConfigFromDB()
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "未配置 AI 服务，请先在系统配置中设置 AI 参数"})
		return
	}

	ctx := c.Request.Context()

	// 获取数据库信息
	dbType, dbName := getDBInfo(req.ConnID)

	// 创建 Agent
	agent, err := NewSQLAgent(ctx, cfg, req.ConnID, dbType, dbName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建 Agent 失败：" + err.Error()})
		return
	}

	// 设置 SSE 头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flush := func(chunk StreamChunk) {
		data, _ := json.Marshal(chunk)
		c.Writer.WriteString(fmt.Sprintf("data: %s\n\n", string(data)))
		c.Writer.Flush()
	}

	// 运行 Agent
	err = agent.RunStream(ctx, req, flush)
	if err != nil {
		logutils.PrintErr(fmt.Errorf("Agent 执行失败：%w", err))
		flush(StreamChunk{Type: "error", Content: fmt.Sprintf("AI 服务错误：%s", err.Error())})
	}
}

// Chat 非流式聊天接口（备用）
func (h *Handler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: 实现非流式版本
	c.JSON(http.StatusOK, gin.H{"message": "not implemented"})
}

// HandleGetSessions 获取所有会话列表
func (h *Handler) HandleGetSessions(c *gin.Context) {
	if h.agent == nil || h.agent.sessions == nil {
		c.JSON(http.StatusOK, gin.H{"sessions": []interface{}{}})
		return
	}

	// 获取会话列表（简化版本，返回空数组）
	// TODO: 实现完整的会话列表功能
	c.JSON(http.StatusOK, gin.H{"sessions": []interface{}{}})
}

// HandleDeleteSession 删除指定会话
func (h *Handler) HandleDeleteSession(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 sessionId 参数"})
		return
	}

	// TODO: 实现删除会话功能
	c.JSON(http.StatusOK, gin.H{"message": "会话删除功能待实现"})
}
