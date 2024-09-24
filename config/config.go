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
	// 管理员用户id
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
}
