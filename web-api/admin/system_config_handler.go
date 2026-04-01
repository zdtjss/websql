package admin

import (
	"encoding/json"
	"fmt"
	"go-web/logutils"
	"go-web/utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SystemConfigAll 所有系统配置
type SystemConfigAll struct {
	AIProvider       string   `json:"aiProvider"`
	AIBaseURL        string   `json:"aiBaseUrl"`
	AIModel          string   `json:"aiModel"`
	AIApiKey         string   `json:"aiApiKey"`
	AITemperature    string   `json:"aiTemperature"`
	AIMaxTokens      string   `json:"aiMaxTokens"`
	AIEnableThinking string   `json:"aiEnableThinking"`
	OutterUser       string   `json:"outterUser"`
	AllowedIP        []string `json:"allowedIP"`
}

// GetSystemConfig 获取系统配置
func GetSystemConfig(c *gin.Context) {
	configType := c.Query("type")
	configs := ListSystemConfig(configType)
	utils.WriteJson(c.Writer, configs)
}

// GetAllSystemConfigHandler 获取所有系统配置
func GetAllSystemConfigHandler(c *gin.Context) {
	logutils.PrintErr(fmt.Errorf("开始获取所有系统配置"))

	cfg := &SystemConfigAll{
		AIProvider:       GetSystemConfigValue("ai.provider"),
		AIBaseURL:        GetSystemConfigValue("ai.baseUrl"),
		AIModel:          GetSystemConfigValue("ai.model"),
		AIApiKey:         GetSystemConfigValue("ai.apiKey"),
		AITemperature:    GetSystemConfigValue("ai.temperature"),
		AIMaxTokens:      GetSystemConfigValue("ai.maxTokens"),
		AIEnableThinking: GetSystemConfigValue("ai.enableThinking"),
		OutterUser:       GetSystemConfigValue("system.outterUser"),
	}

	logutils.PrintErr(fmt.Errorf("获取 AI 配置：provider=%s, baseUrl=%s, model=%s", cfg.AIProvider, cfg.AIBaseURL, cfg.AIModel))

	// 获取 IP 列表
	ipStr := GetSystemConfigValue("system.allowedIP")
	logutils.PrintErr(fmt.Errorf("获取 IP 配置：%s", ipStr))
	if ipStr != "" {
		var ips []string
		err := json.Unmarshal([]byte(ipStr), &ips)
		if err == nil {
			cfg.AllowedIP = ips
		}
	}

	// 隐藏 apiKey
	if cfg.AIApiKey != "" {
		cfg.AIApiKey = "****"
	}

	logutils.PrintErr(fmt.Errorf("返回配置数据：%+v", cfg))
	utils.WriteJson(c.Writer, cfg)
}

// SaveAllSystemConfigHandler 保存所有系统配置
func SaveAllSystemConfigHandler(c *gin.Context) {
	CheckAdminPower(c)
	cfg := &SystemConfigAll{}
	utils.UnmarshalJson(c.Request.Body, cfg)

	// 保存 AI 配置
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.provider", ConfigValue: cfg.AIProvider, ConfigType: "ai", Remark: "AI 服务提供商",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.baseUrl", ConfigValue: cfg.AIBaseURL, ConfigType: "ai", Remark: "AI 服务基础 URL",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.model", ConfigValue: cfg.AIModel, ConfigType: "ai", Remark: "AI 模型名称",
	})
	if cfg.AIApiKey != "" && cfg.AIApiKey != "****" {
		SaveSystemConfig(&SystemConfigSave{
			ConfigKey: "ai.apiKey", ConfigValue: cfg.AIApiKey, ConfigType: "ai", Remark: "AI API 密钥",
		})
	}
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.temperature", ConfigValue: cfg.AITemperature, ConfigType: "ai", Remark: "生成随机性 0.0-2.0",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.maxTokens", ConfigValue: cfg.AIMaxTokens, ConfigType: "ai", Remark: "最大生成 token 数",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.enableThinking", ConfigValue: cfg.AIEnableThinking, ConfigType: "ai", Remark: "启用思考模式",
	})

	// 保存外部用户配置
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.outterUser", ConfigValue: cfg.OutterUser, ConfigType: "system", Remark: "外部用户认证接口 URL",
	})

	// 保存 IP 列表
	ipJSON, _ := json.Marshal(cfg.AllowedIP)
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.allowedIP", ConfigValue: string(ipJSON), ConfigType: "system", Remark: "允许的 IP 地址列表",
	})

	utils.WriteJson(c.Writer, "")
}

// SaveSystemConfigHandler 保存系统配置
func SaveSystemConfigHandler(c *gin.Context) {
	CheckAdminPower(c)
	cfg := &SystemConfigSave{}
	utils.UnmarshalJson(c.Request.Body, cfg)
	SaveSystemConfig(cfg)
	utils.WriteJson(c.Writer, "")
}

// GetAIConfigHandler 获取 AI 配置
func GetAIConfigHandler(c *gin.Context) {
	cfg := GetAIConfigFromDB()
	if cfg == nil {
		utils.WriteJson(c.Writer, map[string]any{
			"provider": "",
			"baseUrl":  "",
			"model":    "",
			"apiKey":   "",
		})
		return
	}
	// 隐藏 apiKey
	if cfg.ApiKey != "" {
		cfg.ApiKey = "****"
	}
	utils.WriteJson(c.Writer, cfg)
}

// SaveAIConfigHandler 保存 AI 配置
func SaveAIConfigHandler(c *gin.Context) {
	CheckAdminPower(c)
	cfg := &AIConfig{}
	utils.UnmarshalJson(c.Request.Body, cfg)
	SaveAIConfigToDB(*cfg)
	utils.WriteJson(c.Writer, "")
}

// GetOutterUserHandler 获取外部用户认证接口配置
func GetOutterUserHandler(c *gin.Context) {
	url := GetOutterUserFromDB()
	utils.WriteJson(c.Writer, map[string]string{"outterUser": url})
}

// SaveOutterUserHandler 保存外部用户认证接口配置
func SaveOutterUserHandler(c *gin.Context) {
	CheckAdminPower(c)
	var req struct {
		OutterUser string `json:"outterUser"`
	}
	utils.UnmarshalJson(c.Request.Body, &req)
	SaveOutterUserToDB(req.OutterUser)
	utils.WriteJson(c.Writer, "")
}

// GetAllowedIPHandler 获取允许的 IP 列表
func GetAllowedIPHandler(c *gin.Context) {
	ips := GetAllowedIPFromDB()
	utils.WriteJson(c.Writer, map[string][]string{"allowedIP": ips})
}

// SaveAllowedIPHandler 保存允许的 IP 列表
func SaveAllowedIPHandler(c *gin.Context) {
	CheckAdminPower(c)
	var req struct {
		AllowedIP []string `json:"allowedIP"`
	}
	utils.UnmarshalJson(c.Request.Body, &req)
	SaveAllowedIPToDB(req.AllowedIP)
	utils.WriteJson(c.Writer, "")
}

// TestOutterUserHandler 测试外部用户认证接口
func TestOutterUserHandler(c *gin.Context) {
	url := GetOutterUserFromDB()
	if url == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "未配置外部用户认证接口"})
		return
	}

	// 发送测试请求
	resp, err := http.Get(url)
	if err != nil {
		logutils.PrintErr(err)
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "接口调用失败：" + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "接口调用成功"})
	} else {
		c.JSON(http.StatusOK, gin.H{"code": resp.StatusCode, "msg": "接口返回异常状态码"})
	}
}
