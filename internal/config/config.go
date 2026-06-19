package config

import (
	"encoding/json"
	logutils "websql/internal/logger"
	"log"
	"os"
	"path/filepath"
)

var (
	// Deprecated: 使用 Container，将在阶段 4 移除
	Cfg *Config
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

func ReadSql(fileName string) string {
	configFile := FindFile(fileName)
	fileData, err := os.ReadFile(configFile)
	logutils.PanicErr(err)
	return string(fileData)
}

func FindFile(fileName string) string {
	exec, err := os.Executable()
	logutils.PanicErr(err)
	configFile := filepath.Join(filepath.Dir(exec), "..", fileName)
	_, err = os.Stat(configFile)
	if err != nil {
		configFile = filepath.Join(filepath.Dir(exec), fileName)
		_, err = os.Stat(configFile)
		logutils.PanicErr(err)
	}
	return configFile
}

type Config struct {
	// true：远程模式，有严格的权限管理；false 本地模式，没有权限管?
	IsRemote bool `json:"isRemote"`
	DB struct {
		DriverName     string `json:"type"`
		DataSourceName string `json:"dsn"`
		MaxOpenConns   int    `json:"maxOpenConns"`
		MaxIdleConns   int    `json:"maxIdleConns"`
		ConnMaxLifeMin int    `json:"connMaxLifeMin"`
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
	Security   struct {
		// AESKey 为 16/24/32 字节密钥的 base64 编码，用于加解密连接密码与备份内容。
		// 留空时回退到内置默认密钥（仅用于兼容存量数据，不安全，生产环境务必配置）。
		AESKey string `json:"aesKey"`
	} `json:"security"`
	AI         struct {
		Provider       string  `json:"provider"`
		BaseURL        string  `json:"baseUrl"`
		Model          string  `json:"model"`
		ApiKey         string  `json:"apiKey"`
		Temperature    float32 `json:"temperature"`
		EnableThinking bool    `json:"enableThinking"`
	} `json:"ai"`
}