package ai

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	agent "websql/internal/ai/agent"
	admin "websql/internal/app/admin"
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
	// 安全：修改全局模型地址/密钥会让所有用户的提示词、表结构与查询结果发往新地址，
	// 必须仅管理员可操作（与 system.SaveAIConfigHandler 保持一致）
	if !admin.CheckAdminPower(c) {
		return
	}
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
	// AI 配置包含明文 apiKey，仅管理员可读取（与 SaveAIConfigHandler 保持一致）
	if !admin.CheckAdminPower(c) {
		return
	}
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
	// 安全：连接测试会由服务端向任意 baseUrl 发起请求（可被用作内网探测），仅管理员可用
	if !admin.CheckAdminPower(c) {
		return
	}
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

	if cfg.Provider != "openai" && cfg.Provider != "ollama" {
		response.WriteErr(c, http.StatusOK, 500, "不支持的 AI 提供商")
		return
	}
	if u, perr := url.Parse(cfg.BaseURL); perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		response.WriteErr(c, http.StatusOK, 500, "baseUrl 必须是 http:// 或 https:// 开头的有效地址")
		return
	}

	cm, err := agent.BuildChatModel(ctx, &cfg)
	if err != nil {
		log.Printf("[AI] 创建模型失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusOK, 500, "创建模型失败，请检查 AI 配置")
		return
	}

	// 发送一条简单消息测试连接
	_, err = cm.Generate(ctx, []*schema.Message{{Role: schema.User, Content: "hi"}})
	if err != nil {
		log.Printf("[AI] 连接测试失败 - err=%v\n", err)
		// 脱敏：错误原文可能包含内网地址、请求路径、密钥前缀等，不回显原文，
		// 仅按错误类型给出可自助排查的提示（完整错误见服务端日志）
		response.WriteErr(c, http.StatusOK, 500, classifyTestConfigError(err))
		return
	}
	response.WriteOK(c, "连接成功")
}

// classifyTestConfigError 把连接测试错误归类为可自助排查的提示文案（不回显原始错误）。
func classifyTestConfigError(err error) string {
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "401") || strings.Contains(lower, "invalid_api_key") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "authentication"):
		return "连接测试失败：认证失败，请检查 API Key"
	case strings.Contains(lower, "403"):
		return "连接测试失败：请求被拒绝（403）"
	case strings.Contains(lower, "404"):
		return "连接测试失败：接口不存在（404），请检查 baseUrl 与模型名"
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate limit"):
		return "连接测试失败：触发限流（429），请稍后重试"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "连接测试失败：连接超时，请检查 baseUrl 是否可达"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "dial tcp"):
		return "连接测试失败：无法连接，请检查服务地址与网络"
	}
	return "连接测试失败：请检查 provider/baseUrl/model/API Key 与网络（详情见服务端日志）"
}
