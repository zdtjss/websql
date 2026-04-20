// checkpoint_store.go — 内存 CheckPointStore 实现
//
// 实现 Eino ADK 的 CheckPointStore 接口，用于 Runner 在 Interrupt 时
// 持久化 Agent 运行状态。当前使用内存存储，带自动过期清理。
// 生产环境可替换为 Redis 实现。
package agentv2

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const checkpointMaxAge = 15 * time.Minute

type checkpointEntry struct {
	Data      []byte
	CreatedAt time.Time
}

// InMemoryCheckPointStore 内存 CheckPointStore
type InMemoryCheckPointStore struct {
	mu    sync.RWMutex
	store map[string]*checkpointEntry
}

func NewInMemoryCheckPointStore() *InMemoryCheckPointStore {
	s := &InMemoryCheckPointStore{
		store: make(map[string]*checkpointEntry),
	}
	go s.cleanLoop()
	return s
}

func (s *InMemoryCheckPointStore) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store[key] = &checkpointEntry{Data: value, CreatedAt: time.Now()}
	log.Printf("[CheckPointStore] Set - key=%s, size=%d bytes\n", key, len(value))
	return nil
}

func (s *InMemoryCheckPointStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.store[key]
	if !ok {
		return nil, false, nil
	}
	if time.Since(entry.CreatedAt) > checkpointMaxAge {
		return nil, false, fmt.Errorf("checkpoint 已过期（key=%s）", key)
	}
	return entry.Data, true, nil
}

func (s *InMemoryCheckPointStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, key)
}

func (s *InMemoryCheckPointStore) cleanLoop() {
	ticker := time.NewTicker(3 * time.Minute)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for k, v := range s.store {
			if now.Sub(v.CreatedAt) > checkpointMaxAge {
				delete(s.store, k)
				log.Printf("[CheckPointStore] 清理过期 - key=%s\n", k)
			}
		}
		s.mu.Unlock()
	}
}
