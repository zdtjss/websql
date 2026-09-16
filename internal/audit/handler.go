package audit

import (
	"strconv"

	"websql/internal/pkg/dbaccess"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// adminChecker 由 admin 包在 init 时通过 SetAdminChecker 注入，避免 audit → admin 循环依赖。
// 返回 false 表示当前请求不具备管理员权限（实现方负责写入 401 响应并中断请求）。
var adminChecker func(*gin.Context) bool

// SetAdminChecker 注册管理员权限校验函数，应由 admin 包在初始化时调用
func SetAdminChecker(fn func(*gin.Context) bool) {
	adminChecker = fn
}

// requireAdmin 校验管理员权限：本地/桌面模式直接放行（由 CheckAdminPower 判定），
// 非本地模式仅管理员可通过。未注入校验器时按拒绝处理（fail-closed）。
func requireAdmin(c *gin.Context) bool {
	if adminChecker == nil {
		response.WriteAuthErr(c, "无权访问")
		return false
	}
	return adminChecker(c)
}

// userNameReader 可选接口：currentUser 实现了该方法时补充记录操作人姓名
// （用接口而非直接依赖 admin 包的类型，避免 audit → admin 循环依赖）
type userNameReader interface {
	AuditUserName() string
}

// OpFromContext 构造带请求上下文的操作审计：
// 用户标识取自 AuthMiddleware 注入的 userId / currentUser。
func OpFromContext(c *gin.Context, source, toolName, sqlType, riskLevel string) *OpContext {
	op := &OpContext{Source: source, ToolName: toolName, SQLType: sqlType, RiskLevel: riskLevel}
	if c == nil {
		return op
	}
	op.UserID = c.GetString("userId")
	if v, ok := c.Get("currentUser"); ok {
		if u, ok := v.(userNameReader); ok {
			op.UserName = u.AuditUserName()
		}
	}
	return op
}

var holder = &dbaccess.Holder{}

func Init(db *sqlx.DB) { holder.Init(db) }

// getDB 返回管理库连接；未初始化时返回 nil 而不是 panic。
// 审计属旁路能力：调用方（LoadAuditConfig / 落库 / 查询）均已按 nil 处理，
// 不得因管理库未就绪而中断业务或使进程崩溃（例如权限拒绝时的审计记录路径）。
func getDB() *sqlx.DB { return holder.GetOrNil() }

func HandleGetAuditLogs(c *gin.Context) {
	// 审计日志包含全部用户的 SQL 文本与操作轨迹，仅管理员可读（防止横向越权读取他人操作记录）
	if !requireAdmin(c) {
		return
	}
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
	if !requireAdmin(c) {
		return
	}
	svc := GetAuditService()
	svc.ReloadConfig()
	response.WriteOK(c, svc.GetConfig())
}

func HandleSaveAuditConfig(c *gin.Context) {
	// 必须校验返回值：仅调用校验器而不判断结果，会让非管理员继续改写审计配置
	if !requireAdmin(c) {
		return
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
	if !requireAdmin(c) {
		return
	}
	stats := make(map[string]any)

	statQueries := map[string]string{
		"totalLast24h":    "SELECT COUNT(*) FROM t_audit_log WHERE exec_time >= datetime('now', '-1 day')",
		"totalLast7d":     "SELECT COUNT(*) FROM t_audit_log WHERE exec_time >= datetime('now', '-7 day')",
		"failedCount":     "SELECT COUNT(*) FROM t_audit_log WHERE status = 'failed'",
		"uniqueUsers":     "SELECT COUNT(DISTINCT user_id) FROM t_audit_log",
	}

	db := getDB()
	if db == nil {
		response.WriteOK(c, stats)
		return
	}
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