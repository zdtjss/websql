package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	system "websql/internal/app/system"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	agentCacheTTL      = 10 * time.Minute
	agentCacheCleanSec = 2 * time.Minute
	// maxCachedAgents Agent 缓存容量上限：按 (连接, schema, 用户, 模型/权限指纹) 组合缓存，
	// 组合数随用户与连接数增长，需设上限防止内存无限增长；淘汰仅移出缓存，不影响进行中的 Run。
	maxCachedAgents = 128
)

type AgentFactory struct {
	buildMu  sync.Mutex
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
		agentCache := NewTTLCache[*SQLAgent](agentCacheTTL, agentCacheCleanSec)
		agentCache.SetMaxEntries(maxCachedAgents)
		globalAgentFactory = &AgentFactory{
			cache:    agentCache,
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
	// 缓存 key 纳入「模型配置指纹 + 权限范围指纹」：
	//   - 用户切换模型 / 管理员修改模型参数后立即生效（旧 key 只含连接与用户，最长 10 分钟仍用旧模型）
	//   - 管理员调整用户权限后，新的权限范围会构建新的 Agent（工具与提示词中的权限描述随之更新）
	key := agentCacheKey(connID, dbSchema, scope.UserID, schemas) + "::" + configFingerprint(cfg, scope)

	if agent, ok := f.cache.Get(key); ok {
		return agent, nil
	}

	// 同一 key 的并发未命中只构建一次（构建包含模型客户端、权限智能体等较重的初始化）
	f.buildMu.Lock()
	defer f.buildMu.Unlock()
	if agent, ok := f.cache.Get(key); ok {
		return agent, nil
	}

	// 构建期使用独立 context：Agent 会被缓存并被后续请求共享，
	// 不能让组件持有首个请求的 ctx（请求结束即取消）
	buildCtx := context.WithoutCancel(ctx)
	newAgent, err := NewSQLAgent(buildCtx, cfg, connID, dbType, dbSchema, dbVersion, schemas, f.sessions, scope, auditCtx)
	if err != nil {
		return nil, err
	}

	f.cache.Set(key, newAgent)
	log.Printf("[AgentFactory] 创建新 Agent - key=%s\n", key)
	return newAgent, nil
}

// configFingerprint 计算模型配置与权限范围的短指纹（sha256 前 12 位，不泄露 apiKey 原文）
func configFingerprint(cfg *system.AIConfig, scope *PermissionScope) string {
	h := sha256.New()
	if cfg != nil {
		fmt.Fprintf(h, "%+v", *cfg)
	}
	if scope != nil {
		// fmt 对 map 按 key 排序输出，结果稳定
		fmt.Fprintf(h, "|%v|%v|%v|%v|%v|%v|%v", scope.IsRemote, scope.HasFullConnAccess, scope.AllSchemasFull,
			scope.AllowModify, scope.FullAccessSchemas, scope.AllowedTables, scope.AllowedColumns)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
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

	// 注册自定义消息合并函数，修复模型返回的 ToolCall Index 冲突问题
	// 部分模型（如 minimax-m3）在流式返回多个 ToolCall 时，可能为不同 ID 的 ToolCall
	// 分配相同的 Index，导致 eino 框架的 concatToolCalls 报错：
	// "cannot concat ToolCalls with different tool id"
	compose.RegisterStreamChunkConcatFunc(lenientConcatMessages)
}

// lenientConcatMessages 在调用 schema.ConcatMessages 之前修复 ToolCall Index 冲突，
// 确保不同 ID 的 ToolCall 拥有不同的 Index 值。
func lenientConcatMessages(msgs []*schema.Message) (*schema.Message, error) {
	fixToolCallIndices(msgs)
	return schema.ConcatMessages(msgs)
}

// fixToolCallIndices 修复消息中 ToolCall 的 Index 冲突。
// 当不同 ID 的 ToolCall 共享同一个 Index 时，为冲突的 ToolCall 分配新的唯一 Index。
func fixToolCallIndices(msgs []*schema.Message) {
	// 找出当前最大的 Index 值
	maxIndex := -1
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if tc.Index != nil && *tc.Index > maxIndex {
				maxIndex = *tc.Index
			}
		}
	}

	// 记录每个 Index 已关联的 ID，发现冲突时重新分配 Index
	indexToID := make(map[int]string)
	for _, msg := range msgs {
		for i := range msg.ToolCalls {
			tc := &msg.ToolCalls[i]
			if tc.Index == nil || tc.ID == "" {
				continue
			}
			idx := *tc.Index
			if existingID, ok := indexToID[idx]; ok && tc.ID != existingID {
				// 同一 Index 下出现不同 ID，分配新 Index
				maxIndex++
				newIdx := maxIndex
				tc.Index = &newIdx
				indexToID[newIdx] = tc.ID
			} else {
				indexToID[idx] = tc.ID
			}
		}
	}
}

// ──────────────────────────────────────────────
// ChatModel 流输出层面的 ToolCall Index 修复包装器
// ──────────────────────────────────────────────

// toolCallIndexFixerModel 包装 ToolCallingChatModel，在流输出层面修复 ToolCall Index 冲突。
//
// 问题根因：部分模型（如 minimax-m3）在流式返回多个 ToolCall 时，可能为不同 ID 的 ToolCall
// 分配相同的 Index。eino 框架的 concatToolCalls 按 Index 分组并校验 ID 一致性，冲突时报错：
// "cannot concat ToolCalls with different tool id"
//
// 虽然 RegisterStreamChunkConcatFunc 可以替换 compose.concatStreamReader 的合并逻辑，
// 但 ADK 框架内部有多处直接调用 schema.ConcatMessages（绕过注册函数），因此仅在流输出层面
// 修复 Index 才能覆盖所有代码路径。
type toolCallIndexFixerModel struct {
	model.ToolCallingChatModel
}

func (m *toolCallIndexFixerModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	msg, err := m.ToolCallingChatModel.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	fixToolCallIndices([]*schema.Message{msg})
	return msg, nil
}

func (m *toolCallIndexFixerModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, err := m.ToolCallingChatModel.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}

	// 跨 chunk 追踪 ID 与 Index 的映射关系
	idToIndex := make(map[string]int)
	indexToID := make(map[int]string)
	maxIndex := -1

	fixed := schema.StreamReaderWithConvert(sr, func(msg *schema.Message) (*schema.Message, error) {
		if len(msg.ToolCalls) == 0 {
			return msg, nil
		}

		for i := range msg.ToolCalls {
			tc := &msg.ToolCalls[i]
			if tc.Index == nil || tc.ID == "" {
				continue
			}
			idx := *tc.Index

			if existingID, ok := indexToID[idx]; ok && tc.ID != existingID {
				// 同一 Index 下出现不同 ID
				if newIdx, ok := idToIndex[tc.ID]; ok {
					// 此 ID 之前已出现过，复用之前分配的 Index
					tc.Index = &newIdx
				} else {
					// 新 ID 与已有 Index 冲突，分配新 Index
					maxIndex++
					newIdx := maxIndex
					tc.Index = &newIdx
					idToIndex[tc.ID] = newIdx
					indexToID[newIdx] = tc.ID
				}
			} else if !ok {
				// 首次出现的 Index-ID 组合
				indexToID[idx] = tc.ID
				idToIndex[tc.ID] = idx
				if idx > maxIndex {
					maxIndex = idx
				}
			}
		}

		return msg, nil
	})

	return fixed, nil
}

func (m *toolCallIndexFixerModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := m.ToolCallingChatModel.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &toolCallIndexFixerModel{ToolCallingChatModel: inner}, nil
}
