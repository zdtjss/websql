package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

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
	mu    sync.RWMutex
	cache map[string]*permAgentEntry
}

var globalPermAgentCache = &PermissionAgentCache{
	cache: make(map[string]*permAgentEntry),
}

func init() {
	go globalPermAgentCache.cleanLoop()
}

func GetPermissionAgentCache() *PermissionAgentCache {
	return globalPermAgentCache
}

func (c *PermissionAgentCache) GetOrCreate(ctx context.Context, cfg *system.AIConfig, connID, dbType, dbSchema, userID string) (tool.BaseTool, error) {
	key := fmt.Sprintf("%s::%s::%s", connID, dbSchema, userID)
	cfgHash := aiConfigHash(cfg)

	c.mu.RLock()
	entry, ok := c.cache[key]
	c.mu.RUnlock()

	if ok && entry.cfgHash == cfgHash && time.Since(entry.createdAt) < permAgentCacheTTL {
		return entry.agentTool, nil
	}

	agent, err := NewPermissionAgent(ctx, cfg, connID, dbType, dbSchema, userID)
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
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, v := range c.cache {
			if now.Sub(v.createdAt) > permAgentCacheTTL {
				delete(c.cache, k)
				log.Printf("[PermAgentCache] 清理过期 - key=%s\n", k)
			}
		}
		c.mu.Unlock()
	}
}