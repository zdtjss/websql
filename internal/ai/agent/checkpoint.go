package agent

import (
	"context"
	"time"
)

const (
	// checkpointMaxAge 中断状态（危险 SQL 待确认）的有效期。
	// 与 SSE 请求超时（30 分钟）对齐：用户在确认弹窗停留过久后仍可恢复执行，
	// 超时后 ResumeStream 会失败并提示重新发起操作。
	checkpointMaxAge   = 30 * time.Minute
	checkpointCleanSec = 3 * time.Minute
)

type InMemoryCheckPointStore struct {
	cache *TTLCache[[]byte]
}

func NewInMemoryCheckPointStore() *InMemoryCheckPointStore {
	return &InMemoryCheckPointStore{
		cache: NewTTLCache[[]byte](checkpointMaxAge, checkpointCleanSec),
	}
}

func (s *InMemoryCheckPointStore) Set(_ context.Context, key string, value []byte) error {
	s.cache.Set(key, value)
	return nil
}

func (s *InMemoryCheckPointStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	val, ok := s.cache.Get(key)
	if !ok {
		return nil, false, nil
	}
	return val, true, nil
}

func (s *InMemoryCheckPointStore) Delete(key string) {
	s.cache.Delete(key)
}
