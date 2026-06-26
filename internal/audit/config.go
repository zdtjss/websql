package audit

import (
	"strconv"
	"strings"

	"websql/internal/logger"
)

func LoadAuditConfig() *AuditConfig {
	cfg := &AuditConfig{
		Enabled:          true,
		RecordQuery:      false,
		RecordWrite:      true,
		RecordDangerous:  true,
		RecordAgentTools: true,
		RecordSQLEditor:  true,
		RetentionDays:    90,
		MinRiskLevel:     "low",
	}

	if getDB() == nil {
		return cfg
	}

	if v := getConfigValue("audit.enabled"); v != "" {
		cfg.Enabled = v == "true"
	}
	if v := getConfigValue("audit.recordQuery"); v != "" {
		cfg.RecordQuery = v == "true"
	}
	if v := getConfigValue("audit.recordWrite"); v != "" {
		cfg.RecordWrite = v == "true"
	}
	if v := getConfigValue("audit.recordDangerous"); v != "" {
		cfg.RecordDangerous = v == "true"
	}
	if v := getConfigValue("audit.recordAgentTools"); v != "" {
		cfg.RecordAgentTools = v == "true"
	}
	if v := getConfigValue("audit.recordSQLEditor"); v != "" {
		cfg.RecordSQLEditor = v == "true"
	}
	if v := getConfigValue("audit.retentionDays"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.RetentionDays = n
		}
	}
	if v := getConfigValue("audit.minRiskLevel"); v != "" {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "low" || v == "medium" || v == "high" {
			cfg.MinRiskLevel = v
		}
	}

	return cfg
}

func SaveAuditConfigToDB(cfg *AuditConfig) {
	saveConfig("audit.enabled", boolToStr(cfg.Enabled), "审计日志全局开关")
	saveConfig("audit.recordQuery", boolToStr(cfg.RecordQuery), "是否审计只读查询（SELECT/SHOW/DESCRIBE）")
	saveConfig("audit.recordWrite", boolToStr(cfg.RecordWrite), "是否审计写操作（INSERT/UPDATE/DELETE）")
	saveConfig("audit.recordDangerous", boolToStr(cfg.RecordDangerous), "是否审计高风险操作（DROP/TRUNCATE/ALTER）")
	saveConfig("audit.recordAgentTools", boolToStr(cfg.RecordAgentTools), "是否审计 AI Agent 工具调用")
	saveConfig("audit.recordSQLEditor", boolToStr(cfg.RecordSQLEditor), "是否审计 SQL 编辑器直接执行")
	saveConfig("audit.retentionDays", strconv.Itoa(cfg.RetentionDays), "审计日志保留天数")
	saveConfig("audit.minRiskLevel", cfg.MinRiskLevel, "最低记录风险等级（low/medium/high）")
}

func getConfigValue(key string) string {
	var value string
	err := getDB().Get(&value, "select config_value from t_system_config where config_key = ?", key)
	if err != nil {
		return ""
	}
	return value
}

func saveConfig(key, value, remark string) {
	db := getDB()
	var existID string
	err := db.Get(&existID, "select id from t_system_config where config_key = ?", key)
	if err != nil {
		_, err := db.Exec(
			"insert into t_system_config (id, config_key, config_value, config_type, remark) values (?, ?, ?, ?, ?)",
			"audit_cfg_"+key, key, value, "audit", remark)
		if err != nil {
			logger.PrintErrf("插入审计配置失败: %s", err, key)
		}
		return
	}
	_, err = db.Exec(
		"update t_system_config set config_value = ?, remark = ?, update_time = datetime('now') where id = ?",
		value, remark, existID)
	if err != nil {
		logger.PrintErrf("更新审计配置失败: %s", err, key)
	}
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}