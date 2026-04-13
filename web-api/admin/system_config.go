package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"strconv"
	"strings"
	"time"
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

func GetSystemConfigByKey(key string) *SystemConfig {
	cfg := &SystemConfig{}
	err := config.Mngtdb.Get(cfg, "select * from t_system_config where config_key = ?", key)
	if err != nil {
		// sql.ErrNoRows 表示没有数据，返回 nil
		// 其他错误可能是表不存在或连接问题
		if err == sql.ErrNoRows || strings.Contains(err.Error(), "no rows") {
			return nil
		}
		logutils.PrintErr(fmt.Errorf("查询系统配置失败：%s, %v", key, err))
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
		// Insert new record
		cfg.Id = utils.RandomStr()
		_, err := config.Mngtdb.Exec("insert into t_system_config (id, config_key, config_value, config_type, remark) values (?, ?, ?, ?, ?)",
			cfg.Id, cfg.ConfigKey, cfg.ConfigValue, cfg.ConfigType, cfg.Remark)
		if err != nil {
			// 如果是唯一约束冲突，说明是并发插入，转为更新操作
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				// 重新查询获取已存在的记录
				existCfg = GetSystemConfigByKey(cfg.ConfigKey)
				if existCfg != nil {
					// 执行更新
					_, err := config.Mngtdb.Exec("update t_system_config set config_value = ?, config_type = ?, remark = ?, update_time = ? where id = ?",
						cfg.ConfigValue, cfg.ConfigType, cfg.Remark, time.Now(), existCfg.Id)
					if err != nil {
						logutils.PanicErr(err)
					}
					return
				}
			}
			logutils.PanicErr(err)
		}
	} else {
		// Update existing record
		_, err := config.Mngtdb.Exec("update t_system_config set config_value = ?, config_type = ?, remark = ?, update_time = ? where id = ?",
			cfg.ConfigValue, cfg.ConfigType, cfg.Remark, time.Now(), existCfg.Id)
		if err != nil {
			logutils.PanicErr(err)
		}
	}
}

func ListSystemConfig(configType string) []*SystemConfig {
	var configs []*SystemConfig
	var err error
	if configType == "" {
		err = config.Mngtdb.Select(&configs, "select * from t_system_config order by config_type, config_key")
	} else {
		err = config.Mngtdb.Select(&configs, "select * from t_system_config where config_type = ? order by config_key", configType)
	}
	logutils.PanicErr(err)
	return configs
}

func GetAIConfigFromDB() *AIConfig {
	provider := GetSystemConfigValue("ai.provider")
	baseUrl := GetSystemConfigValue("ai.baseUrl")
	model := GetSystemConfigValue("ai.model")
	apiKey := GetSystemConfigValue("ai.apiKey")
	temperatureStr := GetSystemConfigValue("ai.temperature")
	maxTokensStr := GetSystemConfigValue("ai.maxTokens")
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
	var maxTokens int
	if maxTokensStr != "" {
		if v, err := strconv.Atoi(maxTokensStr); err == nil {
			maxTokens = v
		}
	}

	return &AIConfig{
		Provider:       provider,
		BaseURL:        baseUrl,
		Model:          model,
		ApiKey:         apiKey,
		Temperature:    temperature,
		MaxTokens:      maxTokens,
		EnableThinking: enableThinkingStr == "true",
	}
}

func SaveAIConfigToDB(cfg AIConfig) {
	configs := []SystemConfigSave{
		{ConfigKey: "ai.provider", ConfigValue: cfg.Provider, ConfigType: "ai", Remark: "AI 服务提供商"},
		{ConfigKey: "ai.baseUrl", ConfigValue: cfg.BaseURL, ConfigType: "ai", Remark: "AI 服务基础 URL"},
		{ConfigKey: "ai.model", ConfigValue: cfg.Model, ConfigType: "ai", Remark: "AI 模型名称"},
		{ConfigKey: "ai.temperature", ConfigValue: fmt.Sprintf("%.1f", cfg.Temperature), ConfigType: "ai", Remark: "生成随机性 0.0-2.0"},
		{ConfigKey: "ai.maxTokens", ConfigValue: strconv.Itoa(cfg.MaxTokens), ConfigType: "ai", Remark: "最大生成 token 数"},
		{ConfigKey: "ai.enableThinking", ConfigValue: fmt.Sprintf("%t", cfg.EnableThinking), ConfigType: "ai", Remark: "启用思考模式"},
	}

	if cfg.ApiKey != "" && cfg.ApiKey != "****" {
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
	logutils.PanicErr(err)

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

	aiCfg := GetAIConfigFromDB()
	if aiCfg != nil {
		config.Cfg.AI.Provider = aiCfg.Provider
		config.Cfg.AI.BaseURL = aiCfg.BaseURL
		config.Cfg.AI.Model = aiCfg.Model
		config.Cfg.AI.ApiKey = aiCfg.ApiKey
	}
}
