package ai

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	agent "websql/internal/ai/agent"
	system "websql/internal/app/system"
	"websql/internal/pkg/response"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
)

func SaveAIConfig(cfg system.AIConfig) error {
	system.SaveAIConfigToDB(cfg)
	// 配置变更后立即失效 Agent 与 Permission 缓存，避免 admin 改了 AI 配置
	// 但前端还是用旧的 ChatModel / Agent / Permission Agent 跑业务。
	// 失效是异步的：当前正在跑的 Run 不受影响（它们持有自己的 ChatModel 引用），
	// 但下一次 GetOrCreate 会用新配置重建。
	if factory := agent.GetAgentFactory(); factory != nil {
		factory.InvalidateAll()
	}
	agent.InvalidatePermissionAgentCache()
	return nil
}

func GetAIConfig() (*system.AIConfig, error) {
	cfg := system.GetAIConfigFromDB()
	if cfg == nil {
		return nil, nil
	}
	return cfg, nil
}

func HandleSaveConfig(c *gin.Context) {
	var cfg system.AIConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		log.Printf("[AI] 参数解析失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusOK, 500, "参数解析失败")
		return
	}
	if err := SaveAIConfig(cfg); err != nil {
		log.Printf("[AI] 保存配置失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusOK, 500, "保存配置失败")
		return
	}
	response.WriteOK(c, "保存成功")
}

func HandleGetConfig(c *gin.Context) {
	cfg, err := GetAIConfig()
	if err != nil {
		log.Printf("[AI] 获取配置失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusOK, 500, "获取配置失败")
		return
	}
	if cfg == nil {
		response.WriteOK(c, nil)
		return
	}
	response.WriteOK(c, cfg)
}

// HandleTestConfig 使用 Eino 组件测试 AI 连接，与 Agent 实际运行路径一致
func HandleTestConfig(c *gin.Context) {
	var cfg system.AIConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		log.Printf("[AI] 参数解析失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusOK, 500, "参数解析失败")
		return
	}
	if cfg.Provider == "" || cfg.BaseURL == "" || cfg.Model == "" {
		response.WriteErr(c, http.StatusOK, 500, "请填写完整的 AI 配置（provider、baseUrl、model）")
		return
	}

	// 使用与 Agent 相同的 BuildChatModel 创建模型，确保测试路径与实际运行路径一致
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	cm, err := agent.BuildChatModel(ctx, &cfg)
	if err != nil {
		log.Printf("[AI] 创建模型失败 - err=%v\n", err)
		response.WriteErrf(c, http.StatusOK, 500, "创建模型失败：%v", err)
		return
	}

	// 发送一条简单消息测试连接
	_, err = cm.Generate(ctx, []*schema.Message{{Role: schema.User, Content: "hi"}})
	if err != nil {
		log.Printf("[AI] 连接测试失败 - err=%v\n", err)
		// 提取根错误信息
		rootErr := err
		for {
			if unwrapped := errors.Unwrap(rootErr); unwrapped != nil {
				rootErr = unwrapped
			} else {
				break
			}
		}
		response.WriteErrf(c, http.StatusOK, 500, "连接测试失败：%v", rootErr)
		return
	}
	response.WriteOK(c, "连接成功")
}
