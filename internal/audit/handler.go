package audit

import (
	"strconv"

	"websql/internal/app/admin"
	"websql/internal/database"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
)

func HandleGetAuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize > 200 {
		pageSize = 200
	}
	if page < 1 {
		page = 1
	}

	userID := c.Query("userId")
	startTime := c.Query("startTime")
	endTime := c.Query("endTime")
	connID := c.Query("connId")
	sqlType := c.Query("sqlType")
	riskLevel := c.Query("riskLevel")
	source := c.Query("source")
	sessionID := c.Query("sessionId")
	keyword := c.Query("keyword")

	logs, total, err := queryAuditLogs(userID, connID, sessionID, sqlType, riskLevel, source, startTime, endTime, keyword, page, pageSize)
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "查询审计日志失败"})
		return
	}
	if logs == nil {
		logs = []AuditLog{}
	}
	c.JSON(200, gin.H{"data": logs, "total": total})
}

func HandleGetAuditConfig(c *gin.Context) {
	svc := GetAuditService()
	svc.ReloadConfig()
	c.JSON(200, gin.H{"code": 200, "data": svc.GetConfig()})
}

func HandleSaveAuditConfig(c *gin.Context) {
	admin.CheckAdminPower(c)
	cfg := &AuditConfig{}
	jsonutil.UnmarshalJson(c.Request.Body, cfg)
	SaveAuditConfigToDB(cfg)
	GetAuditService().ReloadConfig()
	c.JSON(200, gin.H{"code": 200, "msg": "审计配置已保存"})
}

func HandleGetAuditStats(c *gin.Context) {
	stats := make(map[string]any)

	statQueries := map[string]string{
		"totalLast24h":    "SELECT COUNT(*) FROM t_audit_log WHERE exec_time >= datetime('now', '-1 day')",
		"totalLast7d":     "SELECT COUNT(*) FROM t_audit_log WHERE exec_time >= datetime('now', '-7 day')",
		"failedCount":     "SELECT COUNT(*) FROM t_audit_log WHERE status = 'failed'",
		"uniqueUsers":     "SELECT COUNT(DISTINCT user_id) FROM t_audit_log",
	}

	for key, query := range statQueries {
		var count int
		if err := database.Mngtdb.Get(&count, query); err == nil {
			stats[key] = count
		}
	}

	type typeCount struct {
		SQLType string `db:"sql_type" json:"sqlType"`
		Count   int    `db:"cnt" json:"count"`
	}
	var typeCounts []typeCount
	err := database.Mngtdb.Select(&typeCounts,
		"SELECT sql_type, COUNT(*) as cnt FROM t_audit_log GROUP BY sql_type ORDER BY cnt DESC")
	if err == nil {
		stats["byType"] = typeCounts
	}

	var sourceCounts []typeCount
	err = database.Mngtdb.Select(&sourceCounts,
		`SELECT source as sql_type, COUNT(*) as cnt FROM t_audit_log GROUP BY source ORDER BY cnt DESC`)
	if err == nil {
		stats["bySource"] = sourceCounts
	}

	c.JSON(200, gin.H{"code": 200, "data": stats})
}