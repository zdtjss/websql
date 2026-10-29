package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	admin "websql/internal/app/admin"
	appperm "websql/internal/app/permission"
	system "websql/internal/app/system"
	"websql/internal/pkg/ttlcache"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

const (
	permAgentCacheTTL = 5 * time.Minute
	permAgentCleanSec = 2 * time.Minute
	// maxCachedPermAgents 权限审核 Agent 缓存上限（按 连接+schema+用户 组合）
	maxCachedPermAgents = 256
)

// permAgentValue 是 Permission Agent 缓存的值类型
type permAgentValue struct {
	agent     *adk.ChatModelAgent
	agentTool tool.BaseTool
}

type PermissionAgentCache struct {
	cache   *ttlcache.TTLCache[permAgentValue]
	stopped sync.Once
}

var globalPermAgentCache = func() *PermissionAgentCache {
	cache := ttlcache.New[permAgentValue](permAgentCacheTTL, permAgentCleanSec)
	cache.SetMaxEntries(maxCachedPermAgents)
	return &PermissionAgentCache{cache: cache}
}()

func init() {
	// 注册权限变更回调。
	//
	// 不再全量清空 SQLAgent / PermissionAgent 缓存：
	//   - SQLAgent 缓存 key 已包含 ScopeDigest（含权限修订号），权限变化后的新请求
	//     自然命中新 key，旧实例在 TTL 内不会被复用；
	//   - PermissionAgent 的权限视图由工具实时查询（get_user_permissions），
	//     不在实例内烘焙权限快照。
	// 全量重建会让一次角色变更触发所有在线用户重新建 Agent，形成风暴。
	admin.OnPermissionChanged(func() {
		globalPermDecisionCache.Clear()
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
	// 缓存 key 必须包含 schemaNames：权限 Agent 的表结构/权限查询范围由 schemaNames 决定，
	// 同一 (conn, dbSchema, user) 在不同 schema 范围下会得到不同权限视图，
	// 若不带该维度会复用错误配置导致越权判定（原实现遗漏此维度）。
	key := fmt.Sprintf("%s::%s::%s", connID, dbSchema, userID)
	for _, s := range schemaNames {
		key += "::" + s
	}

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
	count := c.cache.Len()
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
	globalPermDecisionCache.Clear()
}

// ──────────────────────────────────────────────
// 权限决策缓存
// ──────────────────────────────────────────────
//
// 权限 Agent 的结论只依赖（用户、连接、权限修订号、工具名、SQL 文本）。
// 同一轮对话中重复执行相同 SQL（重试、分页、导出复用）不应重复调用 LLM。
const (
	permDecisionTTL        = 5 * time.Minute
	permDecisionMaxEntries = 2048
)

type permDecisionEntry struct {
	decision  *PermDecisionOutput
	expiresAt time.Time
}

type permDecisionCache struct {
	mu      sync.Mutex
	entries map[string]permDecisionEntry
}

var globalPermDecisionCache = &permDecisionCache{entries: make(map[string]permDecisionEntry)}

func permDecisionCacheKey(userID, connID string, revision uint64, toolName, sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return fmt.Sprintf("%s|%s|%d|%s|%s", userID, connID, revision, toolName, hex.EncodeToString(sum[:16]))
}

func (c *permDecisionCache) Get(key string) (*PermDecisionOutput, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	cp := *e.decision
	cp.DeniedTables = append([]string(nil), e.decision.DeniedTables...)
	cp.DeniedColumns = append([]string(nil), e.decision.DeniedColumns...)
	return &cp, true
}

func (c *permDecisionCache) Set(key string, decision *PermDecisionOutput) {
	if c == nil || key == "" || decision == nil {
		return
	}
	cp := *decision
	cp.DeniedTables = append([]string(nil), decision.DeniedTables...)
	cp.DeniedColumns = append([]string(nil), decision.DeniedColumns...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= permDecisionMaxEntries {
		// 简单淘汰：清空过期项；仍满则整体清空（权限决策可重算）。
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= permDecisionMaxEntries {
			c.entries = make(map[string]permDecisionEntry, permDecisionMaxEntries)
		}
	}
	c.entries[key] = permDecisionEntry{decision: &cp, expiresAt: time.Now().Add(permDecisionTTL)}
}

func (c *permDecisionCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = make(map[string]permDecisionEntry)
	c.mu.Unlock()
}

// permAgentMode 返回权限 Agent 运行模式：
//   - "on"：始终调用（旧行为，保留为逃生阀）
//   - "off"：从不调用，完全依赖程序化校验
//   - 其它/未设置：fallback（默认）——程序化可判定时不调用，无法判定时兜底调用
func permAgentMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_PERM_AGENT"))) {
	case "on", "always", "true", "1":
		return "on"
	case "off", "never", "false", "0":
		return "off"
	case "shadow":
		return "shadow"
	default:
		return "fallback"
	}
}

// shadowPermAgent 在 shadow 模式下调用权限 Agent 并记录其结论，但不改变执行结果；
// 执行仍由程序化校验兜底。用于灰度对比 LLM 判定与程序化判定的差异。
func (m *PermissionMiddleware) shadowPermAgent(ctx context.Context, toolName, sql string) {
	decision, err := m.checkSQLPermissionViaAgent(ctx, sql, toolName)
	if err != nil {
		recordAgentMetric("ai_perm_agent_shadow_total", toolName, "error")
		return
	}
	if decision.Allowed {
		recordAgentMetric("ai_perm_agent_shadow_total", toolName, "allow")
		return
	}
	recordAgentMetric("ai_perm_agent_shadow_total", toolName, "deny")
}

// needsPermAgentFallback 判断程序化校验是否无法判定，需要 LLM 权限 Agent 兜底。
func needsPermAgentFallback(scope *PermissionScope, toolName, sql string, tables []string) bool {
	if scope == nil || scope.SkipChecks() {
		return false
	}
	if len(tables) > 0 {
		return false
	}
	if toolName == "query_data" && appperm.IsVerifiedNoResourceQuery(sql) {
		return false
	}
	return true
}
