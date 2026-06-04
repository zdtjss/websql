package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	system "websql/internal/app/system"

	"github.com/cloudwego/eino/adk"
)

const (
	agentCacheTTL      = 10 * time.Minute
	agentCacheCleanSec = 2 * time.Minute
)

type AgentFactory struct {
	cache    *TTLCache[*SQLAgent]
	sessions *SessionStore
}

var globalAgentFactory *AgentFactory
var factoryOnce sync.Once

func GetAgentFactory() *AgentFactory {
	factoryOnce.Do(func() {
		sessions, err := NewSessionStore()
		if err != nil {
			log.Printf("[AgentFactory] 创建 SessionStore 失败 - err=%v\n", err)
			sessions, _ = NewSessionStore()
		}
		globalAgentFactory = &AgentFactory{
			cache:    NewTTLCache[*SQLAgent](agentCacheTTL, agentCacheCleanSec),
			sessions: sessions,
		}
	})
	return globalAgentFactory
}

// Close 关闭 AgentFactory，停止后台清理 goroutine（防止 goroutine 泄漏）
func (f *AgentFactory) Close() {
	f.cache.Close()
	f.sessions.Close()
	log.Printf("[AgentFactory] 已关闭\n")
}

func (f *AgentFactory) GetOrCreate(ctx context.Context, cfg *system.AIConfig, connID, dbType, dbSchema, dbVersion string, schemas []SchemaRef, scope *PermissionScope, auditCtx *ExecAuditCtx) (*SQLAgent, error) {
	key := agentCacheKey(connID, dbSchema, scope.UserID, schemas)

	if agent, ok := f.cache.Get(key); ok {
		return agent, nil
	}

	newAgent, err := NewSQLAgent(ctx, cfg, connID, dbType, dbSchema, dbVersion, schemas, f.sessions, scope, auditCtx)
	if err != nil {
		return nil, err
	}

	f.cache.Set(key, newAgent)
	log.Printf("[AgentFactory] 创建新 Agent - key=%s\n", key)
	return newAgent, nil
}

func (f *AgentFactory) InvalidateAll() {
	f.cache.InvalidateAll()
	log.Printf("[AgentFactory] 缓存已全部清空\n")
}

func (f *AgentFactory) Invalidate(connID, dbSchema, userID string, schemas []SchemaRef) {
	key := agentCacheKey(connID, dbSchema, userID, schemas)
	f.cache.Delete(key)
}

func (f *AgentFactory) GetSessions() *SessionStore {
	return f.sessions
}

func agentCacheKey(connID, dbSchema, userID string, schemas []SchemaRef) string {
	key := fmt.Sprintf("%s::%s::%s", connID, dbSchema, userID)
	for _, s := range schemas {
		key += fmt.Sprintf("::%s/%s", s.ConnID, s.Schema)
	}
	return key
}

func init() {
	adk.SetLanguage(adk.LanguageChinese)
}
