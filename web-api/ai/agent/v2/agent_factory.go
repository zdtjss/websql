package agentv2

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"sync"
	"time"

	admin "go-web/web-api/admin"

	"github.com/cloudwego/eino/adk"
)

const (
	agentCacheTTL      = 10 * time.Minute
	agentCacheCleanSec = 2 * time.Minute
)

type agentCacheEntry struct {
	agent     *SQLAgent
	createdAt time.Time
	cfgHash   string
}

type AgentFactory struct {
	mu      sync.RWMutex
	cache   map[string]*agentCacheEntry
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
			cache:    make(map[string]*agentCacheEntry),
			sessions: sessions,
		}
		go globalAgentFactory.cleanLoop()
	})
	return globalAgentFactory
}

func (f *AgentFactory) GetOrCreate(ctx context.Context, cfg *admin.AIConfig, connID, dbType, dbSchema, dbVersion string, schemas []SchemaRef, scope *PermissionScope, auditCtx *ExecAuditCtx) (*SQLAgent, error) {
	key := agentCacheKey(connID, dbSchema, scope.UserID)
	cfgHash := aiConfigHash(cfg)

	f.mu.RLock()
	entry, ok := f.cache[key]
	f.mu.RUnlock()

	if ok && entry.cfgHash == cfgHash && time.Since(entry.createdAt) < agentCacheTTL {
		return entry.agent, nil
	}

	agent, err := NewSQLAgent(ctx, cfg, connID, dbType, dbSchema, dbVersion, schemas, f.sessions, scope, auditCtx)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.cache[key] = &agentCacheEntry{
		agent:     agent,
		createdAt: time.Now(),
		cfgHash:   cfgHash,
	}
	f.mu.Unlock()

	log.Printf("[AgentFactory] 创建新 Agent - key=%s, cfgHash=%s\n", key, cfgHash[:8])
	return agent, nil
}

func (f *AgentFactory) InvalidateAll() {
	f.mu.Lock()
	f.cache = make(map[string]*agentCacheEntry)
	f.mu.Unlock()
	log.Printf("[AgentFactory] 缓存已全部清空\n")
}

func (f *AgentFactory) Invalidate(connID, dbSchema, userID string) {
	key := agentCacheKey(connID, dbSchema, userID)
	f.mu.Lock()
	delete(f.cache, key)
	f.mu.Unlock()
}

func (f *AgentFactory) GetSessions() *SessionStore {
	return f.sessions
}

func (f *AgentFactory) cleanLoop() {
	ticker := time.NewTicker(agentCacheCleanSec)
	for range ticker.C {
		f.mu.Lock()
		now := time.Now()
		for k, v := range f.cache {
			if now.Sub(v.createdAt) > agentCacheTTL {
				delete(f.cache, k)
				log.Printf("[AgentFactory] 清理过期 - key=%s\n", k)
			}
		}
		f.mu.Unlock()
	}
}

func agentCacheKey(connID, dbSchema, userID string) string {
	return fmt.Sprintf("%s::%s::%s", connID, dbSchema, userID)
}

func aiConfigHash(cfg *admin.AIConfig) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%.2f|%d|%t", cfg.Provider, cfg.BaseURL, cfg.Model, cfg.ApiKey, cfg.Temperature, cfg.MaxTokens, cfg.EnableThinking)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func init() {
	adk.SetLanguage(adk.LanguageChinese)
}
