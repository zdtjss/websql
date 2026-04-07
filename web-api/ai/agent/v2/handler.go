// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
package agentv2

import (
	"encoding/json"
	"fmt"
	"log"
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

// getDBInfo 获取数据库信息
func getDBInfo(connID string) (string, string, string) {
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connID)
	if err != nil || len(cfgList) == 0 {
		return "", "", ""
	}

	dbSchema := ""
	if cfgList[0].DbSchema != nil {
		dbSchema = *cfgList[0].DbSchema
	}

	dbVersion := ""
	if cfgList[0].DbVersion != nil {
		dbVersion = *cfgList[0].DbVersion
	}

	return cfgList[0].DbType, dbSchema, dbVersion
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
	log.Printf("[Handler:ChatStream] 收到请求\n")
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler:ChatStream] 参数解析失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[Handler:ChatStream] 请求参数 - userID=%s, sessionID=%s, connID=%s, question=%s\n", req.UserID, req.SessionID, req.ConnID, req.Question)

	// 如果是确认执行请求，直接执行 SQL
	if req.Confirmed && req.PendingSQL != "" {
		log.Printf("[Handler:ChatStream] 确认执行请求 - pendingSQL=%s\n", req.PendingSQL)
		h.handleConfirmedExec(c, req)
		return
	}

	// 获取配置
	cfg := admin.GetAIConfigFromDB()
	if cfg == nil {
		log.Printf("[Handler:ChatStream] AI 配置未设置\n")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "未配置 AI 服务，请先在系统配置中设置 AI 参数"})
		return
	}
	log.Printf("[Handler:ChatStream] AI 配置已加载 - provider=%s, model=%s\n", cfg.Provider, cfg.Model)

	ctx := c.Request.Context()

	// 从 Authorization header 获取用户 ID
	authorization := c.GetHeader("Authorization")
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		log.Printf("[Handler:ChatStream] 用户认证失败\n")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}
	log.Printf("[Handler:ChatStream] 用户认证成功 - userID=%s, loginName=%s\n", user.Id, user.LoginName)

	// 设置 userId：如果请求中传了 userId 则使用请求的，否则使用当前登录用户 ID
	if req.UserID == "" {
		req.UserID = user.Id
		log.Printf("[Handler:ChatStream] 使用当前用户 ID 作为 userID - userID=%s\n", req.UserID)
	}

	// 获取数据库信息
	dbType, dbSchema, dbVersion := getDBInfo(req.ConnID)
	log.Printf("[Handler:ChatStream] 数据库信息 - connID=%s, dbType=%s, dbSchema=%s, dbVersion=%s\n", req.ConnID, dbType, dbSchema, dbVersion)

	// Build permission scope
	scope := BuildPermissionScope(user.Id, req.ConnID, dbSchema)
	if scope.IsRemote && !scope.HasAnyAccess() {
		log.Printf("[Handler:ChatStream] 用户无权限访问 - userID=%s, connID=%s\n", user.Id, req.ConnID)
		c.JSON(http.StatusForbidden, gin.H{"error": "你没有此数据库连接的访问权限"})
		return
	}
	log.Printf("[Handler:ChatStream] 权限检查通过 - hasAnyAccess=%v\n", scope.HasAnyAccess())

	// 创建 Agent，使用全局会话存储
	log.Printf("[Handler:ChatStream] 开始创建 Agent\n")
	agent, err := NewSQLAgent(ctx, cfg, req.ConnID, dbType, dbSchema, dbVersion, h.sessions, scope)
	if err != nil {
		log.Printf("[Handler:ChatStream] 创建 Agent 失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建 Agent 失败：" + err.Error()})
		return
	}
	log.Printf("[Handler:ChatStream] Agent 创建成功\n")

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
	log.Printf("[Handler:ChatStream] 开始运行 Agent\n")
	err = agent.RunStream(ctx, req, flush)

	// 停止 keep-alive
	close(kaStop)

	if err != nil {
		log.Printf("[Handler:ChatStream] Agent 执行失败 - err=%v\n", err)
		logutils.PrintErr(fmt.Errorf("Agent 执行失败：%w", err))
		flush(StreamChunk{Type: "error", Content: fmt.Sprintf("AI 服务错误：%s", err.Error())})
	} else {
		log.Printf("[Handler:ChatStream] Agent 执行完成\n")
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

	// 解码密码
	pwd := ""
	if cfg.Pwd != nil {
		pwd = utils.AESDecode(*cfg.Pwd)
	}

	// 处理可能为 nil 的字段
	name := ""
	if cfg.Name != nil {
		name = *cfg.Name
	}
	cfgUser := ""
	if cfg.User != nil {
		cfgUser = *cfg.User
	}
	url := ""
	if cfg.Url != nil {
		url = *cfg.Url
	}

	conn := config.GetConn(&config.DBParam{
		Id: cfg.Id, Name: name, DbType: cfg.DbType,
		User: cfgUser, Pwd: pwd, Url: url,
	})
	if conn == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接不存在"})
		return
	}

	// 权限检查
	authorization := c.GetHeader("Authorization")
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	_, dbSchema, _ := getDBInfo(req.ConnID)
	scope := BuildPermissionScope(user.Id, req.ConnID, dbSchema)
	if scope.IsRemote {
		tables := extractTablesFromSQL(req.PendingSQL)
		for _, table := range tables {
			if !scope.IsTableAllowed(table) {
				c.Header("Content-Type", "text/event-stream")
				c.Header("Cache-Control", "no-cache")
				c.Header("Connection", "keep-alive")
				flush := func(chunk StreamChunk) {
					data, _ := json.Marshal(chunk)
					c.Writer.WriteString(fmt.Sprintf("data: %s\n\n", string(data)))
					c.Writer.Flush()
				}
				flush(StreamChunk{Type: "error", Content: fmt.Sprintf("无权访问表：%s", table)})
				return
			}
		}
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
		log.Printf("[Handler:HandleGetSessions] 用户认证失败\n")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}
	log.Printf("[Handler:HandleGetSessions] 用户认证成功 - userID=%s, loginName=%s\n", user.Id, user.LoginName)

	// 权限验证：确保用户只能访问自己的会话
	log.Printf("[Handler:HandleGetSessions] 开始获取用户会话列表 - userID=%s\n", user.Id)
	metas, err := h.sessions.ListByUserID(user.Id)
	if err != nil {
		log.Printf("[Handler:HandleGetSessions] 获取会话列表失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if metas == nil {
		metas = []SessionMeta{}
	}
	log.Printf("[Handler:HandleGetSessions] 获取会话列表成功 - count=%d\n", len(metas))
	c.JSON(http.StatusOK, gin.H{"sessions": metas})
}

// HandleGetSession 获取指定会话的详细信息
func (h *Handler) HandleGetSession(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 sessionId 参数"})
		return
	}

	// 从 Authorization header 获取用户 ID
	authorization := c.GetHeader("Authorization")
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	if h.sessions == nil {
		c.JSON(http.StatusOK, gin.H{"session": nil})
		return
	}

	// 权限验证：确保用户只能访问自己的会话
	sessDB, err := GetSessionByID(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sessDB == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	if sessDB.UserID != user.Id {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问此会话"})
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

	// 从 Authorization header 获取用户 ID
	authorization := c.GetHeader("Authorization")
	user := admin.GetUser(authorization)
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	// 权限验证：确保用户只能删除自己的会话
	sessDB, err := GetSessionByID(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sessDB == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	if sessDB.UserID != user.Id {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权删除此会话"})
		return
	}

	if err := h.sessions.Delete(sessionID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "会话已删除"})
}
