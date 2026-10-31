package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
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

// allowedSSETypes SSE 事件类型白名单。
//
// 未知类型默认不推送：避免未来新增字段（如内部调试信息）未经审计就出域。
var allowedSSETypes = map[string]bool{
	"session":        true,
	"content":        true,
	"thinking":       true,
	"done":           true,
	"error":          true,
	"retract":        true,
	"cancelled":      true,
	"danger_confirm": true,
	"queue_notice":   true,
	"tool_status":    true,
}

// flush 推送一个事件（JSON）
func (s *sseStream) flush(chunk StreamChunk) {
	if !allowedSSETypes[chunk.Type] {
		log.Printf("[SSE] 拒绝推送未登记事件类型 - type=%s\n", chunk.Type)
		return
	}
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

// HandleSkillDegrade 用户选择「改用 Go 原生导出」：唤醒当前排队中的技能执行，
// 由 execute 返回普通提示，引导模型在同一次运行内改用 Go 原生导出工具。
//
// 语义：只跳过本次调用，不锁定整轮运行（用户可能只是不愿等这一次）。
func (h *Handler) HandleSkillDegrade(c *gin.Context) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SessionID) == "" {
		response.WriteErr(c, http.StatusBadRequest, http.StatusBadRequest, "sessionId 不能为空")
		return
	}

	user := admin.GetUser(c.GetHeader("Authorization"))
	if user == nil || user.Id == "" {
		response.WriteAuthErr(c, "未认证或认证已过期")
		return
	}

	// 与下载接口一致：无权限与不存在返回同样的提示，避免探测他人会话。
	if err := h.sessions.CheckOwnership(req.SessionID, user.Id); err != nil {
		response.WriteErr(c, http.StatusNotFound, http.StatusNotFound, "会话不存在或无权访问")
		return
	}

	degraded := RequestSkillDegrade(req.SessionID)
	log.Printf("[SkillQueue] 用户请求降级 - session=%s, user=%s, active=%v\n", req.SessionID, user.Id, degraded)
	response.WriteOK(c, gin.H{"degraded": degraded})
}

// runSecurityCache 缓存权限投影构建结果（30s），避免每个请求重复加载角色/权限明细。
//
// 只缓存“权限投影”；Masker/Mode/请求级标志在命中后重新注入，
// 避免脱敏规则更新后最长 30s 仍用旧快照。
type runSecurityCacheEntry struct {
	sec *RunSecurityContext
	at  time.Time
}

var runSecurityCache struct {
	mu    sync.Mutex
	items map[string]runSecurityCacheEntry
}

const runSecurityCacheTTL = 30 * time.Second

func runSecurityCacheKey(profile RuntimeProfile, userID, connID string, refs []SchemaRef, revision uint64) string {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, ref.ConnID+"/"+ref.Schema)
	}
	sort.Strings(parts)
	return fmt.Sprintf("%d|%s|%s|%d|%s", profile, userID, connID, revision, strings.Join(parts, ","))
}

func getCachedRunSecurity(key string) *RunSecurityContext {
	runSecurityCache.mu.Lock()
	defer runSecurityCache.mu.Unlock()
	entry, ok := runSecurityCache.items[key]
	if !ok || time.Since(entry.at) > runSecurityCacheTTL {
		if ok {
			delete(runSecurityCache.items, key)
		}
		return nil
	}
	return entry.sec
}

func putCachedRunSecurity(key string, sec *RunSecurityContext) {
	if sec == nil {
		return
	}
	runSecurityCache.mu.Lock()
	defer runSecurityCache.mu.Unlock()
	if runSecurityCache.items == nil {
		runSecurityCache.items = make(map[string]runSecurityCacheEntry)
	}
	// 简单容量保护：超过 512 条时丢弃过期项；仍超限则整体清空（缓存非正确性依赖）。
	if len(runSecurityCache.items) > 512 {
		for k, entry := range runSecurityCache.items {
			if time.Since(entry.at) > runSecurityCacheTTL {
				delete(runSecurityCache.items, k)
			}
		}
		if len(runSecurityCache.items) > 512 {
			runSecurityCache.items = make(map[string]runSecurityCacheEntry)
		}
	}
	runSecurityCache.items[key] = runSecurityCacheEntry{sec: sec, at: time.Now()}
}

// resolveCachedRunSecurity 优先取缓存；未命中时用 build 构造并回填。
//
// 抽成独立函数是为了能注入 build 计数，回归验证 F-22：
// 同一用户/连接/权限修订的持续对话不应反复加载角色与权限明细。
func resolveCachedRunSecurity(key string, build func() (*RunSecurityContext, error)) (*RunSecurityContext, error) {
	if base := getCachedRunSecurity(key); base != nil {
		return base, nil
	}
	built, err := build()
	if err != nil {
		return nil, err
	}
	putCachedRunSecurity(key, built)
	return built, nil
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
	sec       *RunSecurityContext
	auditCtx  *ExecAuditCtx
}

func resolveRequestParams(c *gin.Context, req *ChatRequest, isResume bool) (*requestParams, error) {
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
	profile := CurrentRuntimeProfile()

	// 脱敏规则不可用（管理库读取失败）时禁止数据出域：不能降级为「不脱敏继续查」。
	masker := GetMaskerProvider().Get()
	egressBlocked := false
	if masker == nil {
		log.Printf("[Handler] 脱敏规则不可用，本轮禁止数据出域 - user=%s\n", user.Id)
		recordDegraded("masker", "unavailable")
		masker = NewNoopMasker()
		egressBlocked = true
	}

	revision := admin.CurrentPermissionRevision()
	revisionUnavailable := false
	if profile.RequiresRevisionLease() {
		if _, fresh := admin.SnapshotPermissionRevision(); !fresh {
			log.Printf("[Handler] 权限修订号不可信（租约过期），本轮禁止数据工具 - user=%s\n", user.Id)
			recordDegraded("revision", "stale_lease")
			revisionUnavailable = true
		}
	}

	cacheKey := runSecurityCacheKey(profile, user.Id, connID, req.Schemas, revision)
	base, cacheErr := resolveCachedRunSecurity(cacheKey, func() (*RunSecurityContext, error) {
		return BuildRunSecurityContext(profile, user.Id, connID, req.Schemas, revision, masker)
	})
	if cacheErr != nil {
		return nil, cacheErr
	}
	// 每次请求使用独立副本：Masker/Mode 与请求级安全标志按当前时刻注入，
	// Scopes 只读可安全共享。
	secCopy := *base
	secCopy.Masker = masker
	secCopy.Mode = ResolveRolloutMode(user.Id)
	sec := &secCopy
	sec.DataEgressBlocked = egressBlocked
	sec.RevisionUnavailable = revisionUnavailable

	// 输入先校验、后进入会话与模型：规范化 + 注入风险 + 写能力收缩。
	// Resume 请求不携带新提问，不能按空输入拒绝（其安全约束由 checkpoint 快照保证）。
	if !isResume {
		clean, err := PreprocessQuestion(req.Question, nil)
		if err != nil {
			var permErr *PermissionError
			if errors.As(err, &permErr) {
				recordSecurityEvent(StageInput, "injection", "deny")
				auditSecurityDenied(StageInput, "preprocess", "input_high_risk", user.Id)
			}
			return nil, err
		}
		req.Question = clean.Text
		if clean.RestrictWrites {
			sec = sec.RestrictForInputRisk()
		}
	}
	// 客户端声明不能扩大权限：表范围只能是受支持标识符且逐表校验授权。
	req.TableContext = sanitizeTableContext(req.TableContext, sec)

	// 默认连接 Scope 作为 Agent 构建期快照；工具层始终按目标连接从 sec.Scopes 取。
	scope, err := sec.DefaultScope()
	if err != nil {
		scope = BuildPermissionScope(user.Id, connID, schemaNames)
	}
	if sec.RestrictWrites {
		scope = scope.CloneWithRestrictedWrites()
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
		sec:       sec,
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
	var permErr *PermissionError
	switch {
	case errors.Is(err, errUnauthenticated):
		response.WriteAuthErr(c, err.Error())
	case errors.Is(err, errAIConfigMissing):
		response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
	case errors.Is(err, ErrInputRejected):
		response.WriteErr(c, http.StatusBadRequest, 400, "提问为空、过长或编码无效")
	case errors.As(err, &permErr):
		// 权限类错误（含无权访问所选连接、输入触发安全策略）在 SSE 建立前直接拒绝，
		// 不进入会话与模型，避免未授权对象名进入上下文。
		response.WriteErr(c, http.StatusForbidden, 403, permErr.Message)
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
		params.dbVersion, params.schemas, params.scope, params.sec, params.auditCtx)
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

	params, err := resolveRequestParams(c, &req, isResume)
	if err != nil {
		writeParamsError(c, err)
		return
	}
	// 未选择数据库时，允许以用户上传的文件作为唯一数据来源
	if params.sec.Profile.Remote() && !params.sec.HasAnyAccess() &&
		!hasFileDataSource(&req) && !h.sessionHasFileDataSource(req.SessionID, req.UserID) {
		response.WriteErr(c, http.StatusForbidden, 403, "你没有此数据库连接的访问权限，请选择有权限的数据库或上传文件")
		return
	}

	sse := newSSEStream(c)
	defer sse.close(false)

	// 安全上下文必须在进入会话与模型之前就绪：
	// 输入预处理、历史过滤、工具授权、输出审计都从这里取同一份事实。
	runCtx := WithRunSecurity(sse.runnerCtx, params.sec)

	sess, err := h.prepareSession(&req, sse.runnerCancel)
	if err != nil {
		log.Printf("[Handler] 会话归属校验失败 - sessionId=%s, userId=%s, err=%v\n", req.SessionID, req.UserID, err)
		sse.fail("会话不存在或无权访问，请新建会话后重试")
		return
	}
	defer sess.ClearCancel()

	agent, err := h.createAgent(runCtx, params)
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
		// checkpoint 绑定权限快照：恢复前比较当前权限，收缩或摘要不一致时要求重新发起。
		binding := CheckpointBinding{
			UserID:         req.UserID,
			SessionID:      req.SessionID,
			ConnID:         params.connID,
			Revision:       params.sec.Revision,
			RevisionLoaded: true,
			ScopeDigest:    params.sec.ScopeDigest,
		}
		if err := agent.ResumeStream(runCtx, runID, req.CheckPointID, binding, targets, sse.flush, sess); err != nil {
			log.Printf("[Handler] 恢复执行失败 - sessionId=%s, err=%v\n", req.SessionID, err)
			switch {
			case errors.Is(err, ErrCheckpointNotFound), errors.Is(err, ErrCheckpointForbidden),
				errors.Is(err, ErrCheckpointInFlight), errors.Is(err, ErrCheckpointStale):
				sse.flush(StreamChunk{Type: "error", Content: err.Error()})
			default:
				sse.flush(StreamChunk{Type: "error", Content: "恢复执行失败，请重新发起操作"})
			}
		}
		sse.close(true)
		return
	}

	// 业务错误已在 RunStream 内通过 SSE 事件推送给前端
	if _, runErr := agent.RunStream(runCtx, runID, req, sse.flush); runErr != nil {
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
	detail, err := h.sessionDetailForDisplay(sessionID, user.Id)
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

// sessionDetailForDisplay 按当前权限投影过滤历史详情。
//
// 权限被回收后，assistant/tool 中的旧数据必须隐藏；无法构建安全上下文
// （连接已删除/完全无权限）时只保留用户提问与标题。
func (h *Handler) sessionDetailForDisplay(sessionID, userID string) (*SessionDetail, error) {
	sessCtx, err := h.sessions.GetContext(sessionID, userID)
	if err != nil || sessCtx == nil || len(sessCtx.Schemas) == 0 {
		return h.sessions.GetDetail(sessionID, userID)
	}
	masker := GetMaskerProvider().Get()
	egressBlocked := false
	if masker == nil {
		masker = NewNoopMasker()
		egressBlocked = true
	}
	profile := CurrentRuntimeProfile()
	defaultConn := sessCtx.Schemas[0].ConnID
	sec, buildErr := BuildRunSecurityContext(profile, userID, defaultConn, sessCtx.Schemas, admin.CurrentPermissionRevision(), masker)
	if buildErr != nil {
		detail, detailErr := h.sessions.GetDetail(sessionID, userID)
		if detailErr != nil {
			return nil, detailErr
		}
		for i := range detail.Messages {
			if detail.Messages[i].Role == "assistant" {
				detail.Messages[i].Content = HistoryQuarantinePlaceholder
			}
		}
		return detail, nil
	}
	sec.DataEgressBlocked = egressBlocked
	return h.sessions.GetDetailFiltered(sessionID, userID, sec)
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
	db := getDBOrNil()
	if db == nil {
		// 启动期/测试环境：提示词与权限判定不应因管理库未就绪而 panic。
		return "", "", ""
	}
	cfgList := []conn.ConnCfg{}
	err := db.Select(&cfgList, "select * from t_conn where id = ?", connID)
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
