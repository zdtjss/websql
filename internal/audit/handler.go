package audit

import (
	"strconv"

	"websql/internal/database"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// adminChecker 由 admin 包在 init 时通过 SetAdminChecker 注入，避免 audit → admin 循环依赖
var adminChecker func(*gin.Context)

// SetAdminChecker 注册管理员权限校验函数，应由 admin 包在初始化时调用
func SetAdminChecker(fn func(*gin.Context)) {
	adminChecker = fn
}

// injectedDB 由 DI 容器通过 Init 注入；为 nil 时回退到全局 database.Mngtdb（向后兼容）。
var injectedDB *sqlx.DB

// Init 由 app 容器在启动阶段调用，将管理库 *sqlx.DB 注入到 audit 包。
// 不调用也能工作——handler 会回退到全局 database.Mngtdb。
func Init(db *sqlx.DB) {
	injectedDB = db
}

// getDB 返回注入的 DB，未注入时回退到全局 database.Mngtdb。
func getDB() *sqlx.DB {
	if injectedDB != nil {
		return injectedDB
	}
	return database.Mngtdb
}

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
		response.WriteErr(c, 200, 500, "查询审计日志失败")
		return
	}
	if logs == nil {
		logs = []AuditLog{}
	}
	response.WriteOK(c, gin.H{"data": logs, "total": total})
}

func HandleGetAuditConfig(c *gin.Context) {
	svc := GetAuditService()
	svc.ReloadConfig()
	response.WriteOK(c, svc.GetConfig())
}

func HandleSaveAuditConfig(c *gin.Context) {
	if adminChecker != nil {
		adminChecker(c)
	}
	cfg := &AuditConfig{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, cfg); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	SaveAuditConfigToDB(cfg)
	GetAuditService().ReloadConfig()
	response.WriteOK(c, "审计配置已保存")
}

func HandleGetAuditStats(c *gin.Context) {
	stats := make(map[string]any)

	statQueries := map[string]string{
		"totalLast24h":    "SELECT COUNT(*) FROM t_audit_log WHERE exec_time >= datetime('now', '-1 day')",
		"totalLast7d":     "SELECT COUNT(*) FROM t_audit_log WHERE exec_time >= datetime('now', '-7 day')",
		"failedCount":     "SELECT COUNT(*) FROM t_audit_log WHERE status = 'failed'",
		"uniqueUsers":     "SELECT COUNT(DISTINCT user_id) FROM t_audit_log",
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

	response.WriteOK(c, stats)
}