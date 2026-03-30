package ai

import (
	admin "go-web/web-api/admin"
)

// SaveAIConfig persists the AI configuration to database.
func SaveAIConfig(cfg admin.AIConfig) error {
	admin.SaveAIConfigToDB(cfg)
	return nil
}

// GetAIConfig returns the AI config with apiKey masked.
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

// GetAIConfigRaw returns the AI config with the real apiKey (for internal use by AI agent).
func GetAIConfigRaw() (*admin.AIConfig, error) {
	cfg := admin.GetAIConfigFromDB()
	return cfg, nil
}
