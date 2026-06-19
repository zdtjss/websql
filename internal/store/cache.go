package store

import (
	"encoding/json"
	"hash/fnv"
	"sync"
	"time"
	"websql/internal/logger"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/safego"
)

const (
	numShards  = 64
	defaultTTL = 3 * time.Hour
)

type storeItem struct {
	value     any
	expiresAt time.Time
}

type shard struct {
	mu   sync.RWMutex
	data map[string]*storeItem
}

type shardedStore struct {
	shards [numShards]*shard
}

func newShardedStore() *shardedStore {
	s := &shardedStore{}
	for i := range numShards {
		s.shards[i] = &shard{data: make(map[string]*storeItem, 16)}
	}
	return s
}

func (s *shardedStore) getShard(key string) *shard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return s.shards[h.Sum32()%numShards]
}

func (s *shardedStore) add(key string, val any) {
	sh := s.getShard(key)
	sh.mu.Lock()
	sh.data[key] = &storeItem{value: val, expiresAt: time.Now().Add(defaultTTL)}
	sh.mu.Unlock()
}

func (s *shardedStore) remove(key string) {
	sh := s.getShard(key)
	sh.mu.Lock()
	delete(sh.data, key)
	sh.mu.Unlock()
}

func (s *shardedStore) get(key string, dist any) {
	if key == "" {
		return
	}
	sh := s.getShard(key)
	sh.mu.RLock()
	item, ok := sh.data[key]
	sh.mu.RUnlock()

	if !ok {
		return
	}

	if time.Now().After(item.expiresAt) {
		sh.mu.Lock()
		if item2, ok2 := sh.data[key]; ok2 && item2 == item {
			delete(sh.data, key)
		}
		sh.mu.Unlock()
		return
	}

	sh.mu.Lock()
	item.expiresAt = time.Now().Add(defaultTTL)
	sh.mu.Unlock()

	// dist 是调用方传入的指针（如 *User），直接传给 json.Unmarshal
	// 使其通过反射写入指针指向的结构体；不能传 &dist（*any），否则只改本地接口值
	json.Unmarshal(jsonutil.ToJsonString(item.value), dist)
}

func (s *shardedStore) cleanExpired() {
	now := time.Now()
	for i := range numShards {
		sh := s.shards[i]
		sh.mu.Lock()
		for k, v := range sh.data {
			if now.After(v.expiresAt) {
				delete(sh.data, k)
			}
		}
		sh.mu.Unlock()
	}
}

var localStore = newShardedStore()

func init() {
	safego.GoWithName("store-cache-cleanup", func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			localStore.cleanExpired()
		}
	})
}

func Add(key string, val any) {
	if RDB != nil {
		// Redis 中以 JSON 字符串存储，确保 Get 时能正确反序列化到调用方的结构体
		fval, mErr := json.Marshal(val)
		if mErr != nil {
			logger.PrintErrf("缓存序列化失败 key:%s", mErr, key)
			localStore.add(key, val)
			return
		}
		err := RDB.Set(ctx, key, string(fval), defaultTTL).Err()
		if err != nil {
			logger.PrintErrf("Redis 写入失败 key:%s，降级到本地缓存", err, key)
			localStore.add(key, val)
			return
		}
	}
	localStore.add(key, val)
}

func Remove(key string) {
	if RDB != nil {
		RDB.Del(ctx, key)
	}
	localStore.remove(key)
}

func Get(key string, dist any) {
	if key == "" {
		return
	}
	if RDB != nil {
		val, err := RDB.Get(ctx, key).Result()
		if err == nil {
			RDB.Expire(ctx, key, defaultTTL)
			// dist 是调用方传入的指针（如 *User），直接传给 json.Unmarshal
			// 使其通过反射写入指针指向的结构体
			if err2 := json.Unmarshal([]byte(val), dist); err2 != nil && val != "" {
				// JSON 解析失败，val 可能是历史遗留的原始字符串，尝试直接赋值给 *string 目标
				if strPtr, ok := dist.(*string); ok {
					*strPtr = val
				}
			}
			return
		}
	}
	localStore.get(key, dist)
}
