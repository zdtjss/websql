package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"websql/internal/store"

	"github.com/redis/go-redis/v9"
)

const (
	checkpointMaxAge      = 15 * time.Minute
	checkpointCleanSec    = 3 * time.Minute
	checkpointRedisPrefix = "websql:cp:"
)

type checkpointEntry struct {
	Data      []byte
	CreatedAt time.Time
}

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
	ticker := time.NewTicker(checkpointCleanSec)
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

type RedisCheckPointStore struct {
	client *redis.Client
}

func NewRedisCheckPointStore(client *redis.Client) *RedisCheckPointStore {
	return &RedisCheckPointStore{client: client}
}

func (s *RedisCheckPointStore) Set(ctx context.Context, key string, value []byte) error {
	redisKey := checkpointRedisPrefix + key
	err := s.client.Set(ctx, redisKey, value, checkpointMaxAge).Err()
	if err != nil {
		return fmt.Errorf("redis set checkpoint failed: %w", err)
	}
	log.Printf("[CheckPointStore:Redis] Set - key=%s, size=%d bytes\n", key, len(value))
	return nil
}

func (s *RedisCheckPointStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	redisKey := checkpointRedisPrefix + key
	val, err := s.client.Get(ctx, redisKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("redis get checkpoint failed: %w", err)
	}
	return val, true, nil
}

func newAutoCheckPointStore() interface {
	Set(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, bool, error)
} {
	if store.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pingErr := store.RDB.Ping(ctx).Err()
		cancel()
		if pingErr == nil {
			log.Printf("[CheckPointStore] 使用 Redis 存储\n")
			return NewRedisCheckPointStore(store.RDB)
		}
		log.Printf("[CheckPointStore] Redis 不可用，降级到内存存储 - err=%v\n", pingErr)
	} else {
		log.Printf("[CheckPointStore] 未配置 Redis，使用内存存储\n")
	}
	return NewInMemoryCheckPointStore()
}