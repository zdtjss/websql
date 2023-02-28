package config

import (
	"encoding/json"
	"go-web/logutils"
	"os"
	"path/filepath"
)

var Cfg *Config
var IsRemote bool

func ReadConfig() *Config {
	configFile := FindFile("config.json")
	fileData, err := os.ReadFile(configFile)
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
	DB struct {
		DriverName     string `json:"type"`
		DataSourceName string `json:"dsn"`
	} `json:"db"`
	Redis struct {
		Addr     string `json:"addr"`
		Password string `json:"password"`
		DB       int    `json:"db"`
	} `json:"redis"`
}
