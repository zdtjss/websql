package ai

import (
	"encoding/json"
	"go-web/config"
	"go-web/logutils"
	"os"
)

type AIConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	ApiKey   string `json:"apiKey"`
}

// SaveAIConfig persists the AI configuration back to config.json and updates the in-memory Cfg.
func SaveAIConfig(cfg AIConfig) error {
	config.Cfg.AI.Provider = cfg.Provider
	config.Cfg.AI.BaseURL = cfg.BaseURL
	config.Cfg.AI.Model = cfg.Model
	if cfg.ApiKey != "" && cfg.ApiKey != "****" {
		config.Cfg.AI.ApiKey = cfg.ApiKey
	}

	configFile := config.FindFile("config.json")
	data, err := os.ReadFile(configFile)
	if err != nil {
		logutils.PanicErrf("读取 config.json 失败", err)
		return err
	}

	var raw map[string]any
	if err = json.Unmarshal(data, &raw); err != nil {
		logutils.PanicErrf("解析 config.json 失败", err)
		return err
	}

	raw["ai"] = map[string]any{
		"provider": config.Cfg.AI.Provider,
		"baseUrl":  config.Cfg.AI.BaseURL,
		"model":    config.Cfg.AI.Model,
		"apiKey":   config.Cfg.AI.ApiKey,
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		logutils.PanicErrf("序列化 config.json 失败", err)
		return err
	}
	if err = os.WriteFile(configFile, out, 0644); err != nil {
		logutils.PanicErrf("写入 config.json 失败", err)
		return err
	}
	return nil
}

// GetAIConfig returns the AI config with apiKey masked.
func GetAIConfig() (*AIConfig, error) {
	cfg := aiConfigFromCfg()
	if cfg.ApiKey != "" {
		cfg.ApiKey = "****"
	}
	return cfg, nil
}

// GetAIConfigRaw returns the AI config with the real apiKey (for internal use by AI agent).
func GetAIConfigRaw() (*AIConfig, error) {
	cfg := aiConfigFromCfg()
	if cfg.Provider == "" {
		return nil, nil
	}
	return cfg, nil
}

func aiConfigFromCfg() *AIConfig {
	return &AIConfig{
		Provider: config.Cfg.AI.Provider,
		BaseURL:  config.Cfg.AI.BaseURL,
		Model:    config.Cfg.AI.Model,
		ApiKey:   config.Cfg.AI.ApiKey,
	}
}
