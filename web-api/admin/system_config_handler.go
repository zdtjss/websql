package admin

import (
	"encoding/json"
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// SystemConfigAll 所有系统配置
type SystemConfigAll struct {
	AIModelList     []AIModelItem `json:"aiModelList"`
	SelectedModelId string        `json:"selectedModelId"`
	OutterUser      string        `json:"outterUser"`
	AllowedIP       []string      `json:"allowedIP"`
	RedisAddr       string        `json:"redisAddr"`
	RedisPassword   string        `json:"redisPassword"`
	RedisDB         int           `json:"redisDB"`
}

// AIModelItem 单个 AI 模型配置
type AIModelItem struct {
	Id             string  `json:"id"`
	Provider       string  `json:"provider"`
	BaseURL        string  `json:"baseUrl"`
	Model          string  `json:"model"`
	ApiKey         string  `json:"apiKey"`
	Temperature    float32 `json:"temperature"`
	MaxTokens      int     `json:"maxTokens"`
	EnableThinking bool    `json:"enableThinking"`
	IsDefault      bool    `json:"isDefault"`
}

// AIModelBrief 大模型简要信息（不含敏感配置）
type AIModelBrief struct {
	Id       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// AIModelListResponse 大模型列表接口响应
type AIModelListResponse struct {
	AIModelList     []AIModelBrief `json:"aiModelList"`
	SelectedModelId string         `json:"selectedModelId"`
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
		OutterUser:      GetSystemConfigValue("system.outterUser"),
		SelectedModelId: GetSystemConfigValue("ai.selectedModelId"),
		RedisAddr:       GetSystemConfigValue("system.redisAddr"),
		RedisPassword:   GetSystemConfigValue("system.redisPassword"),
	}
	redisDBStr := GetSystemConfigValue("system.redisDB")
	if redisDBStr != "" {
		fmt.Sscanf(redisDBStr, "%d", &cfg.RedisDB)
	}

	// 获取模型列表
	modelListJSON := GetSystemConfigValue("ai.modelList")
	if modelListJSON != "" && modelListJSON != "[]" {
		var modelList []AIModelItem
		err := json.Unmarshal([]byte(modelListJSON), &modelList)
		if err == nil {
			cfg.AIModelList = modelList
		} else {
			cfg.AIModelList = []AIModelItem{}
		}
	} else {
		// 没有模型列表时，从旧字段迁移数据到新结构
		provider := GetSystemConfigValue("ai.provider")
		baseURL := GetSystemConfigValue("ai.baseUrl")
		model := GetSystemConfigValue("ai.model")
		apiKey := GetSystemConfigValue("ai.apiKey")
		temperatureStr := GetSystemConfigValue("ai.temperature")
		maxTokensStr := GetSystemConfigValue("ai.maxTokens")
		enableThinkingStr := GetSystemConfigValue("ai.enableThinking")

		if provider != "" || baseURL != "" || model != "" {
			temperature := float32(0.7)
			if temperatureStr != "" {
				fmt.Sscanf(temperatureStr, "%f", &temperature)
			}
			maxTokens := 0
			if maxTokensStr != "" {
				fmt.Sscanf(maxTokensStr, "%d", &maxTokens)
			}
			enableThinking := enableThinkingStr == "true"

			migratedModel := AIModelItem{
				Id:             "migrated_" + fmt.Sprintf("%d", time.Now().Unix()),
				Provider:       provider,
				BaseURL:        baseURL,
				Model:          model,
				ApiKey:         apiKey,
				Temperature:    temperature,
				MaxTokens:      maxTokens,
				EnableThinking: enableThinking,
				IsDefault:      true,
			}
			cfg.AIModelList = []AIModelItem{migratedModel}
			cfg.SelectedModelId = migratedModel.Id

			// 立即保存到数据库的新结构字段
			modelListJSON, _ := json.Marshal(cfg.AIModelList)
			SaveSystemConfig(&SystemConfigSave{
				ConfigKey: "ai.modelList", ConfigValue: string(modelListJSON), ConfigType: "ai", Remark: "AI 模型配置列表",
			})
			SaveSystemConfig(&SystemConfigSave{
				ConfigKey: "ai.selectedModelId", ConfigValue: cfg.SelectedModelId, ConfigType: "ai", Remark: "当前选中的模型ID",
			})

			// 删除旧的配置字段
			if config.Mngtdb != nil {
				config.Mngtdb.Exec("DELETE FROM t_system_config WHERE config_key IN ('ai.provider', 'ai.baseUrl', 'ai.model', 'ai.apiKey', 'ai.temperature', 'ai.maxTokens', 'ai.enableThinking')")
				logutils.PrintErr(fmt.Errorf("已删除旧的 AI 配置字段"))
			}

			logutils.PrintErr(fmt.Errorf("已将旧 AI 配置迁移到模型列表"))
		} else {
			cfg.AIModelList = []AIModelItem{}
		}
	}

	logutils.PrintErr(fmt.Errorf("获取模型列表：count=%d", len(cfg.AIModelList)))

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

	logutils.PrintErr(fmt.Errorf("返回配置数据：%+v", cfg))
	utils.WriteJson(c.Writer, cfg)
}

// SaveAllSystemConfigHandler 保存所有系统配置
func SaveAllSystemConfigHandler(c *gin.Context) {
	CheckAdminPower(c)
	cfg := &SystemConfigAll{}
	utils.UnmarshalJson(c.Request.Body, cfg)

	// 保存模型列表
	modelListJSON, _ := json.Marshal(cfg.AIModelList)
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.modelList", ConfigValue: string(modelListJSON), ConfigType: "ai", Remark: "AI 模型配置列表",
	})

	// 保存当前选中的模型 ID
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.selectedModelId", ConfigValue: cfg.SelectedModelId, ConfigType: "ai", Remark: "当前选中的模型ID",
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

	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.redisAddr", ConfigValue: cfg.RedisAddr, ConfigType: "system", Remark: "Redis 地址",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.redisPassword", ConfigValue: cfg.RedisPassword, ConfigType: "system", Remark: "Redis 密码",
	})
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.redisDB", ConfigValue: fmt.Sprintf("%d", cfg.RedisDB), ConfigType: "system", Remark: "Redis 数据库编号",
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
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "接口调用失败，请检查网络和配置"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "接口调用成功"})
	} else {
		c.JSON(http.StatusOK, gin.H{"code": resp.StatusCode, "msg": "接口返回异常状态码"})
	}
}

// GetAIModelListHandler 获取大模型列表（仅返回必要信息，不含敏感配置）
func GetAIModelListHandler(c *gin.Context) {
	selectedModelId := GetSystemConfigValue("ai.selectedModelId")

	modelListJSON := GetSystemConfigValue("ai.modelList")
	if modelListJSON == "" || modelListJSON == "[]" {
		utils.WriteJson(c.Writer, &AIModelListResponse{
			AIModelList:     []AIModelBrief{},
			SelectedModelId: selectedModelId,
		})
		return
	}

	var modelList []AIModelItem
	err := json.Unmarshal([]byte(modelListJSON), &modelList)
	if err != nil {
		utils.WriteJson(c.Writer, &AIModelListResponse{
			AIModelList:     []AIModelBrief{},
			SelectedModelId: selectedModelId,
		})
		return
	}

	briefList := make([]AIModelBrief, 0, len(modelList))
	for _, m := range modelList {
		briefList = append(briefList, AIModelBrief{
			Id:       m.Id,
			Provider: m.Provider,
			Model:    m.Model,
		})
	}

	utils.WriteJson(c.Writer, &AIModelListResponse{
		AIModelList:     briefList,
		SelectedModelId: selectedModelId,
	})
}
