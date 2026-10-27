package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/schema"

	"websql/internal/pkg/dberr"
	"websql/internal/pkg/safego"
)

// ──────────────────────────────────────────────
// 数据结构
// ──────────────────────────────────────────────

// SessionDB 数据库中的会话记录
type SessionDB struct {
	ID        string    `db:"id" json:"id"`
	UserID    string    `db:"user_id" json:"userId"`
	Title     string    `db:"title" json:"title"`
	Messages  string    `db:"messages" json:"-"`
	Context   string    `db:"context" json:"-"`
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
}

// SessionToolCall mirrors the tool call structure stored in session messages
type SessionToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function SessionFunctionCall `json:"function"`
}

// SessionFunctionCall mirrors the function call within a tool call
type SessionFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// SessionMessage 存储在 messages JSON 数组中的单条消息
type SessionMessage struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ToolCalls        []SessionToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	ToolName         string            `json:"tool_name,omitempty"`
	Name             string            `json:"name,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	// Attachments 该条用户消息携带的上传文件引用（仅 user 消息使用）。
	// 用途：历史回看时还原文件卡片、追问时向模型保留 fileId；内容仍按需读取。
	Attachments []AttachmentRef `json:"attachments,omitempty"`
}

// ──────────────────────────────────────────────
// Session — 单个会话的内存表示
// ──────────────────────────────────────────────

const sessionDebounceInterval = 300 * time.Millisecond

type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	Context   string
	mu        sync.Mutex
	messages  []SessionMessage
	cancelFn  context.CancelFunc

	// running 标记会话是否正在执行一次 Run/Resume。
	// 同一会话并发运行会交错写消息、破坏 tool_calls 配对（多 tab / 重复提交），
	// RunStream/ResumeStream 通过 TryBeginRun 互斥。
	running atomic.Bool

	debounceTimer *time.Timer
	pendingSave   bool
	lastAccess    time.Time
}

// TryBeginRun 尝试进入运行态；已有运行中的 Run 时返回 false。
func (s *Session) TryBeginRun() bool {
	return s.running.CompareAndSwap(false, true)
}

// BeginRunWait 在 timeout 内等待进入运行态；超时返回 false。
//
// 用途：确认（resume）请求。中断事件推送与 RunStream 真正返回之间存在毫秒级收尾窗口
// （Release/SaveToDB/SSE 关闭），用户在该窗口内点击确认会被误判为「会话处理中」，
// 故先短暂等待而不是立即拒绝。
func (s *Session) BeginRunWait(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if s.TryBeginRun() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// EndRun 退出运行态。
func (s *Session) EndRun() {
	s.running.Store(false)
}

// IsRunning 会话是否正在运行。
func (s *Session) IsRunning() bool {
	return s.running.Load()
}

// Append 追加消息并持久化到数据库
func (s *Session) Append(role, content string) error {
	s.mu.Lock()
	s.messages = append(s.messages, SessionMessage{Role: role, Content: content})
	s.scheduleSave()
	s.mu.Unlock()
	return nil
}

// AppendMessage 追加完整的 SessionMessage（包含工具调用信息）并持久化到数据库
func (s *Session) AppendMessage(msg SessionMessage) error {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.scheduleSave()
	s.mu.Unlock()
	return nil
}

// AppendMessageNoSave 追加消息但暂时不持久化（用于批量追加场景）
func (s *Session) AppendMessageNoSave(msg SessionMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, msg)
}

// ReplaceMessages 替换整个消息列表（用于 summarization 压缩后同步）
func (s *Session) ReplaceMessages(msgs []SessionMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = msgs
	s.scheduleSave()
}

// GetMessages 获取所有消息的副本
func (s *Session) GetMessages() []SessionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]SessionMessage, len(s.messages))
	copy(result, s.messages)
	return result
}

func (s *Session) SetCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	s.cancelFn = cancel
	s.mu.Unlock()
}

func (s *Session) Cancel() {
	s.mu.Lock()
	if s.cancelFn != nil {
		s.cancelFn()
		s.cancelFn = nil
	}
	s.mu.Unlock()
}

func (s *Session) ClearCancel() {
	s.mu.Lock()
	s.cancelFn = nil
	s.mu.Unlock()
}

// Title 取第一条用户消息的前60个字符作为标题
func (s *Session) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, msg := range s.messages {
		if msg.Role == "user" && msg.Content != "" {
			title := msg.Content
			if len([]rune(title)) > 60 {
				title = string([]rune(title)[:60]) + "..."
			}
			return title
		}
	}
	return ""
}

// SetContext 设置会话上下文（schemas/tables）并持久化
func (s *Session) SetContext(ctxJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Context = ctxJSON
	return s.doSave()
}

// MergeContext 将会话上下文与现有上下文合并后持久化
// 与 SetContext 的区别：不会丢失已有数据，新的 schemas/tables 会被合并到现有上下文中（去重）
func (s *Session) MergeContext(ctxJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ctxJSON == "" || ctxJSON == "{}" {
		return nil
	}

	var newCtx SessionContext
	if err := json.Unmarshal([]byte(ctxJSON), &newCtx); err != nil {
		return s.doSave()
	}

	var existingCtx SessionContext
	if s.Context != "" && s.Context != "{}" {
		_ = json.Unmarshal([]byte(s.Context), &existingCtx)
	}

	merged := SessionContext{
		Schemas: mergeSchemaRefs(existingCtx.Schemas, newCtx.Schemas),
		Tables:  mergeStringSlices(existingCtx.Tables, newCtx.Tables),
	}

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	s.Context = string(mergedJSON)
	return s.doSave()
}

// mergeSchemaRefs 合并两个 SchemaRef 切片，按 (connId + schema) 去重
func mergeSchemaRefs(existing, incoming []SchemaRef) []SchemaRef {
	seen := make(map[string]bool)
	result := make([]SchemaRef, 0, len(existing)+len(incoming))

	for _, s := range existing {
		key := s.ConnID + "::" + s.Schema
		if !seen[key] {
			seen[key] = true
			result = append(result, s)
		}
	}
	for _, s := range incoming {
		key := s.ConnID + "::" + s.Schema
		if !seen[key] {
			seen[key] = true
			result = append(result, s)
		}
	}
	return result
}

// mergeStringSlices 合并两个字符串切片，去重
func mergeStringSlices(existing, incoming []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(existing)+len(incoming))

	for _, s := range existing {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range incoming {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// saveToDB 将消息和上下文序列化后写入数据库（调用方需持有锁）
// scheduleSave 安排一次延迟写入。如果已有等待中的写入，延长等待时间。
func (s *Session) scheduleSave() {
	if s.pendingSave {
		s.debounceTimer.Reset(sessionDebounceInterval)
		return
	}
	s.pendingSave = true
	s.debounceTimer = time.AfterFunc(sessionDebounceInterval, func() {
		// 定时器回调运行在独立 goroutine 中，panic 会直接终止进程，
		// 因此这里必须自兜底（DB 未初始化/连接被替换等异常场景）。
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[SessionStore] debounce 保存 panic 已恢复 - session=%s, err=%v\n", s.ID, r)
			}
		}()
		s.mu.Lock()
		s.pendingSave = false
		_ = s.doSave()
		s.mu.Unlock()
	})
}

// SaveToDB 立即同步持久化（不等待 debounce）。
//
// 与 RemoveTrailingIncompleteToolCalls 的关系：
// 推荐使用 CleanAndSave()，它在一个原子操作中完成"清理+保存"，避免
// 清理与 debounce 触发 doSave 之间的 10ms 窗口内出现"已清理但被 debounce
// 写脏数据"的问题（详见 EINO_DEEP_ANALYSIS §1.2）。
func (s *Session) SaveToDB() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
		s.pendingSave = false
	}
	return s.doSave()
}

// CleanAndSave 在一个原子操作中：
//  1. 移除末尾不完整的 tool_calls 消息链
//  2. 停止 debounce timer
//  3. 同步持久化到 DB
//
// 这是 cancel / deadline 路径推荐的清理方式：避免 debounce goroutine
// 在清理与保存之间抢先写脏数据。
func (s *Session) CleanAndSave() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 在持锁状态下清理消息
	s.removeTrailingIncompleteToolCallsLocked()

	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
		s.pendingSave = false
	}
	return s.doSave()
}

// RemoveTrailingIncompleteToolCalls 仅清理内存，不保证落库一致性。
// 如需落库，请使用 CleanAndSave()。
func (s *Session) RemoveTrailingIncompleteToolCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeTrailingIncompleteToolCallsLocked()
}

// removeTrailingIncompleteToolCallsLocked 调用方必须持有 s.mu
func (s *Session) removeTrailingIncompleteToolCallsLocked() {
	for len(s.messages) > 0 {
		last := s.messages[len(s.messages)-1]
		switch last.Role {
		case "tool":
			s.messages = s.messages[:len(s.messages)-1]
		case "assistant":
			if len(last.ToolCalls) > 0 {
				if last.Content == "" {
					s.messages = s.messages[:len(s.messages)-1]
				} else {
					last.ToolCalls = nil
					s.messages[len(s.messages)-1] = last
				}
			}
			return
		default:
			return
		}
	}
}

// patchDanglingToolCallsOnLoad 在会话从 DB 加载时修复合并/重复的 tool_calls。
//
// 背景（EINO_DEEP_ANALYSIS §3）：
// 历史会话可能因进程崩溃、5min runnerCtx 超时、debounce 窗口内故障，导致：
//   - assistant(tool_calls=[X]) 后缺失对应的 tool(X) 消息
//   - 同样的 tool_call_id 出现多次（debounce + 显式 Save 双重写入）
//
// 本函数与 patchtoolcalls 中间件分工：
//   - 本函数：作用于持久化层（SessionMessage JSON），负责加载时一次性修复
//   - patchtoolcalls：作用于 LLM 上下文层（*schema.Message），每次 BeforeModel 兜底
//
// 行为：扫描所有 assistant 消息，对每个 ToolCall 检查是否在**后续**消息中存在
// 匹配的 tool 消息（按 ToolCallID 匹配）。缺失的则补一个占位 tool 消息，
// 避免 LLM 看到 dangling tool_calls 拒绝继续生成。
func patchDanglingToolCallsOnLoad(messages []SessionMessage) []SessionMessage {
	if len(messages) == 0 {
		return messages
	}
	// 先收集已存在的 tool 消息 id 集合
	haveTool := make(map[string]bool)
	for _, m := range messages {
		if m.Role == "tool" && m.ToolCallID != "" {
			haveTool[m.ToolCallID] = true
		}
	}
	// 扫描 assistant 消息，为缺失 tool 的 tool_call 补占位
	result := make([]SessionMessage, 0, len(messages))
	for _, m := range messages {
		result = append(result, m)
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					continue
				}
				if haveTool[tc.ID] {
					continue
				}
				// 缺失对应 tool 消息 → 补占位，紧跟在本条 assistant 之后
				result = append(result, SessionMessage{
					Role:       "tool",
					Content:    `{"status":"patched_on_load","message":"工具 ` + tc.Function.Name + ` (call_id=` + tc.ID + `) 的结果在持久化层缺失（可能是上次对话被取消或进程异常退出）。请基于此状态继续回答，必要时重新调用该工具。"}`,
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
				})
				haveTool[tc.ID] = true // 防止同 batch 重复补
			}
		}
	}
	return result
}

func (s *Session) doSave() error {
	data, err := json.Marshal(s.messages)
	if err != nil {
		return err
	}
	title := ""
	for _, msg := range s.messages {
		if msg.Role == "user" && msg.Content != "" {
			title = msg.Content
			if len([]rune(title)) > 60 {
				title = string([]rune(title)[:60]) + "..."
			}
			break
		}
	}
	ctx := s.Context
	if ctx == "" {
		ctx = "{}"
	}
	db := getDBOrNil()
	if db == nil {
		return nil // 管理库不可用（启动期/测试环境）：跳过持久化，不阻断对话
	}
	// WHERE 追加归属条件：即使内存中出现了 ID 冲突的会话对象，也不会整行覆盖他人会话
	// （user_id 为空的存量会话按兼容策略允许写入）
	_, err = db.Exec(`
		UPDATE t_ai_session SET messages = ?, title = ?, context = ?, updated_at = ?
		WHERE id = ? AND (user_id = ? OR user_id = '' OR user_id IS NULL)
	`, string(data), title, ctx, time.Now(), s.ID, s.UserID)
	if err != nil && dberr.IsColumnNotExist(err) {
		_, err = db.Exec(`
			UPDATE t_ai_session SET messages = ?, title = ?, updated_at = ?
			WHERE id = ? AND (user_id = ? OR user_id = '' OR user_id IS NULL)
		`, string(data), title, time.Now(), s.ID, s.UserID)
	}
	if err != nil && dberr.IsTableNotExist(err) {
		return nil
	}
	return err
}

// ──────────────────────────────────────────────
// SessionStore — 会话存储管理器
// ──────────────────────────────────────────────

const (
	sessionMaxIdleTime   = 30 * time.Minute
	sessionCleanInterval = 5 * time.Minute
	sessionMaxCacheSize  = 500
)

type SessionStore struct {
	mu      sync.Mutex
	cache   map[string]*Session
	stopCh  chan struct{}
	stopped sync.Once
}

func NewSessionStore() (*SessionStore, error) {
	ss := &SessionStore{
		cache:  make(map[string]*Session),
		stopCh: make(chan struct{}),
	}
	safego.GoWithName("session-clean", ss.cleanLoop)
	return ss, nil
}

func (s *SessionStore) cleanLoop() {
	ticker := time.NewTicker(sessionCleanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.evictIdleSessions()
		case <-s.stopCh:
			return
		}
	}
}

func (s *SessionStore) evictIdleSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var evicted int
	for id, sess := range s.cache {
		sess.mu.Lock()
		idle := now.Sub(sess.lastAccess)
		sess.mu.Unlock()
		if idle > sessionMaxIdleTime {
			sess.Cancel()
			delete(s.cache, id)
			evicted++
		}
	}
	if evicted > 0 {
		log.Printf("[SessionStore] 清理不活跃会话 - evicted=%d, remaining=%d\n", evicted, len(s.cache))
	}
}

func (s *SessionStore) Close() {
	s.stopped.Do(func() {
		close(s.stopCh)
		// 清理所有缓存的会话，防止 goroutine 泄漏
		s.mu.Lock()
		for _, sess := range s.cache {
			sess.Cancel()
		}
		s.cache = make(map[string]*Session)
		s.mu.Unlock()
		log.Printf("[SessionStore] 已关闭，所有会话已清理\n")
	})
}

// ErrSessionForbidden 表示会话不存在或不属于当前用户。
// 两种情况返回同一错误，避免通过响应差异枚举他人会话 ID。
var ErrSessionForbidden = errors.New("会话不存在或无权访问")

// sessionBelongsTo 判断会话归属是否匹配请求用户。
// 存量数据 user_id 为空时记为“无归属”，仅告警并放行（避免历史会话全部不可用），
// 其余情况严格比对，防止跨用户读写/删除会话。
func sessionBelongsTo(ownerID, requesterID string) bool {
	if requesterID == "" {
		return false
	}
	if ownerID == "" {
		log.Printf("[SessionStore] 会话缺少归属用户，按兼容策略放行 - requester=%s\n", requesterID)
		return true
	}
	return ownerID == requesterID
}

// GetOrCreate 获取或创建会话。
//
// 并发设计：数据库 I/O（加载/建行）在全局锁之外执行，避免单个用户的会话加载
// 阻塞所有其他用户（此前持 store.mu 期间调用 loadSessionFromDB，全局串行化）；
// 写回缓存时二次校验，并发加载同一会话以先入缓存者为准。
func (s *SessionStore) GetOrCreate(id, userID string) (*Session, error) {
	// 快路径：缓存命中
	s.mu.Lock()
	if sess, ok := s.cache[id]; ok {
		if !sessionBelongsTo(sess.UserID, userID) {
			s.mu.Unlock()
			log.Printf("[SessionStore] 拒绝跨用户访问缓存会话 - id=%s, owner=%s, requester=%s\n", id, sess.UserID, userID)
			return nil, ErrSessionForbidden
		}
		sess.mu.Lock()
		sess.lastAccess = time.Now()
		sess.mu.Unlock()
		s.mu.Unlock()
		return sess, nil
	}
	if len(s.cache) >= sessionMaxCacheSize {
		s.evictOldestUnlocked()
	}
	s.mu.Unlock()

	// 慢路径：DB I/O 不持全局锁
	sess, err := s.loadOrCreateSession(id, userID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.cache[id]; ok {
		// 并发加载：已有缓存则以其为准（保证同一会话只共享一个内存实例）
		if !sessionBelongsTo(cached.UserID, userID) {
			return nil, ErrSessionForbidden
		}
		cached.mu.Lock()
		cached.lastAccess = time.Now()
		cached.mu.Unlock()
		return cached, nil
	}
	s.cache[id] = sess
	return sess, nil
}

// loadOrCreateSession 从数据库加载会话或创建新会话记录（不持有全局锁）。
func (s *SessionStore) loadOrCreateSession(id, userID string) (*Session, error) {
	sess, err := loadSessionFromDB(id)
	if err != nil {
		// 管理库暂时不可用（如 SQLite database is locked、网络抖动）时不能退化为「新建空会话」：
		// 否则后续保存会用空历史整行覆盖原会话，甚至覆盖他人会话。直接报错让用户重试。
		log.Printf("[SessionStore] 加载会话失败，拒绝以空会话替代 - id=%s, err=%v\n", id, err)
		return nil, fmt.Errorf("加载会话失败，请稍后重试：%w", err)
	}
	if sess == nil {
		sess = &Session{
			ID:         id,
			UserID:     userID,
			CreatedAt:  time.Now(),
			messages:   make([]SessionMessage, 0),
			lastAccess: time.Now(),
		}
		if err := createSessionInDB(id, userID); err != nil {
			// 主键冲突说明会话已被并发创建或加载时漏读：再读一次，按库内归属校验
			if existing, loadErr := loadSessionFromDB(id); loadErr == nil && existing != nil {
				if !sessionBelongsTo(existing.UserID, userID) {
					return nil, ErrSessionForbidden
				}
				existing.lastAccess = time.Now()
				return existing, nil
			}
			log.Printf("[SessionStore] 创建会话记录失败 - id=%s, err=%v\n", id, err)
		}
	} else {
		if !sessionBelongsTo(sess.UserID, userID) {
			log.Printf("[SessionStore] 拒绝跨用户加载会话 - id=%s, owner=%s, requester=%s\n", id, sess.UserID, userID)
			return nil, ErrSessionForbidden
		}
		sess.lastAccess = time.Now()
	}
	return sess, nil
}

func (s *SessionStore) evictOldestUnlocked() {
	var oldestID string
	var oldestTime time.Time
	for id, sess := range s.cache {
		sess.mu.Lock()
		la := sess.lastAccess
		sess.mu.Unlock()
		if oldestID == "" || la.Before(oldestTime) {
			oldestID = id
			oldestTime = la
		}
	}
	if oldestID != "" {
		if sess, ok := s.cache[oldestID]; ok {
			sess.Cancel()
		}
		delete(s.cache, oldestID)
		log.Printf("[SessionStore] 缓存满淘汰 - evicted=%s\n", oldestID)
	}
}

// ListByUserID 获取用户的会话列表
func (s *SessionStore) ListByUserID(userID string) ([]SessionMeta, error) {
	sessions, err := listSessionsByUserID(userID)
	if err != nil {
		return nil, err
	}
	metas := make([]SessionMeta, 0, len(sessions))
	for _, sess := range sessions {
		title := sess.Title
		if title == "" {
			title = "未命名会话"
		}
		metas = append(metas, SessionMeta{ID: sess.ID, Title: title, CreatedAt: sess.CreatedAt})
	}
	return metas, nil
}

// checkOwnership 校验会话归属：缓存命中时用内存态判断，未命中时回源数据库。
// 返回 ErrSessionForbidden 表示会话不存在或不属于该用户。
func (s *SessionStore) checkOwnership(id, userID string) error {
	s.mu.Lock()
	sess, ok := s.cache[id]
	s.mu.Unlock()
	if ok && sessionBelongsTo(sess.UserID, userID) {
		return nil
	}
	sessDB, err := getSessionByID(id)
	if err != nil {
		return err
	}
	if sessDB == nil || !sessionBelongsTo(sessDB.UserID, userID) {
		return ErrSessionForbidden
	}
	return nil
}

// Delete 删除会话（仅允许删除归属 userID 的会话）
func (s *SessionStore) Delete(id, userID string) error {
	if err := s.checkOwnership(id, userID); err != nil {
		return err
	}
	// 会话内引用的上传文件随会话一起清理：每次上传生成独立 fileId，
	// 删除会话后这些文件不再有引用方，不必等 TTL（30 分钟）到期才释放磁盘。
	fileIDs := s.sessionAttachmentIDs(id)
	s.mu.Lock()
	if sess, ok := s.cache[id]; ok {
		sess.Cancel()
	}
	delete(s.cache, id)
	s.mu.Unlock()
	if err := deleteSessionInDB(id); err != nil {
		// 会话删除失败时不清理文件，避免历史会话中的文件卡片指向已删除的文件
		return err
	}
	RemoveSessionAttachments(fileIDs, userID)
	return nil
}

// sessionAttachmentIDs 收集会话消息中引用的上传文件 ID（去重，保持出现顺序）。
//
// 优先取内存态（正在使用的会话），未命中缓存时回源数据库；
// 读取/解析失败按「无附件」处理：删除会话本身不因文件清理失败而中断。
func (s *SessionStore) sessionAttachmentIDs(id string) []string {
	s.mu.Lock()
	sess, ok := s.cache[id]
	s.mu.Unlock()

	var messages []SessionMessage
	if ok {
		messages = sess.GetMessages()
	} else {
		sessDB, err := getSessionByID(id)
		if err != nil || sessDB == nil || sessDB.Messages == "" {
			return nil
		}
		if err := json.Unmarshal([]byte(sessDB.Messages), &messages); err != nil {
			log.Printf("[SessionStore] 解析会话消息失败，跳过附件清理 - id=%s, err=%v\n", id, err)
			return nil
		}
	}
	return collectAttachmentIDs(messages)
}

// collectAttachmentIDs 汇总消息列表中引用的上传文件 ID（去重，保持出现顺序）。
func collectAttachmentIDs(messages []SessionMessage) []string {
	seen := make(map[string]bool, len(messages))
	var ids []string
	for _, m := range messages {
		for _, ref := range m.Attachments {
			fileID := strings.TrimSpace(ref.FileID)
			if fileID == "" || seen[fileID] {
				continue
			}
			seen[fileID] = true
			ids = append(ids, fileID)
		}
	}
	return ids
}

// GetDetail 获取会话详情（含完整消息和上下文），仅允许读取归属 userID 的会话
func (s *SessionStore) GetDetail(id, userID string) (*SessionDetail, error) {
	if err := s.checkOwnership(id, userID); err != nil {
		return nil, err
	}
	sessDB, err := getSessionByID(id)
	if err != nil {
		return nil, err
	}
	if sessDB == nil {
		return nil, fmt.Errorf("会话不存在：%s", id)
	}

	var messages []SessionMessage
	if sessDB.Messages != "" {
		_ = json.Unmarshal([]byte(sessDB.Messages), &messages)
	}

	var sessionCtx *SessionContext
	if sessDB.Context != "" && sessDB.Context != "{}" {
		_ = json.Unmarshal([]byte(sessDB.Context), &sessionCtx)
	}

	displayMsgs := buildDisplayMessages(messages)

	detail := &SessionDetail{
		ID:        sessDB.ID,
		Title:     sessDB.Title,
		CreatedAt: sessDB.CreatedAt,
		Messages:  make([]SessionDetailMessage, 0, len(displayMsgs)),
		Context:   sessionCtx,
	}
	for _, msg := range displayMsgs {
		detail.Messages = append(detail.Messages, SessionDetailMessage{
			Role:        msg.Role,
			Content:     msg.Content,
			Attachments: msg.Attachments, // 供前端还原文件卡片（含过期态判定）
		})
	}
	if detail.Title == "" {
		detail.Title = "未命名会话"
	}
	return detail, nil
}

// buildDisplayMessages 把 summarization 压缩块中的原始 user 消息还原为独立条目，
// 并合并同一轮对话中的多条 assistant 消息（工具调用流程会产生中间 assistant 消息）。
//
// 背景：
//  1. summarization 压缩（EINO_DEEP_ANALYSIS §9.2）：eino 会把所有 user 消息嵌入
//     到一个 summary 消息的 <all_user_messages> 块里，用户回看时看不到原始提问。
//  2. 工具调用拆分：agent 在一次回复中可能产生多条 assistant 消息（带 tool_calls
//     的中间消息 + 最终答案），中间还夹杂 tool 消息。直接展示会导致一条回复被
//     拆成多个气泡。这里将同一轮（两次 user 消息之间）的所有 assistant 内容合并
//     为一条，并跳过 tool 消息（前端不渲染）。
//
// 返回的列表仅供展示，**不能**再喂给 LLM（缺 assistant 响应会导致 LLM 困惑）。
func buildDisplayMessages(messages []SessionMessage) []SessionMessage {
	if len(messages) == 0 {
		return messages
	}

	// 第一步：还原 summarization 压缩块中的原始 user 消息
	extracted := make([]SessionMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == "user" && strings.Contains(m.Content, "<all_user_messages>") {
			for _, content := range extractUserMessagesFromSummary(m.Content) {
				extracted = append(extracted, SessionMessage{Role: "user", Content: content})
			}
			continue
		}
		extracted = append(extracted, m)
	}

	// 第二步：合并同一轮的 assistant 消息，跳过 tool 消息
	out := make([]SessionMessage, 0, len(extracted))
	var assistantBuf strings.Builder
	flushAssistant := func() {
		if assistantBuf.Len() > 0 {
			out = append(out, SessionMessage{Role: "assistant", Content: assistantBuf.String()})
			assistantBuf.Reset()
		}
	}
	for _, m := range extracted {
		switch m.Role {
		case "assistant":
			if m.Content != "" {
				if assistantBuf.Len() > 0 {
					assistantBuf.WriteString("\n\n")
				}
				assistantBuf.WriteString(m.Content)
			}
		case "tool":
			// 前端不渲染 tool 消息，跳过
			continue
		default:
			// user 或其他角色：先冲刷积攒的 assistant 内容，再追加当前消息
			flushAssistant()
			out = append(out, m)
		}
	}
	flushAssistant()
	return out
}

// extractUserMessagesFromSummary 从 summarization 压缩块中提取原始 user 消息。
//
// eino summarization 的 <all_user_messages> 格式：
//
//	<all_user_messages>
//	    - 第一条用户消息
//	    - 第二条用户消息
//	</all_user_messages>
//
// 每条消息以 "    - " 开头（4空格+短横线+空格）。
func extractUserMessagesFromSummary(content string) []string {
	startIdx := strings.Index(content, "<all_user_messages>")
	endIdx := strings.Index(content, "</all_user_messages>")
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return nil
	}
	block := content[startIdx+len("<all_user_messages>") : endIdx]
	var result []string
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			result = append(result, strings.TrimPrefix(trimmed, "- "))
		}
	}
	return result
}

// ──────────────────────────────────────────────
// 数据库操作（内部函数）
// ──────────────────────────────────────────────

func createSessionInDB(id, userID string) error {
	db := getDBOrNil()
	if db == nil {
		return nil
	}
	_, err := db.Exec(`
		INSERT INTO t_ai_session (id, user_id, title, messages, created_at, updated_at)
		VALUES (?, ?, '', '[]', ?, ?)
	`, id, userID, time.Now(), time.Now())
	if err != nil && dberr.IsTableNotExist(err) {
		return nil
	}
	return err
}

func loadSessionFromDB(id string) (*Session, error) {
	if getDBOrNil() == nil {
		return nil, nil // 管理库不可用（测试/启动期）：按新会话处理，仅内存态
	}
	sessDB, err := getSessionByID(id)
	if err != nil || sessDB == nil {
		return nil, err
	}

	var rawMessages []SessionMessage
	if sessDB.Messages != "" {
		_ = json.Unmarshal([]byte(sessDB.Messages), &rawMessages)
	}

	rawMessages = patchDanglingToolCallsOnLoad(rawMessages)

	ctx := sessDB.Context
	if ctx == "" {
		ctx = "{}"
	}

	return &Session{
		ID:        sessDB.ID,
		UserID:    sessDB.UserID,
		CreatedAt: sessDB.CreatedAt,
		Context:   ctx,
		messages:  rawMessages,
	}, nil
}

func getSessionByID(id string) (*SessionDB, error) {
	var session SessionDB
	err := getDB().Get(&session, `
		SELECT id, user_id, COALESCE(title,'') as title, COALESCE(messages,'[]') as messages, COALESCE(context,'{}') as context, created_at, updated_at
		FROM t_ai_session WHERE id = ?
	`, id)
	if err != nil {
		if dberr.IsNoRows(err) {
			return nil, nil
		}
		if dberr.IsColumnNotExist(err) {
			return getSessionByIDWithoutContext(id)
		}
		log.Printf("[SessionDB] 查询会话失败 - id=%s, err=%v\n", id, err)
		return nil, err
	}
	return &session, nil
}

func getSessionByIDWithoutContext(id string) (*SessionDB, error) {
	var session SessionDB
	err := getDB().Get(&session, `
		SELECT id, user_id, COALESCE(title,'') as title, COALESCE(messages,'[]') as messages, '{}' as context, created_at, updated_at
		FROM t_ai_session WHERE id = ?
	`, id)
	if err != nil {
		if dberr.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func listSessionsByUserID(userID string) ([]SessionDB, error) {
	var sessions []SessionDB
	err := getDB().Select(&sessions, `
		SELECT id, user_id, COALESCE(title,'') as title, created_at, updated_at
		FROM t_ai_session WHERE user_id = ? ORDER BY created_at DESC
	`, userID)
	if err != nil {
		if dberr.IsTableNotExist(err) {
			return []SessionDB{}, nil
		}
		return nil, err
	}
	return sessions, nil
}

// escapeLikeKeyword 转义 LIKE 模式中的通配符（%、_、\），
// 使用户搜索词中的 "%"、"_" 按字面量匹配而不是通配（管理库为 MySQL/MariaDB/SQLite，
// 均默认以 \ 作为 LIKE 转义符）。
func escapeLikeKeyword(keyword string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(keyword)
}

func listSessionsByUserIDPaged(userID, keyword string, limit, offset int) ([]SessionDB, error) {
	var sessions []SessionDB
	var (
		sqlStr string
		args   []any
	)
	if keyword == "" {
		sqlStr = `
			SELECT id, user_id, COALESCE(title,'') as title, created_at, updated_at
			FROM t_ai_session WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?
		`
		args = []any{userID, limit, offset}
	} else {
		sqlStr = `
			SELECT id, user_id, COALESCE(title,'') as title, created_at, updated_at
			FROM t_ai_session WHERE user_id = ? AND title LIKE ? ORDER BY created_at DESC LIMIT ? OFFSET ?
		`
		args = []any{userID, "%" + escapeLikeKeyword(keyword) + "%", limit, offset}
	}
	if err := getDB().Select(&sessions, sqlStr, args...); err != nil {
		if dberr.IsTableNotExist(err) {
			return []SessionDB{}, nil
		}
		return nil, err
	}
	return sessions, nil
}

func countSessionsByUserID(userID, keyword string) (int, error) {
	var (
		sqlStr string
		args   []any
	)
	if keyword == "" {
		sqlStr = `SELECT COUNT(*) FROM t_ai_session WHERE user_id = ?`
		args = []any{userID}
	} else {
		sqlStr = `SELECT COUNT(*) FROM t_ai_session WHERE user_id = ? AND title LIKE ?`
		args = []any{userID, "%" + escapeLikeKeyword(keyword) + "%"}
	}
	var count int
	if err := getDB().Get(&count, sqlStr, args...); err != nil {
		if dberr.IsTableNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func deleteSessionInDB(id string) error {
	_, err := getDB().Exec(`DELETE FROM t_ai_session WHERE id = ?`, id)
	if err != nil && dberr.IsTableNotExist(err) {
		return nil
	}
	return err
}

// ──────────────────────────────────────────────
// schema ↔ SessionMessage 转换
// ──────────────────────────────────────────────

func sessionToolCallsFromSchema(toolCalls []schema.ToolCall) []SessionToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	result := make([]SessionToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = SessionToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: SessionFunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}
	}
	return result
}

func sessionToolCallsToSchema(toolCalls []SessionToolCall) []schema.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	result := make([]schema.ToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = schema.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: schema.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}
	}
	return result
}

func mergeToolCalls(chunks []schema.ToolCall) []schema.ToolCall {
	if len(chunks) == 0 {
		return nil
	}
	type tcBuilder struct {
		id        string
		typeField string
		name      strings.Builder
		args      strings.Builder
	}
	// 分组规则：
	//   - 有 Index：按 Index 分组（OpenAI 兼容流式协议）
	//   - 无 Index 但有 ID：按 ID 分组（Ollama 等不设置 Index 的实现，并行工具调用各自带 ID）
	//   - 两者皆无：视为上一个工具调用的续传片段
	// 旧实现把 Index 为 nil 的片段全部并入 index 0，导致 Ollama 并行工具调用的
	// 名称与参数被拼接成一个非法调用。
	builders := make(map[string]*tcBuilder)
	indices := make([]string, 0, len(chunks))
	lastKey := ""
	for _, tc := range chunks {
		var key string
		switch {
		case tc.Index != nil:
			key = fmt.Sprintf("i:%d", *tc.Index)
		case tc.ID != "":
			key = "id:" + tc.ID
		case lastKey != "":
			key = lastKey
		default:
			key = "i:0"
		}
		lastKey = key
		b, ok := builders[key]
		if !ok {
			b = &tcBuilder{}
			builders[key] = b
			indices = append(indices, key)
		}
		if tc.ID != "" {
			b.id = tc.ID
		}
		if tc.Type != "" {
			b.typeField = tc.Type
		}
		if tc.Function.Name != "" {
			b.name.WriteString(tc.Function.Name)
		}
		if tc.Function.Arguments != "" {
			b.args.WriteString(tc.Function.Arguments)
		}
	}
	result := make([]schema.ToolCall, 0, len(builders))
	for _, idx := range indices {
		b := builders[idx]
		result = append(result, schema.ToolCall{
			ID:   b.id,
			Type: b.typeField,
			Function: schema.FunctionCall{
				Name:      b.name.String(),
				Arguments: b.args.String(),
			},
		})
	}
	return result
}
