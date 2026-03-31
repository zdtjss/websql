package config

import (
	"encoding/json"
	"go-web/logutils"
	"log"
	"os"
	"path/filepath"
)

var (
	Cfg *Config
	// 管理员用户 id
	AdminId string = "825683877312860160"
)

func ReadConfig() *Config {
	configFile := FindFile("config.json")
	log.Printf("使用配置文件 %s", configFile)
	fileData, err := os.ReadFile(configFile)
	logutils.PanicErr(err)
	var config Config
	err = json.Unmarshal(fileData, &config)
	logutils.PanicErr(err)
	return &config
}

// LoadConfigFromDB 从数据库加载配置（覆盖配置文件中的配置）
func LoadConfigFromDB() {
	if Mngtdb == nil {
		return
	}

	// 使用 admin 包中的函数加载配置（避免循环依赖）
	// 这里我们通过查询系统配置表来加载
	loadSystemConfigValue("system.outterUser", &Cfg.OutterUser)
	loadSystemConfigValue("system.allowedIP", &Cfg.AllowedIP)
	loadSystemConfigValue("ai.provider", &Cfg.AI.Provider)
	loadSystemConfigValue("ai.baseUrl", &Cfg.AI.BaseURL)
	loadSystemConfigValue("ai.model", &Cfg.AI.Model)
	loadSystemConfigValue("ai.apiKey", &Cfg.AI.ApiKey)
	// temperature, maxTokens, enableThinking 通过 GetAIConfigFromDB() 加载
}

func loadSystemConfigValue(key string, target interface{}) {
	var value string
	err := Mngtdb.Get(&value, "select config_value from t_system_config where config_key = ?", key)
	if err != nil || value == "" {
		return
	}

	// 根据目标类型进行转换
	switch t := target.(type) {
	case *string:
		*t = value
	case *[]string:
		var arr []string
		err := json.Unmarshal([]byte(value), &arr)
		if err == nil {
			*t = arr
		}
	}
}

func ReadSql(fileName string) *string {
	configFile := FindFile(fileName)
	fileData, err := os.ReadFile(configFile)
	logutils.PanicErr(err)
	sql := string(fileData)
	return &sql
}

func FindFile(fileName string) string {
	exec, err := os.Executable()
	logutils.PanicErr(err)
	configFile := filepath.Join(filepath.Join(filepath.Dir(exec), "../"), fileName)
	_, err = os.Lstat(configFile)
	if err != nil {
		configFile = filepath.Join(filepath.Dir(exec), fileName)
		_, err = os.Lstat(configFile)
		logutils.PanicErr(err)
	}
	return configFile
}

type Config struct {
	// true：远程模式，有严格的权限管理；false 本地模式，没有权限管理
	IsRemote bool `json:"isRemote"`
	DB       struct {
		DriverName     string `json:"type"`
		DataSourceName string `json:"dsn"`
	} `json:"db"`
	Redis struct {
		Addr     string `json:"addr"`
		Password string `json:"password"`
		DB       int    `json:"db"`
	} `json:"redis"`
	Https struct {
		Organization string `json:"organization"`
		CommonName   string `json:"commonName"`
	} `json:"https"`
	OutterUser string   `json:"outterUser"`
	AllowedIP  []string `json:"allowedIP"`
	AI         struct {
		Provider       string  `json:"provider"`
		BaseURL        string  `json:"baseUrl"`
		Model          string  `json:"model"`
		ApiKey         string  `json:"apiKey"`
		Temperature    float32 `json:"temperature"`
		MaxTokens      int     `json:"maxTokens"`
		EnableThinking bool    `json:"enableThinking"`
	} `json:"ai"`
}
