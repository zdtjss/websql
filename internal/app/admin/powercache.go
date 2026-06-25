package admin

import (
	"sync"
	"time"

	"websql/internal/pkg/safego"
)

// powerCacheTTL 权限详情缓存的有效期。
// 权限变更时通过 InvalidateAllAuthCache / InvalidatePowerCacheByUserId 主动失效，
// TTL 作为兜底保护，防止极端情况下缓存不一致。
const powerCacheTTL = 5 * time.Minute

// powerCacheEntry 单个用户的权限详情与角色缓存条目。
// details 和 roles 各自独立加载，避免相互不必要地触发查询。
type powerCacheEntry struct {
	details       []*PowerDetail
	detailsLoaded bool
	roles         []*Role
	rolesLoaded   bool
	expiresAt     time.Time
}

// powerDetailsCache 缓存 userId -> 权限详情/角色，避免每次权限检查都查询数据库。
//
// 设计说明：
//   - authCache 仅缓存 UserPower（connId 列表），不包含 PowerDetail（schema/table/column 规则）。
//   - 此前每次 CheckSchemaAccess / CheckTableAccess / CheckAnalysisPermission 都会调用
//     findUserPowerDetails 查询 t_power 表，是权限检查的主要性能瓶颈。
//   - 本缓存与 authCache 同步失效：InvalidateAllAuthCache 会清空本缓存。
type powerDetailsCache struct {
	mu      sync.RWMutex
	entries map[string]*powerCacheEntry
}

var powerCache = &powerDetailsCache{
	entries: make(map[string]*powerCacheEntry, 64),
}

func init() {
	// 后台定期清理过期条目，防止内存泄漏
	safego.GoWithName("powercache-cleanup", func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			powerCache.mu.Lock()
			now := time.Now()
			for k, v := range powerCache.entries {
				if now.After(v.expiresAt) {
					delete(powerCache.entries, k)
				}
			}
			powerCache.mu.Unlock()
		}
	})
}

// getDetails 从缓存读取权限详情，未命中或已过期返回 false。
func (c *powerDetailsCache) getDetails(userId string) ([]*PowerDetail, bool) {
	c.mu.RLock()
	entry, ok := c.entries[userId]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) || !entry.detailsLoaded {
		return nil, false
	}
	return entry.details, true
}

// getRoles 从缓存读取角色列表，未命中或已过期返回 false。
func (c *powerDetailsCache) getRoles(userId string) ([]*Role, bool) {
	c.mu.RLock()
	entry, ok := c.entries[userId]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) || !entry.rolesLoaded {
		return nil, false
	}
	return entry.roles, true
}

// setDetails 写入权限详情，若条目不存在则创建并刷新 TTL。
func (c *powerDetailsCache) setDetails(userId string, details []*PowerDetail) {
	c.mu.Lock()
	entry := c.entries[userId]
	if entry == nil {
		entry = &powerCacheEntry{expiresAt: time.Now().Add(powerCacheTTL)}
		c.entries[userId] = entry
	} else {
		entry.expiresAt = time.Now().Add(powerCacheTTL)
	}
	entry.details = details
	entry.detailsLoaded = true
	c.mu.Unlock()
}

// setRoles 写入角色列表，若条目不存在则创建并刷新 TTL。
func (c *powerDetailsCache) setRoles(userId string, roles []*Role) {
	c.mu.Lock()
	entry := c.entries[userId]
	if entry == nil {
		entry = &powerCacheEntry{expiresAt: time.Now().Add(powerCacheTTL)}
		c.entries[userId] = entry
	} else {
		entry.expiresAt = time.Now().Add(powerCacheTTL)
	}
	entry.roles = roles
	entry.rolesLoaded = true
	c.mu.Unlock()
}

// invalidateUser 清除指定用户的所有缓存条目。
func (c *powerDetailsCache) invalidateUser(userId string) {
	c.mu.Lock()
	delete(c.entries, userId)
	c.mu.Unlock()
}

// invalidateAll 清除全部缓存条目。
func (c *powerDetailsCache) invalidateAll() {
	c.mu.Lock()
	c.entries = make(map[string]*powerCacheEntry, 64)
	c.mu.Unlock()
}

// invalidatePowerCacheAll 清空全部权限详情缓存（供 InvalidateAllAuthCache 内部调用）。
func invalidatePowerCacheAll() {
	powerCache.invalidateAll()
}
