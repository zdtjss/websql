package agentv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-web/config"
	"go-web/utils"
	admin "go-web/web-api/admin"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	sessions *SessionStore
}

type writeGuard struct {
	mu   sync.Mutex
	dead bool
}

func (g *writeGuard) markDead() {
	g.mu.Lock()
	g.dead = true
	g.mu.Unlock()
}

func (g *writeGuard) tryWrite(fn func()) (ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dead {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			g.dead = true
			ok = false
		}
	}()
	fn()
	return true
}

func NewHandler() (*Handler, error) {
	factory := GetAgentFactory()
	return &Handler{sessions: factory.GetSessions()}, nil
}

type sseContext struct {
	wg         *writeGuard
	kaStop     chan struct{}
	flush      func(StreamChunk)
	runnerCtx  context.Context
	runnerCancel context.CancelFunc
}

func setupSSE(c *gin.Context, parentCtx context.Context) *sseContext {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	defer c.Writer.Flush()

	wg := &writeGuard{}
	kaStop := make(chan struct{})
	runnerCtx, runnerCancel := context.WithTimeout(context.Background(), 5*time.Minute)

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-kaStop:
				return
			case <-runnerCtx.Done():
				return
			case <-ticker.C:
				wg.tryWrite(func() {
					c.Writer.WriteString("data: \n\n")
					c.Writer.Flush()
				})
			}
		}
	}()

	flush := func(chunk StreamChunk) {
		wg.tryWrite(func() {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
			c.Writer.Flush()
		})
	}

	go func() {
		select {
		case <-parentCtx.Done():
			wg.markDead()
			runnerCancel()
		case <-runnerCtx.Done():
		}
	}()

	return &sseContext{
		wg:           wg,
		kaStop:       kaStop,
		flush:        flush,
		runnerCtx:    runnerCtx,
		runnerCancel: runnerCancel,
	}
}

type requestParams struct {
	cfg       *admin.AIConfig
	user      *admin.User
	connID    string
	dbType    string
	dbSchema  string
	dbVersion string
	schemas   []SchemaRef
	scope     *PermissionScope
	auditCtx  *ExecAuditCtx
}

func resolveRequestParams(c *gin.Context, req *ChatRequest) (*requestParams, error) {
	cfg := admin.GetSelectedModelConfig(req.ModelId)
	if cfg == nil {
		return nil, fmt.Errorf("未配置 AI 服务")
	}

	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		return nil, fmt.Errorf("未认证或认证已过期")
	}

	if req.UserID == "" {
		req.UserID = user.Id
	}

	dbType, dbSchema, dbVersion := getDBInfo(req.ConnID)
	if len(req.Schemas) > 0 {
		dbType, dbSchema, dbVersion = getDBInfo(req.Schemas[0].ConnID)
	}
	permConnID := req.ConnID
	if permConnID == "" && len(req.Schemas) > 0 {
		permConnID = req.Schemas[0].ConnID
	}
	scope := BuildPermissionScope(user.Id, permConnID, dbSchema)

	connID := req.ConnID
	if connID == "" && len(req.Schemas) > 0 {
		connID = req.Schemas[0].ConnID
	}

	return &requestParams{
		cfg:       cfg,
		user:      user,
		connID:    connID,
		dbType:    dbType,
		dbSchema:  dbSchema,
		dbVersion: dbVersion,
		schemas:   req.Schemas,
		scope:     scope,
		auditCtx: &ExecAuditCtx{
			ConnID:    connID,
			UserID:    user.Id,
			UserName:  user.Name,
			SessionID: req.SessionID,
		},
	}, nil
}

func (h *Handler) prepareSession(req *ChatRequest, runnerCancel context.CancelFunc) (*Session, string) {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = utils.RandomStr()
	}
	sess, _ := h.sessions.GetOrCreate(sessionID, req.UserID)
	sess.SetCancel(runnerCancel)

	if len(req.Schemas) > 0 || len(req.TableContext) > 0 {
		ctxData := SessionContext{
			Schemas: req.Schemas,
			Tables:  req.TableContext,
		}
		if ctxJSON, err := json.Marshal(ctxData); err == nil {
			_ = sess.MergeContext(string(ctxJSON))
		}
	}

	return sess, sessionID
}

func (h *Handler) createAgent(sse *sseContext, params *requestParams, schemas []SchemaRef) (*SQLAgent, error) {
	return GetAgentFactory().GetOrCreate(sse.runnerCtx, params.cfg, params.connID, params.dbType, params.dbSchema, params.dbVersion, schemas, params.scope, params.auditCtx)
}

func (h *Handler) ChatStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler] 请求参数绑定失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式错误"})
		return
	}

	if len(req.InterruptIDs) > 0 && req.CheckPointID != "" {
		h.handleResumeExec(c, req)
		return
	}

	params, err := resolveRequestParams(c, &req)
	if err != nil {
		switch err.Error() {
		case "未配置 AI 服务":
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		case "未认证或认证已过期":
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	if params.scope.IsRemote && !params.scope.HasAnyAccess() {
		c.JSON(http.StatusForbidden, gin.H{"error": "你没有此数据库连接的访问权限"})
		return
	}

	sse := setupSSE(c, c.Request.Context())
	defer sse.runnerCancel()

	sess, sessionID := h.prepareSession(&req, sse.runnerCancel)

	agent, agentErr := h.createAgent(sse, params, req.Schemas)
	if agentErr != nil {
		log.Printf("[Handler] 创建 Agent 失败 - err=%v\n", agentErr)
		close(sse.kaStop)
		sse.flush(StreamChunk{Type: "error", Content: "创建 Agent 失败，请稍后重试"})
		sse.flush(StreamChunk{Type: "done"})
		return
	}

	sessionID, runErr := agent.RunStream(sse.runnerCtx, req, sse.flush)
	close(sse.kaStop)
	sess.ClearCancel()

	if runErr != nil {
		log.Printf("[Handler] Agent 执行失败 - err=%+v\n", runErr)
		if !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
			sse.flush(StreamChunk{Type: "error", Content: "AI 处理出错，请稍后重试"})
		}
	}

	sse.flush(StreamChunk{Type: "done"})
	_ = sessionID
}

func (h *Handler) handleResumeExec(c *gin.Context, req ChatRequest) {
	params, err := resolveRequestParams(c, &req)
	if err != nil {
		switch err.Error() {
		case "未配置 AI 服务":
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		case "未认证或认证已过期":
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	sse := setupSSE(c, c.Request.Context())
	defer sse.runnerCancel()

	sess, _ := h.prepareSession(&req, sse.runnerCancel)

	agent, agentErr := h.createAgent(sse, params, req.Schemas)
	if agentErr != nil {
		log.Printf("[Handler] 创建 Agent 失败 - err=%v\n", agentErr)
		close(sse.kaStop)
		sse.flush(StreamChunk{Type: "error", Content: "恢复执行失败，请重新操作"})
		sse.flush(StreamChunk{Type: "done"})
		return
	}

	targets := make(map[string]bool, len(req.InterruptIDs))
	for _, id := range req.InterruptIDs {
		targets[id] = req.Confirmed
	}

	if err := agent.ResumeStream(sse.runnerCtx, req.CheckPointID, targets, sse.flush, sess); err != nil {
		log.Printf("[Handler] resume failed - err=%v\n", err)
		sse.flush(StreamChunk{Type: "error", Content: "resume failed: " + err.Error()})
	}
	sess.ClearCancel()
	close(sse.kaStop)
	sse.flush(StreamChunk{Type: "done"})
}

func (h *Handler) HandleGetSessions(c *gin.Context) {
	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}
	metas, err := h.sessions.ListByUserID(user.Id)
	if err != nil {
		log.Printf("[Handler] 获取会话列表失败 - userId=%s, err=%v\n", user.Id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取会话列表失败"})
		return
	}
	if metas == nil {
		metas = []SessionMeta{}
	}
	c.JSON(http.StatusOK, gin.H{"sessions": metas})
}

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
		log.Printf("[Handler] 获取会话详情失败 - sessionId=%s, err=%v\n", sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取会话详情失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": detail})
}

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
		log.Printf("[Handler] 删除会话失败 - sessionId=%s, err=%v\n", sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除会话失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "会话已删除"})
}

func (h *Handler) HandleGetSQLAuditLogs(c *gin.Context) {
	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证或认证已过期"})
		return
	}

	filterUserID := c.Query("userId")
	startTime := c.Query("startTime")
	endTime := c.Query("endTime")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize > 200 {
		pageSize = 200
	}

	scopeUserID := ""
	if user.Id != config.AdminId {
		scopeUserID = user.Id
		filterUserID = ""
	}

	logs, total, err := ListSQLAuditLogsFiltered(scopeUserID, filterUserID, startTime, endTime, page, pageSize)
	if err != nil {
		log.Printf("[Handler] 获取审计日志失败 - userId=%s, err=%v\n", user.Id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取审计日志失败"})
		return
	}
	if logs == nil {
		logs = []SQLAuditLog{}
	}
	c.JSON(http.StatusOK, gin.H{"data": logs, "total": total})
}

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
