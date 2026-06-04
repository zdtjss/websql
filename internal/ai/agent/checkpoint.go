package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"websql/internal/store"

	"github.com/redis/go-redis/v9"
)

const (
	checkpointMaxAge      = 15 * time.Minute
	checkpointCleanSec    = 3 * time.Minute
	checkpointRedisPrefix = "websql:cp:"
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
	log.Printf("[CheckPointStore] Set - key=%s, size=%d bytes\n", key, len(value))
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
