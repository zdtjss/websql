package ai

import (
	"context"
	"errors"
	"log"
	"time"

	agent "websql/internal/ai/agent"
	system "websql/internal/app/system"

	"github.com/cloudwego/eino/schema"
)

// TestAIConfigByService 测试 AI 连接是否可用。
// 业务来自 HandleTestConfig handler。
// 返回 (message, error) - 成功时 message 为 "连接成功"。
func TestAIConfigByService(cfg *system.AIConfig) (string, error) {
	if cfg == nil {
		return "", errors.New("参数解析失败")
	}
	if cfg.Provider == "" || cfg.BaseURL == "" || cfg.Model == "" {
		return "", errors.New("请填写完整的 AI 配置（provider、baseUrl、model）")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cm, err := agent.BuildChatModel(ctx, cfg)
	if err != nil {
		log.Printf("[AI] 创建模型失败 - err=%v\n", err)
		return "", errors.New("创建模型失败：" + err.Error())
	}

	_, err = cm.Generate(ctx, []*schema.Message{{Role: schema.User, Content: "hi"}})
	if err != nil {
		log.Printf("[AI] 连接测试失败 - err=%v\n", err)
		rootErr := err
		for {
			if unwrapped := errors.Unwrap(rootErr); unwrapped != nil {
				rootErr = unwrapped
			} else {
				break
			}
		}
		return "", errors.New("连接测试失败：" + rootErr.Error())
	}
	return "连接成功", nil
}
