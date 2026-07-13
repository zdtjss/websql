package agent

import (
	"context"
	"time"
)

const (
	checkpointMaxAge   = 15 * time.Minute
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
