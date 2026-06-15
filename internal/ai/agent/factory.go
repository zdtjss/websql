package agent

import (
	"context"
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
