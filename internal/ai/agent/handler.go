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

const (
	// sseRunTimeout 单次对话（含多轮工具调用）的服务端上限
	sseRunTimeout = 30 * time.Minute
	// sseKeepAliveInterval 心跳间隔：防止反向代理/浏览器在长时间工具调用期间断开空闲连接
	sseKeepAliveInterval = 5 * time.Second
	// sseWriteTimeout 单次写入超时：客户端卡住（不读数据）时及时放弃，避免事件在内存中无限堆积
	sseWriteTimeout = 30 * time.Second
)

var (
	errAIConfigMissing = errors.New("未配置 AI 服务")
	errUnauthenticated = errors.New("未认证或认证已过期")
)

type Handler struct {
	sessions *SessionStore
}

// writeGuard 串行化 SSE 写入，并在连接失效后拒绝继续写入。
type writeGuard struct {
	mu   sync.Mutex
	dead bool
}

func (g *writeGuard) markDead() {
	g.mu.Lock()
	g.dead = true
	g.mu.Unlock()
}

func (g *writeGuard) tryWrite(fn func() error) (ok bool) {
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
	if err := fn(); err != nil {
		g.dead = true
		return false
	}
	return true
}

func NewHandler() (*Handler, error) {
	factory := GetAgentFactory()
	// 启动阶段输出文件解析能力概览（含 Python 依赖探测预热），便于排查
	// 「某格式不可用」类问题；幂等调用，测试环境下开销可忽略。
	InitUploadStoreLog()
	return &Handler{sessions: factory.GetSessions()}, nil
}

// sseStream 一次 SSE 响应的生命周期管理。
//
// 生命周期约束（gin 要求）：handler 返回后 gin.Context 会被回收复用，
// 因此所有后台 goroutine 必须在 handler 返回前退出，不能再触碰 c.Writer / c.Request。
// close() 负责：标记连接失效 → 停止心跳 → 等待后台 goroutine 退出。
type sseStream struct {
	wg        *writeGuard
	writer    gin.ResponseWriter
	ctl       *http.ResponseController
	stopCh    chan struct{}
	bg        sync.WaitGroup
	closeOnce sync.Once

	runnerCtx    context.Context
	runnerCancel context.CancelFunc
}

func newSSEStream(c *gin.Context) *sseStream {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // 关闭 Nginx 缓冲，保证流式实时性
	c.Status(http.StatusOK)

	reqCtx := c.Request.Context()
	runnerCtx, runnerCancel := context.WithTimeout(reqCtx, sseRunTimeout)
	s := &sseStream{
		wg:           &writeGuard{},
		writer:       c.Writer,
		ctl:          http.NewResponseController(c.Writer),
		stopCh:       make(chan struct{}),
		runnerCtx:    runnerCtx,
		runnerCancel: runnerCancel,
	}
	s.write(": connected\n\n")

	// 心跳：SSE 注释行（冒号开头），客户端解析器会忽略，不会产生空 data 事件
	s.bg.Add(1)
	safego.GoWithName("sse-keepalive", func() {
		defer s.bg.Done()
		ticker := time.NewTicker(sseKeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-runnerCtx.Done():
				return
			case <-ticker.C:
				s.write(": ping\n\n")
			}
		}
	})

	// 客户端断开 → 停止写入并取消运行
	s.bg.Add(1)
	safego.GoWithName("sse-ctx-watch", func() {
		defer s.bg.Done()
		select {
		case <-reqCtx.Done():
			s.wg.markDead()
			runnerCancel()
		case <-s.stopCh:
		}
	})
	return s
}

func (s *sseStream) write(payload string) bool {
	return s.wg.tryWrite(func() error {
		_ = s.ctl.SetWriteDeadline(time.Now().Add(sseWriteTimeout)) // 不支持时忽略
		if _, err := s.writer.WriteString(payload); err != nil {
			return err
		}
		s.writer.Flush()
		return nil
	})
}

// flush 推送一个事件（JSON）
func (s *sseStream) flush(chunk StreamChunk) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	s.write("data: " + string(data) + "\n\n")
}

// close 结束 SSE：推送 done（可选）、停止后台 goroutine 并等待退出。可重复调用。
func (s *sseStream) close(sendDone bool) {
	s.closeOnce.Do(func() {
		if sendDone {
			s.flush(StreamChunk{Type: "done"})
		}
		close(s.stopCh)
		s.bg.Wait()
		s.wg.markDead()
		s.runnerCancel()
	})
}

// fail 推送错误并结束流
func (s *sseStream) fail(msg string) {
	s.flush(StreamChunk{Type: "error", Content: msg})
	s.close(true)
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
		return nil, errAIConfigMissing
	}

	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		return nil, errUnauthenticated
	}

	// 安全：会话归属一律以认证态用户为准，忽略客户端传入的 userId，
	// 防止伪造 userId 读写/续写他人会话（IDOR）。
	req.UserID = user.Id

	// 上传文件归属校验：fileId 由客户端传入，可能伪造/枚举他人文件。
	// 非本人文件直接丢弃引用，避免他人文件的列名/行数等元信息经系统提示词泄露，
	// read_file_data 工具内还有一层内容级归属校验（GetUploadedFile）。
	if req.ExcelData != nil && req.ExcelData.FileID != "" && !IsUploadOwner(req.ExcelData.FileID, user.Id) {
		log.Printf("[Handler] 丢弃非本人上传的文件引用 - user=%s, fileId=%s\n", user.Id, req.ExcelData.FileID)
		req.ExcelData = nil
	}
	// 新上传链路（多文件）：校验归属并剔除已过期引用，展示字段一律以服务端元数据回填
	if len(req.Attachments) > 0 {
		req.Attachments = SanitizeChatAttachments(req.Attachments, user.Id)
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
			ConnID:   connID,
			UserID:   user.Id,
			UserName: user.Name,
			// SessionID 在每次工具调用时经 context（WithRunIdentity）取当前会话，
			// 这里的值仅作兜底
			SessionID: req.SessionID,
		},
	}, nil
}

// writeParamsError 在 SSE 建立前以普通 JSON 响应返回参数/鉴权错误
func writeParamsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errUnauthenticated):
		response.WriteAuthErr(c, err.Error())
	case errors.Is(err, errAIConfigMissing):
		response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
	default:
		response.WriteErr(c, http.StatusInternalServerError, 500, "请求处理失败")
	}
}

func (h *Handler) prepareSession(req *ChatRequest, runnerCancel context.CancelFunc) (*Session, error) {
	if req.SessionID == "" {
		req.SessionID = idgen.RandomStr()
	}
	sess, err := h.sessions.GetOrCreate(req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	sess.SetCancel(runnerCancel)

	if len(req.Schemas) > 0 || len(req.TableContext) > 0 {
		ctxData := SessionContext{Schemas: req.Schemas, Tables: req.TableContext}
		if ctxJSON, err := json.Marshal(ctxData); err == nil {
			if err := sess.MergeContext(string(ctxJSON)); err != nil {
				log.Printf("[Handler] 合并会话上下文失败 - sessionId=%s, err=%v\n", req.SessionID, err)
			}
		}
	}
	return sess, nil
}

func (h *Handler) createAgent(ctx context.Context, params *requestParams) (*SQLAgent, error) {
	return GetAgentFactory().GetOrCreate(ctx, params.cfg, params.connID, params.dbType, params.dbSchema,
		params.dbVersion, params.schemas, params.scope, params.auditCtx)
}

// hasFileDataSource 判断本次请求是否携带文件数据来源（多文件附件或 legacy 单文件）。
// 大模型分析的数据来源可以是数据库，也可以是文件，二者至少有其一即可。
func hasFileDataSource(req *ChatRequest) bool {
	return len(req.Attachments) > 0 || (req.ExcelData != nil && req.ExcelData.FileID != "")
}

// sessionHasFileDataSource 判断会话历史中是否已有文件附件。
// 用于纯文件会话的后续追问：本轮不再重复携带附件，但文件仍是该会话的数据来源。
func (h *Handler) sessionHasFileDataSource(sessionID, userID string) bool {
	if sessionID == "" {
		return false
	}
	detail, err := h.sessions.GetDetail(sessionID, userID)
	if err != nil {
		return false
	}
	for _, msg := range detail.Messages {
		if len(msg.Attachments) > 0 {
			return true
		}
	}
	return false
}

func (h *Handler) ChatStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler] 请求参数绑定失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 400, "请求参数格式错误")
		return
	}

	isResume := len(req.InterruptIDs) > 0 && req.CheckPointID != ""
	if isResume && req.SessionID == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "恢复执行缺少会话 ID")
		return
	}

	params, err := resolveRequestParams(c, &req)
	if err != nil {
		writeParamsError(c, err)
		return
	}
	// 未选择数据库时，允许以用户上传的文件作为唯一数据来源
	if params.scope.IsRemote && !params.scope.HasAnyAccess() &&
		!hasFileDataSource(&req) && !h.sessionHasFileDataSource(req.SessionID, req.UserID) {
		response.WriteErr(c, http.StatusForbidden, 403, "你没有此数据库连接的访问权限，请选择有权限的数据库或上传文件")
		return
	}

	sse := newSSEStream(c)
	defer sse.close(false)

	sess, err := h.prepareSession(&req, sse.runnerCancel)
	if err != nil {
		log.Printf("[Handler] 会话归属校验失败 - sessionId=%s, userId=%s, err=%v\n", req.SessionID, req.UserID, err)
		sse.fail("会话不存在或无权访问，请新建会话后重试")
		return
	}
	defer sess.ClearCancel()

	agent, err := h.createAgent(sse.runnerCtx, params)
	if err != nil {
		log.Printf("[Handler] 创建 Agent 失败 - err=%v\n", err)
		sse.fail("创建 AI 智能体失败，请稍后重试")
		return
	}

	// runID 精确标识本次运行，用于安全点取消（多 tab / 多 SSE 并发时不会错位）
	runID := fmt.Sprintf("%s_%d", req.SessionID, time.Now().UnixNano())
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	runnerCtx := sse.runnerCtx
	safego.GoWithName("agent-cancel-watch", func() {
		defer close(watchDone)
		select {
		case <-runnerCtx.Done():
			agent.Cancel(runID)
		case <-stopWatch:
		}
	})
	defer func() {
		close(stopWatch)
		<-watchDone
	}()

	if isResume {
		targets := make(map[string]bool, len(req.InterruptIDs))
		for _, id := range req.InterruptIDs {
			targets[id] = req.Confirmed
		}
		binding := CheckpointBinding{UserID: req.UserID, SessionID: req.SessionID, ConnID: params.connID}
		if err := agent.ResumeStream(sse.runnerCtx, runID, req.CheckPointID, binding, targets, sse.flush, sess); err != nil {
			log.Printf("[Handler] 恢复执行失败 - sessionId=%s, err=%v\n", req.SessionID, err)
			switch {
			case errors.Is(err, ErrCheckpointNotFound), errors.Is(err, ErrCheckpointForbidden), errors.Is(err, ErrCheckpointInFlight):
				sse.flush(StreamChunk{Type: "error", Content: err.Error()})
			default:
				sse.flush(StreamChunk{Type: "error", Content: "恢复执行失败，请重新发起操作"})
			}
		}
		sse.close(true)
		return
	}

	// 业务错误已在 RunStream 内通过 SSE 事件推送给前端
	if _, runErr := agent.RunStream(sse.runnerCtx, runID, req, sse.flush); runErr != nil {
		log.Printf("[Handler] Agent 执行失败 - err=%v\n", runErr)
		if !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
			sse.flush(StreamChunk{Type: "error", Content: "AI 处理出错，请稍后重试"})
		}
	}
	sse.close(true)
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
	detail, err := h.sessions.GetDetail(sessionID, user.Id)
	if err != nil {
		if errors.Is(err, ErrSessionForbidden) {
			log.Printf("[Handler] 会话越权访问被拒绝 - sessionId=%s, userId=%s\n", sessionID, user.Id)
			response.WriteErr(c, http.StatusForbidden, 403, "会话不存在或无权访问")
			return
		}
		log.Printf("[Handler] 获取会话详情失败 - sessionId=%s, err=%v\n", sessionID, err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "获取会话详情失败")
		return
	}
	response.WriteOK(c, gin.H{"session": detail})
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
	if err := h.sessions.Delete(sessionID, user.Id); err != nil {
		if errors.Is(err, ErrSessionForbidden) {
			log.Printf("[Handler] 会话越权删除被拒绝 - sessionId=%s, userId=%s\n", sessionID, user.Id)
			response.WriteErr(c, http.StatusForbidden, 403, "会话不存在或无权访问")
			return
		}
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
