package agent

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

	admin "websql/internal/app/admin"
	conn "websql/internal/app/conn"
	system "websql/internal/app/system"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"

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
	wg           *writeGuard
	kaStop       chan struct{}
	flush        func(StreamChunk)
	runnerCtx    context.Context
	runnerCancel context.CancelFunc
}

func setupSSE(c *gin.Context, parentCtx context.Context) *sseContext {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	defer c.Writer.Flush()

	wg := &writeGuard{}
	kaStop := make(chan struct{})
	// 使用请求级 context 作为父级，确保服务关闭时请求被优雅取消
	// 超时设为 30 分钟，与 ChatModel 的 HTTP Timeout 对齐。
	// 复杂问题（多轮工具调用 + LLM 思考）5 分钟不够用，会导致 context deadline exceeded。
	// 客户端主动断开时 parentCtx.Done() 会触发 runnerCancel，无需依赖此超时兜底。
	runnerCtx, runnerCancel := context.WithTimeout(parentCtx, 30*time.Minute)

	safego.GoWithName("sse-keepalive", func() {
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
	})

	flush := func(chunk StreamChunk) {
		wg.tryWrite(func() {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
			c.Writer.Flush()
		})
	}

	sse := &sseContext{
		wg:           wg,
		kaStop:       kaStop,
		flush:        flush,
		runnerCtx:    runnerCtx,
		runnerCancel: runnerCancel,
	}

	// 监听客户端断开 → 触发 runnerCancel
	safego.GoWithName("sse-ctx-watch", func() {
		select {
		case <-parentCtx.Done():
			wg.markDead()
			sse.runnerCancel()
		case <-runnerCtx.Done():
		}
	})

	return sse
}

type requestParams struct {
	cfg       *system.AIConfig
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
	cfg := system.GetSelectedModelConfig(req.ModelId)
	if cfg == nil {
		return nil, errors.New("未配置 AI 服务")
	}

	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		return nil, errors.New("未认证或认证已过期")
	}

	if req.UserID == "" {
		req.UserID = user.Id
	}

	connID := req.ConnID
	if connID == "" && len(req.Schemas) > 0 {
		connID = req.Schemas[0].ConnID
	}
	dbType, dbSchema, dbVersion := GetDBInfo(connID)
	if len(req.Schemas) > 0 && req.Schemas[0].Schema != "" {
		dbSchema = req.Schemas[0].Schema
	} else if req.Schema != "" {
		dbSchema = req.Schema
	}
	schemaNames := collectSchemaNames(connID, dbSchema, req)
	scope := BuildPermissionScope(user.Id, connID, schemaNames)

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
		sessionID = idgen.RandomStr()
	}
	req.SessionID = sessionID // 回写，避免 RunStream 重复生成新 ID
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
		response.WriteErr(c, http.StatusBadRequest, 400, "请求参数格式错误")
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
			response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
		case "未认证或认证已过期":
			response.WriteAuthErr(c, err.Error())
		default:
			response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
		}
		return
	}

	if params.scope.IsRemote && !params.scope.HasAnyAccess() {
		response.WriteErr(c, http.StatusForbidden, 403, "你没有此数据库连接的访问权限")
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

	// 注册 Agent Cancel：当客户端断开连接时，触发 Agent 安全点取消
	// 用 runID 精确指定要取消的 run，避免多 SSE / 多 tab 并发时错位
	runID := fmt.Sprintf("%s_%d", req.SessionID, time.Now().UnixNano())
	safego.GoWithName("agent-cancel-watch", func() {
		<-c.Request.Context().Done()
		agent.Cancel(runID)
	})

	sessionID, runErr := agent.RunStream(sse.runnerCtx, runID, req, sse.flush)
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
			response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
		case "未认证或认证已过期":
			response.WriteAuthErr(c, err.Error())
		default:
			response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
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
		response.WriteAuthErr(c, "未认证或认证已过期")
		return
	}

	keyword := strings.TrimSpace(c.Query("keyword"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "0"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	total, err := countSessionsByUserID(user.Id, keyword)
	if err != nil {
		log.Printf("[Handler] 统计会话数量失败 - userId=%s, err=%v\n", user.Id, err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "获取会话列表失败")
		return
	}

	sessions, err := listSessionsByUserIDPaged(user.Id, keyword, pageSize, offset)
	if err != nil {
		log.Printf("[Handler] 获取会话列表失败 - userId=%s, err=%v\n", user.Id, err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "获取会话列表失败")
		return
	}

	metas := make([]SessionMeta, 0, len(sessions))
	for _, sess := range sessions {
		title := sess.Title
		if title == "" {
			title = "未命名会话"
		}
		metas = append(metas, SessionMeta{ID: sess.ID, Title: title, CreatedAt: sess.CreatedAt})
	}
	response.WriteOK(c, gin.H{
		"sessions": metas,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (h *Handler) HandleGetSession(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "缺少 sessionId 参数")
		return
	}
	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		response.WriteAuthErr(c, "未认证或认证已过期")
		return
	}
	detail, err := h.sessions.GetDetail(sessionID)
	if err != nil {
		log.Printf("[Handler] 获取会话详情失败 - sessionId=%s, err=%v\n", sessionID, err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "获取会话详情失败")
		return
	}
	response.WriteOK(c, gin.H{"session": detail})
}

// HandleCancelAgent 主动取消正在运行的 Agent（Eino v0.9 Agent Cancel）
func (h *Handler) HandleCancelAgent(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "缺少 sessionId 参数")
		return
	}
	// 通过 session 的 cancel 机制触发取消
	h.sessions.Cancel(sessionID)
	response.WriteOK(c, gin.H{"message": "已发送取消请求"})
}

func (h *Handler) HandleDeleteSession(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "缺少 sessionId 参数")
		return
	}
	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		response.WriteAuthErr(c, "未认证或认证已过期")
		return
	}
	if err := h.sessions.Delete(sessionID); err != nil {
		log.Printf("[Handler] 删除会话失败 - sessionId=%s, err=%v\n", sessionID, err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "删除会话失败")
		return
	}
	response.WriteOK(c, gin.H{"message": "会话已删除"})
}

func GetDBInfo(connID string) (string, string, string) {
	if connID == "" {
		return "", "", ""
	}
	cfgList := []conn.ConnCfg{}
	err := getDB().Select(&cfgList, "select * from t_conn where id = ?", connID)
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

func collectSchemaNames(connID, dbSchema string, req *ChatRequest) []string {
	seen := make(map[string]bool)
	var names []string
	if dbSchema != "" {
		seen[dbSchema] = true
		names = append(names, dbSchema)
	}
	for _, s := range req.Schemas {
		if s.Schema != "" && !seen[s.Schema] {
			seen[s.Schema] = true
			names = append(names, s.Schema)
		}
	}
	if req.Schema != "" && !seen[req.Schema] {
		seen[req.Schema] = true
		names = append(names, req.Schema)
	}
	if len(names) == 0 {
		names = append(names, dbSchema)
	}
	return names
}
