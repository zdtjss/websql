// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"

	"github.com/gin-gonic/gin"
)

// sseLineWriter 实现 io.Writer 接口，将数据缓冲直到找到换行符，然后将每一行作为 SSE 事件发布
// 参考官方 server.go:407-437
type sseLineWriter struct {
	c   *gin.Context
	buf []byte
}

// Write 实现 io.Writer 接口
func (w *sseLineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := w.buf[:idx]
		w.buf = w.buf[idx+1:]
		if len(line) == 0 {
			continue
		}
		w.c.Writer.WriteString(fmt.Sprintf("data: %s\n\n", string(line)))
		w.c.Writer.Flush()
	}
	return len(p), nil
}

// Handler v2 版本的 HTTP Handler
type Handler struct {
	sessions *SessionStore
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
	sessions, err := NewSessionStore("./data/sessions")
	if err != nil {
		return nil, err
	}
	return &Handler{
		sessions: sessions,
	}, nil
}

// ChatStream 流式聊天接口 - 参考官方 server.go:150-219
func (h *Handler) ChatStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 如果是确认执行请求，直接执行 SQL
	if req.Confirmed && req.PendingSQL != "" {
		h.handleConfirmedExec(c, req)
		return
	}

	// 获取配置
	cfg := admin.GetAIConfigFromDB()
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "未配置 AI 服务，请先在系统配置中设置 AI 参数"})
		return
	}

	ctx := c.Request.Context()

	// 从 Authorization header 获取用户 ID
	authorization := c.GetHeader("Authorization")
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	// 设置 userId：如果请求中传了 userId 则使用请求的，否则使用当前登录用户 ID
	if req.UserID == "" {
		req.UserID = user.Id
	}

	// 获取数据库信息
	dbType, dbName := getDBInfo(req.ConnID)

	// 创建 Agent，使用全局会话存储
	agent, err := NewSQLAgent(ctx, cfg, req.ConnID, dbType, dbName, h.sessions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建 Agent 失败：" + err.Error()})
		return
	}

	// 设置 SSE 头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	defer c.Writer.Flush()

	// 发送 keep-alive 心跳，每 5 秒一次，防止 SSE 连接超时
	// 参考官方 server.go:185-200
	kaStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-kaStop:
				return
			case <-ticker.C:
				// 发送空消息作为心跳
				c.Writer.WriteString("data: \n\n")
				c.Writer.Flush()
			}
		}
	}()

	// 创建 flush 函数
	flush := func(chunk StreamChunk) {
		data, _ := json.Marshal(chunk)
		c.Writer.WriteString(fmt.Sprintf("data: %s\n\n", string(data)))
		c.Writer.Flush()
	}

	// 运行 Agent
	err = agent.RunStream(ctx, req, flush)

	// 停止 keep-alive
	close(kaStop)

	if err != nil {
		logutils.PrintErr(fmt.Errorf("Agent 执行失败：%w", err))
		flush(StreamChunk{Type: "error", Content: fmt.Sprintf("AI 服务错误：%s", err.Error())})
	}
}

// handleConfirmedExec 处理确认执行请求
func (h *Handler) handleConfirmedExec(c *gin.Context, req ChatRequest) {
	// 获取数据库连接
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", req.ConnID)
	if err != nil || len(cfgList) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接不存在"})
		return
	}
	cfg := &cfgList[0]
	cfg.Pwd = utils.AESDecode(cfg.Pwd)
	conn := config.GetConn(&config.DBParam{
		Id: cfg.Id, Name: cfg.Name, DbType: cfg.DbType,
		User: cfg.User, Pwd: cfg.Pwd, Url: cfg.Url,
	})
	if conn == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接不存在"})
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

	// 执行 SQL
	sql := strings.TrimSpace(req.PendingSQL)
	result, err := conn.Exec(sql)
	if err != nil {
		flush(StreamChunk{Type: "error", Content: fmt.Sprintf("执行失败：%s", err.Error())})
		return
	}

	affected, _ := result.RowsAffected()
	flush(StreamChunk{Type: "content", Content: fmt.Sprintf("执行成功，影响 %d 行", affected)})
	flush(StreamChunk{Type: "done"})
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

// HandleGetSessions 获取当前用户的会话列表
func (h *Handler) HandleGetSessions(c *gin.Context) {
	if h.sessions == nil {
		c.JSON(http.StatusOK, gin.H{"sessions": []SessionMeta{}})
		return
	}

	// 从 Authorization header 获取用户 ID
	authorization := c.GetHeader("Authorization")
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	// 只返回当前用户的会话
	metas, err := h.sessions.ListByUserID(user.Id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if metas == nil {
		metas = []SessionMeta{}
	}
	c.JSON(http.StatusOK, gin.H{"sessions": metas})
}

// HandleGetSession 获取指定会话的详细信息
func (h *Handler) HandleGetSession(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 sessionId 参数"})
		return
	}

	if h.sessions == nil {
		c.JSON(http.StatusOK, gin.H{"session": nil})
		return
	}

	detail, err := h.sessions.GetDetail(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": detail})
}

// HandleDeleteSession 删除指定会话
func (h *Handler) HandleDeleteSession(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 sessionId 参数"})
		return
	}

	if err := h.sessions.Delete(sessionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "会话已删除"})
}
