package agent

import (
	"context"
	"crypto/sha256"
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

type agentCacheEntry struct {
	agent     *SQLAgent
	createdAt time.Time
	cfgHash   string
}

type AgentFactory struct {
	mu       sync.RWMutex
	cache    map[string]*agentCacheEntry
	sessions *SessionStore
	stopCh   chan struct{}
	stopped  sync.Once
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
			stopCh:   make(chan struct{}),
		}
		go globalAgentFactory.cleanLoop()
	})
	return globalAgentFactory
}

// Close 关闭 AgentFactory，停止后台清理 goroutine（防止 goroutine 泄漏）
func (f *AgentFactory) Close() {
	f.stopped.Do(func() {
		close(f.stopCh)
		f.sessions.Close()
		log.Printf("[AgentFactory] 已关闭\n")
	})
}

func (f *AgentFactory) GetOrCreate(ctx context.Context, cfg *system.AIConfig, connID, dbType, dbSchema, dbVersion string, schemas []SchemaRef, scope *PermissionScope, auditCtx *ExecAuditCtx) (*SQLAgent, error) {
	key := agentCacheKey(connID, dbSchema, scope.UserID, schemas)
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

func (f *AgentFactory) Invalidate(connID, dbSchema, userID string, schemas []SchemaRef) {
	key := agentCacheKey(connID, dbSchema, userID, schemas)
	f.mu.Lock()
	delete(f.cache, key)
	f.mu.Unlock()
}

func (f *AgentFactory) GetSessions() *SessionStore {
	return f.sessions
}

func (f *AgentFactory) cleanLoop() {
	ticker := time.NewTicker(agentCacheCleanSec)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f.mu.Lock()
			now := time.Now()
			for k, v := range f.cache {
				if now.Sub(v.createdAt) > agentCacheTTL {
					delete(f.cache, k)
					log.Printf("[AgentFactory] 清理过期 - key=%s\n", k)
				}
			}
			f.mu.Unlock()
		case <-f.stopCh:
			return
		}
	}
}

func agentCacheKey(connID, dbSchema, userID string, schemas []SchemaRef) string {
	key := fmt.Sprintf("%s::%s::%s", connID, dbSchema, userID)
	for _, s := range schemas {
		key += fmt.Sprintf("::%s/%s", s.ConnID, s.Schema)
	}
	return key
}

func aiConfigHash(cfg *system.AIConfig) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%.2f|%d|%t", cfg.Provider, cfg.BaseURL, cfg.Model, cfg.ApiKey, cfg.Temperature, cfg.MaxContextTokens, cfg.EnableThinking)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func init() {
	adk.SetLanguage(adk.LanguageChinese)
}
