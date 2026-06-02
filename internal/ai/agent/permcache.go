package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	admin "websql/internal/app/admin"
	system "websql/internal/app/system"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

const (
	permAgentCacheTTL = 5 * time.Minute
	permAgentCleanSec = 2 * time.Minute
)

type permAgentEntry struct {
	agent     *adk.ChatModelAgent
	agentTool tool.BaseTool
	createdAt time.Time
	cfgHash   string
}

type PermissionAgentCache struct {
	mu      sync.RWMutex
	cache   map[string]*permAgentEntry
	stopCh  chan struct{}
	stopped sync.Once
}

var globalPermAgentCache = &PermissionAgentCache{
	cache:  make(map[string]*permAgentEntry),
	stopCh: make(chan struct{}),
}

func init() {
	go globalPermAgentCache.cleanLoop()
	// 注册权限变更回调，当角色权限变更时清除 Permission Agent 缓存
	admin.OnPermissionChanged(func() {
		globalPermAgentCache.InvalidateAll()
	})
}

func GetPermissionAgentCache() *PermissionAgentCache {
	return globalPermAgentCache
}

// Close 关闭 PermissionAgentCache，停止后台清理 goroutine
func (c *PermissionAgentCache) Close() {
	c.stopped.Do(func() {
		close(c.stopCh)
		log.Printf("[PermAgentCache] 已关闭\n")
	})
}

func (c *PermissionAgentCache) GetOrCreate(ctx context.Context, cfg *system.AIConfig, connID, dbType, dbSchema, userID string, schemaNames []string) (tool.BaseTool, error) {
	key := fmt.Sprintf("%s::%s::%s", connID, dbSchema, userID)
	cfgHash := aiConfigHash(cfg)

	c.mu.RLock()
	entry, ok := c.cache[key]
	c.mu.RUnlock()

	if ok && entry.cfgHash == cfgHash && time.Since(entry.createdAt) < permAgentCacheTTL {
		return entry.agentTool, nil
	}

	agent, err := NewPermissionAgent(ctx, cfg, connID, dbType, dbSchema, userID, schemaNames)
	if err != nil {
		return nil, err
	}

	agentTool := adk.NewAgentTool(ctx, agent)

	c.mu.Lock()
	c.cache[key] = &permAgentEntry{
		agent:     agent,
		agentTool: agentTool,
		createdAt: time.Now(),
		cfgHash:   cfgHash,
	}
	c.mu.Unlock()

	log.Printf("[PermAgentCache] 创建 PermissionAgent - key=%s\n", key)
	return agentTool, nil
}

func (c *PermissionAgentCache) cleanLoop() {
	ticker := time.NewTicker(permAgentCleanSec)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for k, v := range c.cache {
				if now.Sub(v.createdAt) > permAgentCacheTTL {
					delete(c.cache, k)
					log.Printf("[PermAgentCache] 清理过期 - key=%s\n", k)
				}
			}
			c.mu.Unlock()
		case <-c.stopCh:
			return
		}
	}
}

// InvalidateAll 清除所有 Permission Agent 缓存（权限变更 / AI 配置变更时调用）
func (c *PermissionAgentCache) InvalidateAll() {
	c.mu.Lock()
	count := len(c.cache)
	c.cache = make(map[string]*permAgentEntry)
	c.mu.Unlock()
	if count > 0 {
		log.Printf("[PermAgentCache] 缓存已清空 - count=%d\n", count)
	}
}

// InvalidatePermissionAgentCache 是包级别的便捷函数：清除全局 Permission Agent 缓存。
//
// 用途：AI 配置变更时（EINO_DEEP_ANALYSIS §11.1）从外部触发失效，
// 避免 admin 改了 ai.apiKey / ai.model 后还跑在旧 ChatModel 上。
func InvalidatePermissionAgentCache() {
	globalPermAgentCache.InvalidateAll()
}
