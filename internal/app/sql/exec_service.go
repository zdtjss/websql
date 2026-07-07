package sql

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	admin "websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/permission"
	"websql/internal/audit"
	"websql/internal/database"
	"websql/internal/pkg/safego"
	"websql/internal/pkg/sanitize"

	"github.com/jmoiron/sqlx"
)

// ExecRequest 是 ExecSQL 的统一入参，gin handler 和 Wails binding 共用。
// 字段语义:
//   - ConnID/Authorization: 鉴权和数据源路由
//   - Schema/TableName: SQL 执行上下文，空时由 service 自动推断
//   - SQL: 待执行的 SQL 文本，service 内部会 TrimSpace
//   - MaxLine/Batch/IsFile: 与原 HTTP 表单字段一一对应
//   - UserID/User: 当前用户，用于审计与权限校验
//   - ClientIP: 审计来源 IP，HTTP 模式传 c.ClientIP()，桌面模式传空字符串
type ExecRequest struct {
	ConnID        string
	Schema        string
	TableName     string
	SQL           string
	MaxLine       string
	Batch         string
	IsFile        bool
	Authorization string
	UserID        string
	User          *admin.User
	ClientIP      string
}

// ExecResult 是 ExecSQL 的统一返回。
// Single 与 Batch 二选一：Batch 字段非空表示批量模式。
type ExecResult struct {
	Single *TableDataList  `json:"single,omitempty"`
	Batch  *BatchSQLResult `json:"batch,omitempty"`
}

// ExecService 封装 SQL 执行业务逻辑，与 gin.Context 解耦。
// 既能被 gin handler 调用，也能被 Wails binding 调用。
type ExecService struct{}

var (
	defaultExecService *ExecService
	defaultExecOnce    sync.Once
)

// ensureDefaultExec 返回单例 ExecService。
func ensureDefaultExec() *ExecService {
	defaultExecOnce.Do(func() {
		defaultExecService = &ExecService{}
	})
	return defaultExecService
}

// Exec 执行 SQL（单条或批量，依据 req.Batch 决定）。
// 业务逻辑来自原 exec.go 的 ExecSQL handler，行为保持等价。
//
// 错误约定:
//   - 校验类错误（SQL 为空、SQL 过长、连接不可用、权限不足）: 返回带可读消息的 error
//   - 执行类错误: 已包含 sanitize.RedactCredentials 与长度截断，可直接展示
func (s *ExecService) Exec(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	sqlStr := strings.TrimSpace(req.SQL)
	startTime := time.Now()

	const maxSQLLength = 1024 * 1024
	const maxFileSQLLength = 50 * 1024 * 1024
	limit := maxSQLLength
	if req.IsFile {
		limit = maxFileSQLLength
	}
	if len(sqlStr) > limit {
		return nil, errors.New("SQL 语句过长，请拆分执行")
	}
	if sqlStr == "" {
		return nil, errors.New("SQL 语句不能为空")
	}

	c := conn.GetConn(req.ConnID, req.Authorization)
	if c == nil {
		return nil, errors.New("数据库连接不可用，请检查连接配置或稍后重试")
	}

	schema := req.Schema
	if schema == "" {
		switch c.DriverName() {
		case "mysql", "mariadb":
			c.Get(&schema, "SELECT DATABASE()")
		case "oracle":
			c.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		case "sqlite":
			schema = "main"
		}
	}

	if strings.Contains(sqlStr, ";") {
		for _, singleSQL := range splitSQLRespectQuotes(sqlStr) {
			if singleSQL == "" {
				continue
			}
			subAnalysis := permission.AnalyzeSQL(singleSQL, schema)
			subResult := permission.CheckAnalysisPermission(subAnalysis, req.ConnID, req.Authorization)
			if !subResult.Allowed {
				return nil, errors.New(subResult.Message)
			}
		}
	} else {
		analysis := permission.AnalyzeSQL(sqlStr, schema)
		permResult := permission.CheckAnalysisPermission(analysis, req.ConnID, req.Authorization)
		if !permResult.Allowed {
			return nil, errors.New(permResult.Message)
		}
	}

	if req.Batch == "true" {
		batchResult := s.execBatch(ctx, sqlStr, c, schema, req.TableName, req.MaxLine, req.User, req.ConnID, req.Authorization, startTime, req.ClientIP)
		if batchResult.err != nil {
			return nil, batchResult.err
		}
		return &ExecResult{Batch: batchResult.result}, nil
	}

	result, err := s.execSingle(ctx, sqlStr, c, schema, req.TableName, req.MaxLine, req.User, req.ConnID, req.Authorization, startTime, req.ClientIP)
	if err != nil {
		return nil, err
	}
	return &ExecResult{Single: result}, nil
}

// execSingle 执行单条 SQL（非批量模式），返回 TableDataList。
// 业务逻辑来自原 exec.go ExecSQL handler 中 batch != "true" 的分支。
func (s *ExecService) execSingle(ctx context.Context, sqlStr string, c *sqlx.DB, schema, tableName, maxLine string, user *admin.User, connId, authorization string, startTime time.Time, clientIP string) (*TableDataList, error) {
	blankIdx := strings.Index(sqlStr, " ")
	nlIdx := strings.Index(sqlStr, "\n")
	if nlIdx == -1 {
		nlIdx = len(sqlStr)
	}

	if checkPrefx(sqlStr, []string{"update", "delete"}) {
		safego.GoWithName("sql-async-backup", func() {
			asyncBackup(sqlStr, user, connId, c)
		})
	} else {
		asyncRecordHistory(sqlStr, user, connId)
	}

	sqlStr = sqlStr[0:min(blankIdx, nlIdx)] + sqlStr[min(blankIdx, nlIdx):]

	if checkPrefx(sqlStr, []string{"update", "delete", "alter", "drop", "insert", "create", "truncate", "replace", "merge"}) {
		rspData := TableDataList{Columns: []Column{{Name: "受影响行数", Type: "VARCHAR(10)"}}}
		result, err := batchExec(sqlStr, c)
		if err != nil {
			recordAudit(user, connId, sqlStr, "failed", 0, startTime, err.Error(), clientIP)
			return nil, sanitizeSQLError(err)
		}
		rspData.Data = result
		totalAffected := 0
		for _, row := range result {
			if v, ok := row["受影响行数"]; ok {
				switch n := v.(type) {
				case int:
					totalAffected += n
				case int64:
					totalAffected += int(n)
				}
			}
		}
		recordAudit(user, connId, sqlStr, "success", totalAffected, startTime, "", clientIP)
		return &rspData, nil
	}

	params := make([]any, 0)
	if checkPrefx(sqlStr, []string{"select"}) && !checkContains(sqlStr, []string{" limit ", " LIMIT ", "\nlimit\n", "\nLIMIT\n"}) {
		maxLineI, _ := strconv.Atoi(maxLine)
		if maxLineI > 0 {
			sqlStr = page(c.DriverName(), sqlStr)
			params = append(params, maxLineI)
		}
	}

	queryCtx, queryCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer queryCancel()

	var rows *sqlx.Rows
	var err2 error
	if len(params) > 0 {
		rows, err2 = c.QueryxContext(queryCtx, sqlStr, params...)
	} else {
		rows, err2 = c.QueryxContext(queryCtx, sqlStr)
	}
	if err2 != nil {
		recordAudit(user, connId, sqlStr, "failed", 0, startTime, err2.Error(), clientIP)
		return nil, sanitizeSQLError(err2)
	}
	defer rows.Close()

	cts, err3 := rows.ColumnTypes()
	if err3 != nil {
		recordAudit(user, connId, sqlStr, "failed", 0, startTime, err3.Error(), clientIP)
		return nil, sanitizeSQLError(err3)
	}
	columnList := make([]Column, len(cts))
	columnNameList := make([]string, 0)

	realTableName, realSchema := tableName, schema
	if strings.Contains(tableName, ".") {
		realTableName = string(tableName[strings.Index(tableName, ".")+1:])
		realSchema = string(tableName[0:strings.Index(tableName, ".")])
	}
	var keyIdx []int
	var keys []string
	columnMap := map[string]string{}

	if IsAlphaNumeric(realTableName) && isSimpleQuery(sqlStr) {
		keys = dbops.QueryPrimaryKeyCached(connId, schema, realTableName, c)
		columnMap = dbops.ColumnMapFiltered(strings.ToLower(realTableName), strings.ToLower(realSchema), connId, authorization, c)
	}

	for idx, val := range cts {
		columnNameList = append(columnNameList, val.Name())
		columnList[idx] = Column{Name: val.Name(), Type: val.DatabaseTypeName(), Comment: columnMap[val.Name()]}
	}

	if len(keys) != 0 {
		keyIdx = database.KeyIdx(keys, columnNameList)
	}

	data, dataErr := database.GetResultRows(c.DriverName(), rows)
	if dataErr != nil {
		recordAudit(user, connId, sqlStr, "failed", 0, startTime, dataErr.Error(), clientIP)
		return nil, sanitizeSQLError(dataErr)
	}

	rspData := &TableDataList{Columns: columnList, Data: data, CanEdit: len(keyIdx) != 0, Keys: keys}
	recordAudit(user, connId, sqlStr, "success", len(data), startTime, "", clientIP)
	return rspData, nil
}

// batchExecResult 是 execBatch 的内部返回。
type batchExecResult struct {
	result *BatchSQLResult
	err    error
}

// execBatch 批量执行 SQL，业务逻辑来自原 exec.go 的 execBatchSQL。
func (s *ExecService) execBatch(ctx context.Context, sqlStr string, c *sqlx.DB, schema, tableName, maxLine string, user *admin.User, connId, authorization string, startTime time.Time, clientIP string) batchExecResult {
	sqlArr := splitSQL(sqlStr)
	if len(sqlArr) == 0 {
		return batchExecResult{err: errors.New("SQL 语句不能为空")}
	}

	permResult := permission.CheckBatchSQLPermission(sqlArr, connId, schema, authorization)
	if permResult != nil {
		return batchExecResult{err: errors.New(permResult.Message)}
	}

	hasWrite := false
	for _, singleSQL := range sqlArr {
		if checkPrefx(singleSQL, []string{"update", "delete", "alter", "drop", "insert", "create", "truncate", "replace", "merge"}) {
			hasWrite = true
			break
		}
	}

	queryCtx, queryCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer queryCancel()

	var tx *sqlx.Tx
	if hasWrite {
		var err error
		tx, err = c.Beginx()
		if err != nil {
			return batchExecResult{err: sanitizeSQLError(err)}
		}
		defer tx.Rollback()
	}

	results := make([]SQLResultItem, 0, len(sqlArr))
	hasError := false
	for _, singleSQL := range sqlArr {
		item := execSingleSQLCore(singleSQL, c, tx, schema, tableName, maxLine, user, connId, authorization, queryCtx)
		results = append(results, *item)
		if item.Status == "error" {
			hasError = true
			if hasWrite {
				break
			}
		}
	}

	if hasWrite && hasError {
		for i := range results {
			if results[i].Status == "success" && results[i].Type == "modify" {
				results[i].Status = "rolled_back"
			}
		}
	}

	if hasWrite && !hasError {
		if err := tx.Commit(); err != nil {
			return batchExecResult{err: sanitizeSQLError(err)}
		}
	}

	totalTime := time.Since(startTime).Milliseconds()
	batchResult := &BatchSQLResult{
		Results:   results,
		TotalTime: totalTime,
	}

	auditStatus := "success"
	auditAffectedRows := 0
	auditErrorMsg := ""
	if hasError {
		auditStatus = "failed"
		for i := range results {
			if results[i].Status == "error" {
				if results[i].AuditError != "" {
					auditErrorMsg = results[i].AuditError
				} else {
					auditErrorMsg = results[i].Error
				}
				break
			}
		}
	}
	for i := range results {
		if results[i].Status == "success" && results[i].Type == "modify" {
			auditAffectedRows += int(results[i].Affected)
		}
	}
	recordAudit(user, connId, sqlStr, auditStatus, auditAffectedRows, startTime, auditErrorMsg, clientIP)

	return batchExecResult{result: batchResult}
}

// recordAudit 记录 SQL 执行审计，与 gin.Context 解耦。
// clientIP 由调用方注入（HTTP 模式从 c.ClientIP() 取，桌面模式传空）。
func recordAudit(user *admin.User, connID, sqlStr, status string, affectedRows int, startTime time.Time, errorMsg, clientIP string) {
	if user == nil {
		return
	}
	execTimeMs := int(time.Since(startTime).Milliseconds())
	sqlType := detectSQLTypeForEditor(sqlStr)
	riskLevel := detectRiskLevelForEditor(sqlStr)

	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:       "sqleditor",
		SQLText:      sqlStr,
		SQLType:      sqlType,
		RiskLevel:    riskLevel,
		Status:       status,
		ConnID:       connID,
		UserID:       user.Id,
		UserName:     user.Name,
		ClientIP:     clientIP,
		AffectedRows: affectedRows,
		ExecTimeMs:   execTimeMs,
		ErrorMsg:     errorMsg,
	})
}

// sanitizeSQLError 对 error 做脱敏与长度截断，保持与原 writeSQLError 行为一致。
func sanitizeSQLError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	msg = sanitize.RedactCredentials(msg)
	if len(msg) > 500 {
		msg = msg[:500] + "..."
	}
	return errors.New(msg)
}

// ExecSQLByService 是包级便捷函数，供 Wails binding 直接调用。
// 与 conn 包级委托函数模式一致（参考 internal/app/conn/conn_service.go）。
func ExecSQLByService(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	return ensureDefaultExec().Exec(ctx, req)
}
