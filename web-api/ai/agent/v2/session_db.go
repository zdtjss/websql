package agentv2

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"go-web/config"
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
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
}

// SessionMessage 存储在 messages JSON 数组中的单条消息
type SessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ──────────────────────────────────────────────
// Session — 单个会话的内存表示
// ──────────────────────────────────────────────

type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	mu        sync.Mutex
	messages  []SessionMessage
	cancelFn  context.CancelFunc
}

// Append 追加消息并持久化到数据库
func (s *Session) Append(role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, SessionMessage{Role: role, Content: content})
	return s.saveToDB()
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

// saveToDB 将消息序列化后写入数据库（调用方需持有锁）
func (s *Session) saveToDB() error {
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
	_, err = config.Mngtdb.Exec(`
		UPDATE t_ai_session SET messages = ?, title = ?, updated_at = ? WHERE id = ?
	`, string(data), title, time.Now(), s.ID)
	if err != nil && (strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table")) {
		return nil
	}
	return err
}

// ──────────────────────────────────────────────
// SessionStore — 会话存储管理器
// ──────────────────────────────────────────────

type SessionStore struct {
	mu      sync.Mutex
	cache   map[string]*Session
	cancels map[string]context.CancelFunc
}

func NewSessionStore() (*SessionStore, error) {
	return &SessionStore{cache: make(map[string]*Session), cancels: make(map[string]context.CancelFunc)}, nil
}

// GetOrCreate 获取或创建会话
func (s *SessionStore) GetOrCreate(id, userID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.cache[id]; ok {
		return sess, nil
	}

	// 尝试从数据库加载
	sess, err := loadSessionFromDB(id)
	if err != nil || sess == nil {
		// 不存在，创建新会话
		sess = &Session{
			ID:        id,
			UserID:    userID,
			CreatedAt: time.Now(),
			messages:  make([]SessionMessage, 0),
		}
		if err := createSessionInDB(id, userID); err != nil {
			log.Printf("[SessionStore] 创建会话记录失败 - id=%s, err=%v\n", id, err)
		}
	}

	s.cache[id] = sess
	return sess, nil
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

// GetDetail 获取会话详情（含完整消息）
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

	detail := SessionDetail{
		ID:        sessDB.ID,
		Title:     sessDB.Title,
		CreatedAt: sessDB.CreatedAt,
		Messages:  make([]SessionDetailMessage, 0, len(messages)),
	}
	for _, msg := range messages {
		detail.Messages = append(detail.Messages, SessionDetailMessage{Role: msg.Role, Content: msg.Content})
	}
	if detail.Title == "" {
		detail.Title = "未命名会话"
	}
	return &detail, nil
}

// ──────────────────────────────────────────────
// 数据库操作（内部函数）
// ──────────────────────────────────────────────

func createSessionInDB(id, userID string) error {
	_, err := config.Mngtdb.Exec(`
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

	var messages []SessionMessage
	if sessDB.Messages != "" {
		_ = json.Unmarshal([]byte(sessDB.Messages), &messages)
	}

	return &Session{
		ID:        sessDB.ID,
		UserID:    sessDB.UserID,
		CreatedAt: sessDB.CreatedAt,
		messages:  messages,
	}, nil
}

func getSessionByID(id string) (*SessionDB, error) {
	var session SessionDB
	err := config.Mngtdb.Get(&session, `
		SELECT id, user_id, COALESCE(title,'') as title, COALESCE(messages,'[]') as messages, created_at, updated_at
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
	err := config.Mngtdb.Select(&sessions, `
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
	_, err := config.Mngtdb.Exec(`DELETE FROM t_ai_session WHERE id = ?`, id)
	if err != nil && (strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "no such table")) {
		return nil
	}
	return err
}
