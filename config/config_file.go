package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

var Cfg *Config

func ReadConfig() *Config {

	exec, err := os.Executable()
	if err != nil {
		println(err)
	}
	configFile := filepath.Join(filepath.Join(filepath.Dir(exec), "../"), "config.json")
	fileData, err := os.ReadFile(configFile)
	if err != nil {
		configFile = filepath.Join(filepath.Dir(exec), "config.json")
		fileData, err = os.ReadFile(configFile)
	}
	if err != nil {
		print(err.Error())
	}
	var config Config
	err = json.Unmarshal(fileData, &config)
	if err != nil {
		panic(err.Error())
	}
	return &config
}

type Config struct {
	DB map[string]map[string]string `json:"db"`
}
