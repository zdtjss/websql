package system

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"websql/internal/app/admin"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/dberr"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

type SystemConfig struct {
	Id          string     `json:"id" db:"id"`
	ConfigKey   string     `json:"configKey" db:"config_key"`
	ConfigValue string     `json:"configValue" db:"config_value"`
	ConfigType  string     `json:"configType" db:"config_type"`
	Remark      string     `json:"remark" db:"remark"`
	CreateTime  *time.Time `json:"createTime" db:"create_time"`
	UpdateTime  *time.Time `json:"updateTime" db:"update_time"`
}

type SystemConfigSave struct {
	Id          string `json:"id"`
	ConfigKey   string `json:"configKey"`
	ConfigValue string `json:"configValue"`
	ConfigType  string `json:"configType"`
	Remark      string `json:"remark"`
}

type SystemConfigAll struct {
	AIModelList     []AIModelItem `json:"aiModelList"`
	SelectedModelId string        `json:"selectedModelId"`
	OutterUser      string        `json:"outterUser"`
	AllowedIP       []string      `json:"allowedIP"`
	RedisAddr       string        `json:"redisAddr"`
	RedisPassword   string        `json:"redisPassword"`
	RedisDB         int           `json:"redisDB"`
	DefaultHomepage string        `json:"defaultHomepage"`
}

type AIModelItem struct {
	Id               string  `json:"id"`
	Provider         string  `json:"provider"`
	BaseURL          string  `json:"baseUrl"`
	Model            string  `json:"model"`
	ApiKey           string  `json:"apiKey"`
	Temperature      float32 `json:"temperature"`
	MaxContextTokens int     `json:"maxContextTokens"`
	EnableThinking   bool    `json:"enableThinking"`
	IsDefault        bool    `json:"isDefault"`
}

type AIModelBrief struct {
	Id       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type AIModelListResponse struct {
	AIModelList     []AIModelBrief `json:"aiModelList"`
	SelectedModelId string         `json:"selectedModelId"`
}

type AIConfig struct {
	Provider         string  `json:"provider"`
	BaseURL          string  `json:"baseUrl"`
	Model            string  `json:"model"`
	ApiKey           string  `json:"apiKey"`
	Temperature      float32 `json:"temperature"`
	MaxContextTokens int     `json:"maxContextTokens"`
	EnableThinking   bool    `json:"enableThinking"`
}

func GetSystemConfigByKey(key string) *SystemConfig {
	cfg := &SystemConfig{}
	err := database.Mngtdb.Get(cfg, "select * from t_system_config where config_key = ?", key)
	if err != nil {
		if dberr.IsNoRows(err) {
			return nil
		}
		logger.PrintErr(fmt.Errorf("查询系统配置失败: %s, %v", key, err))
		return nil
	}
	return cfg
}

func GetSystemConfigValue(key string) string {
	cfg := GetSystemConfigByKey(key)
	if cfg == nil {
		return ""
	}
	return cfg.ConfigValue
}

func SaveSystemConfig(cfg *SystemConfigSave) {
	existCfg := GetSystemConfigByKey(cfg.ConfigKey)

	if existCfg == nil {
		cfg.Id = idgen.RandomStr()
		_, err := database.Mngtdb.Exec("insert into t_system_config (id, config_key, config_value, config_type, remark) values (?, ?, ?, ?, ?)",
			cfg.Id, cfg.ConfigKey, cfg.ConfigValue, cfg.ConfigType, cfg.Remark)
		if err != nil {
			if dberr.IsUniqueConstraint(err) {
				existCfg = GetSystemConfigByKey(cfg.ConfigKey)
				if existCfg != nil {
					_, err := database.Mngtdb.Exec("update t_system_config set config_value = ?, config_type = ?, remark = ?, update_time = ? where id = ?",
						cfg.ConfigValue, cfg.ConfigType, cfg.Remark, time.Now(), existCfg.Id)
					if err != nil {
						log.Printf("[SaveSystemConfig] 更新配置失败: %v", err)
						return
					}
					return
				}
			}
			log.Printf("[SaveSystemConfig] 插入配置失败: %v", err)
			return
		}
	} else {
		_, err := database.Mngtdb.Exec("update t_system_config set config_value = ?, config_type = ?, remark = ?, update_time = ? where id = ?",
			cfg.ConfigValue, cfg.ConfigType, cfg.Remark, time.Now(), existCfg.Id)
		if err != nil {
			log.Printf("[SaveSystemConfig] 更新配置失败: %v", err)
			return
		}
	}
}

func ListSystemConfig(configType string) []*SystemConfig {
	var configs []*SystemConfig
	var err error
	if configType == "" {
		err = database.Mngtdb.Select(&configs, "select * from t_system_config order by config_type, config_key")
	} else {
		err = database.Mngtdb.Select(&configs, "select * from t_system_config where config_type = ? order by config_key", configType)
	}
	if err != nil {
		log.Printf("[ListSystemConfig] 查询系统配置列表失败: %v", err)
		return nil
	}
	return configs
}

func GetAIConfigFromDB() *AIConfig {
	provider := GetSystemConfigValue("ai.provider")
	baseUrl := GetSystemConfigValue("ai.baseUrl")
	model := GetSystemConfigValue("ai.model")
	apiKey := GetSystemConfigValue("ai.apiKey")
	temperatureStr := GetSystemConfigValue("ai.temperature")
	enableThinkingStr := GetSystemConfigValue("ai.enableThinking")

	if provider == "" && baseUrl == "" && model == "" && apiKey == "" {
		return nil
	}

	var temperature float32 = 0.7
	if temperatureStr != "" {
		if v, err := strconv.ParseFloat(temperatureStr, 32); err == nil {
			temperature = float32(v)
		}
	}
	return &AIConfig{
		Provider:       provider,
		BaseURL:        baseUrl,
		Model:          model,
		ApiKey:         apiKey,
		Temperature:    temperature,
		EnableThinking: enableThinkingStr == "true",
	}
}

func SaveAIConfigToDB(cfg AIConfig) {
	configs := []SystemConfigSave{
		{ConfigKey: "ai.provider", ConfigValue: cfg.Provider, ConfigType: "ai", Remark: "AI 服务提供商"},
		{ConfigKey: "ai.baseUrl", ConfigValue: cfg.BaseURL, ConfigType: "ai", Remark: "AI 服务基础 URL"},
		{ConfigKey: "ai.model", ConfigValue: cfg.Model, ConfigType: "ai", Remark: "AI 模型名称"},
		{ConfigKey: "ai.temperature", ConfigValue: fmt.Sprintf("%.1f", cfg.Temperature), ConfigType: "ai", Remark: "生成随机性 0.0-2.0"},
		{ConfigKey: "ai.enableThinking", ConfigValue: fmt.Sprintf("%t", cfg.EnableThinking), ConfigType: "ai", Remark: "启用思考模式"},
	}

	if cfg.ApiKey != "" {
		configs = append(configs, SystemConfigSave{
			ConfigKey: "ai.apiKey", ConfigValue: cfg.ApiKey, ConfigType: "ai", Remark: "AI API 密钥",
		})
	}

	for _, c := range configs {
		SaveSystemConfig(&c)
	}
}

func GetOutterUserFromDB() string {
	return GetSystemConfigValue("system.outterUser")
}

func SaveOutterUserToDB(url string) {
	cfg := GetSystemConfigByKey("system.outterUser")
	saveCfg := &SystemConfigSave{
		ConfigKey:   "system.outterUser",
		ConfigValue: url,
		ConfigType:  "system",
		Remark:      "外部用户认证接口 URL",
	}
	if cfg != nil {
		saveCfg.Id = cfg.Id
	}
	SaveSystemConfig(saveCfg)
}

func GetAllowedIPFromDB() []string {
	ipJSON := GetSystemConfigValue("system.allowedIP")
	if ipJSON == "" {
		return []string{"[::1]", "127.0.0.1"}
	}
	var ips []string
	err := json.Unmarshal([]byte(ipJSON), &ips)
	if err != nil {
		return []string{"[::1]", "127.0.0.1"}
	}
	return ips
}

func SaveAllowedIPToDB(ips []string) {
	ipJSON, err := json.Marshal(ips)
	if err != nil {
		log.Printf("[SaveAllowedIPToDB] 序列化IP列表失败: %v", err)
		return
	}

	cfg := GetSystemConfigByKey("system.allowedIP")
	saveCfg := &SystemConfigSave{
		ConfigKey:   "system.allowedIP",
		ConfigValue: string(ipJSON),
		ConfigType:  "system",
		Remark:      "允许的 IP 地址列表（JSON 格式）",
	}
	if cfg != nil {
		saveCfg.Id = cfg.Id
	}
	SaveSystemConfig(saveCfg)
}

func LoadSystemConfigToMemory() {
	config.Cfg.OutterUser = GetOutterUserFromDB()
	config.Cfg.AllowedIP = GetAllowedIPFromDB()

	selectedId := GetSystemConfigValue("ai.selectedModelId")
	if selectedId != "" {
		modelListJSON := GetSystemConfigValue("ai.modelList")
		if modelListJSON != "" && modelListJSON != "[]" {
			var modelList []AIModelItem
			err := json.Unmarshal([]byte(modelListJSON), &modelList)
			if err == nil {
				for _, m := range modelList {
					if m.Id == selectedId {
						config.Cfg.AI.Provider = m.Provider
						config.Cfg.AI.BaseURL = m.BaseURL
						config.Cfg.AI.Model = m.Model
						config.Cfg.AI.ApiKey = m.ApiKey
						return
					}
				}
			}
		}
	}

	modelListJSON := GetSystemConfigValue("ai.modelList")
	if modelListJSON != "" && modelListJSON != "[]" {
		var modelList []AIModelItem
		err := json.Unmarshal([]byte(modelListJSON), &modelList)
		if err == nil && len(modelList) > 0 {
			config.Cfg.AI.Provider = modelList[0].Provider
			config.Cfg.AI.BaseURL = modelList[0].BaseURL
			config.Cfg.AI.Model = modelList[0].Model
			config.Cfg.AI.ApiKey = modelList[0].ApiKey
		}
	}
}

func GetSelectedModelConfig(modelId string) *AIConfig {
	targetId := modelId
	if targetId == "" {
		targetId = GetSystemConfigValue("ai.selectedModelId")
	}

	if targetId != "" {
		modelListJSON := GetSystemConfigValue("ai.modelList")
		if modelListJSON != "" && modelListJSON != "[]" {
			var modelList []AIModelItem
			err := json.Unmarshal([]byte(modelListJSON), &modelList)
			if err == nil {
				for _, m := range modelList {
					if m.Id == targetId {
						return &AIConfig{
							Provider:         m.Provider,
							BaseURL:          m.BaseURL,
							Model:            m.Model,
							ApiKey:           m.ApiKey,
							Temperature:      m.Temperature,
							MaxContextTokens: m.MaxContextTokens,
							EnableThinking:   m.EnableThinking,
						}
					}
				}
			}
		}
	}

	modelListJSON := GetSystemConfigValue("ai.modelList")
	if modelListJSON != "" && modelListJSON != "[]" {
		var modelList []AIModelItem
		err := json.Unmarshal([]byte(modelListJSON), &modelList)
		if err == nil && len(modelList) > 0 {
			m := modelList[0]
			return &AIConfig{
				Provider:         m.Provider,
				BaseURL:          m.BaseURL,
				Model:            m.Model,
				ApiKey:           m.ApiKey,
				Temperature:      m.Temperature,
				MaxContextTokens: m.MaxContextTokens,
				EnableThinking:   m.EnableThinking,
			}
		}
	}

	return nil
}

func GetSystemConfig(c *gin.Context) {
	configType := c.Query("type")
	configs := ListSystemConfig(configType)
	response.WriteOK(c, configs)
}

func GetAllSystemConfigHandler(c *gin.Context) {
	logger.PrintErr(errors.New("开始获取所有系统配置"))

	cfg := &SystemConfigAll{
		OutterUser:      GetSystemConfigValue("system.outterUser"),
		SelectedModelId: GetSystemConfigValue("ai.selectedModelId"),
		RedisAddr:       GetSystemConfigValue("system.redisAddr"),
		RedisPassword:   GetSystemConfigValue("system.redisPassword"),
		DefaultHomepage: GetSystemConfigValue("system.defaultHomepage"),
	}
	redisDBStr := GetSystemConfigValue("system.redisDB")
	if redisDBStr != "" {
		fmt.Sscanf(redisDBStr, "%d", &cfg.RedisDB)
	}

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
		provider := GetSystemConfigValue("ai.provider")
		baseURL := GetSystemConfigValue("ai.baseUrl")
		model := GetSystemConfigValue("ai.model")
		apiKey := GetSystemConfigValue("ai.apiKey")
		temperatureStr := GetSystemConfigValue("ai.temperature")
		enableThinkingStr := GetSystemConfigValue("ai.enableThinking")

		if provider != "" || baseURL != "" || model != "" {
			temperature := float32(0.7)
			if temperatureStr != "" {
				fmt.Sscanf(temperatureStr, "%f", &temperature)
			}
			enableThinking := enableThinkingStr == "true"

			migratedModel := AIModelItem{
				Id:             "migrated_" + fmt.Sprintf("%d", time.Now().Unix()),
				Provider:       provider,
				BaseURL:        baseURL,
				Model:          model,
				ApiKey:         apiKey,
				Temperature:    temperature,
				EnableThinking: enableThinking,
				IsDefault:      true,
			}
			cfg.AIModelList = []AIModelItem{migratedModel}
			cfg.SelectedModelId = migratedModel.Id

			modelListJSON, _ := json.Marshal(cfg.AIModelList)
			SaveSystemConfig(&SystemConfigSave{
				ConfigKey: "ai.modelList", ConfigValue: string(modelListJSON), ConfigType: "ai", Remark: "AI 模型配置列表",
			})
			SaveSystemConfig(&SystemConfigSave{
				ConfigKey: "ai.selectedModelId", ConfigValue: cfg.SelectedModelId, ConfigType: "ai", Remark: "当前选中的模型ID",
			})

			if database.Mngtdb != nil {
				database.Mngtdb.Exec("DELETE FROM t_system_config WHERE config_key IN ('ai.provider', 'ai.baseUrl', 'ai.model', 'ai.apiKey', 'ai.temperature', 'ai.enableThinking')")
				logger.PrintErr(errors.New("已删除旧的 AI 配置字段"))
			}

			logger.PrintErr(errors.New("已将旧 AI 配置迁移到模型列表"))
		} else {
			cfg.AIModelList = []AIModelItem{}
		}
	}

	logger.PrintErr(fmt.Errorf("获取模型列表：count=%d", len(cfg.AIModelList)))

	ipStr := GetSystemConfigValue("system.allowedIP")
	logger.PrintErr(fmt.Errorf("获取 IP 配置：%s", ipStr))
	if ipStr != "" {
		var ips []string
		err := json.Unmarshal([]byte(ipStr), &ips)
		if err == nil {
			cfg.AllowedIP = ips
		}
	}

	logger.PrintErr(fmt.Errorf("返回配置数据：%+v", cfg))
	response.WriteOK(c, cfg)
}

func SaveAllSystemConfigHandler(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	cfg := &SystemConfigAll{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, cfg); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}

	modelListJSON, _ := json.Marshal(cfg.AIModelList)
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.modelList", ConfigValue: string(modelListJSON), ConfigType: "ai", Remark: "AI 模型配置列表",
	})

	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.selectedModelId", ConfigValue: cfg.SelectedModelId, ConfigType: "ai", Remark: "当前选中的模型ID",
	})

	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.outterUser", ConfigValue: cfg.OutterUser, ConfigType: "system", Remark: "外部用户认证接口 URL",
	})

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

	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "system.defaultHomepage", ConfigValue: cfg.DefaultHomepage, ConfigType: "system", Remark: "默认首页",
	})

	response.WriteOK(c, "")
}

func SaveSystemConfigHandler(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	cfg := &SystemConfigSave{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, cfg); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	SaveSystemConfig(cfg)
	response.WriteOK(c, "")
}

func GetAIConfigHandler(c *gin.Context) {
	cfg := GetAIConfigFromDB()
	if cfg == nil {
		response.WriteOK(c, map[string]any{
			"provider": "",
			"baseUrl":  "",
			"model":    "",
			"apiKey":   "",
		})
		return
	}
	response.WriteOK(c, cfg)
}

func SaveAIConfigHandler(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	cfg := &AIConfig{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, cfg); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	SaveAIConfigToDB(*cfg)
	response.WriteOK(c, "")
}

func GetOutterUserHandler(c *gin.Context) {
	url := GetOutterUserFromDB()
	response.WriteOK(c, map[string]string{"outterUser": url})
}

func SaveOutterUserHandler(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	var req struct {
		OutterUser string `json:"outterUser"`
	}
	if err := jsonutil.UnmarshalJson(c.Request.Body, &req); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	SaveOutterUserToDB(req.OutterUser)
	response.WriteOK(c, "")
}

func GetAllowedIPHandler(c *gin.Context) {
	ips := GetAllowedIPFromDB()
	response.WriteOK(c, map[string][]string{"allowedIP": ips})
}

func SaveAllowedIPHandler(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	var req struct {
		AllowedIP []string `json:"allowedIP"`
	}
	if err := jsonutil.UnmarshalJson(c.Request.Body, &req); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	SaveAllowedIPToDB(req.AllowedIP)
	response.WriteOK(c, "")
}

func TestOutterUserHandler(c *gin.Context) {
	url := GetOutterUserFromDB()
	if url == "" {
		response.WriteErr(c, http.StatusOK, 400, "未配置外部用户认证接口")
		return
	}

	resp, err := http.Get(url)
	if err != nil {
		logger.PrintErr(err)
		response.WriteErr(c, http.StatusOK, 500, "接口调用失败，请检查网络和配置")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		response.WriteOK(c, "接口调用成功")
	} else {
		response.WriteErr(c, http.StatusOK, resp.StatusCode, "接口返回异常状态码")
	}
}

func getModelListFromDB() []AIModelItem {
	modelListJSON := GetSystemConfigValue("ai.modelList")
	if modelListJSON == "" || modelListJSON == "[]" {
		return []AIModelItem{}
	}
	var modelList []AIModelItem
	err := json.Unmarshal([]byte(modelListJSON), &modelList)
	if err != nil {
		return []AIModelItem{}
	}
	return modelList
}

func saveModelListToDB(modelList []AIModelItem) {
	modelListJSON, _ := json.Marshal(modelList)
	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.modelList", ConfigValue: string(modelListJSON), ConfigType: "ai", Remark: "AI 模型配置列表",
	})
}

func SaveAIModelHandler(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	var model AIModelItem
	if err := jsonutil.UnmarshalJson(c.Request.Body, &model); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}

	modelList := getModelListFromDB()

	if model.Id == "" {
		model.Id = "model_" + idgen.RandomStr()
		modelList = append(modelList, model)
	} else {
		found := false
		for i, m := range modelList {
			if m.Id == model.Id {
				modelList[i] = model
				found = true
				break
			}
		}
		if !found {
			modelList = append(modelList, model)
		}
	}

	saveModelListToDB(modelList)
	response.WriteOK(c, model)
}

func DeleteAIModelHandler(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	var req struct {
		Id string `json:"id"`
	}
	if err := jsonutil.UnmarshalJson(c.Request.Body, &req); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	if req.Id == "" {
		response.WriteErr(c, http.StatusOK, 400, "模型 ID 不能为空")
		return
	}

	modelList := getModelListFromDB()
	newList := make([]AIModelItem, 0, len(modelList))
	for _, m := range modelList {
		if m.Id != req.Id {
			newList = append(newList, m)
		}
	}
	saveModelListToDB(newList)

	// 如果删除的是当前选中的模型，自动选中第一个
	selectedId := GetSystemConfigValue("ai.selectedModelId")
	if selectedId == req.Id && len(newList) > 0 {
		SaveSystemConfig(&SystemConfigSave{
			ConfigKey: "ai.selectedModelId", ConfigValue: newList[0].Id, ConfigType: "ai", Remark: "当前选中的模型ID",
		})
		LoadSystemConfigToMemory()
	} else if selectedId == req.Id {
		SaveSystemConfig(&SystemConfigSave{
			ConfigKey: "ai.selectedModelId", ConfigValue: "", ConfigType: "ai", Remark: "当前选中的模型ID",
		})
	}

	response.WriteOK(c, "")
}

func SelectAIModelHandler(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	var req struct {
		Id string `json:"id"`
	}
	if err := jsonutil.UnmarshalJson(c.Request.Body, &req); err != nil {
		response.WriteErr(c, http.StatusOK, 400, "请求参数解析失败")
		return
	}
	if req.Id == "" {
		response.WriteErr(c, http.StatusOK, 400, "模型 ID 不能为空")
		return
	}

	SaveSystemConfig(&SystemConfigSave{
		ConfigKey: "ai.selectedModelId", ConfigValue: req.Id, ConfigType: "ai", Remark: "当前选中的模型ID",
	})
	LoadSystemConfigToMemory()
	response.WriteOK(c, "")
}

func GetAIModelListHandler(c *gin.Context) {
	selectedModelId := GetSystemConfigValue("ai.selectedModelId")

	modelListJSON := GetSystemConfigValue("ai.modelList")
	if modelListJSON == "" || modelListJSON == "[]" {
		response.WriteOK(c, &AIModelListResponse{
			AIModelList:     []AIModelBrief{},
			SelectedModelId: selectedModelId,
		})
		return
	}

	var modelList []AIModelItem
	err := json.Unmarshal([]byte(modelListJSON), &modelList)
	if err != nil {
		response.WriteOK(c, &AIModelListResponse{
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

	response.WriteOK(c, &AIModelListResponse{
		AIModelList:     briefList,
		SelectedModelId: selectedModelId,
	})
}
