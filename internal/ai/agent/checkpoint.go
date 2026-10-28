package agent

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"websql/internal/pkg/safego"
)

const (
	// checkpointMaxAge 中断状态（危险 SQL 待确认）的有效期。
	// 与 SSE 请求超时（30 分钟）对齐：用户在确认弹窗停留过久后仍可恢复执行，
	// 超时后 ResumeStream 会失败并提示重新发起操作。
	checkpointMaxAge   = 30 * time.Minute
	checkpointCleanSec = 3 * time.Minute
	// checkpointMaxEntries 内存中最多保留的中断状态数，超出后淘汰最旧条目，
	// 防止频繁取消/中断把完整会话状态无限堆积在内存里。
	checkpointMaxEntries = 2000
)

var (
	// ErrCheckpointNotFound 中断状态不存在、已过期或已被消费
	ErrCheckpointNotFound = errors.New("待确认的操作不存在或已过期，请重新发起")
	// ErrCheckpointForbidden 中断状态不属于当前用户/会话/连接
	ErrCheckpointForbidden = errors.New("无权恢复该操作")
	// ErrCheckpointInFlight 同一中断正在恢复执行（重复提交）
	ErrCheckpointInFlight = errors.New("该操作正在执行中，请勿重复提交")
	// ErrCheckpointStale 中断产生后权限已变化（收缩或无法确认），旧 checkpoint 不再可恢复。
	// 安全语义：不允许用旧快照恢复写操作，必须由用户基于当前权限重新发起。
	ErrCheckpointStale = errors.New("权限已发生变化，待确认的操作已失效，请重新发起")
)

// CheckpointBinding 中断状态的归属信息。
//
// 安全背景：eino Runner 只负责 Set/Get，不会在恢复后删除 checkpoint。
// 若不做绑定与一次性消费，同一个已批准的写操作可以被重复提交的 resume 请求
// （双击、网络重试、恶意重放）再次执行；也可能被其他会话/连接的请求拿去恢复。
type CheckpointBinding struct {
	UserID    string
	SessionID string
	ConnID    string
	// Revision 创建 checkpoint 时的权限修订号
	Revision uint64
	// ScopeDigest 创建 checkpoint 时的权限快照摘要
	ScopeDigest string
}

type cpEntry struct {
	data      []byte
	binding   CheckpointBinding
	version   uint64 // 每次 Set 递增，用于判断恢复过程中是否产生了新的中断
	inflight  bool
	updatedAt time.Time
}

// InMemoryCheckPointStore 实现 eino 的 CheckPointStore 与 CheckPointDeleter。
// 单实例部署有效；多实例部署需要替换为共享存储（并保留同样的绑定/消费语义）。
type InMemoryCheckPointStore struct {
	mu         sync.Mutex
	items      map[string]*cpEntry
	maxAge     time.Duration
	maxEntries int
}

func NewInMemoryCheckPointStore() *InMemoryCheckPointStore {
	s := &InMemoryCheckPointStore{
		items:      make(map[string]*cpEntry),
		maxAge:     checkpointMaxAge,
		maxEntries: checkpointMaxEntries,
	}
	safego.GoWithName("checkpoint-clean", func() {
		ticker := time.NewTicker(checkpointCleanSec)
		defer ticker.Stop()
		for range ticker.C {
			s.cleanup()
		}
	})
	return s
}

// Bind 在 Run 开始前登记 checkpoint 的归属。Runner 之后的 Set 会沿用该归属。
func (s *InMemoryCheckPointStore) Bind(id string, b CheckpointBinding) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[id]
	if !ok {
		e = &cpEntry{}
		s.items[id] = e
	}
	e.binding = b
	e.updatedAt = time.Now()
	s.evictOverflowLocked()
}

// Set 由 eino Runner 在中断（或带中断信号的取消）时调用。
func (s *InMemoryCheckPointStore) Set(_ context.Context, id string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[id]
	if !ok {
		e = &cpEntry{}
		s.items[id] = e
	}
	e.data = value
	e.version++
	e.inflight = false // 恢复过程中再次中断：允许用户对新的中断继续确认
	e.updatedAt = time.Now()
	s.evictOverflowLocked()
	return nil
}

// Get 由 eino Runner 在恢复时调用。
func (s *InMemoryCheckPointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[id]
	if !ok || e.data == nil || time.Since(e.updatedAt) > s.maxAge {
		return nil, false, nil
	}
	return e.data, true, nil
}

// Delete 实现 eino CheckPointDeleter。
func (s *InMemoryCheckPointStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.items, id)
	s.mu.Unlock()
	return nil
}

// BeginResume 校验归属并把 checkpoint 标记为「恢复中」，返回当前版本号。
// 同一 checkpoint 同一时刻只允许一个恢复请求。
func (s *InMemoryCheckPointStore) BeginResume(id string, b CheckpointBinding) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[id]
	if !ok || e.data == nil || time.Since(e.updatedAt) > s.maxAge {
		return 0, ErrCheckpointNotFound
	}
	if e.binding.UserID == "" || e.binding.UserID != b.UserID || e.binding.SessionID != b.SessionID {
		log.Printf("[CheckPoint] 拒绝跨用户/跨会话恢复 - id=%s, owner=%s/%s, requester=%s/%s\n",
			id, e.binding.UserID, e.binding.SessionID, b.UserID, b.SessionID)
		return 0, ErrCheckpointForbidden
	}
	if e.binding.ConnID != "" && b.ConnID != "" && e.binding.ConnID != b.ConnID {
		log.Printf("[CheckPoint] 拒绝跨连接恢复 - id=%s, conn=%s, requester=%s\n", id, e.binding.ConnID, b.ConnID)
		return 0, ErrCheckpointForbidden
	}
	if e.inflight {
		return 0, ErrCheckpointInFlight
	}
	// 权限快照必须一致：权限收缩、无法确认（摘要为空）或修订号不同都不允许沿用旧 checkpoint，
	// 即使修订号变化只代表权限增加，也要求重新发起（不沿用旧参数扩大能力）。
	if b.ScopeDigest != "" || e.binding.ScopeDigest != "" {
		if b.ScopeDigest != e.binding.ScopeDigest {
			log.Printf("[CheckPoint] 拒绝权限快照不一致的恢复 - id=%s\n", id)
			return 0, ErrCheckpointStale
		}
	}
	if b.Revision != 0 && e.binding.Revision != 0 && b.Revision != e.binding.Revision {
		log.Printf("[CheckPoint] 拒绝修订号不一致的恢复 - id=%s, old=%d, new=%d\n", id, e.binding.Revision, b.Revision)
		return 0, ErrCheckpointStale
	}
	e.inflight = true
	return e.version, nil
}

// EndResume 结束一次恢复：若恢复期间没有产生新的中断（版本未变），checkpoint 即被消费删除；
// 若产生了新的中断（Runner 以同一 ID 重新 Set），保留新状态等待用户再次确认。
func (s *InMemoryCheckPointStore) EndResume(id string, version uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[id]
	if !ok {
		return
	}
	if e.version == version {
		delete(s.items, id)
		return
	}
	e.inflight = false
}

// Release 在一次 Run 结束后调用：未发生可恢复中断的 checkpoint（含取消时 Runner 保存的
// 不可恢复状态）直接删除，避免完整会话状态在内存中滞留 30 分钟。
func (s *InMemoryCheckPointStore) Release(id string, keep bool) {
	if keep {
		return
	}
	s.mu.Lock()
	delete(s.items, id)
	s.mu.Unlock()
}

func (s *InMemoryCheckPointStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, e := range s.items {
		if now.Sub(e.updatedAt) > s.maxAge {
			delete(s.items, id)
		}
	}
}

// evictOverflowLocked 超出容量时淘汰最旧条目（调用方持锁）
func (s *InMemoryCheckPointStore) evictOverflowLocked() {
	for len(s.items) > s.maxEntries {
		var oldestID string
		var oldest time.Time
		for id, e := range s.items {
			if oldestID == "" || e.updatedAt.Before(oldest) {
				oldestID, oldest = id, e.updatedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.items, oldestID)
	}
}
