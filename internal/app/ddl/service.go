package ddl

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/audit"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// planTTL 计划有效期：预览与实际执行之间通常只有几秒，
// 过期即作废可避免「改了别处再拿旧计划执行」。
const planTTL = 10 * time.Minute

// applyStatementTimeout 单条 DDL 的执行超时。
const applyStatementTimeout = 60 * time.Second

type planEntry struct {
	plan      *Plan
	connId    string
	expiresAt time.Time
}

var planStore = struct {
	sync.Mutex
	m map[string]*planEntry
}{m: make(map[string]*planEntry)}

func storePlan(plan *Plan, connId string) {
	now := time.Now()
	planStore.Lock()
	// 顺带清理过期计划，避免长期驻留
	for id, e := range planStore.m {
		if now.After(e.expiresAt) {
			delete(planStore.m, id)
		}
	}
	planStore.m[plan.PlanId] = &planEntry{plan: plan, connId: connId, expiresAt: now.Add(planTTL)}
	planStore.Unlock()
}

// takePlan 取出并删除计划（一次性消费）。
func takePlan(planId, connId string) (*Plan, error) {
	planStore.Lock()
	entry, ok := planStore.m[planId]
	if ok {
		delete(planStore.m, planId)
	}
	planStore.Unlock()

	if !ok {
		return nil, fmt.Errorf("变更计划不存在或已执行，请重新生成预览")
	}
	if time.Now().After(entry.expiresAt) {
		return nil, fmt.Errorf("变更计划已过期，请重新生成预览")
	}
	if entry.connId != "" && connId != "" && entry.connId != connId {
		return nil, fmt.Errorf("变更计划与当前连接不匹配")
	}
	return entry.plan, nil
}

// BuildPlan 生成变更计划。
func BuildPlan(dbType, table string, changes []Change) (*Plan, error) {
	stmts, warnings, unsupported, err := buildStatements(dbType, table, changes)
	if err != nil {
		return nil, err
	}
	return &Plan{
		PlanId:      idgen.SecureRandomToken()[:24],
		DbType:      dbType,
		Table:       table,
		Statements:  stmts,
		Warnings:    warnings,
		Unsupported: unsupported,
	}, nil
}

// applyResult 执行结果（逐条状态）。
type applyResult struct {
	Executed []PlannedStatement `json:"executed"`
	Failed   *PlannedStatement  `json:"failed,omitempty"`
	Error    string             `json:"error,omitempty"`
}

// applyPlan 逐条执行计划中的语句。
// DDL 在 MySQL/Oracle 下无法回滚（隐式提交），因此这里不做事务包裹，
// 执行到第一条失败即停止，并把已执行的语句回传给前端展示。
func applyPlan(ctx context.Context, db *sqlx.DB, plan *Plan) (*applyResult, error) {
	result := &applyResult{Executed: make([]PlannedStatement, 0, len(plan.Statements))}
	for _, stmt := range plan.Statements {
		stmtCtx, cancel := context.WithTimeout(ctx, applyStatementTimeout)
		_, err := db.ExecContext(stmtCtx, stmt.SQL)
		cancel()
		if err != nil {
			failed := stmt
			result.Failed = &failed
			result.Error = err.Error()
			return result, err
		}
		result.Executed = append(result.Executed, stmt)
	}
	return result, nil
}

// aggregateRisk 汇总整批变更的风险等级，用于审计。
func aggregateRisk(stmts []PlannedStatement) string {
	risk := RiskLow
	for _, s := range stmts {
		switch s.Risk {
		case RiskHigh:
			return RiskHigh
		case RiskMedium:
			risk = RiskMedium
		}
	}
	return risk
}

func auditPlan(connID, schema string, stmts []PlannedStatement, user *admin.User, clientIP, status, errMsg string, elapsed time.Duration) {
	if len(stmts) == 0 {
		return
	}
	sqls := make([]string, 0, len(stmts))
	for _, s := range stmts {
		sqls = append(sqls, s.SQL)
	}
	connName, _ := conn.GetConnInfo(connID)
	entry := &audit.AuditEntry{
		Source:     "ddl",
		SQLText:    strings.Join(sqls, ";\n"),
		SQLType:    "DDL",
		RiskLevel:  aggregateRisk(stmts),
		Status:     status,
		ConnID:     connID,
		ConnName:   connName,
		SchemaName: schema,
		ClientIP:   clientIP,
		ExecTimeMs: int(elapsed.Milliseconds()),
		ErrorMsg:   errMsg,
	}
	if user != nil {
		entry.UserID = user.Id
		entry.UserName = user.Name
	}
	audit.GetAuditService().Record(entry)
}

// PlanHandler POST /api/ddl/plan：只生成 SQL 供预览，不改库。
func PlanHandler(c *gin.Context) {
	req := &PlanRequest{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	if strings.TrimSpace(req.ConnId) == "" || strings.TrimSpace(req.Table) == "" {
		response.WriteErr(c, 200, 400, "缺少连接或表名")
		return
	}

	authorization := appctx.Ctx.GetAuthorization(c)
	db := conn.GetConn(req.ConnId, authorization)
	if db == nil {
		response.WriteErr(c, 200, 500, "数据库连接不可用")
		return
	}

	plan, err := BuildPlan(db.DriverName(), req.Table, req.Changes)
	if err != nil {
		log.Printf("[DDL] 生成变更计划失败 - connId=%s, table=%s, err=%v", req.ConnId, req.Table, err)
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	// 生成阶段就做一次权限预检，避免用户确认后才被拒绝
	sqls := make([]string, 0, len(plan.Statements))
	for _, s := range plan.Statements {
		sqls = append(sqls, s.SQL)
	}
	if len(sqls) > 0 {
		if denied := permission.CheckBatchSQLPermission(sqls, req.ConnId, req.Schema, authorization); denied != nil {
			response.WriteErr(c, 200, 500, denied.Message)
			return
		}
	}

	storePlan(plan, req.ConnId)
	response.WriteOK(c, plan)
}

// ApplyHandler POST /api/ddl/apply：按计划 ID 执行。
func ApplyHandler(c *gin.Context) {
	req := &ApplyRequest{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	if strings.TrimSpace(req.PlanId) == "" || strings.TrimSpace(req.ConnId) == "" {
		response.WriteErr(c, 200, 400, "缺少计划或连接")
		return
	}

	authorization := appctx.Ctx.GetAuthorization(c)
	userVal, _ := c.Get("currentUser")
	user, _ := userVal.(*admin.User)

	db := conn.GetConn(req.ConnId, authorization)
	if db == nil {
		response.WriteErr(c, 200, 500, "数据库连接不可用")
		return
	}

	plan, err := takePlan(req.PlanId, req.ConnId)
	if err != nil {
		response.WriteErr(c, 200, 400, err.Error())
		return
	}
	// 只读连接：DDL 属于写操作，需处于解锁窗口内
	if allowed, reason := conn.CheckWritable(req.ConnId); !allowed {
		response.WriteErr(c, 200, 403, reason)
		return
	}
	if len(plan.Statements) == 0 {
		msg := "当前数据库不支持所选变更，请在 SQL 编辑器中手工执行"
		if len(plan.Unsupported) > 0 {
			msg = "以下变更当前数据库不支持：" + strings.Join(plan.Unsupported, ", ")
		}
		response.WriteErr(c, 200, 400, msg)
		return
	}

	// 执行前再校验一次权限：计划生成到执行之间权限可能已被收回
	sqls := make([]string, 0, len(plan.Statements))
	for _, s := range plan.Statements {
		sqls = append(sqls, s.SQL)
	}
	if denied := permission.CheckBatchSQLPermission(sqls, req.ConnId, req.Schema, authorization); denied != nil {
		response.WriteErr(c, 200, 500, denied.Message)
		return
	}

	start := time.Now()
	result, applyErr := applyPlan(c.Request.Context(), db, plan)
	elapsed := time.Since(start)

	if applyErr != nil {
		status, errMsg := "failed", applyErr.Error()
		if len(result.Executed) > 0 {
			status = "partial"
			errMsg = fmt.Sprintf("%d 条执行成功，第 %d 条失败: %v", len(result.Executed), len(result.Executed)+1, applyErr)
		}
		log.Printf("[DDL] 执行变更失败 - connId=%s, table=%s, executed=%d, err=%v", req.ConnId, plan.Table, len(result.Executed), applyErr)
		auditPlan(req.ConnId, req.Schema, plan.Statements, user, c.ClientIP(), status, errMsg, elapsed)
		// DDL 已部分生效时同样按 200 返回，让前端展示已执行/失败明细
		response.WriteOK(c, gin.H{
			"executed": result.Executed,
			"failed":   result.Failed,
			"error":    errMsg,
			"partial":  true,
		})
		return
	}

	auditPlan(req.ConnId, req.Schema, plan.Statements, user, c.ClientIP(), "success", "", elapsed)
	response.WriteOK(c, gin.H{
		"executed": result.Executed,
		"partial":  false,
	})
}
