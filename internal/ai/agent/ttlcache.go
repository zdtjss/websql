package agent

import (
	"sync"
	"time"

	"websql/internal/pkg/safego"
)

// ttlEntry 是 TTLCache 中的缓存条目
type ttlEntry[V any] struct {
	value     V
	createdAt time.Time
}

// OnEvict 是缓存条目被淘汰时的回调函数
type OnEvict[V any] func(key string, value V)

// TTLCache 是一个带 TTL 过期和后台清理的泛型缓存
type TTLCache[V any] struct {
	mu       sync.RWMutex
	items    map[string]*ttlEntry[V]
	ttl      time.Duration
	cleanSec time.Duration
	stopCh   chan struct{}
	stopped  sync.Once
	onEvict  OnEvict[V]
}

// NewTTLCache 创建一个泛型 TTL 缓存，自动启动后台清理 goroutine
func NewTTLCache[V any](ttl, cleanSec time.Duration, onEvict ...OnEvict[V]) *TTLCache[V] {
	c := &TTLCache[V]{
		items:    make(map[string]*ttlEntry[V]),
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

// Get 获取缓存值，未命中或已过期返回零值和 false
func (c *TTLCache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		var zero V
		return zero, false
	}
	if time.Since(entry.createdAt) > c.ttl {
		var zero V
		return zero, false
	}
	return entry.value, true
}

// Set 写入缓存值
func (c *TTLCache[V]) Set(key string, value V) {
	c.mu.Lock()
	c.items[key] = &ttlEntry[V]{value: value, createdAt: time.Now()}
	c.mu.Unlock()
}

// Delete 删除缓存条目，触发 onEvict 回调
func (c *TTLCache[V]) Delete(key string) {
	c.mu.Lock()
	if entry, ok := c.items[key]; ok {
		if c.onEvict != nil {
			c.onEvict(key, entry.value)
		}
		delete(c.items, key)
	}
	c.mu.Unlock()
}

// InvalidateAll 清空所有缓存条目（不触发 onEvict）
func (c *TTLCache[V]) InvalidateAll() {
	c.mu.Lock()
	c.items = make(map[string]*ttlEntry[V])
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
			c.mu.Lock()
			now := time.Now()
			for k, v := range c.items {
				if now.Sub(v.createdAt) > c.ttl {
					if c.onEvict != nil {
						c.onEvict(k, v.value)
					}
					delete(c.items, k)
				}
			}
			c.mu.Unlock()
		case <-c.stopCh:
			return
		}
	}
}
