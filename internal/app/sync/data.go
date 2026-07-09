package sync

import (
	"bytes"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/sanitize"
	"websql/internal/pkg/sqlguard"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type DataDiffResult struct {
	TableName    string           `json:"tableName"`
	TotalSource  int              `json:"totalSource"`
	TotalTarget  int              `json:"totalTarget"`
	AddedRows    []map[string]any `json:"addedRows"`
	DeletedRows  []map[string]any `json:"deletedRows"`
	ModifiedRows []ModifiedRow    `json:"modifiedRows"`
	AddCount     int              `json:"addCount"`
	DeleteCount  int              `json:"deleteCount"`
	ModifyCount  int              `json:"modifyCount"`
	Columns      []DataDiffColumn `json:"columns"`
}

type ModifiedRow struct {
	Key       map[string]any `json:"key"`
	Changes   []FieldChange  `json:"changes"`
	SourceRow map[string]any `json:"sourceRow"`
	TargetRow map[string]any `json:"targetRow"`
}

type FieldChange struct {
	ColumnName string `json:"columnName"`
	OldValue   any    `json:"oldValue"`
	NewValue   any    `json:"newValue"`
}

type DataDiffColumn struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Comment string `json:"comment"`
}

const maxCompareRows = 100000

func CompareData(c *gin.Context) {
	connId1 := c.PostForm("sourceConnId")
	connId2 := c.PostForm("targetConnId")
	schema1 := c.PostForm("sourceSchema")
	schema2 := c.PostForm("targetSchema")
	table := c.PostForm("table")
	keyColumnsStr := c.PostForm("keyColumns")
	page := c.DefaultPostForm("page", "1")
	pageSize := c.DefaultPostForm("pageSize", "500")

	authorization := appctx.Ctx.GetAuthorization(c)
	conn1 := conn.GetConn(connId1, authorization)
	conn2 := conn.GetConn(connId2, authorization)

	if conn1 == nil {
		response.WriteOK(c, map[string]any{
			"error": "源数据库连接不可用，请检查连接配置或权限",
		})
		return
	}
	if conn2 == nil {
		response.WriteOK(c, map[string]any{
			"error": "目标数据库连接不可用，请检查连接配置或权限",
		})
		return
	}

	dbType1 := conn1.DriverName()
	dbType2 := conn2.DriverName()
	if dbType1 != dbType2 {
		response.WriteOK(c, map[string]any{
			"error": fmt.Sprintf("不允许跨数据库类型比较数据: 源=%s, 目标=%s", dbType1, dbType2),
		})
		return
	}
	dbType := dbType1

	if table == "" {
		response.WriteOK(c, map[string]any{
			"error": "表名不能为空",
		})
		return
	}

	if !isValidIdentifier(table) {
		response.WriteOK(c, map[string]any{
			"error": "表名包含非法字符",
		})
		return
	}

	// 权限校验：检查用户对源表和目标表的读权限
	if config.Get().IsRemote {
		permission.CheckTablePermission(connId1, schema1, table, authorization)
		permission.CheckTablePermission(connId2, schema2, table, authorization)
	}

	sourceCount := getRowCount(conn1, dbType, schema1, table)
	targetCount := getRowCount(conn2, dbType, schema2, table)
	if sourceCount > maxCompareRows || targetCount > maxCompareRows {
		response.WriteOK(c, map[string]any{
			"error": fmt.Sprintf("数据量过大，源表%d行，目标%d行，上限%d行，请使用数据导出功能", sourceCount, targetCount, maxCompareRows),
		})
		return
	}

	keyColumns := getKeyColumns(conn1, dbType, schema1, table, keyColumnsStr)

	columns := getTableColumnsDetail(conn1, dbType, schema1, table)

	sourceSQL := buildSelectSQL(dbType, schema1, table)
	targetSQL := buildSelectSQL(dbType, schema2, table)

	sourceData := queryAllData(conn1, dbType, sourceSQL)
	targetData := queryAllData(conn2, dbType, targetSQL)

	if len(keyColumns) == 0 {
		keyColumns = findCommonColumns(sourceData, targetData)
		if len(keyColumns) == 0 {
			response.WriteOK(c, map[string]any{
				"error": "无法确定比较键列，请确保表有主键",
			})
			return
		}
	}

	sourceMap := buildRowMap(sourceData, keyColumns)
	targetMap := buildRowMap(targetData, keyColumns)

	result := &DataDiffResult{
		TableName:    table,
		TotalSource:  len(sourceData),
		TotalTarget:  len(targetData),
		AddedRows:    make([]map[string]any, 0),
		DeletedRows:  make([]map[string]any, 0),
		ModifiedRows: make([]ModifiedRow, 0),
		Columns:      columns,
	}

	for key, srcRow := range sourceMap {
		if tgtRow, ok := targetMap[key]; ok {
			changes := diffRows(srcRow, tgtRow, keyColumns)
			if len(changes) > 0 {
				keyMap := make(map[string]any)
				for _, kc := range keyColumns {
					keyMap[kc] = srcRow[kc]
				}
				result.ModifiedRows = append(result.ModifiedRows, ModifiedRow{
					Key:       keyMap,
					Changes:   changes,
					SourceRow: srcRow,
					TargetRow: tgtRow,
				})
			}
		} else {
			result.AddedRows = append(result.AddedRows, srcRow)
		}
	}

	for key, tgtRow := range targetMap {
		if _, ok := sourceMap[key]; !ok {
			result.DeletedRows = append(result.DeletedRows, tgtRow)
		}
	}

	result.AddCount = len(result.AddedRows)
	result.DeleteCount = len(result.DeletedRows)
	result.ModifyCount = len(result.ModifiedRows)

	sort.Slice(result.ModifiedRows, func(i, j int) bool {
		ki := ""
		for _, kc := range keyColumns {
			if v, ok := result.ModifiedRows[i].Key[kc]; ok {
				ki += fmt.Sprintf("%v", v)
			}
		}
		kj := ""
		for _, kc := range keyColumns {
			if v, ok := result.ModifiedRows[j].Key[kc]; ok {
				kj += fmt.Sprintf("%v", v)
			}
		}
		return ki < kj
	})

	pagedResult := applyPagination(result, page, pageSize)

	response.WriteOK(c, pagedResult)
}

func applyPagination(result *DataDiffResult, pageStr, pageSizeStr string) map[string]any {
	pageNum := 1
	pageSize := 500
	if p, ok := parseStrToInt(pageStr); ok {
		pageNum = p
	}
	if ps, ok := parseStrToInt(pageSizeStr); ok {
		pageSize = ps
	}

	totalChanges := result.AddCount + result.DeleteCount + result.ModifyCount
	totalPages := (totalChanges + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	start := (pageNum - 1) * pageSize
	end := start + pageSize

	pagedAdded := createPagedSlice(result.AddedRows, &start, &end)
	pagedDeleted := createPagedSlice(result.DeletedRows, &start, &end)
	pagedModified := createPagedSliceForModified(result.ModifiedRows, &start, &end)

	remaining := end - start
	if remaining < 0 {
		remaining = 0
	}

	return map[string]any{
		"tableName":    result.TableName,
		"totalSource":  result.TotalSource,
		"totalTarget":  result.TotalTarget,
		"addedRows":    pagedAdded,
		"deletedRows":  pagedDeleted,
		"modifiedRows": pagedModified,
		"addCount":     result.AddCount,
		"deleteCount":  result.DeleteCount,
		"modifyCount":  result.ModifyCount,
		"columns":      result.Columns,
		"page":         pageNum,
		"pageSize":     pageSize,
		"totalPages":   totalPages,
		"totalChanges": totalChanges,
		"remaining":    remaining,
	}
}

func createPagedSlice(data []map[string]any, start, end *int) []map[string]any {
	if len(data) == 0 {
		return nil
	}
	if *start >= len(data) {
		*start -= len(data)
		*end -= len(data)
		return nil
	}
	sliceEnd := *end
	if sliceEnd > len(data) {
		sliceEnd = len(data)
	}
	result := data[*start:sliceEnd]
	*start = 0
	*end -= len(data)
	return result
}

func createPagedSliceForModified(data []ModifiedRow, start, end *int) []ModifiedRow {
	if len(data) == 0 {
		return nil
	}
	if *start >= len(data) {
		*start -= len(data)
		*end -= len(data)
		return nil
	}
	sliceEnd := *end
	if sliceEnd > len(data) {
		sliceEnd = len(data)
	}
	result := data[*start:sliceEnd]
	*start = 0
	*end -= len(data)
	return result
}

func parseStrToInt(s string) (int, bool) {
	var result int
	n, _ := fmt.Sscanf(s, "%d", &result)
	return result, n == 1
}

func buildRowMap(data []map[string]any, keyColumns []string) map[string]map[string]any {
	result := make(map[string]map[string]any)
	for _, row := range data {
		key := buildRowKey(row, keyColumns)
		result[key] = row
	}
	return result
}

func buildRowKey(row map[string]any, keyColumns []string) string {
	var parts []string
	for _, kc := range keyColumns {
		parts = append(parts, fmt.Sprintf("%v", row[kc]))
	}
	return strings.Join(parts, "\x00")
}

func diffRows(source, target map[string]any, keyColumns []string) []FieldChange {
	keySet := make(map[string]bool)
	for _, kc := range keyColumns {
		keySet[kc] = true
	}

	changes := make([]FieldChange, 0)
	for col, srcVal := range source {
		if keySet[col] {
			continue
		}
		tgtVal, ok := target[col]
		if !ok {
			continue
		}
		srcStr := fmt.Sprintf("%v", srcVal)
		tgtStr := fmt.Sprintf("%v", tgtVal)
		if srcStr != tgtStr {
			changes = append(changes, FieldChange{
				ColumnName: col,
				OldValue:   tgtVal,
				NewValue:   srcVal,
			})
		}
	}
	return changes
}

func getKeyColumns(conn *sqlx.DB, dbType, schema, table string, explicitKeys string) []string {
	if explicitKeys != "" {
		return strings.Split(explicitKeys, ",")
	}

	var rows *sqlx.Rows
	var err error
	switch dbType {
	case "oracle":
		rows, err = conn.Queryx("SELECT b.COLUMN_NAME FROM user_constraints a LEFT JOIN user_cons_columns b ON a.TABLE_NAME = b.TABLE_NAME WHERE a.TABLE_NAME = :1 AND CONSTRAINT_TYPE = 'P'", table)
	default:
		rows, err = conn.Queryx("SELECT column_name FROM information_schema.columns WHERE TABLE_SCHEMA = ? AND table_name = ? AND column_key = 'PRI'", schema, table)
	}
	if err != nil {
		logger.PrintErrf("获取主键失败", err)
		return nil
	}
	defer rows.Close()

	keys := make([]string, 0)
	for rows.Next() {
		var col string
		rows.Scan(&col)
		keys = append(keys, strings.TrimSpace(col))
	}
	return keys
}

func getTableColumnsDetail(conn *sqlx.DB, dbType, schema, table string) []DataDiffColumn {
	var rows *sqlx.Rows
	var err error
	switch dbType {
	case "oracle":
		rows, err = conn.Queryx("SELECT COLUMN_NAME, DATA_TYPE, '' AS COLUMN_COMMENT FROM USER_TAB_COLUMNS WHERE TABLE_NAME = :1 ORDER BY COLUMN_ID", table)
	default:
		rows, err = conn.Queryx("SELECT column_name, data_type, column_comment FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION", schema, table)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	columns := make([]DataDiffColumn, 0)
	for rows.Next() {
		var col DataDiffColumn
		rows.Scan(&col.Name, &col.Type, &col.Comment)
		columns = append(columns, col)
	}
	return columns
}

func queryAllData(conn *sqlx.DB, dbType string, sql string) []map[string]any {
	rows, err := conn.Queryx(sql)
	if err != nil {
		logger.PrintErrf("数据查询失败", err)
		return nil
	}
	defer rows.Close()
	data, err := database.GetResultRows(dbType, rows)
	if err != nil {
		logger.PrintErrf("数据查询失败", err)
		return nil
	}
	return data
}

func findCommonColumns(data1, data2 []map[string]any) []string {
	colSet1 := make(map[string]bool)
	colSet2 := make(map[string]bool)

	if len(data1) > 0 {
		for k := range data1[0] {
			colSet1[k] = true
		}
	}
	if len(data2) > 0 {
		for k := range data2[0] {
			colSet2[k] = true
		}
	}

	common := make([]string, 0)
	for col := range colSet1 {
		if colSet2[col] {
			common = append(common, col)
		}
	}
	sort.Strings(common)
	return common
}

func escapeSQLValue(v any) string {
	if v == nil {
		return "NULL"
	}
	s := fmt.Sprintf("%v", v)
	if s == "<nil>" {
		return "NULL"
	}
	// 转义单引号（SQL 标准）
	s = strings.ReplaceAll(s, "'", "''")
	// 转义反斜杠（MySQL 兼容）
	s = strings.ReplaceAll(s, "\\", "\\\\")
	// 转义换行和制表符
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	// 转义 NULL 字节
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

func ApplyDataSync(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.PostForm("schema")
	sqlStr := c.PostForm("sql")
	// 同步会话 ID（前端生成），用于关联回滚日志。为空则不记录回滚信息。
	syncSessionId := c.PostForm("syncSessionId")

	authorization := appctx.Ctx.GetAuthorization(c)
	dbConn := conn.GetConn(connId, authorization)

	if dbConn == nil {
		response.WriteOK(c, map[string]any{"success": false, "message": "数据库连接不可用，请检查连接配置或权限"})
		return
	}
	dbType := dbConn.DriverName()

	if strings.TrimSpace(sqlStr) == "" {
		response.WriteOK(c, map[string]any{"success": false, "message": "SQL不能为空"})
		return
	}

	// 获取当前用户信息用于审计
	userVal, _ := c.Get("currentUser")
	user, _ := userVal.(*admin.User)

	sqlList := strings.Split(sqlStr, ";")
	validatedSQLs := make([]string, 0, len(sqlList))
	for _, s := range sqlList {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if err := validateDataSQL(s); err != nil {
			response.WriteOK(c, map[string]any{"success": false, "message": err.Error()})
			return
		}
		validatedSQLs = append(validatedSQLs, s)
	}

	// 权限校验：对每条 SQL 做表级/列级权限检查
	if config.Get().IsRemote {
		// 检查用户是否有写权限
		if !permission.CheckUserCanModify(authorization) {
			response.WriteOK(c, map[string]any{"success": false, "message": "当前角色禁止修改数据，无法执行同步操作"})
			return
		}
		for _, s := range validatedSQLs {
			analysis := permission.AnalyzeSQL(s, schema)
			permResult := permission.CheckAnalysisPermission(analysis, connId, authorization)
			if !permResult.Allowed {
				response.WriteOK(c, map[string]any{"success": false, "message": permResult.Message})
				return
			}
		}
	}

	startTime := time.Now()

	tx, err := dbConn.Beginx()
	if err != nil {
		response.WriteOK(c, map[string]any{"success": false, "message": fmt.Sprintf("开启事务失败: %v", err)})
		return
	}

	insertCount := 0
	updateCount := 0
	deleteCount := 0
	errors := make([]string, 0)
	// 仅在提供 syncSessionId 时记录回滚日志（避免无谓的 SELECT 开销）
	recordUndo := syncSessionId != ""
	executedOriginals := make([]string, 0, len(validatedSQLs))
	undoSQLs := make([]string, 0, len(validatedSQLs))

	for _, s := range validatedSQLs {
		upper := strings.ToUpper(s)
		// 执行前生成撤销 SQL（UPDATE/DELETE 会先 SELECT 旧值）。
		// 仅对成功执行的语句保留撤销 SQL，失败语句不记录。
		var undo string
		if recordUndo {
			undo = generateUndoSQL(s, dbConn, dbType)
		}
		result, err := tx.Exec(s)
		if err != nil {
			errors = append(errors, fmt.Sprintf("执行失败: %s - %s", s[:minStr(80, s)], err.Error()))
			continue
		}
		if recordUndo {
			executedOriginals = append(executedOriginals, s)
			undoSQLs = append(undoSQLs, undo)
		}
		affected, _ := result.RowsAffected()
		if strings.HasPrefix(upper, "INSERT") {
			insertCount += int(affected)
		} else if strings.HasPrefix(upper, "UPDATE") {
			updateCount += int(affected)
		} else if strings.HasPrefix(upper, "DELETE") {
			deleteCount += int(affected)
		}
	}

	committed := false
	if len(errors) > 0 {
		tx.Rollback()
	} else {
		if err := tx.Commit(); err == nil {
			committed = true
		}
	}

	// 事务提交成功后，将本批次的撤销 SQL 追加到会话回滚日志（30 分钟后自动清理）
	if committed && recordUndo && len(executedOriginals) > 0 {
		rollbackLog := getOrCreateRollbackLog(syncSessionId, connId, schema, dbType)
		appendRollbackEntries(rollbackLog, executedOriginals, undoSQLs)
	}

	totalAffected := insertCount + updateCount + deleteCount
	execTimeMs := int(time.Since(startTime).Milliseconds())

	// 审计日志
	auditStatus := "success"
	auditError := ""
	if len(errors) > 0 {
		auditStatus = "failed"
		auditError = strings.Join(errors, "; ")
		if len(auditError) > 500 {
			auditError = auditError[:500] + "..."
		}
	}
	if user != nil {
		audit.GetAuditService().Record(&audit.AuditEntry{
			Source:       "datasync",
			SQLText:      fmt.Sprintf("[DataSync] %d statements (INSERT:%d UPDATE:%d DELETE:%d)", len(validatedSQLs), insertCount, updateCount, deleteCount),
			SQLType:      "SYNC",
			RiskLevel:    "medium",
			Status:       auditStatus,
			ConnID:       connId,
			SchemaName:   schema,
			UserID:       user.Id,
			UserName:     user.Name,
			ClientIP:     c.ClientIP(),
			AffectedRows: totalAffected,
			ExecTimeMs:   execTimeMs,
			ErrorMsg:     auditError,
		})
	}

	log.Printf("[SyncAudit] ApplyDataSync connId=%s schema=%s sqlCount=%d insert=%d update=%d delete=%d success=%v user=%s",
		connId, schema, len(validatedSQLs), insertCount, updateCount, deleteCount, len(errors) == 0, authorization)

	response.WriteOK(c, map[string]any{
		"success":     len(errors) == 0,
		"sessionId":   syncSessionId,
		"insertCount": insertCount,
		"updateCount": updateCount,
		"deleteCount": deleteCount,
		"errorCount":  len(errors),
		"errors":      errors,
	})
}

func validateDataSQL(sql string) error {
	return sqlguard.ValidateDataSQL(sql)
}

func GenerateSyncSQL(c *gin.Context) {
	connId1 := c.PostForm("sourceConnId")
	schema1 := c.PostForm("sourceSchema")
	connId2 := c.PostForm("targetConnId")
	schema2 := c.PostForm("targetSchema")
	table := c.PostForm("table")
	syncDirection := c.DefaultPostForm("direction", "source_to_target")
	// 冲突处理策略（默认 update）。详见 handler.go 中的策略常量。
	conflictStrategy := c.DefaultPostForm("conflictStrategy", StrategyUpdate)

	authorization := appctx.Ctx.GetAuthorization(c)
	conn1 := conn.GetConn(connId1, authorization)
	conn2 := conn.GetConn(connId2, authorization)

	if conn1 == nil {
		response.WriteOK(c, map[string]any{"error": "源数据库连接不可用，请检查连接配置或权限"})
		return
	}
	if conn2 == nil {
		response.WriteOK(c, map[string]any{"error": "目标数据库连接不可用，请检查连接配置或权限"})
		return
	}

	dbType := conn1.DriverName()

	if !isValidIdentifier(table) {
		response.WriteOK(c, map[string]any{
			"error": "表名包含非法字符",
		})
		return
	}

	// 权限校验
	if config.Get().IsRemote {
		permission.CheckTablePermission(connId1, schema1, table, authorization)
		permission.CheckTablePermission(connId2, schema2, table, authorization)
	}

	sourceCount := getRowCount(conn1, dbType, schema1, table)
	targetCount := getRowCount(conn2, dbType, schema2, table)
	if sourceCount > maxCompareRows || targetCount > maxCompareRows {
		response.WriteOK(c, map[string]any{
			"error": fmt.Sprintf("数据量过大，源表%d行，目标%d行，上限%d行", sourceCount, targetCount, maxCompareRows),
		})
		return
	}

	sourceMap, targetMap, keyColumns := buildSyncData(conn1, conn2, dbType, schema1, schema2, table, syncDirection)

	sqlBuf := new(bytes.Buffer)
	qi := newQuoteInfo(dbType)

	// 统一处理两个方向：fromMap 为"数据源（真相来源）"，toMap 为"被对齐的一端"，
	// writeSchema 为实际写入目标 schema。
	var fromMap, toMap map[string]map[string]any
	var writeSchema string
	if syncDirection == "source_to_target" {
		fromMap, toMap, writeSchema = sourceMap, targetMap, schema2
	} else {
		fromMap, toMap, writeSchema = targetMap, sourceMap, schema1
	}

	// 新增/修改：遍历数据源行
	for key, fromRow := range fromMap {
		if toRow, ok := toMap[key]; ok {
			// 目标已存在：比对差异，skip 策略下不生成 UPDATE
			changes := diffRows(fromRow, toRow, keyColumns)
			if len(changes) > 0 && conflictStrategy != StrategySkip {
				sqlBuf.WriteString(buildUpdateStmt(dbType, writeSchema, table, changes, keyColumns, fromRow, qi))
				sqlBuf.WriteString("\n")
			}
		} else {
			// 目标不存在：按冲突策略生成 INSERT
			sqlBuf.WriteString(buildInsertStmt(conflictStrategy, dbType, writeSchema, table, fromRow, keyColumns, qi))
			sqlBuf.WriteString("\n")
		}
	}
	// 删除：目标多余行（不属于冲突，所有策略都生成 DELETE）
	for key, toRow := range toMap {
		if _, ok := fromMap[key]; !ok {
			sqlBuf.WriteString(buildDeleteStmt(dbType, writeSchema, table, toRow, keyColumns, qi))
			sqlBuf.WriteString("\n")
		}
	}

	response.WriteOK(c, map[string]any{
		"sql": sqlBuf.String(),
	})
}

func buildSyncData(conn1, conn2 *sqlx.DB, dbType, schema1, schema2, table, direction string) (map[string]map[string]any, map[string]map[string]any, []string) {
	keyColumns := getKeyColumns(conn1, dbType, schema1, table, "")

	sourceSQL := buildSelectSQL(dbType, schema1, table)
	targetSQL := buildSelectSQL(dbType, schema2, table)

	sourceData := queryAllData(conn1, dbType, sourceSQL)
	targetData := queryAllData(conn2, dbType, targetSQL)

	if len(keyColumns) == 0 {
		keyColumns = findCommonColumns(sourceData, targetData)
	}

	sourceMap := buildRowMap(sourceData, keyColumns)
	targetMap := buildRowMap(targetData, keyColumns)

	return sourceMap, targetMap, keyColumns
}

func minStr(a int, s string) int {
	if a < len(s) {
		return a
	}
	return len(s)
}

type quoteInfo struct {
	col  string
	colR string
}

func newQuoteInfo(dbType string) *quoteInfo {
	switch dbType {
	case "oracle":
		return &quoteInfo{col: `"`, colR: `"`}
	default:
		return &quoteInfo{col: "`", colR: "`"}
	}
}

func buildSelectSQL(dbType, schema, table string) string {
	switch dbType {
	case "oracle":
		return fmt.Sprintf("SELECT * FROM \"%s\"", table)
	default:
		return fmt.Sprintf("SELECT * FROM `%s`.`%s`", schema, table)
	}
}

func getRowCount(conn *sqlx.DB, dbType, schema, table string) int {
	if !sanitize.IsValidIdentifier(table) {
		return 0
	}
	var count int
	switch dbType {
	case "oracle":
		conn.Get(&count, fmt.Sprintf("SELECT COUNT(*) FROM \"%s\"", table))
	default:
		if !sanitize.IsValidIdentifier(schema) {
			return 0
		}
		conn.Get(&count, fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", schema, table))
	}
	return count
}

func CompareDataChunked(c *gin.Context) {
	connId1 := c.PostForm("sourceConnId")
	connId2 := c.PostForm("targetConnId")
	schema1 := c.PostForm("sourceSchema")
	schema2 := c.PostForm("targetSchema")
	table := c.PostForm("table")
	keyColumnsStr := c.PostForm("keyColumns")
	chunkSizeStr := c.DefaultPostForm("chunkSize", "5000")
	chunkIndexStr := c.DefaultPostForm("chunkIndex", "0")
	direction := c.DefaultPostForm("direction", "source_to_target")
	generateSQLFlag := c.DefaultPostForm("generateSQL", "false")
	phase := c.DefaultPostForm("phase", "compare")
	// 冲突处理策略（默认 update）。生成 SQL 时按此策略产出 INSERT 语句。
	conflictStrategy := c.DefaultPostForm("conflictStrategy", StrategyUpdate)

	authorization := appctx.Ctx.GetAuthorization(c)
	conn1 := conn.GetConn(connId1, authorization)
	conn2 := conn.GetConn(connId2, authorization)

	if conn1 == nil {
		response.WriteOK(c, map[string]any{"error": "源数据库连接不可用，请检查连接配置或权限"})
		return
	}
	if conn2 == nil {
		response.WriteOK(c, map[string]any{"error": "目标数据库连接不可用，请检查连接配置或权限"})
		return
	}

	dbType := conn1.DriverName()

	if table == "" {
		response.WriteOK(c, map[string]any{"error": "表名不能为空"})
		return
	}
	if !isValidIdentifier(table) {
		response.WriteOK(c, map[string]any{"error": "表名包含非法字符"})
		return
	}

	keyColumns := getKeyColumns(conn1, dbType, schema1, table, keyColumnsStr)
	if len(keyColumns) == 0 {
		response.WriteOK(c, map[string]any{"error": "无法确定比较键列，请确保表有主键或指定keyColumns参数"})
		return
	}

	columns := getTableColumnsDetail(conn1, dbType, schema1, table)
	sourceCount := getRowCount(conn1, dbType, schema1, table)
	targetCount := getRowCount(conn2, dbType, schema2, table)

	chunkSize := 5000
	if cs, ok := parseStrToInt(chunkSizeStr); ok && cs > 0 {
		chunkSize = cs
	}
	chunkIndex := 0
	if ci, ok := parseStrToInt(chunkIndexStr); ok && ci >= 0 {
		chunkIndex = ci
	}

	qi := newQuoteInfo(dbType)

	var srcConn, tgtConn *sqlx.DB
	var srcSchema, tgtSchema string
	if direction == "target_to_source" {
		srcConn = conn2
		tgtConn = conn1
		srcSchema = schema2
		tgtSchema = schema1
	} else {
		srcConn = conn1
		tgtConn = conn2
		srcSchema = schema1
		tgtSchema = schema2
	}

	if phase == "out_of_range" {
		handleOutOfRangeDeletions(c, srcConn, tgtConn, dbType, srcSchema, tgtSchema, table, keyColumns, qi, direction, generateSQLFlag, conflictStrategy, chunkSize, chunkIndex, sourceCount, targetCount, columns)
		return
	}

	totalChunks := (sourceCount + chunkSize - 1) / chunkSize
	if totalChunks == 0 {
		totalChunks = 1
	}

	startKey := getChunkStartKey(srcConn, dbType, srcSchema, table, keyColumns, chunkIndex*chunkSize, qi)
	endKey := getChunkStartKey(srcConn, dbType, srcSchema, table, keyColumns, (chunkIndex+1)*chunkSize, qi)

	sourceData := queryKeyRangeData(srcConn, dbType, srcSchema, table, keyColumns, startKey, endKey, qi)
	targetData := queryKeyRangeData(tgtConn, dbType, tgtSchema, table, keyColumns, startKey, endKey, qi)

	sourceMap := buildRowMap(sourceData, keyColumns)
	targetMap := buildRowMap(targetData, keyColumns)

	addedRows := make([]map[string]any, 0)
	deletedRows := make([]map[string]any, 0)
	modifiedRows := make([]ModifiedRow, 0)

	for key, srcRow := range sourceMap {
		if tgtRow, ok := targetMap[key]; ok {
			changes := diffRows(srcRow, tgtRow, keyColumns)
			if len(changes) > 0 {
				keyMap := make(map[string]any)
				for _, kc := range keyColumns {
					keyMap[kc] = srcRow[kc]
				}
				modifiedRows = append(modifiedRows, ModifiedRow{
					Key:       keyMap,
					Changes:   changes,
					SourceRow: srcRow,
					TargetRow: tgtRow,
				})
			}
		} else {
			addedRows = append(addedRows, srcRow)
		}
	}

	for key, tgtRow := range targetMap {
		if _, ok := sourceMap[key]; !ok {
			deletedRows = append(deletedRows, tgtRow)
		}
	}

	var sqlStr string
	if generateSQLFlag == "true" {
		sqlStr = generateChunkSQLWithStrategy(addedRows, deletedRows, modifiedRows, keyColumns, tgtSchema, table, qi, conflictStrategy, dbType)
	}

	hasMore := chunkIndex < totalChunks-1

	response.WriteOK(c, map[string]any{
		"tableName":    table,
		"totalSource":  sourceCount,
		"totalTarget":  targetCount,
		"keyColumns":   keyColumns,
		"chunkIndex":   chunkIndex,
		"chunkSize":    chunkSize,
		"totalChunks":  totalChunks,
		"hasMore":      hasMore,
		"addedRows":    addedRows,
		"deletedRows":  deletedRows,
		"modifiedRows": modifiedRows,
		"addCount":     len(addedRows),
		"deleteCount":  len(deletedRows),
		"modifyCount":  len(modifiedRows),
		"columns":      columns,
		"sql":          sqlStr,
	})
}

func handleOutOfRangeDeletions(c *gin.Context, srcConn, tgtConn *sqlx.DB, dbType, srcSchema, tgtSchema, table string, keyColumns []string, qi *quoteInfo, direction, generateSQLFlag, conflictStrategy string, chunkSize, chunkIndex, sourceCount, targetCount int, columns []DataDiffColumn) {
	if sourceCount == 0 {
		offset := chunkIndex * chunkSize
		var pagedSQL string
		switch dbType {
		case "oracle":
			pagedSQL = fmt.Sprintf("SELECT * FROM (SELECT a.*, ROWNUM rn FROM (SELECT * FROM \"%s\" ORDER BY %s) a WHERE ROWNUM <= %d) WHERE rn > %d", table, buildKeyOrderBy(keyColumns, qi), offset+chunkSize, offset)
		default:
			pagedSQL = fmt.Sprintf("SELECT * FROM `%s`.`%s` ORDER BY %s LIMIT %d OFFSET %d", tgtSchema, table, buildKeyOrderBy(keyColumns, qi), chunkSize, offset)
		}
		allData := queryAllData(tgtConn, dbType, pagedSQL)
		hasMore := offset+chunkSize < targetCount
		var sqlStr string
		if generateSQLFlag == "true" && len(allData) > 0 {
			sqlStr = generateChunkSQLWithStrategy(nil, allData, nil, keyColumns, tgtSchema, table, qi, conflictStrategy, dbType)
		}
		response.WriteOK(c, map[string]any{
			"tableName":    table,
			"totalSource":  sourceCount,
			"totalTarget":  targetCount,
			"keyColumns":   keyColumns,
			"chunkIndex":   chunkIndex,
			"chunkSize":    chunkSize,
			"hasMore":      hasMore,
			"addedRows":    []map[string]any{},
			"deletedRows":  allData,
			"modifiedRows": []ModifiedRow{},
			"addCount":     0,
			"deleteCount":  len(allData),
			"modifyCount":  0,
			"columns":      columns,
			"sql":          sqlStr,
		})
		return
	}

	minKey := getChunkStartKey(srcConn, dbType, srcSchema, table, keyColumns, 0, qi)
	maxKey := getLastKey(srcConn, dbType, srcSchema, table, keyColumns, qi)

	var whereParts []string
	if minKey != nil {
		whereParts = append(whereParts, fmt.Sprintf("(%s) < (%s)", buildKeyColsTuple(keyColumns, qi), buildKeyValueTuple(keyColumns, minKey, qi)))
	}
	if maxKey != nil {
		whereParts = append(whereParts, fmt.Sprintf("(%s) > (%s)", buildKeyColsTuple(keyColumns, qi), buildKeyValueTuple(keyColumns, maxKey, qi)))
	}

	if len(whereParts) == 0 {
		response.WriteOK(c, map[string]any{
			"tableName":    table,
			"totalSource":  sourceCount,
			"totalTarget":  targetCount,
			"keyColumns":   keyColumns,
			"chunkIndex":   chunkIndex,
			"chunkSize":    chunkSize,
			"hasMore":      false,
			"addedRows":    []map[string]any{},
			"deletedRows":  []map[string]any{},
			"modifiedRows": []ModifiedRow{},
			"addCount":     0,
			"deleteCount":  0,
			"modifyCount":  0,
			"columns":      columns,
			"sql":          "",
		})
		return
	}

	whereClause := strings.Join(whereParts, " OR ")
	offset := chunkIndex * chunkSize

	var pagedSQL string
	switch dbType {
	case "oracle":
		pagedSQL = fmt.Sprintf("SELECT * FROM (SELECT a.*, ROWNUM rn FROM (SELECT * FROM \"%s\" WHERE %s ORDER BY %s) a WHERE ROWNUM <= %d) WHERE rn > %d", table, whereClause, buildKeyOrderBy(keyColumns, qi), offset+chunkSize, offset)
	default:
		pagedSQL = fmt.Sprintf("SELECT * FROM `%s`.`%s` WHERE %s ORDER BY %s LIMIT %d OFFSET %d", tgtSchema, table, whereClause, buildKeyOrderBy(keyColumns, qi), chunkSize, offset)
	}

	outOfRangeData := queryAllData(tgtConn, dbType, pagedSQL)

	var outOfRangeCount int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s` WHERE %s", tgtSchema, table, whereClause)
	if dbType == "oracle" {
		countSQL = fmt.Sprintf("SELECT COUNT(*) FROM \"%s\" WHERE %s", table, whereClause)
	}
	tgtConn.Get(&outOfRangeCount, countSQL)

	hasMore := offset+chunkSize < outOfRangeCount

	var sqlStr string
	if generateSQLFlag == "true" && len(outOfRangeData) > 0 {
		sqlStr = generateChunkSQLWithStrategy(nil, outOfRangeData, nil, keyColumns, tgtSchema, table, qi, conflictStrategy, dbType)
	}

	response.WriteOK(c, map[string]any{
		"tableName":    table,
		"totalSource":  sourceCount,
		"totalTarget":  targetCount,
		"keyColumns":   keyColumns,
		"chunkIndex":   chunkIndex,
		"chunkSize":    chunkSize,
		"hasMore":      hasMore,
		"addedRows":    []map[string]any{},
		"deletedRows":  outOfRangeData,
		"modifiedRows": []ModifiedRow{},
		"addCount":     0,
		"deleteCount":  len(outOfRangeData),
		"modifyCount":  0,
		"columns":      columns,
		"sql":          sqlStr,
	})
}

func getChunkStartKey(conn *sqlx.DB, dbType, schema, table string, keyColumns []string, offset int, qi *quoteInfo) map[string]any {
	if offset <= 0 {
		return nil
	}
	orderBy := buildKeyOrderBy(keyColumns, qi)
	selectCols := buildKeySelectCols(keyColumns, qi)
	var sql string
	switch dbType {
	case "oracle":
		sql = fmt.Sprintf("SELECT %s FROM \"%s\" ORDER BY %s OFFSET %d ROWS FETCH NEXT 1 ROWS ONLY", selectCols, table, orderBy, offset)
	default:
		sql = fmt.Sprintf("SELECT %s FROM `%s`.`%s` ORDER BY %s LIMIT 1 OFFSET %d", selectCols, schema, table, orderBy, offset)
	}
	rows, err := conn.Queryx(sql)
	if err != nil {
		return nil
	}
	defer rows.Close()
	if rows.Next() {
		result := make(map[string]any)
		if err := rows.MapScan(result); err != nil {
			return nil
		}
		return result
	}
	return nil
}

func getLastKey(conn *sqlx.DB, dbType, schema, table string, keyColumns []string, qi *quoteInfo) map[string]any {
	orderBy := buildKeyOrderBy(keyColumns, qi)
	selectCols := buildKeySelectCols(keyColumns, qi)
	var sql string
	switch dbType {
	case "oracle":
		sql = fmt.Sprintf("SELECT %s FROM \"%s\" ORDER BY %s DESC OFFSET 0 ROWS FETCH NEXT 1 ROWS ONLY", selectCols, table, orderBy)
	default:
		sql = fmt.Sprintf("SELECT %s FROM `%s`.`%s` ORDER BY %s DESC LIMIT 1", selectCols, schema, table, orderBy)
	}
	rows, err := conn.Queryx(sql)
	if err != nil {
		return nil
	}
	defer rows.Close()
	if rows.Next() {
		result := make(map[string]any)
		if err := rows.MapScan(result); err != nil {
			return nil
		}
		return result
	}
	return nil
}

func queryKeyRangeData(conn *sqlx.DB, dbType, schema, table string, keyColumns []string, startKey, endKey map[string]any, qi *quoteInfo) []map[string]any {
	whereClause := buildKeyRangeWhere(keyColumns, startKey, endKey, qi)
	orderBy := buildKeyOrderBy(keyColumns, qi)
	var sql string
	switch dbType {
	case "oracle":
		if whereClause != "" {
			sql = fmt.Sprintf("SELECT * FROM \"%s\" WHERE %s ORDER BY %s", table, whereClause, orderBy)
		} else {
			sql = fmt.Sprintf("SELECT * FROM \"%s\" ORDER BY %s", table, orderBy)
		}
	default:
		if whereClause != "" {
			sql = fmt.Sprintf("SELECT * FROM `%s`.`%s` WHERE %s ORDER BY %s", schema, table, whereClause, orderBy)
		} else {
			sql = fmt.Sprintf("SELECT * FROM `%s`.`%s` ORDER BY %s", schema, table, orderBy)
		}
	}
	return queryAllData(conn, dbType, sql)
}

func buildKeyRangeWhere(keyColumns []string, startKey, endKey map[string]any, qi *quoteInfo) string {
	if startKey == nil && endKey == nil {
		return ""
	}
	var conditions []string
	if startKey != nil {
		if len(keyColumns) == 1 {
			col := keyColumns[0]
			conditions = append(conditions, fmt.Sprintf("%s%s%s >= '%s'", qi.col, col, qi.colR, escapeSQLValue(startKey[col])))
		} else {
			conditions = append(conditions, fmt.Sprintf("(%s) >= (%s)", buildKeyColsTuple(keyColumns, qi), buildKeyValueTuple(keyColumns, startKey, qi)))
		}
	}
	if endKey != nil {
		if len(keyColumns) == 1 {
			col := keyColumns[0]
			conditions = append(conditions, fmt.Sprintf("%s%s%s < '%s'", qi.col, col, qi.colR, escapeSQLValue(endKey[col])))
		} else {
			conditions = append(conditions, fmt.Sprintf("(%s) < (%s)", buildKeyColsTuple(keyColumns, qi), buildKeyValueTuple(keyColumns, endKey, qi)))
		}
	}
	return strings.Join(conditions, " AND ")
}

func buildKeyOrderBy(keyColumns []string, qi *quoteInfo) string {
	parts := make([]string, len(keyColumns))
	for i, col := range keyColumns {
		parts[i] = fmt.Sprintf("%s%s%s", qi.col, col, qi.colR)
	}
	return strings.Join(parts, ", ")
}

func buildKeySelectCols(keyColumns []string, qi *quoteInfo) string {
	parts := make([]string, len(keyColumns))
	for i, col := range keyColumns {
		parts[i] = fmt.Sprintf("%s%s%s", qi.col, col, qi.colR)
	}
	return strings.Join(parts, ", ")
}

func buildKeyColsTuple(keyColumns []string, qi *quoteInfo) string {
	parts := make([]string, len(keyColumns))
	for i, col := range keyColumns {
		parts[i] = fmt.Sprintf("%s%s%s", qi.col, col, qi.colR)
	}
	return strings.Join(parts, ", ")
}

func buildKeyValueTuple(keyColumns []string, keyValues map[string]any, qi *quoteInfo) string {
	parts := make([]string, len(keyColumns))
	for i, col := range keyColumns {
		parts[i] = fmt.Sprintf("'%s'", escapeSQLValue(keyValues[col]))
	}
	return strings.Join(parts, ", ")
}

func generateChunkSQL(addedRows []map[string]any, deletedRows []map[string]any, modifiedRows []ModifiedRow, keyColumns []string, tgtSchema, table string, qi *quoteInfo) string {
	sqlBuf := new(bytes.Buffer)
	for _, row := range addedRows {
		cols := make([]string, 0)
		vals := make([]string, 0)
		for k, v := range row {
			cols = append(cols, fmt.Sprintf("%s%s%s", qi.col, k, qi.colR))
			vals = append(vals, fmt.Sprintf("'%s'", escapeSQLValue(v)))
		}
		sqlBuf.WriteString(fmt.Sprintf("INSERT INTO %s%s%s.%s%s%s (%s) VALUES (%s);\n",
			qi.col, tgtSchema, qi.colR, qi.col, table, qi.colR, strings.Join(cols, ", "), strings.Join(vals, ", ")))
	}
	for _, mr := range modifiedRows {
		setParts := make([]string, 0)
		for _, ch := range mr.Changes {
			setParts = append(setParts, fmt.Sprintf("%s%s%s = '%s'", qi.col, ch.ColumnName, qi.colR, escapeSQLValue(ch.NewValue)))
		}
		whereParts := make([]string, 0)
		for _, kc := range keyColumns {
			whereParts = append(whereParts, fmt.Sprintf("%s%s%s = '%s'", qi.col, kc, qi.colR, escapeSQLValue(mr.SourceRow[kc])))
		}
		sqlBuf.WriteString(fmt.Sprintf("UPDATE %s%s%s.%s%s%s SET %s WHERE %s;\n",
			qi.col, tgtSchema, qi.colR, qi.col, table, qi.colR, strings.Join(setParts, ", "), strings.Join(whereParts, " AND ")))
	}
	for _, row := range deletedRows {
		whereParts := make([]string, 0)
		for _, kc := range keyColumns {
			whereParts = append(whereParts, fmt.Sprintf("%s%s%s = '%s'", qi.col, kc, qi.colR, escapeSQLValue(row[kc])))
		}
		sqlBuf.WriteString(fmt.Sprintf("DELETE FROM %s%s%s.%s%s%s WHERE %s;\n",
			qi.col, tgtSchema, qi.colR, qi.col, table, qi.colR, strings.Join(whereParts, " AND ")))
	}
	return sqlBuf.String()
}
