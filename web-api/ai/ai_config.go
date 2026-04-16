package ai

import (
	admin "go-web/web-api/admin"
)

func SaveAIConfig(cfg admin.AIConfig) error {
	admin.SaveAIConfigToDB(cfg)
	return nil
}

func GetAIConfig() (*admin.AIConfig, error) {
	cfg := admin.GetAIConfigFromDB()
	if cfg == nil {
		return nil, nil
	}
	if cfg.ApiKey != "" {
		cfg.ApiKey = "****"
	}
	return cfg, nil
}
