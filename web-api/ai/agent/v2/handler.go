package agentv2

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-web/utils"
	"log"
	"net/http"
	"strings"
	"time"

	"go-web/config"
	admin "go-web/web-api/admin"

	"github.com/gin-gonic/gin"
)

// Handler v2 版本的 HTTP Handler
type Handler struct {
	sessions *SessionStore
}

// NewHandler 创建 Handler
func NewHandler() (*Handler, error) {
	sessions, err := NewSessionStore()
	if err != nil {
		return nil, err
	}
	return &Handler{sessions: sessions}, nil
}

// ChatStream 流式聊天接口
func (h *Handler) ChatStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler] 请求参数绑定失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式错误"})
		return
	}

	// 确认执行请求走单独逻辑
	if req.Confirmed && req.PendingSQL != "" {
		h.handleConfirmedExec(c, req)
		return
	}

	cfg := admin.GetAIConfigFromDB()
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "未配置 AI 服务，请先在系统配置中设置 AI 参数"})
		return
	}

	ctx := c.Request.Context()

	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	if req.UserID == "" {
		req.UserID = user.Id
	}

	dbType, dbSchema, dbVersion := getDBInfo(req.ConnID)
	scope := BuildPermissionScope(user.Id, req.ConnID, dbSchema)
	if scope.IsRemote && !scope.HasAnyAccess() {
		c.JSON(http.StatusForbidden, gin.H{"error": "你没有此数据库连接的访问权限"})
		return
	}

	// SSE 设置
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	defer c.Writer.Flush()

	// 心跳
	kaStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-kaStop:
				return
			case <-ticker.C:
				c.Writer.WriteString("data: \n\n")
				c.Writer.Flush()
			}
		}
	}()

	flush := func(chunk StreamChunk) {
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		c.Writer.Flush()
	}

	agent, err := NewSQLAgent(ctx, cfg, req.ConnID, dbType, dbSchema, dbVersion, h.sessions, scope)
	if err != nil {
		log.Printf("[Handler] 创建 Agent 失败 - err=%v\n", err)
		close(kaStop)
		flush(StreamChunk{Type: "error", Content: "创建 Agent 失败，请稍后重试"})
		flush(StreamChunk{Type: "done"})
		return
	}

	// 执行 Agent
	// - ChatModel 的临时错误（503 等）由 ModelRetryConfig 在 agent 内部自动重试
	// - 工具调用错误（SQL 语法错误、字段不存在等）由 Eino ReAct 循环内部自动处理：
	//   工具返回错误 → 错误作为 tool result 反馈给模型 → 模型重新思考并调整工具调用
	// - MaxIterations=25 防止无限循环
	_, runErr := agent.RunStream(ctx, req, flush)
	close(kaStop)

	if runErr != nil {
		var dangerousErr *DangerousSQLError
		if !errors.As(runErr, &dangerousErr) {
			log.Printf("[Handler] Agent 执行失败 - err=%+v\n", runErr)
			flush(StreamChunk{Type: "error", Content: "AI 处理出错，请稍后重试"})
		}
		flush(StreamChunk{Type: "done"})
	}
}

// handleConfirmedExec 处理用户确认后的危险 SQL 执行
func (h *Handler) handleConfirmedExec(c *gin.Context, req ChatRequest) {
	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	// SQL 签名验证：防止前端篡改确认执行的 SQL
	if req.PendingSign == "" || !VerifySQLSign(req.PendingSQL, req.PendingSign) {
		log.Printf("[Handler] SQL 签名验证失败 - userId=%s, sql=%s\n", user.Id, req.PendingSQL)
		c.JSON(http.StatusForbidden, gin.H{"error": "SQL 签名验证失败，请重新操作"})
		return
	}

	// 权限检查
	_, dbSchema, _ := getDBInfo(req.ConnID)
	scope := BuildPermissionScope(user.Id, req.ConnID, dbSchema)
	if scope.IsRemote {
		tables := extractTablesFromSQL(req.PendingSQL)
		for _, table := range tables {
			if !scope.IsTableAllowed(table) {
				c.Header("Content-Type", "text/event-stream")
				c.Header("Cache-Control", "no-cache")
				c.Header("Connection", "keep-alive")
				data, _ := json.Marshal(StreamChunk{Type: "error", Content: fmt.Sprintf("无权访问表：%s", table)})
				fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
				c.Writer.Flush()
				return
			}
		}
	}

	// 获取数据库连接
	conn, _ := getConn(req.ConnID)
	if conn == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接不存在"})
		return
	}

	// SSE 设置
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flush := func(chunk StreamChunk) {
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		c.Writer.Flush()
	}

	sql := strings.TrimSpace(req.PendingSQL)

	// 记录审计日志
	auditID := utils.RandomStr()
	sqlType := detectSQLType(sql)
	riskLevel := detectRiskLevel(sql)
	userName := user.Name

	// 执行 SQL
	result, err := conn.Exec(sql)
	var affected int64
	var errMsg string

	if err != nil {
		errMsg = err.Error()
		log.Printf("[Handler] SQL 执行失败 - err=%v\n", err)
		InsertSQLAudit(auditID, user.Id, userName, req.ConnID, req.SessionID, sql, sqlType, riskLevel, "failed", 0, errMsg)
		flush(StreamChunk{Type: "error", Content: "SQL 执行失败，请检查语句是否正确"})
		return
	}

	affected, _ = result.RowsAffected()

	// 记录成功的审计日志
	InsertSQLAudit(auditID, user.Id, userName, req.ConnID, req.SessionID, sql, sqlType, riskLevel, "success", int(affected), "")

	flush(StreamChunk{Type: "content", Content: fmt.Sprintf("✅ 执行成功，影响 %d 行\n\n%s", affected, sql)})
	flush(StreamChunk{Type: "done"})
}

// HandleGetSessions 获取当前用户的会话列表
func (h *Handler) HandleGetSessions(c *gin.Context) {
	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	metas, err := h.sessions.ListByUserID(user.Id)
	if err != nil {
		log.Printf("[Handler] 获取会话列表失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取会话列表失败"})
		return
	}
	if metas == nil {
		metas = []SessionMeta{}
	}
	c.JSON(http.StatusOK, gin.H{"sessions": metas})
}

// HandleGetSession 获取指定会话详情
func (h *Handler) HandleGetSession(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 sessionId 参数"})
		return
	}

	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	detail, err := h.sessions.GetDetail(sessionID)
	if err != nil {
		log.Printf("[Handler] 获取会话详情失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取会话详情失败"})
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

	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	if err := h.sessions.Delete(sessionID); err != nil {
		log.Printf("[Handler] 删除会话失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除会话失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "会话已删除"})
}

// HandleGetSQLAuditLogs 获取 SQL 审计日志
func (h *Handler) HandleGetSQLAuditLogs(c *gin.Context) {
	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	// 管理员可以查看所有日志，普通用户只能查看自己的
	var logs []SQLAuditLog
	var err error
	if user.Id == config.AdminId {
		logs, err = ListSQLAuditLogs("", 100)
	} else {
		logs, err = ListSQLAuditLogs(user.Id, 100)
	}
	if err != nil {
		log.Printf("[Handler] 获取审计日志失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取审计日志失败"})
		return
	}
	if logs == nil {
		logs = []SQLAuditLog{}
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// ──────────────────────────────────────────────
// 辅助函数
// ──────────────────────────────────────────────

func getDBInfo(connID string) (string, string, string) {
	if connID == "" {
		return "", "", ""
	}
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connID)
	if err != nil || len(cfgList) == 0 {
		return "", "", ""
	}
	cfg := cfgList[0]

	deref := func(p *string) string {
		if p != nil {
			return *p
		}
		return ""
	}
	return cfg.DbType, deref(cfg.DbSchema), deref(cfg.DbVersion)
}

func detectSQLType(sql string) string {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	for _, prefix := range []string{"DROP", "TRUNCATE", "DELETE", "ALTER", "CREATE", "INSERT", "UPDATE", "REPLACE", "MERGE"} {
		if strings.HasPrefix(upper, prefix) {
			return prefix
		}
	}
	return "UNKNOWN"
}

func detectRiskLevel(sql string) string {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(upper, "DROP") || strings.HasPrefix(upper, "TRUNCATE") {
		return "high"
	}
	if strings.HasPrefix(upper, "DELETE") || strings.HasPrefix(upper, "ALTER") {
		if !strings.Contains(upper, "WHERE") {
			return "high"
		}
		return "medium"
	}
	return "medium"
}
