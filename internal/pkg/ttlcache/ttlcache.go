// Package ttlcache 提供带 TTL 过期与后台清理的泛型内存缓存。
//
// 从 internal/ai/agent/ttlcache.go 下沉而来：Agent 缓存、上传文件暂存、
// 解析结果缓存等多个域都需要同一实现，避免各自重复造一份带锁清理逻辑。
package ttlcache

import (
	"sync"
	"time"

	"websql/internal/pkg/safego"
)

// entry 是缓存中的条目
type entry[V any] struct {
	value     V
	createdAt time.Time
}

// OnEvict 是缓存条目被淘汰时的回调函数
type OnEvict[V any] func(key string, value V)

// TTLCache 是一个带 TTL 过期和后台清理的泛型缓存
type TTLCache[V any] struct {
	mu       sync.RWMutex
	items    map[string]*entry[V]
	ttl      time.Duration
	cleanSec time.Duration
	stopCh   chan struct{}
	stopped  sync.Once
	onEvict  OnEvict[V]
	// maxEntries 容量上限（0 表示不限）；Set 时按创建时间淘汰最旧条目，
	// 防止不同连接/用户维度组合无限增长（如 Agent 缓存）。
	maxEntries int
}

// New 创建一个泛型 TTL 缓存，自动启动后台清理 goroutine
func New[V any](ttl, cleanSec time.Duration, onEvict ...OnEvict[V]) *TTLCache[V] {
	c := &TTLCache[V]{
		items:    make(map[string]*entry[V]),
		ttl:      ttl,
		cleanSec: cleanSec,
		stopCh:   make(chan struct{}),
	}
	if len(onEvict) > 0 {
		c.onEvict = onEvict[0]
	}
	safego.GoWithName("ttlcache-clean", c.cleanLoop)
	return c
}

// SetMaxEntries 设置容量上限（0 表示不限）。
func (c *TTLCache[V]) SetMaxEntries(n int) {
	c.mu.Lock()
	c.maxEntries = n
	c.mu.Unlock()
}

// Get 获取缓存值，未命中或已过期返回零值和 false
func (c *TTLCache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		var zero V
		return zero, false
	}
	if time.Since(item.createdAt) > c.ttl {
		var zero V
		return zero, false
	}
	return item.value, true
}

// Set 写入缓存值；超过容量上限时按创建时间淘汰最旧条目
func (c *TTLCache[V]) Set(key string, value V) {
	var evicted *evictedEntry[V]

	c.mu.Lock()
	if c.maxEntries > 0 && len(c.items) >= c.maxEntries {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range c.items {
			if oldestKey == "" || v.createdAt.Before(oldestTime) {
				oldestKey, oldestTime = k, v.createdAt
			}
		}
		if oldestKey != "" && oldestKey != key {
			evicted = &evictedEntry[V]{key: oldestKey, value: c.items[oldestKey].value}
			delete(c.items, oldestKey)
		}
	}
	c.items[key] = &entry[V]{value: value, createdAt: time.Now()}
	c.mu.Unlock()

	// 回调移出锁：回调可能访问缓存/做 I/O，持锁调用会造成死锁与阻塞
	if evicted != nil && c.onEvict != nil {
		c.onEvict(evicted.key, evicted.value)
	}
}

// Delete 删除缓存条目，触发 onEvict 回调
func (c *TTLCache[V]) Delete(key string) {
	c.mu.Lock()
	item, ok := c.items[key]
	if ok {
		delete(c.items, key)
	}
	c.mu.Unlock()

	if ok && c.onEvict != nil {
		c.onEvict(key, item.value)
	}
}

// Len 返回当前缓存条目数（含已过期但尚未清理的条目）
func (c *TTLCache[V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// InvalidateAll 清空所有缓存条目（不触发 onEvict）
func (c *TTLCache[V]) InvalidateAll() {
	c.mu.Lock()
	c.items = make(map[string]*entry[V])
	c.mu.Unlock()
}

// Close 停止后台清理 goroutine
func (c *TTLCache[V]) Close() {
	c.stopped.Do(func() {
		close(c.stopCh)
	})
}

func (c *TTLCache[V]) cleanLoop() {
	ticker := time.NewTicker(c.cleanSec)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			expired := c.collectExpired()
			if len(expired) == 0 || c.onEvict == nil {
				continue
			}
			for _, e := range expired {
				c.onEvict(e.key, e.value) // 回调移出锁
			}
		case <-c.stopCh:
			return
		}
	}
}

// evictedEntry 被淘汰的缓存条目（用于把 onEvict 回调移出锁）
type evictedEntry[V any] struct {
	key   string
	value V
}

// collectExpired 移除过期条目并返回被淘汰条目
func (c *TTLCache[V]) collectExpired() []evictedEntry[V] {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	var out []evictedEntry[V]
	for k, v := range c.items {
		if now.Sub(v.createdAt) > c.ttl {
			out = append(out, evictedEntry[V]{key: k, value: v.value})
			delete(c.items, k)
		}
	}
	return out
}
