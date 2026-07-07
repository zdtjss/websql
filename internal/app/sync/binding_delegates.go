package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// CompareSchemaParams CompareSchema 的入参集合。
type CompareSchemaParams struct {
	SourceConnId string
	TargetConnId string
	SourceSchema string
	TargetSchema string
	Tables       string // 可选，逗号分隔的表名过滤
	Authorization string
}

// CompareSchemaByService 比较两个数据库 schema 的结构差异。
// 业务来自 CompareSchema handler。
func CompareSchemaByService(p *CompareSchemaParams) map[string]any {
	authorization := p.Authorization
	conn1 := conn.GetConn(p.SourceConnId, authorization)
	conn2 := conn.GetConn(p.TargetConnId, authorization)

	if conn1 == nil {
		return map[string]any{
			"diffs": []SchemaDiffItem{},
			"error": "源数据库连接不可用，请检查连接配置或权限",
		}
	}
	if conn2 == nil {
		return map[string]any{
			"diffs": []SchemaDiffItem{},
			"error": "目标数据库连接不可用，请检查连接配置或权限",
		}
	}

	dbType1 := conn1.DriverName()
	dbType2 := conn2.DriverName()
	if dbType1 != dbType2 {
		return map[string]any{
			"diffs": []SchemaDiffItem{},
			"error": fmt.Sprintf("不允许跨数据库类型比较: 源=%s, 目标=%s", dbType1, dbType2),
		}
	}

	tables1, err1 := getTableList(conn1, dbType1, p.SourceSchema)
	tables2, err2 := getTableList(conn2, dbType2, p.TargetSchema)
	if err1 != nil || err2 != nil {
		errMsg := ""
		if err1 != nil {
			errMsg += fmt.Sprintf("源库: %v", err1)
		}
		if err2 != nil {
			if errMsg != "" {
				errMsg += "; "
			}
			errMsg += fmt.Sprintf("目标库: %v", err2)
		}
		return map[string]any{
			"diffs": []SchemaDiffItem{},
			"error": errMsg,
		}
	}

	filterSet := make(map[string]bool)
	if p.Tables != "" {
		for _, t := range strings.Split(p.Tables, ",") {
			filterSet[strings.TrimSpace(t)] = true
		}
	}

	diffs := make([]SchemaDiffItem, 0)
	tableMap1 := make(map[string]bool)
	for _, t := range tables1 {
		tableMap1[t] = true
	}
	tableMap2 := make(map[string]bool)
	for _, t := range tables2 {
		tableMap2[t] = true
	}

	for _, table := range tables1 {
		if len(filterSet) > 0 && !filterSet[table] {
			continue
		}
		if !tableMap2[table] {
			schema, err := getTableSchema(conn1, dbType1, p.SourceSchema, table)
			if err != nil {
				diffs = append(diffs, SchemaDiffItem{TableName: table, DiffType: "ADD"})
				continue
			}
			diffs = append(diffs, SchemaDiffItem{
				TableName: table,
				DiffType:  "ADD",
				SourceDDL: schema.DDL,
				TargetDDL: "",
			})
		}
	}

	for _, table := range tables2 {
		if len(filterSet) > 0 && !filterSet[table] {
			continue
		}
		if !tableMap1[table] {
			schema, err := getTableSchema(conn2, dbType2, p.TargetSchema, table)
			if err != nil {
				diffs = append(diffs, SchemaDiffItem{TableName: table, DiffType: "DROP"})
				continue
			}
			diffs = append(diffs, SchemaDiffItem{
				TableName: table,
				DiffType:  "DROP",
				SourceDDL: "",
				TargetDDL: schema.DDL,
			})
		} else {
			diff := compareTable(conn1, conn2, dbType1, p.SourceSchema, p.TargetSchema, table)
			if diff != nil {
				diffs = append(diffs, *diff)
			}
		}
	}

	// 按表名排序
	sortDiffsByName(diffs)

	return map[string]any{
		"diffs":       diffs,
		"totalCount":  len(diffs),
		"addCount":    countByType(diffs, "ADD"),
		"dropCount":   countByType(diffs, "DROP"),
		"modifyCount": countByType(diffs, "MODIFY"),
	}
}

// sortDiffsByName 按表名排序差异列表。
func sortDiffsByName(diffs []SchemaDiffItem) {
	for i := 1; i < len(diffs); i++ {
		for j := i; j > 0 && diffs[j-1].TableName > diffs[j].TableName; j-- {
			diffs[j-1], diffs[j] = diffs[j], diffs[j-1]
		}
	}
}

// CompareDataParams CompareData 的入参集合。
type CompareDataParams struct {
	SourceConnId   string
	TargetConnId   string
	SourceSchema   string
	TargetSchema   string
	Table          string
	KeyColumns     string
	Page           string
	PageSize       string
	Authorization  string
}

// CompareDataByService 比较两个数据库表的数据差异（带分页）。
// 业务来自 CompareData handler。
func CompareDataByService(p *CompareDataParams) map[string]any {
	authorization := p.Authorization
	conn1 := conn.GetConn(p.SourceConnId, authorization)
	conn2 := conn.GetConn(p.TargetConnId, authorization)

	if conn1 == nil {
		return map[string]any{"error": "源数据库连接不可用，请检查连接配置或权限"}
	}
	if conn2 == nil {
		return map[string]any{"error": "目标数据库连接不可用，请检查连接配置或权限"}
	}

	dbType1 := conn1.DriverName()
	dbType2 := conn2.DriverName()
	if dbType1 != dbType2 {
		return map[string]any{"error": fmt.Sprintf("不允许跨数据库类型比较数据: 源=%s, 目标=%s", dbType1, dbType2)}
	}
	dbType := dbType1

	if p.Table == "" {
		return map[string]any{"error": "表名不能为空"}
	}
	if !isValidIdentifier(p.Table) {
		return map[string]any{"error": "表名包含非法字符"}
	}

	// 权限校验：仅 HTTP 远程模式生效
	if config.Cfg.IsRemote {
		permission.CheckTablePermission(p.SourceConnId, p.SourceSchema, p.Table, authorization)
		permission.CheckTablePermission(p.TargetConnId, p.TargetSchema, p.Table, authorization)
	}

	sourceCount := getRowCount(conn1, dbType, p.SourceSchema, p.Table)
	targetCount := getRowCount(conn2, dbType, p.TargetSchema, p.Table)
	if sourceCount > maxCompareRows || targetCount > maxCompareRows {
		return map[string]any{"error": fmt.Sprintf("数据量过大，源表%d行，目标%d行，上限%d行，请使用数据导出功能", sourceCount, targetCount, maxCompareRows)}
	}

	keyColumns := getKeyColumns(conn1, dbType, p.SourceSchema, p.Table, p.KeyColumns)
	columns := getTableColumnsDetail(conn1, dbType, p.SourceSchema, p.Table)

	sourceSQL := buildSelectSQL(dbType, p.SourceSchema, p.Table)
	targetSQL := buildSelectSQL(dbType, p.TargetSchema, p.Table)
	sourceData := queryAllData(conn1, dbType, sourceSQL)
	targetData := queryAllData(conn2, dbType, targetSQL)

	if len(keyColumns) == 0 {
		keyColumns = findCommonColumns(sourceData, targetData)
		if len(keyColumns) == 0 {
			return map[string]any{"error": "无法确定比较键列，请确保表有主键"}
		}
	}

	sourceMap := buildRowMap(sourceData, keyColumns)
	targetMap := buildRowMap(targetData, keyColumns)

	result := &DataDiffResult{
		TableName:    p.Table,
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

	sortModifiedByKey(result.ModifiedRows, keyColumns)

	return applyPagination(result, p.Page, p.PageSize)
}

// sortModifiedByKey 按 keyColumns 拼接的字符串排序 ModifiedRow 列表。
func sortModifiedByKey(rows []ModifiedRow, keyColumns []string) {
	keyOf := func(r ModifiedRow) string {
		var sb strings.Builder
		for _, kc := range keyColumns {
			if v, ok := r.Key[kc]; ok {
				sb.WriteString(fmt.Sprintf("%v", v))
			}
		}
		return sb.String()
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && keyOf(rows[j-1]) > keyOf(rows[j]); j-- {
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

// CompareDataChunkedParams CompareDataChunked 的入参集合。
type CompareDataChunkedParams struct {
	SourceConnId     string
	TargetConnId     string
	SourceSchema     string
	TargetSchema     string
	Table            string
	KeyColumns       string
	ChunkSize        string
	ChunkIndex       string
	Direction        string
	GenerateSQL      string
	Phase            string
	ConflictStrategy string
	Authorization    string
}

// CompareDataChunkedByService 分块比较数据差异，可选生成同步 SQL。
// 业务来自 CompareDataChunked handler。
func CompareDataChunkedByService(p *CompareDataChunkedParams) map[string]any {
	authorization := p.Authorization
	conn1 := conn.GetConn(p.SourceConnId, authorization)
	conn2 := conn.GetConn(p.TargetConnId, authorization)

	if conn1 == nil {
		return map[string]any{"error": "源数据库连接不可用，请检查连接配置或权限"}
	}
	if conn2 == nil {
		return map[string]any{"error": "目标数据库连接不可用，请检查连接配置或权限"}
	}
	dbType := conn1.DriverName()

	if p.Table == "" {
		return map[string]any{"error": "表名不能为空"}
	}
	if !isValidIdentifier(p.Table) {
		return map[string]any{"error": "表名包含非法字符"}
	}

	keyColumns := getKeyColumns(conn1, dbType, p.SourceSchema, p.Table, p.KeyColumns)
	if len(keyColumns) == 0 {
		return map[string]any{"error": "无法确定比较键列，请确保表有主键或指定keyColumns参数"}
	}

	columns := getTableColumnsDetail(conn1, dbType, p.SourceSchema, p.Table)
	sourceCount := getRowCount(conn1, dbType, p.SourceSchema, p.Table)
	targetCount := getRowCount(conn2, dbType, p.TargetSchema, p.Table)

	chunkSize := 5000
	if cs, ok := parseStrToInt(p.ChunkSize); ok && cs > 0 {
		chunkSize = cs
	}
	chunkIndex := 0
	if ci, ok := parseStrToInt(p.ChunkIndex); ok && ci >= 0 {
		chunkIndex = ci
	}

	qi := newQuoteInfo(dbType)

	var srcConn, tgtConn *sqlx.DB
	var srcSchema, tgtSchema string
	if p.Direction == "target_to_source" {
		srcConn = conn2
		tgtConn = conn1
		srcSchema = p.TargetSchema
		tgtSchema = p.SourceSchema
	} else {
		srcConn = conn1
		tgtConn = conn2
		srcSchema = p.SourceSchema
		tgtSchema = p.TargetSchema
	}

	if p.Phase == "out_of_range" {
		return handleOutOfRangeDeletionsByService(srcConn, tgtConn, dbType, srcSchema, tgtSchema, p.Table, keyColumns, qi, p.Direction, p.GenerateSQL, p.ConflictStrategy, chunkSize, chunkIndex, sourceCount, targetCount, columns)
	}

	totalChunks := (sourceCount + chunkSize - 1) / chunkSize
	if totalChunks == 0 {
		totalChunks = 1
	}

	startKey := getChunkStartKey(srcConn, dbType, srcSchema, p.Table, keyColumns, chunkIndex*chunkSize, qi)
	endKey := getChunkStartKey(srcConn, dbType, srcSchema, p.Table, keyColumns, (chunkIndex+1)*chunkSize, qi)

	sourceData := queryKeyRangeData(srcConn, dbType, srcSchema, p.Table, keyColumns, startKey, endKey, qi)
	targetData := queryKeyRangeData(tgtConn, dbType, tgtSchema, p.Table, keyColumns, startKey, endKey, qi)

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
	if p.GenerateSQL == "true" {
		sqlStr = generateChunkSQLWithStrategy(addedRows, deletedRows, modifiedRows, keyColumns, tgtSchema, p.Table, qi, p.ConflictStrategy, dbType)
	}

	hasMore := chunkIndex < totalChunks-1

	return map[string]any{
		"tableName":    p.Table,
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
	}
}

// handleOutOfRangeDeletionsByService 处理源表已无数据但目标仍有冗余行的场景。
// 业务来自 handler.go 中的 handleOutOfRangeDeletions。
// 此函数原本接收 *gin.Context 仅用于响应写入，service 化后改为返回 map。
func handleOutOfRangeDeletionsByService(srcConn, tgtConn *sqlx.DB, dbType, srcSchema, tgtSchema, table string, keyColumns []string, qi *quoteInfo, direction, generateSQLFlag, conflictStrategy string, chunkSize, chunkIndex, sourceCount, targetCount int, columns []DataDiffColumn) map[string]any {
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
		return map[string]any{
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
		}
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
		return map[string]any{
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
		}
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

	return map[string]any{
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
	}
}

// ApplySchemaDiffByService 执行 Schema DDL，带事务和审计。
// 业务来自 ApplySchemaDiff handler。
// user 可为 nil（桌面模式默认 admin）。
// clientIP 在桌面模式传空串。
func ApplySchemaDiffByService(connId, schema, sqlStr, authorization string, user *admin.User, clientIP string) map[string]any {
	dbConn := conn.GetConn(connId, authorization)
	if dbConn == nil {
		return map[string]any{"success": false, "message": "数据库连接不可用，请检查连接配置或权限"}
	}
	if strings.TrimSpace(sqlStr) == "" {
		return map[string]any{"success": false, "message": "SQL不能为空"}
	}

	// 权限校验：仅 HTTP 远程模式生效
	if config.Cfg.IsRemote {
		if !permission.CheckUserCanModify(authorization) {
			return map[string]any{"success": false, "message": "当前角色禁止修改数据，无法执行 Schema 变更"}
		}
	}

	sqlList := strings.Split(sqlStr, ";")
	validatedSQLs := make([]string, 0, len(sqlList))
	for _, s := range sqlList {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if err := validateSchemaSQL(s); err != nil {
			return map[string]any{"success": false, "message": err.Error()}
		}
		validatedSQLs = append(validatedSQLs, s)
	}

	startTime := time.Now()
	tx, err := dbConn.Beginx()
	if err != nil {
		return map[string]any{"success": false, "message": fmt.Sprintf("开启事务失败: %v", err)}
	}

	executedCount := 0
	errors := make([]string, 0)
	for _, s := range validatedSQLs {
		_, err := tx.Exec(s)
		if err != nil {
			if len(s) > 80 {
				s = s[:80]
			}
			errors = append(errors, fmt.Sprintf("执行失败: %s - %s", s, err.Error()))
		} else {
			executedCount++
		}
	}

	if len(errors) > 0 {
		tx.Rollback()
	} else {
		tx.Commit()
	}

	execTimeMs := int(time.Since(startTime).Milliseconds())

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
			Source:       "schemasync",
			SQLText:      fmt.Sprintf("[SchemaDiff] %d statements, executed=%d", len(validatedSQLs), executedCount),
			SQLType:      "DDL",
			RiskLevel:    "high",
			Status:       auditStatus,
			ConnID:       connId,
			SchemaName:   schema,
			UserID:       user.Id,
			UserName:     user.Name,
			ClientIP:     clientIP,
			AffectedRows: executedCount,
			ExecTimeMs:   execTimeMs,
			ErrorMsg:     auditError,
		})
	}

	return map[string]any{
		"success":       len(errors) == 0,
		"executedCount": executedCount,
		"errorCount":    len(errors),
		"errors":        errors,
	}
}

// ApplyDataSyncByService 执行数据同步 SQL，带事务、审计和回滚日志。
// 业务来自 ApplyDataSync handler。
// syncSessionId 非空时记录回滚日志（30 分钟内可回滚）。
// user 可为 nil；clientIP 在桌面模式传空串。
func ApplyDataSyncByService(connId, schema, sqlStr, syncSessionId, authorization string, user *admin.User, clientIP string) map[string]any {
	dbConn := conn.GetConn(connId, authorization)
	if dbConn == nil {
		return map[string]any{"success": false, "message": "数据库连接不可用，请检查连接配置或权限"}
	}
	dbType := dbConn.DriverName()

	if strings.TrimSpace(sqlStr) == "" {
		return map[string]any{"success": false, "message": "SQL不能为空"}
	}

	sqlList := strings.Split(sqlStr, ";")
	validatedSQLs := make([]string, 0, len(sqlList))
	for _, s := range sqlList {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if err := validateDataSQL(s); err != nil {
			return map[string]any{"success": false, "message": err.Error()}
		}
		validatedSQLs = append(validatedSQLs, s)
	}

	// 权限校验：仅 HTTP 远程模式生效
	if config.Cfg.IsRemote {
		if !permission.CheckUserCanModify(authorization) {
			return map[string]any{"success": false, "message": "当前角色禁止修改数据，无法执行同步操作"}
		}
		for _, s := range validatedSQLs {
			analysis := permission.AnalyzeSQL(s, schema)
			permResult := permission.CheckAnalysisPermission(analysis, connId, authorization)
			if !permResult.Allowed {
				return map[string]any{"success": false, "message": permResult.Message}
			}
		}
	}

	startTime := time.Now()
	tx, err := dbConn.Beginx()
	if err != nil {
		return map[string]any{"success": false, "message": fmt.Sprintf("开启事务失败: %v", err)}
	}

	insertCount := 0
	updateCount := 0
	deleteCount := 0
	errors := make([]string, 0)
	recordUndo := syncSessionId != ""
	executedOriginals := make([]string, 0, len(validatedSQLs))
	undoSQLs := make([]string, 0, len(validatedSQLs))

	for _, s := range validatedSQLs {
		upper := strings.ToUpper(s)
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

	if committed && recordUndo && len(executedOriginals) > 0 {
		rollbackLog := getOrCreateRollbackLog(syncSessionId, connId, schema, dbType)
		appendRollbackEntries(rollbackLog, executedOriginals, undoSQLs)
	}

	totalAffected := insertCount + updateCount + deleteCount
	execTimeMs := int(time.Since(startTime).Milliseconds())

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
			ClientIP:     clientIP,
			AffectedRows: totalAffected,
			ExecTimeMs:   execTimeMs,
			ErrorMsg:     auditError,
		})
	}

	return map[string]any{
		"success":     len(errors) == 0,
		"sessionId":   syncSessionId,
		"insertCount": insertCount,
		"updateCount": updateCount,
		"deleteCount": deleteCount,
		"errorCount":  len(errors),
		"errors":      errors,
	}
}

// GenerateSyncSQLParams GenerateSyncSQL 的入参集合。
type GenerateSyncSQLParams struct {
	SourceConnId    string
	TargetConnId    string
	SourceSchema    string
	TargetSchema    string
	Table           string
	Direction       string
	ConflictStrategy string
	Authorization   string
}

// GenerateSyncSQLByService 生成同步 SQL（不执行）。
// 业务来自 GenerateSyncSQL handler。
func GenerateSyncSQLByService(p *GenerateSyncSQLParams) map[string]any {
	authorization := p.Authorization
	conn1 := conn.GetConn(p.SourceConnId, authorization)
	conn2 := conn.GetConn(p.TargetConnId, authorization)
	if conn1 == nil {
		return map[string]any{"error": "源数据库连接不可用，请检查连接配置或权限"}
	}
	if conn2 == nil {
		return map[string]any{"error": "目标数据库连接不可用，请检查连接配置或权限"}
	}

	dbType := conn1.DriverName()
	if !isValidIdentifier(p.Table) {
		return map[string]any{"error": "表名包含非法字符"}
	}

	if config.Cfg.IsRemote {
		permission.CheckTablePermission(p.SourceConnId, p.SourceSchema, p.Table, authorization)
		permission.CheckTablePermission(p.TargetConnId, p.TargetSchema, p.Table, authorization)
	}

	sourceCount := getRowCount(conn1, dbType, p.SourceSchema, p.Table)
	targetCount := getRowCount(conn2, dbType, p.TargetSchema, p.Table)
	if sourceCount > maxCompareRows || targetCount > maxCompareRows {
		return map[string]any{"error": fmt.Sprintf("数据量过大，源表%d行，目标%d行，上限%d行", sourceCount, targetCount, maxCompareRows)}
	}

	sourceMap, targetMap, keyColumns := buildSyncData(conn1, conn2, dbType, p.SourceSchema, p.TargetSchema, p.Table, p.Direction)

	sqlBuf := new(strings.Builder)
	qi := newQuoteInfo(dbType)

	var fromMap, toMap map[string]map[string]any
	var writeSchema string
	if p.Direction == "source_to_target" {
		fromMap, toMap, writeSchema = sourceMap, targetMap, p.TargetSchema
	} else {
		fromMap, toMap, writeSchema = targetMap, sourceMap, p.SourceSchema
	}

	for key, fromRow := range fromMap {
		if toRow, ok := toMap[key]; ok {
			changes := diffRows(fromRow, toRow, keyColumns)
			if len(changes) > 0 && p.ConflictStrategy != StrategySkip {
				sqlBuf.WriteString(buildUpdateStmt(dbType, writeSchema, p.Table, changes, keyColumns, fromRow, qi))
				sqlBuf.WriteString("\n")
			}
		} else {
			sqlBuf.WriteString(buildInsertStmt(p.ConflictStrategy, dbType, writeSchema, p.Table, fromRow, keyColumns, qi))
			sqlBuf.WriteString("\n")
		}
	}
	for key, toRow := range toMap {
		if _, ok := fromMap[key]; !ok {
			sqlBuf.WriteString(buildDeleteStmt(dbType, writeSchema, p.Table, toRow, keyColumns, qi))
			sqlBuf.WriteString("\n")
		}
	}

	return map[string]any{"sql": sqlBuf.String()}
}

// GetSyncTargetsByService 返回可同步的 schemas 和 tables。
// 业务来自 GetSyncTargets handler。
func GetSyncTargetsByService(connId, schema, authorization string) map[string]any {
	dbConn := conn.GetConn(connId, authorization)
	if dbConn == nil {
		return map[string]any{
			"tables":  []string{},
			"schemas": []string{},
			"error":   "数据库连接不可用，请检查连接配置或权限",
		}
	}
	dbType := dbConn.DriverName()

	if schema == "" {
		switch dbType {
		case "mysql", "mariadb":
			dbConn.Get(&schema, "SELECT DATABASE()")
		case "oracle":
			dbConn.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		case "sqlite":
			schema = "main"
		}
	}

	tables, _ := getTableList(dbConn, dbType, schema)
	schemas := getSchemaList(dbConn, dbType)

	return map[string]any{
		"tables":  tables,
		"schemas": schemas,
	}
}

// DryRunParams DryRunSync 的入参集合。
type DryRunParams struct {
	SourceConnId    string
	TargetConnId    string
	SourceSchema    string
	TargetSchema    string
	Table           string
	Direction       string
	ConflictStrategy string
	Authorization   string
}

// DryRunSyncByService 试运行同步，返回预估影响行数和示例 SQL，不执行写操作。
// 业务来自 DryRunSync handler。
func DryRunSyncByService(p *DryRunParams) map[string]any {
	authorization := p.Authorization
	srcConn := conn.GetConn(p.SourceConnId, authorization)
	tgtConn := conn.GetConn(p.TargetConnId, authorization)
	if srcConn == nil || tgtConn == nil {
		return map[string]any{"error": "源或目标数据库连接不可用，请检查连接配置或权限"}
	}
	dbType := srcConn.DriverName()

	if p.Table == "" || !isValidIdentifier(p.Table) {
		return map[string]any{"error": "表名无效"}
	}

	if config.Cfg.IsRemote {
		permission.CheckTablePermission(p.SourceConnId, p.SourceSchema, p.Table, authorization)
		permission.CheckTablePermission(p.TargetConnId, p.TargetSchema, p.Table, authorization)
	}

	srcCount := getRowCount(srcConn, dbType, p.SourceSchema, p.Table)
	tgtCount := getRowCount(tgtConn, dbType, p.TargetSchema, p.Table)
	if srcCount > maxCompareRows || tgtCount > maxCompareRows {
		return map[string]any{"error": fmt.Sprintf("数据量过大，源表%d行，目标%d行，上限%d行", srcCount, tgtCount, maxCompareRows)}
	}

	tgtSchema := p.TargetSchema
	if p.Direction == "target_to_source" {
		tgtSchema = p.SourceSchema
	}

	sourceMap, targetMap, keyCols := buildSyncData(srcConn, tgtConn, dbType, p.SourceSchema, p.TargetSchema, p.Table, p.Direction)
	if len(keyCols) == 0 {
		return map[string]any{"error": "无法确定比较键列，请确保表有主键"}
	}

	qi := newQuoteInfo(dbType)

	addedRows := make([]map[string]any, 0)
	deletedRows := make([]map[string]any, 0)
	modifiedRows := make([]ModifiedRow, 0)

	for key, srcRow := range sourceMap {
		if tgtRow, ok := targetMap[key]; ok {
			changes := diffRows(srcRow, tgtRow, keyCols)
			if len(changes) > 0 {
				keyMap := make(map[string]any)
				for _, kc := range keyCols {
					keyMap[kc] = srcRow[kc]
				}
				modifiedRows = append(modifiedRows, ModifiedRow{Key: keyMap, Changes: changes, SourceRow: srcRow, TargetRow: tgtRow})
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

	updateCount := len(modifiedRows)
	if p.ConflictStrategy == StrategySkip {
		updateCount = 0
	}

	report := DryRunTableReport{
		TableName:   p.Table,
		TotalSource: srcCount,
		TotalTarget: tgtCount,
		OperationCounts: map[string]int{
			"INSERT":   len(addedRows),
			"UPDATE":   updateCount,
			"DELETE":   len(deletedRows),
			"CONFLICT": len(modifiedRows),
		},
		Samples:   make([]DryRunSample, 0, 3),
		Conflicts: takeModified(modifiedRows, 10),
	}

	report.Samples = append(report.Samples, DryRunSample{
		Operation:  "INSERT",
		Estimate:   len(addedRows),
		SqlPreview: previewInserts(addedRows, p.ConflictStrategy, dbType, tgtSchema, p.Table, keyCols, qi, 5),
	})
	report.Samples = append(report.Samples, DryRunSample{
		Operation:  "UPDATE",
		Estimate:   updateCount,
		SqlPreview: previewUpdates(modifiedRows, dbType, tgtSchema, p.Table, keyCols, qi, 5, p.ConflictStrategy),
	})
	report.Samples = append(report.Samples, DryRunSample{
		Operation:  "DELETE",
		Estimate:   len(deletedRows),
		SqlPreview: previewDeletes(deletedRows, dbType, tgtSchema, p.Table, keyCols, qi, 5),
	})

	return map[string]any(report.toMap())
}

// toMap 将 DryRunTableReport 转换为 map，供 service 返回。
// 不直接在 service 中 marshal 是为了保持与 handler 返回结构一致。
func (r DryRunTableReport) toMap() map[string]any {
	return map[string]any{
		"tableName":      r.TableName,
		"totalSource":    r.TotalSource,
		"totalTarget":    r.TotalTarget,
		"operationCounts": r.OperationCounts,
		"samples":        r.Samples,
		"conflicts":      r.Conflicts,
	}
}

// GetRollbackLogByService 返回指定会话的回滚日志。
// 业务来自 GetRollbackLog handler。
func GetRollbackLogByService(sessionId string) map[string]any {
	if sessionId == "" {
		return map[string]any{"error": "sessionId 不能为空"}
	}
	v, ok := rollbackStore.Load(sessionId)
	if !ok {
		return map[string]any{"error": "回滚日志不存在或已过期（保留 30 分钟）"}
	}
	log := v.(*RollbackLog)
	log.mu.Lock()
	defer log.mu.Unlock()
	return map[string]any{
		"sessionId":   log.SessionId,
		"connId":      log.ConnId,
		"schema":      log.Schema,
		"undoCount":   len(log.UndoSQLs),
		"undoSQLs":    log.UndoSQLs,
		"originalSQLs": log.OriginalSQLs,
		"createdAt":   log.CreatedAt.Format("2006-01-02 15:04:05"),
		"expiresIn":   int((rollbackLogTTL - time.Since(log.CreatedAt)).Seconds()),
	}
}

// RollbackSyncByService 执行指定会话的回滚，按逆序执行撤销 SQL。
// 业务来自 RollbackSync handler。
// user 可为 nil；clientIP 在桌面模式传空串。
func RollbackSyncByService(sessionId, authorization string, user *admin.User, clientIP string) map[string]any {
	if sessionId == "" {
		return map[string]any{"success": false, "message": "sessionId 不能为空"}
	}
	v, ok := rollbackStore.Load(sessionId)
	if !ok {
		return map[string]any{"success": false, "message": "回滚日志不存在或已过期"}
	}
	logEntry := v.(*RollbackLog)

	dbConn := conn.GetConn(logEntry.ConnId, authorization)
	if dbConn == nil {
		return map[string]any{"success": false, "message": "目标数据库连接不可用"}
	}

	if config.Cfg.IsRemote && !permission.CheckUserCanModify(authorization) {
		return map[string]any{"success": false, "message": "当前角色禁止修改数据，无法执行回滚"}
	}

	logEntry.mu.Lock()
	undoSQLs := make([]string, len(logEntry.UndoSQLs))
	copy(undoSQLs, logEntry.UndoSQLs)
	logEntry.mu.Unlock()

	tx, err := dbConn.Beginx()
	if err != nil {
		return map[string]any{"success": false, "message": fmt.Sprintf("开启事务失败: %v", err)}
	}

	executed := 0
	errors := make([]string, 0)
	for i := len(undoSQLs) - 1; i >= 0; i-- {
		stmt := strings.TrimSpace(undoSQLs[i])
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		if err := validateDataSQL(stmt); err != nil {
			errors = append(errors, fmt.Sprintf("校验失败: %s - %s", truncate(stmt, 60), err.Error()))
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			errors = append(errors, fmt.Sprintf("执行失败: %s - %s", truncate(stmt, 60), err.Error()))
		} else {
			executed++
		}
	}

	if len(errors) > 0 {
		tx.Rollback()
	} else {
		tx.Commit()
	}

	if user != nil {
		recordRollbackAuditByService(sessionId, logEntry, executed, len(errors), user, clientIP)
	}

	if len(errors) > 0 {
		return map[string]any{
			"success":    false,
			"message":    fmt.Sprintf("回滚完成但存在 %d 条错误", len(errors)),
			"executed":   executed,
			"errorCount": len(errors),
			"errors":     errors,
		}
	}

	rollbackStore.Delete(sessionId)
	return map[string]any{
		"success":  true,
		"message":  fmt.Sprintf("回滚成功，执行 %d 条撤销语句", executed),
		"executed": executed,
	}
}

// recordRollbackAuditByService 记录回滚操作的审计日志（service 版本，脱离 gin.Context）。
func recordRollbackAuditByService(sessionId string, logEntry *RollbackLog, executed, errCount int, user *admin.User, clientIP string) {
	status := "success"
	if errCount > 0 {
		status = "failed"
	}
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:       "datasync-rollback",
		SQLText:      fmt.Sprintf("[DataSyncRollback] session=%s undo=%d executed=%d errors=%d", sessionId, len(logEntry.UndoSQLs), executed, errCount),
		SQLType:      "SYNC",
		RiskLevel:    "high",
		Status:       status,
		ConnID:       logEntry.ConnId,
		SchemaName:   logEntry.Schema,
		UserID:       user.Id,
		UserName:     user.Name,
		ClientIP:     clientIP,
		AffectedRows: executed,
	})
}

// ExportSyncReportByService 生成同步报告文件，返回 (filename, content, error)。
// 业务来自 ExportSyncReport handler。
// 桌面 binding 调用此函数后写入临时文件并返回 BlobResult。
// HTTP handler 可直接调用并写文件到 exports/ 目录。
func ExportSyncReportByService(input *SyncReportInput) (filename, content string, err error) {
	if input.Format == "" {
		input.Format = "html"
	}

	stamp := time.Now().Format("20060102_150405")
	if input.Format == "csv" {
		filename = fmt.Sprintf("syncreport_%s.csv", stamp)
		content, err = renderReportCSV(input)
	} else {
		filename = fmt.Sprintf("syncreport_%s.html", stamp)
		content = renderReportHTML(input)
	}
	return filename, content, err
}

// WriteSyncReportToFile 将报告写入 exports/ 目录（HTTP 模式使用）。
// 桌面模式不使用此函数，改用 writeHTMLToTemp 写入临时文件。
func WriteSyncReportToFile(filename, content string) (string, error) {
	if err := os.MkdirAll("exports", 0755); err != nil {
		return "", fmt.Errorf("创建导出目录失败: %w", err)
	}
	filePath := filepath.Join("exports", filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("写入报告文件失败: %w", err)
	}
	return filePath, nil
}

// 下面是 HTTP handler 的薄包装层，调用上述 service 函数。
// 保留原 handler 函数签名以最小化 router.go 改动。

// compareSchemaHandlerFromService 是 CompareSchema handler 的 service 化版本。
// 在原 handler 文件中通过此函数调用 service，避免重复实现。
func compareSchemaHandlerFromService(c *gin.Context) {
	p := &CompareSchemaParams{
		SourceConnId:  c.PostForm("sourceConnId"),
		TargetConnId:  c.PostForm("targetConnId"),
		SourceSchema:  c.PostForm("sourceSchema"),
		TargetSchema:  c.PostForm("targetSchema"),
		Tables:        c.PostForm("tables"),
		Authorization: appctx.Ctx.GetAuthorization(c),
	}
	result := CompareSchemaByService(p)
	response.WriteOK(c, result)
}

// compareDataHandlerFromService 是 CompareData handler 的 service 化版本。
func compareDataHandlerFromService(c *gin.Context) {
	p := &CompareDataParams{
		SourceConnId:  c.PostForm("sourceConnId"),
		TargetConnId:  c.PostForm("targetConnId"),
		SourceSchema:  c.PostForm("sourceSchema"),
		TargetSchema:  c.PostForm("targetSchema"),
		Table:         c.PostForm("table"),
		KeyColumns:    c.PostForm("keyColumns"),
		Page:          c.DefaultPostForm("page", "1"),
		PageSize:      c.DefaultPostForm("pageSize", "500"),
		Authorization: appctx.Ctx.GetAuthorization(c),
	}
	result := CompareDataByService(p)
	response.WriteOK(c, result)
}

// applySchemaDiffHandlerFromService 是 ApplySchemaDiff handler 的 service 化版本。
func applySchemaDiffHandlerFromService(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.PostForm("schema")
	sqlStr := c.PostForm("sql")
	authorization := appctx.Ctx.GetAuthorization(c)

	userVal, _ := c.Get("currentUser")
	user, _ := userVal.(*admin.User)

	result := ApplySchemaDiffByService(connId, schema, sqlStr, authorization, user, c.ClientIP())
	response.WriteOK(c, result)
}

// applyDataSyncHandlerFromService 是 ApplyDataSync handler 的 service 化版本。
func applyDataSyncHandlerFromService(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.PostForm("schema")
	sqlStr := c.PostForm("sql")
	syncSessionId := c.PostForm("syncSessionId")
	authorization := appctx.Ctx.GetAuthorization(c)

	userVal, _ := c.Get("currentUser")
	user, _ := userVal.(*admin.User)

	result := ApplyDataSyncByService(connId, schema, sqlStr, syncSessionId, authorization, user, c.ClientIP())
	response.WriteOK(c, result)
}

// generateSyncSQLHandlerFromService 是 GenerateSyncSQL handler 的 service 化版本。
func generateSyncSQLHandlerFromService(c *gin.Context) {
	p := &GenerateSyncSQLParams{
		SourceConnId:    c.PostForm("sourceConnId"),
		TargetConnId:    c.PostForm("targetConnId"),
		SourceSchema:    c.PostForm("sourceSchema"),
		TargetSchema:    c.PostForm("targetSchema"),
		Table:           c.PostForm("table"),
		Direction:       c.DefaultPostForm("direction", "source_to_target"),
		ConflictStrategy: c.DefaultPostForm("conflictStrategy", StrategyUpdate),
		Authorization:   appctx.Ctx.GetAuthorization(c),
	}
	result := GenerateSyncSQLByService(p)
	response.WriteOK(c, result)
}

// getSyncTargetsHandlerFromService 是 GetSyncTargets handler 的 service 化版本。
func getSyncTargetsHandlerFromService(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	authorization := appctx.Ctx.GetAuthorization(c)
	result := GetSyncTargetsByService(connId, schema, authorization)
	response.WriteOK(c, result)
}

// dryRunSyncHandlerFromService 是 DryRunSync handler 的 service 化版本。
func dryRunSyncHandlerFromService(c *gin.Context) {
	p := &DryRunParams{
		SourceConnId:    c.PostForm("sourceConnId"),
		TargetConnId:    c.PostForm("targetConnId"),
		SourceSchema:    c.PostForm("sourceSchema"),
		TargetSchema:    c.PostForm("targetSchema"),
		Table:           c.PostForm("table"),
		Direction:       c.DefaultPostForm("direction", "source_to_target"),
		ConflictStrategy: c.DefaultPostForm("conflictStrategy", StrategyUpdate),
		Authorization:   appctx.Ctx.GetAuthorization(c),
	}
	result := DryRunSyncByService(p)
	response.WriteOK(c, result)
}

// getRollbackLogHandlerFromService 是 GetRollbackLog handler 的 service 化版本。
func getRollbackLogHandlerFromService(c *gin.Context) {
	sessionId := c.Query("sessionId")
	result := GetRollbackLogByService(sessionId)
	response.WriteOK(c, result)
}

// rollbackSyncHandlerFromService 是 RollbackSync handler 的 service 化版本。
func rollbackSyncHandlerFromService(c *gin.Context) {
	sessionId := c.PostForm("sessionId")
	authorization := appctx.Ctx.GetAuthorization(c)

	userVal, _ := c.Get("currentUser")
	user, _ := userVal.(*admin.User)

	result := RollbackSyncByService(sessionId, authorization, user, c.ClientIP())
	response.WriteOK(c, result)
}

// exportSyncReportHandlerFromService 是 ExportSyncReport handler 的 service 化版本。
func exportSyncReportHandlerFromService(c *gin.Context) {
	var input SyncReportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.WriteOK(c, map[string]any{"error": "请求参数解析失败: " + err.Error()})
		return
	}
	filename, content, err := ExportSyncReportByService(&input)
	if err != nil {
		response.WriteOK(c, map[string]any{"error": "生成报告失败: " + err.Error()})
		return
	}
	filePath, err := WriteSyncReportToFile(filename, content)
	if err != nil {
		response.WriteOK(c, map[string]any{"error": err.Error()})
		return
	}
	response.WriteOK(c, map[string]any{
		"filename": filename,
		"url":      "/exports/" + filename,
		"format":   input.Format,
		"path":     filePath,
	})
}
