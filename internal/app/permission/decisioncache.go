package permission

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"websql/internal/app/admin"
	"websql/internal/pkg/safego"
)

// decisionCacheTTL 权限决策缓存的有效期，可按需调整。
// 权限变更时通过 OnPermissionChanged 回调主动失效，TTL 仅作兜底保护。
const decisionCacheTTL = 5 * time.Minute

// decisionCacheEntry 权限决策缓存条目。
// level/allowedColumns 仅在表级访问决策（GetTableColumnAccess）时填充，
// 用于缓存完整的访问级别结果，避免重复解析权限规则。
type decisionCacheEntry struct {
	allow        bool                 // schema/table 级是否允许访问
	level        ColumnAccessLevel    // 表级访问级别：full/column/none
	allowedCols  map[string]bool      // 列级权限时允许的列集合（小写）
	decidedAt    time.Time            // 决策时间，用于 TTL 判断
}

// decisionCache 缓存权限决策结果，key 格式为 "userId:connId:schema:table"。
// table 为空时表示 schema 级决策。
//
// 设计说明：
//   - admin 包的 powerCache 缓存了 PowerDetail 原始数据（消除 DB 查询）；
//   - 本缓存进一步缓存"解析后的决策结果"，消除重复的 ResolveRolePermissions 计算；
//   - 两者均通过 admin.OnPermissionChanged 回调在权限变更时失效。
type decisionCache struct {
	mu      sync.RWMutex
	entries map[string]*decisionCacheEntry
}

var globalDecisionCache = &decisionCache{
	entries: make(map[string]*decisionCacheEntry, 256),
}

func init() {
	// 注册权限变更回调：角色/权限变更时清空全部决策缓存
	admin.OnPermissionChanged(func() {
		InvalidateAllPermissionCache()
	})

	// 后台定期清理过期条目，防止内存泄漏
	safego.GoWithName("permdecision-cache-cleanup", func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			globalDecisionCache.mu.Lock()
			now := time.Now()
			for k, v := range globalDecisionCache.entries {
				if now.Sub(v.decidedAt) > decisionCacheTTL {
					delete(globalDecisionCache.entries, k)
				}
			}
			globalDecisionCache.mu.Unlock()
		}
	})
}

// decisionKey 构造决策缓存 key：userId:connId:schema:table
// table 为空时表示 schema 级决策。
func decisionKey(userId, connId, schema, table string) string {
	return fmt.Sprintf("%s:%s:%s:%s", userId, connId, schema, table)
}

// getDecision 读取决策缓存，未命中或已过期返回 false。
func (c *decisionCache) getDecision(userId, connId, schema, table string) (*decisionCacheEntry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[decisionKey(userId, connId, schema, table)]
	c.mu.RUnlock()
	if !ok || time.Since(entry.decidedAt) > decisionCacheTTL {
		return nil, false
	}
	return entry, true
}

// setDecision 写入决策缓存。
func (c *decisionCache) setDecision(userId, connId, schema, table string, entry *decisionCacheEntry) {
	entry.decidedAt = time.Now()
	c.mu.Lock()
	c.entries[decisionKey(userId, connId, schema, table)] = entry
	c.mu.Unlock()
}

// invalidateUser 清除指定用户的所有决策缓存条目。
func (c *decisionCache) invalidateUser(userId string) {
	prefix := userId + ":"
	c.mu.Lock()
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

// invalidateAll 清除全部决策缓存条目。
func (c *decisionCache) invalidateAll() {
	c.mu.Lock()
	c.entries = make(map[string]*decisionCacheEntry, 256)
	c.mu.Unlock()
}

// InvalidateAllPermissionCache 清空全部权限决策缓存。
// 通常由 admin.OnPermissionChanged 回调自动触发，无需手动调用。
func InvalidateAllPermissionCache() {
	count := len(globalDecisionCache.entries)
	globalDecisionCache.invalidateAll()
	if count > 0 {
		log.Printf("[PermDecisionCache] 全量清空决策缓存 - count=%d\n", count)
	}
}
