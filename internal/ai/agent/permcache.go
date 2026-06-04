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

// permAgentValue 是 Permission Agent 缓存的值类型
type permAgentValue struct {
	agent     *adk.ChatModelAgent
	agentTool tool.BaseTool
}

type PermissionAgentCache struct {
	cache   *TTLCache[permAgentValue]
	stopped sync.Once
}

var globalPermAgentCache = &PermissionAgentCache{
	cache: NewTTLCache[permAgentValue](permAgentCacheTTL, permAgentCleanSec),
}

func init() {
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
		c.cache.Close()
		log.Printf("[PermAgentCache] 已关闭\n")
	})
}

func (c *PermissionAgentCache) GetOrCreate(ctx context.Context, cfg *system.AIConfig, connID, dbType, dbSchema, userID string, schemaNames []string) (tool.BaseTool, error) {
	key := fmt.Sprintf("%s::%s::%s", connID, dbSchema, userID)

	if val, ok := c.cache.Get(key); ok {
		return val.agentTool, nil
	}

	agent, err := NewPermissionAgent(ctx, cfg, connID, dbType, dbSchema, userID, schemaNames)
	if err != nil {
		return nil, err
	}

	agentTool := adk.NewAgentTool(ctx, agent)
	c.cache.Set(key, permAgentValue{agent: agent, agentTool: agentTool})

	log.Printf("[PermAgentCache] 创建 PermissionAgent - key=%s\n", key)
	return agentTool, nil
}

// InvalidateAll 清除所有 Permission Agent 缓存（权限变更 / AI 配置变更时调用）
func (c *PermissionAgentCache) InvalidateAll() {
	count := len(c.cache.items)
	c.cache.InvalidateAll()
	if count > 0 {
		log.Printf("[PermAgentCache] 缓存已清空 - count=%d\n", count)
	}
}

// InvalidatePermissionAgentCache 是包级别的便捷函数：清除全局 Permission Agent 缓存。
//
// 用途：AI 配置变更时从外部触发失效，
// 避免 admin 改了 ai.apiKey / ai.model 后还跑在旧 ChatModel 上。
func InvalidatePermissionAgentCache() {
	globalPermAgentCache.InvalidateAll()
}
