package audit

import (
	"encoding/json"
	"fmt"
)

// AuditLogsQuery 审计日志查询参数。
type AuditLogsQuery struct {
	UserID     string
	ConnID     string
	SessionID  string
	SQLType    string
	RiskLevel  string
	Source     string
	StartTime  string
	EndTime    string
	Keyword    string
	Page       int
	PageSize   int
}

// GetAuditLogsByService 查询审计日志(分页)。
// 业务来自 HandleGetAuditLogs handler。
func GetAuditLogsByService(q *AuditLogsQuery) (map[string]any, error) {
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	if q.Page < 1 {
		q.Page = 1
	}

	logs, total, err := queryAuditLogs(q.UserID, q.ConnID, q.SessionID, q.SQLType, q.RiskLevel, q.Source, q.StartTime, q.EndTime, q.Keyword, q.Page, q.PageSize)
	if err != nil {
		return nil, fmt.Errorf("查询审计日志失败: %w", err)
	}
	if logs == nil {
		logs = []AuditLog{}
	}
	return map[string]any{"data": logs, "total": total}, nil
}

// GetAuditConfigByService 读取审计配置。
// 业务来自 HandleGetAuditConfig handler。
func GetAuditConfigByService() *AuditConfig {
	svc := GetAuditService()
	svc.ReloadConfig()
	return svc.GetConfig()
}

// SaveAuditConfigByService 保存审计配置。
// 业务来自 HandleSaveAuditConfig handler。
// 桌面模式默认 IsRemote=false,无 admin 权限校验。
// HTTP handler 通过 jsonutil.UnmarshalJson 解析 body,service 直接接收已解析的 *AuditConfig。
func SaveAuditConfigByService(cfg *AuditConfig) error {
	if cfg == nil {
		return fmt.Errorf("请求参数解析失败")
	}
	SaveAuditConfigToDB(cfg)
	GetAuditService().ReloadConfig()
	return nil
}

// SaveAuditConfigByServiceFromBytes 保留原 body bytes 版本,供 HTTP handler 调用。
func SaveAuditConfigByServiceFromBytes(body []byte) error {
	cfg := &AuditConfig{}
	if err := json.Unmarshal(body, cfg); err != nil {
		return fmt.Errorf("请求参数解析失败")
	}
	return SaveAuditConfigByService(cfg)
}

// GetAuditStatsByService 返回审计日志统计信息。
// 业务来自 HandleGetAuditStats handler。
func GetAuditStatsByService() map[string]any {
	stats := make(map[string]any)

	statQueries := map[string]string{
		"totalLast24h": "SELECT COUNT(*) FROM t_audit_log WHERE exec_time >= datetime('now', '-1 day')",
		"totalLast7d":  "SELECT COUNT(*) FROM t_audit_log WHERE exec_time >= datetime('now', '-7 day')",
		"failedCount":  "SELECT COUNT(*) FROM t_audit_log WHERE status = 'failed'",
		"uniqueUsers":  "SELECT COUNT(DISTINCT user_id) FROM t_audit_log",
	}

	db := getDB()
	for key, query := range statQueries {
		var count int
		if err := db.Get(&count, query); err == nil {
			stats[key] = count
		}
	}

	type typeCount struct {
		SQLType string `db:"sql_type" json:"sqlType"`
		Count   int    `db:"cnt" json:"count"`
	}
	var typeCounts []typeCount
	err := db.Select(&typeCounts,
		"SELECT sql_type, COUNT(*) as cnt FROM t_audit_log GROUP BY sql_type ORDER BY cnt DESC")
	if err == nil {
		stats["byType"] = typeCounts
	}

	var sourceCounts []typeCount
	err = db.Select(&sourceCounts,
		`SELECT source as sql_type, COUNT(*) as cnt FROM t_audit_log GROUP BY source ORDER BY cnt DESC`)
	if err == nil {
		stats["bySource"] = sourceCounts
	}

	return stats
}


