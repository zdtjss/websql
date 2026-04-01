package agent

import (
	"sync"
	"time"

	"go-web/utils"
)

// SessionStore 管理会话的内存存储，支持 TTL 自动过期。
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionStore 创建会话存储，ttl 为会话过期时间。
func NewSessionStore(ttl time.Duration) *SessionStore {
	s := &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
	go s.cleanupLoop()
	return s
}

// GetOrCreate 获取已有会话或创建新会话。
func (s *SessionStore) GetOrCreate(sessionID string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionID == "" {
		sessionID = utils.RandomStr()
	}

	if sess, ok := s.sessions[sessionID]; ok {
		sess.UpdatedAt = time.Now()
		return sess
	}

	sess := &Session{
		ID:        sessionID,
		Messages:  make([]Message, 0, 16),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.sessions[sessionID] = sess
	return sess
}

// Append 向会话追加消息。
func (s *SessionStore) Append(sessionID string, msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return
	}
	msg.Timestamp = time.Now()
	sess.Messages = append(sess.Messages, msg)
	sess.UpdatedAt = time.Now()
}

// GetMessages 获取会话的所有消息。
func (s *SessionStore) GetMessages(sessionID string) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil
	}
	cp := make([]Message, len(sess.Messages))
	copy(cp, sess.Messages)
	return cp
}

// Delete 删除会话。
func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// cleanupLoop 定期清理过期会话。
func (s *SessionStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, sess := range s.sessions {
			if now.Sub(sess.UpdatedAt) > s.ttl {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
