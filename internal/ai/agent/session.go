package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"websql/internal/database"
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

	debounceTimer *time.Timer
	pendingSave   bool
	lastAccess    time.Time
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
	_, err = database.Mngtdb.Exec(`
		UPDATE t_ai_session SET messages = ?, title = ?, context = ?, updated_at = ? WHERE id = ?
	`, string(data), title, ctx, time.Now(), s.ID)
	if err != nil && strings.Contains(err.Error(), "no such column: context") {
		_, err = database.Mngtdb.Exec(`
			UPDATE t_ai_session SET messages = ?, title = ?, updated_at = ? WHERE id = ?
		`, string(data), title, time.Now(), s.ID)
	}
	if err != nil && (strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table")) {
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
	cancels map[string]context.CancelFunc
	stopCh  chan struct{}
	stopped sync.Once
}

func NewSessionStore() (*SessionStore, error) {
	ss := &SessionStore{
		cache:   make(map[string]*Session),
		cancels: make(map[string]context.CancelFunc),
		stopCh:  make(chan struct{}),
	}
	go ss.cleanLoop()
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

// GetOrCreate 获取或创建会话
func (s *SessionStore) GetOrCreate(id, userID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.cache[id]; ok {
		sess.mu.Lock()
		sess.lastAccess = time.Now()
		sess.mu.Unlock()
		return sess, nil
	}

	if len(s.cache) >= sessionMaxCacheSize {
		s.evictOldestUnlocked()
	}

	sess, err := loadSessionFromDB(id)
	if err != nil || sess == nil {
		sess = &Session{
			ID:         id,
			UserID:     userID,
			CreatedAt:  time.Now(),
			messages:   make([]SessionMessage, 0),
			lastAccess: time.Now(),
		}
		if err := createSessionInDB(id, userID); err != nil {
			log.Printf("[SessionStore] 创建会话记录失败 - id=%s, err=%v\n", id, err)
		}
	} else {
		sess.lastAccess = time.Now()
	}

	s.cache[id] = sess
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

// Delete 删除会话
func (s *SessionStore) Delete(id string) error {
	s.mu.Lock()
	if sess, ok := s.cache[id]; ok {
		sess.Cancel()
	}
	delete(s.cache, id)
	s.mu.Unlock()
	return deleteSessionInDB(id)
}

func (s *SessionStore) RegisterCancel(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	s.cancels[id] = cancel
	s.mu.Unlock()
}

func (s *SessionStore) Cancel(id string) {
	s.mu.Lock()
	if cancel, ok := s.cancels[id]; ok {
		cancel()
		delete(s.cancels, id)
	}
	s.mu.Unlock()
}

func (s *SessionStore) UnregisterCancel(id string) {
	s.mu.Lock()
	delete(s.cancels, id)
	s.mu.Unlock()
}

// GetDetail 获取会话详情（含完整消息和上下文）
func (s *SessionStore) GetDetail(id string) (*SessionDetail, error) {
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
		detail.Messages = append(detail.Messages, SessionDetailMessage{Role: msg.Role, Content: msg.Content})
	}
	if detail.Title == "" {
		detail.Title = "未命名会话"
	}
	return detail, nil
}

// buildDisplayMessages 把 summarization 压缩块中的原始 user 消息还原为独立条目。
//
// 背景（EINO_DEEP_ANALYSIS §9.2）：eino 的 summarization 中间件在压缩历史时，
// 会把所有 user 消息嵌入到一个新的 user summary 消息的 <all_user_messages> 块里。
// 用户回看历史时看到的就不再是自己当初的提问。
//
// 本函数在展示层（GetDetail）解析 <all_user_messages> 块，提取其中的原始 user 消息
// 并还原为独立条目。无需额外的持久化机制——summarization 块本身已包含原始消息。
//
// 返回的列表仅供展示，**不能**再喂给 LLM（缺 assistant 响应会导致 LLM 困惑）。
func buildDisplayMessages(messages []SessionMessage) []SessionMessage {
	if len(messages) == 0 {
		return messages
	}
	out := make([]SessionMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == "user" && strings.Contains(m.Content, "<all_user_messages>") {
			extracted := extractUserMessagesFromSummary(m.Content)
			for _, content := range extracted {
				out = append(out, SessionMessage{Role: "user", Content: content})
			}
			continue
		}
		out = append(out, m)
	}
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
	_, err := database.Mngtdb.Exec(`
		INSERT INTO t_ai_session (id, user_id, title, messages, created_at, updated_at)
		VALUES (?, ?, '', '[]', ?, ?)
	`, id, userID, time.Now(), time.Now())
	if err != nil && (strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table")) {
		return nil
	}
	return err
}

func loadSessionFromDB(id string) (*Session, error) {
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
	err := database.Mngtdb.Get(&session, `
		SELECT id, user_id, COALESCE(title,'') as title, COALESCE(messages,'[]') as messages, COALESCE(context,'{}') as context, created_at, updated_at
		FROM t_ai_session WHERE id = ?
	`, id)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") || strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		if strings.Contains(err.Error(), "no such column: context") {
			return getSessionByIDWithoutContext(id)
		}
		log.Printf("[SessionDB] 查询会话失败 - id=%s, err=%v\n", id, err)
		return nil, err
	}
	return &session, nil
}

func getSessionByIDWithoutContext(id string) (*SessionDB, error) {
	var session SessionDB
	err := database.Mngtdb.Get(&session, `
		SELECT id, user_id, COALESCE(title,'') as title, COALESCE(messages,'[]') as messages, '{}' as context, created_at, updated_at
		FROM t_ai_session WHERE id = ?
	`, id)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") || strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func listSessionsByUserID(userID string) ([]SessionDB, error) {
	var sessions []SessionDB
	err := database.Mngtdb.Select(&sessions, `
		SELECT id, user_id, COALESCE(title,'') as title, created_at, updated_at
		FROM t_ai_session WHERE user_id = ? ORDER BY created_at DESC
	`, userID)
	if err != nil {
		if strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table") {
			return []SessionDB{}, nil
		}
		return nil, err
	}
	return sessions, nil
}

func deleteSessionInDB(id string) error {
	_, err := database.Mngtdb.Exec(`DELETE FROM t_ai_session WHERE id = ?`, id)
	if err != nil && (strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table")) {
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
	builders := make(map[int]*tcBuilder)
	indices := make([]int, 0, len(chunks))
	for _, tc := range chunks {
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		b, ok := builders[idx]
		if !ok {
			b = &tcBuilder{}
			builders[idx] = b
			indices = append(indices, idx)
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
