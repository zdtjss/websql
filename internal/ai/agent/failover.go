// failover.go — Model Failover 配置，在主模型不可用时自动切换到备用模型。
//
// 与 ModelRetryConfig 协同：
//   - retry 是"同模型重试"（处理瞬时错误）
//   - failover 是"换模型重试"（处理模型持续不可用）
//   - 执行顺序：failover 外层 → retry 内层 → 实际模型调用
//   - retry 耗尽后返回 *RetryExhaustedError，failover 的 ShouldFailover 据此决定是否切换
//
// 备用模型来源：系统配置中的 AI 模型列表（ai.modelList），排除当前选中的主模型。
// 按列表顺序作为 failover 候选，每个备用模型各尝试一次。
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	system "websql/internal/app/system"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// buildFailoverConfig 从系统配置构建 ModelFailoverConfig。
// 返回 nil 表示无可用备用模型（不启用 failover）。
func buildFailoverConfig(ctx context.Context, primaryCfg *system.AIConfig) *adk.ModelFailoverConfig[*schema.Message] {
	fallbacks := buildFallbackModels(ctx, primaryCfg)
	if len(fallbacks) == 0 {
		return nil
	}

	log.Printf("[Failover] 启用模型故障转移，备用模型数=%d\n", len(fallbacks))

	return &adk.ModelFailoverConfig[*schema.Message]{
		MaxRetries: uint(len(fallbacks)),

		ShouldFailover: func(ctx context.Context, outputMsg *schema.Message, outputErr error) bool {
			if ctx.Err() != nil {
				return false
			}

			// 与 ModelRetryConfig 协同：outputErr 可能是 *RetryExhaustedError
			// 提取原始错误判断是否值得切换模型
			origErr := outputErr
			var retryErr *adk.RetryExhaustedError
			if errors.As(outputErr, &retryErr) {
				origErr = retryErr.LastErr
			}

			if origErr == nil {
				return false
			}

			errStr := origErr.Error()

			// 不可 failover 的错误：认证失败、内容安全（换模型也没用）
			if strings.Contains(errStr, "401") ||
				strings.Contains(errStr, "403") ||
				strings.Contains(errStr, "invalid_api_key") ||
				strings.Contains(errStr, "content_filter") ||
				strings.Contains(errStr, "content_policy") ||
				strings.Contains(errStr, "safety") {
				log.Printf("[Failover] 不可 failover 错误（认证/内容安全）- err=%s\n", errStr)
				return false
			}

			// 可 failover 的错误：服务端错误、速率限制、网络问题
			// 注意：retry 已经处理了瞬时错误，到这里通常是模型持续不可用
			shouldSwitch := strings.Contains(errStr, "429") ||
				strings.Contains(errStr, "500") ||
				strings.Contains(errStr, "502") ||
				strings.Contains(errStr, "503") ||
				strings.Contains(errStr, "504") ||
				strings.Contains(errStr, "timeout") ||
				strings.Contains(errStr, "connection") ||
				strings.Contains(errStr, "rate limit") ||
				strings.Contains(errStr, "too many requests") ||
				strings.Contains(errStr, "EOF")

			if shouldSwitch {
				log.Printf("[Failover] 触发故障转移 - err=%s\n", errStr)
				return true
			}

			return false
		},

		GetFailoverModel: func(ctx context.Context, failoverCtx *adk.FailoverContext[*schema.Message]) (
			model.BaseModel[*schema.Message], []*schema.Message, error) {

			attempt := failoverCtx.FailoverAttempt // 从 1 开始
			idx := int(attempt) - 1
			if idx >= len(fallbacks) {
				return nil, nil, fmt.Errorf("no more fallback models (attempt=%d)", attempt)
			}

			selected := fallbacks[idx]
			log.Printf("[Failover] 切换到备用模型 - attempt=%d, model=%T\n", attempt, selected)

			// 返回 nil 表示使用原始输入消息
			return selected, nil, nil
		},
	}
}

// buildFallbackModels 从系统配置加载备用模型，排除当前选中的主模型。
// 返回的模型经过与主模型相同的包装链（toolCallIndexFixer + logging）。
func buildFallbackModels(ctx context.Context, primaryCfg *system.AIConfig) []model.BaseModel[*schema.Message] {
	modelList := system.GetAIModelList()
	if len(modelList) == 0 {
		return nil
	}

	selectedId := system.GetSelectedModelId()

	// 通过模型配置匹配主模型来排除，而非仅靠 ID
	// （主模型 cfg 可能是通过 GetSelectedModelConfig 获取的，其字段与 AIModelItem 一致）
	var fallbacks []model.BaseModel[*schema.Message]
	for _, m := range modelList {
		// 跳过当前选中的模型
		if m.Id == selectedId {
			continue
		}
		// 跳过与主模型配置完全相同的模型（同 BaseURL + Model）
		if m.BaseURL == primaryCfg.BaseURL && m.Model == primaryCfg.Model {
			continue
		}

		subCfg := &system.AIConfig{
			Provider:         m.Provider,
			BaseURL:          m.BaseURL,
			Model:            m.Model,
			ApiKey:           m.ApiKey,
			Temperature:      m.Temperature,
			MaxContextTokens: m.MaxContextTokens,
			EnableThinking:   m.EnableThinking,
		}

		// 复用 BuildChatModel 创建备用模型（含 toolCallIndexFixer + logging 包装）
		fbModel, err := BuildChatModel(ctx, subCfg)
		if err != nil {
			log.Printf("[Failover] 创建备用模型失败 - model=%s, err=%v\n", m.Model, err)
			continue
		}
		fallbacks = append(fallbacks, fbModel)
	}

	return fallbacks
}
