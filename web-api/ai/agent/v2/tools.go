package agentv2

import (
	"context"
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/jmoiron/sqlx"
)

var validTableNameRegex = regexp.MustCompile("^[a-zA-Z0-9_`.]+$")

func isValidTableName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && len(name) <= 128 && validTableNameRegex.MatchString(name)
}

type QueryInput struct {
	SQL string `json:"sql" jsonschema:"required"`
}
type QueryOutput struct {
	Columns []string         `json:"columns"`
	Data    []map[string]any `json:"data"`
	Count   int              `json:"count"`
}
type ExecInput struct {
	SQL string `json:"sql" jsonschema:"required"`
}
type ExecOutput struct {
	AffectedRows int64  `json:"affectedRows"`
	Message      string `json:"message"`
}
type SchemaInput struct {
	Tables []string `json:"tables" jsonschema:"required"`
}
type SchemaOutput struct {
	Schema string `json:"schema"`
}
type ExportInput struct {
	SQL      string `json:"sql" jsonschema:"required"`
	FileName string `json:"fileName"`
}
type ExportOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
}
type CurrentDateTimeInput struct{}
type CurrentDateTimeOutput struct {
	DateTime string `json:"dateTime"`
}

func getConn(connId string) (*sqlx.DB, string) {
	if connId == "" {
		return nil, ""
	}
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connId)
	if err != nil || len(cfgList) == 0 {
		return nil, ""
	}
	cfg := &cfgList[0]
	deref := func(p *string) string {
		if p != nil {
			return *p
		}
		return ""
	}
	pwd := ""
	if cfg.Pwd != nil {
		pwd = utils.AESDecode(*cfg.Pwd)
	}
	conn := config.GetConn(&config.DBParam{
		Id: cfg.Id, Name: deref(cfg.Name), DbType: cfg.DbType,
		User: deref(cfg.User), Pwd: pwd, Url: deref(cfg.Url),
	})
	return conn, cfg.DbType
}

func GetCurrentDateTime() func(ctx context.Context, input *CurrentDateTimeInput) (*CurrentDateTimeOutput, error) {
	return func(ctx context.Context, input *CurrentDateTimeInput) (*CurrentDateTimeOutput, error) {
		return &CurrentDateTimeOutput{DateTime: time.Now().Format("2006-01-02 15:04:05")}, nil
	}
}

func NewQueryFunc(connId string) func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
	return func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
		log.Printf("[Tool:query_data] sql=%s\n", input.SQL)
		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("db conn not found: %s", connId)
		}
		sql := strings.TrimSpace(input.SQL)
		stripped := stripSQLComments(sql)
		upper := strings.ToUpper(stripped)
		if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "SHOW") &&
			!strings.HasPrefix(upper, "DESCRIBE") && !strings.HasPrefix(upper, "EXPLAIN") &&
			!strings.HasPrefix(upper, "WITH") {
			return nil, fmt.Errorf("query_data only supports SELECT/SHOW/DESCRIBE/EXPLAIN/WITH")
		}
		if strings.HasPrefix(upper, "WITH") {
			writeKW := []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "TRUNCATE ", "ALTER ", "CREATE ", "REPLACE ", "MERGE "}
			for _, kw := range writeKW {
				if strings.Contains(strings.ToUpper(stripped), kw) {
					return nil, fmt.Errorf("query_data does not allow write operations (%s) in WITH, use exec_sql", strings.TrimSpace(kw))
				}
			}
		}
		if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") {
			sql = applyRowLimit(sql, conn.DriverName(), 2000)
		}
		rows, err := conn.Queryx(sql)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w", err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		data := dbutils.GetResultRows(conn.DriverName(), rows)
		return &QueryOutput{Columns: cols, Data: data, Count: len(data)}, nil
	}
}

// ExecAuditCtx holds context for audit logging within exec_sql tool
type ExecAuditCtx struct {
	ConnID    string
	UserID    string
	UserName  string
	SessionID string
}

func NewExecFunc(connId string, auditCtx *ExecAuditCtx) func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
	return func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
		log.Printf("[Tool:exec_sql] sql=%s\n", input.SQL)
		sql := strings.TrimSpace(input.SQL)
		if sql == "" {
			return nil, fmt.Errorf("SQL is empty")
		}

		// ── Interrupt/Resume 处理 ──
		// 检查是否从中断恢复（用户确认了之前被拦截的危险 SQL）
		wasInterrupted, hasState, savedSQL := tool.GetInterruptState[string](ctx)
		if wasInterrupted && hasState {
			isTarget, hasData, approved := tool.GetResumeContext[bool](ctx)
			if isTarget {
				// 本工具是恢复目标
				if hasData && approved {
					// 用户批准 → 使用服务端保存的 SQL（不是前端传来的）
					log.Printf("[Tool:exec_sql] user approved saved SQL - sql=%s\n", savedSQL)
					sql = savedSQL
					// 直接执行，不再检查 isDangerousSQL（用户已确认）
					goto doExec
				}
				// 用户拒绝
				return &ExecOutput{AffectedRows: 0, Message: "cancelled by user"}, nil
			}
			// 不是恢复目标 → 重新中断以保持状态
			return nil, tool.StatefulInterrupt(ctx, &DangerousSQLInfo{
				SQL: savedSQL, RiskLevel: detectRiskLevel(savedSQL), SQLType: detectSQLType(savedSQL),
			}, savedSQL)
		}

		// ── 首次执行 — 安全红线：所有危险 SQL 必须中断等待用户确认 ──
		for _, line := range strings.Split(sql, ";") {
			line = strings.TrimSpace(line)
			if line != "" && isDangerousSQL(line) {
				log.Printf("[Tool:exec_sql] DANGEROUS SQL INTERCEPTED - sql=%s\n", sql)
				return nil, tool.StatefulInterrupt(ctx, &DangerousSQLInfo{
					SQL: sql, RiskLevel: detectRiskLevel(sql), SQLType: detectSQLType(sql),
				}, sql)
			}
		}

	doExec:
		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("db conn not found: %s", connId)
		}

		// 审计日志
		auditID := utils.RandomStr()
		sqlType := detectSQLType(sql)
		riskLevel := detectRiskLevel(sql)

		result, err := conn.Exec(sql)
		if err != nil {
			if auditCtx != nil {
				InsertSQLAudit(auditID, auditCtx.UserID, auditCtx.UserName, auditCtx.ConnID, auditCtx.SessionID, sql, sqlType, riskLevel, "failed", 0, err.Error())
			}
			return nil, fmt.Errorf("exec failed: %w", err)
		}
		affected, _ := result.RowsAffected()
		if auditCtx != nil {
			InsertSQLAudit(auditID, auditCtx.UserID, auditCtx.UserName, auditCtx.ConnID, auditCtx.SessionID, sql, sqlType, riskLevel, "success", int(affected), "")
		}
		log.Printf("[Tool:exec_sql] ok - affectedRows=%d\n", affected)
		return &ExecOutput{AffectedRows: affected, Message: fmt.Sprintf("ok, %d rows affected", affected)}, nil
	}
}

func NewSchemaFunc(connId, dbType, dbSchema string) func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
	return func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
		log.Printf("[Tool:get_table_schema] tables=%v\n", input.Tables)
		conn, actualDBType := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("db conn not found: %s", connId)
		}
		var sb strings.Builder
		for _, table := range input.Tables {
			if !isValidTableName(table) {
				logutils.PrintErr(fmt.Errorf("invalid table name: %s", table))
				continue
			}
			var schemaSQL string
			switch actualDBType {
			case "mysql", "mariadb":
				schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`", table)
			case "sqlite":
				schemaSQL = "SELECT sql FROM sqlite_master WHERE type='table' AND name=?"
			case "oracle":
				schemaSQL = "SELECT DBMS_METADATA.GET_DDL('TABLE', :1) FROM DUAL"
			default:
				schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`", table)
			}
			var rows *sqlx.Rows
			var err error
			switch actualDBType {
			case "sqlite":
				rows, err = conn.Queryx(schemaSQL, table)
			case "oracle":
				rows, err = conn.Queryx(schemaSQL, strings.ToUpper(table))
			default:
				rows, err = conn.Queryx(schemaSQL)
			}
			if err != nil {
				logutils.PrintErr(fmt.Errorf("get schema failed %s: %w", table, err))
				sb.WriteString(fallbackColumnInfo(conn, actualDBType, dbSchema, table))
				continue
			}
			for rows.Next() {
				switch actualDBType {
				case "sqlite":
					var createSQL string
					if err := rows.Scan(&createSQL); err == nil && createSQL != "" {
						sb.WriteString(createSQL)
						sb.WriteString(";\n\n")
					}
				case "oracle":
					var ddl string
					if err := rows.Scan(&ddl); err == nil && ddl != "" {
						sb.WriteString(ddl)
						sb.WriteString(";\n\n")
					}
				default:
					var tableName, createTable string
					if err := rows.Scan(&tableName, &createTable); err == nil {
						sb.WriteString(createTable)
						sb.WriteString(";\n\n")
					}
				}
			}
			rows.Close()
		}
		return &SchemaOutput{Schema: sb.String()}, nil
	}
}

func fallbackColumnInfo(conn *sqlx.DB, dbType, dbSchema, table string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "-- Table: %s\n", table)
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		query = "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
		args = []any{dbSchema, table}
	case "sqlite":
		query = fmt.Sprintf("PRAGMA table_info('%s')", table)
	default:
		return sb.String()
	}
	rows, err := conn.Queryx(query, args...)
	if err != nil {
		return sb.String()
	}
	defer rows.Close()
	data := dbutils.GetResultRows(conn.DriverName(), rows)
	for _, row := range data {
		fmt.Fprintf(&sb, "  %v\n", row)
	}
	sb.WriteString("\n")
	return sb.String()
}

type ImportDataInput struct {
	FileID    string            `json:"fileId" jsonschema:"required"`
	TableName string            `json:"tableName" jsonschema:"required"`
	Mapping   map[string]string `json:"mapping"`
	Mode      string            `json:"mode"`
}
type ImportDataOutput struct {
	Message      string `json:"message"`
	InsertedRows int    `json:"insertedRows"`
	UpdatedRows  int    `json:"updatedRows"`
}

func NewImportDataFunc(connID, dbType, dbSchema string) func(ctx context.Context, input *ImportDataInput) (*ImportDataOutput, error) {
	return func(ctx context.Context, input *ImportDataInput) (*ImportDataOutput, error) {
		log.Printf("[Tool:import_data] fileId=%s, table=%s\n", input.FileID, input.TableName)
		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("db conn not found: %s", connID)
		}
		if input.FileID == "" {
			return nil, fmt.Errorf("fileId is required")
		}
		mode := strings.ToLower(input.Mode)
		if mode == "" {
			mode = "insert"
		}
		upload, err := GetUploadedFile(input.FileID)
		if err != nil {
			return nil, err
		}
		tableColumns, err := getTableColumns(conn, dbType, dbSchema, input.TableName)
		if err != nil {
			return nil, fmt.Errorf("get columns failed for %s: %w", input.TableName, err)
		}
		if len(tableColumns) == 0 {
			return nil, fmt.Errorf("table %s not found or has no columns", input.TableName)
		}
		finalMapping, err := buildFinalMapping(upload.Columns, tableColumns, input.Mapping)
		if err != nil {
			return nil, err
		}
		if len(finalMapping) == 0 {
			return nil, fmt.Errorf("no columns matched for table %s", input.TableName)
		}
		var dbColumns []string
		var excelIndices []int
		for excelIdx, dbCol := range finalMapping {
			dbColumns = append(dbColumns, dbCol)
			excelIndices = append(excelIndices, excelIdx)
		}
		primaryKeys, _ := getPrimaryKeys(conn, dbType, dbSchema, input.TableName)
		tx, err := conn.Beginx()
		if err != nil {
			return nil, fmt.Errorf("begin tx failed: %w", err)
		}
		defer tx.Rollback()
		insertedRows, updatedRows := 0, 0
		for rowNum, excelRow := range upload.Data {
			row := make([]string, len(dbColumns))
			for i, idx := range excelIndices {
				if idx < len(excelRow) {
					row[i] = excelRow[idx]
				}
			}
			if mode == "upsert" && len(primaryKeys) > 0 {
				exists, err := checkRowExists(tx, dbType, dbSchema, input.TableName, dbColumns, row, primaryKeys)
				if err != nil {
					return nil, fmt.Errorf("row %d check failed: %w", rowNum+2, err)
				}
				if exists {
					if err := updateRow(tx, dbType, dbSchema, input.TableName, dbColumns, row, primaryKeys); err != nil {
						return nil, fmt.Errorf("row %d update failed: %w", rowNum+2, err)
					}
					updatedRows++
				} else {
					if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
						return nil, fmt.Errorf("row %d insert failed: %w", rowNum+2, err)
					}
					insertedRows++
				}
			} else {
				if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
					return nil, fmt.Errorf("row %d insert failed: %w", rowNum+2, err)
				}
				insertedRows++
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit failed: %w", err)
		}
		RemoveUploadedFile(input.FileID)
		var mappingDesc strings.Builder
		for i, idx := range excelIndices {
			if i > 0 {
				mappingDesc.WriteString(", ")
			}
			fmt.Fprintf(&mappingDesc, "%s->%s", upload.Columns[idx], dbColumns[i])
		}
		msg := fmt.Sprintf("imported %d rows", insertedRows)
		if updatedRows > 0 {
			msg += fmt.Sprintf(", updated %d rows", updatedRows)
		}
		msg += fmt.Sprintf(". mapping: %s", mappingDesc.String())
		return &ImportDataOutput{Message: msg, InsertedRows: insertedRows, UpdatedRows: updatedRows}, nil
	}
}

func normalizeColName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "")
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}

func buildFinalMapping(excelColumns, tableColumns []string, aiMapping map[string]string) (map[int]string, error) {
	result := make(map[int]string)
	usedDBCols := make(map[string]bool)
	excelByExact := make(map[string]int)
	excelByNorm := make(map[string]int)
	for i, c := range excelColumns {
		excelByExact[c] = i
		excelByNorm[normalizeColName(c)] = i
	}
	dbByUpper := make(map[string]string)
	dbByNorm := make(map[string]string)
	for _, c := range tableColumns {
		dbByUpper[strings.ToUpper(c)] = c
		dbByNorm[normalizeColName(c)] = c
	}
	if len(aiMapping) > 0 {
		for aiExcelCol, aiDBCol := range aiMapping {
			excelIdx := -1
			if idx, ok := excelByExact[aiExcelCol]; ok {
				excelIdx = idx
			} else if idx, ok := excelByNorm[normalizeColName(aiExcelCol)]; ok {
				excelIdx = idx
			}
			if excelIdx == -1 {
				continue
			}
			dbCol := ""
			if orig, ok := dbByUpper[strings.ToUpper(aiDBCol)]; ok {
				dbCol = orig
			} else if orig, ok := dbByNorm[normalizeColName(aiDBCol)]; ok {
				dbCol = orig
			}
			if dbCol == "" || usedDBCols[strings.ToUpper(dbCol)] {
				continue
			}
			if _, exists := result[excelIdx]; exists {
				continue
			}
			result[excelIdx] = dbCol
			usedDBCols[strings.ToUpper(dbCol)] = true
		}
	}
	for i, excelCol := range excelColumns {
		if _, exists := result[i]; exists {
			continue
		}
		if orig, ok := dbByUpper[strings.ToUpper(excelCol)]; ok && !usedDBCols[strings.ToUpper(orig)] {
			result[i] = orig
			usedDBCols[strings.ToUpper(orig)] = true
			continue
		}
		norm := normalizeColName(excelCol)
		if orig, ok := dbByNorm[norm]; ok && !usedDBCols[strings.ToUpper(orig)] {
			result[i] = orig
			usedDBCols[strings.ToUpper(orig)] = true
		}
	}
	return result, nil
}

func getTableColumns(conn *sqlx.DB, dbType, dbSchema, tableName string) ([]string, error) {
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{tableName}
		}
	case "oracle":
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM ALL_TAB_COLUMNS WHERE OWNER = :1 AND TABLE_NAME = :2 ORDER BY COLUMN_ID"
			args = []any{strings.ToUpper(dbSchema), strings.ToUpper(tableName)}
		} else {
			query = "SELECT COLUMN_NAME FROM USER_TAB_COLUMNS WHERE TABLE_NAME = :1 ORDER BY COLUMN_ID"
			args = []any{strings.ToUpper(tableName)}
		}
	case "sqlite":
		query = fmt.Sprintf("PRAGMA table_info('%s')", tableName)
	default:
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{tableName}
		}
	}
	if dbType == "sqlite" {
		rows, err := conn.Queryx(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var cols []string
		for rows.Next() {
			var cid int
			var name, colType string
			var notNull int
			var dfltValue *string
			var pk int
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
				continue
			}
			cols = append(cols, name)
		}
		return cols, nil
	}
	rows, err := conn.Queryx(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			continue
		}
		cols = append(cols, colName)
	}
	return cols, nil
}

func getPrimaryKeys(conn *sqlx.DB, dbType, dbSchema, tableName string) ([]string, error) {
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = ? AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = DATABASE() AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
			args = []any{tableName}
		}
	case "oracle":
		if dbSchema != "" {
			query = `SELECT cols.COLUMN_NAME FROM ALL_CONSTRAINTS cons JOIN ALL_CONS_COLUMNS cols ON cons.CONSTRAINT_NAME = cols.CONSTRAINT_NAME AND cons.OWNER = cols.OWNER WHERE cons.CONSTRAINT_TYPE = 'P' AND cons.OWNER = :1 AND cons.TABLE_NAME = :2 ORDER BY cols.POSITION`
			args = []any{strings.ToUpper(dbSchema), strings.ToUpper(tableName)}
		} else {
			query = `SELECT cols.COLUMN_NAME FROM USER_CONSTRAINTS cons JOIN USER_CONS_COLUMNS cols ON cons.CONSTRAINT_NAME = cols.CONSTRAINT_NAME WHERE cons.CONSTRAINT_TYPE = 'P' AND cons.TABLE_NAME = :1 ORDER BY cols.POSITION`
			args = []any{strings.ToUpper(tableName)}
		}
	case "sqlite":
		query = fmt.Sprintf("PRAGMA table_info('%s')", tableName)
		rows, err := conn.Queryx(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var keys []string
		for rows.Next() {
			var cid int
			var name, colType string
			var notNull int
			var dfltValue *string
			var pk int
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
				continue
			}
			if pk > 0 {
				keys = append(keys, name)
			}
		}
		return keys, nil
	default:
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = ? AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = DATABASE() AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
			args = []any{tableName}
		}
	}
	rows, err := conn.Queryx(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			continue
		}
		keys = append(keys, colName)
	}
	return keys, nil
}

func checkRowExists(tx *sqlx.Tx, dbType, dbSchema, tableName string, columns []string, row []string, primaryKeys []string) (bool, error) {
	var whereParts []string
	var args []any
	argIdx := 0
	for _, pk := range primaryKeys {
		for i, col := range columns {
			if strings.EqualFold(col, pk) {
				if dbType == "oracle" {
					argIdx++
					whereParts = append(whereParts, fmt.Sprintf("%s = :%d", pk, argIdx))
				} else {
					whereParts = append(whereParts, fmt.Sprintf("%s = ?", pk))
				}
				args = append(args, row[i])
				break
			}
		}
	}
	if len(whereParts) == 0 {
		return false, nil
	}
	tableRef := tableName
	if dbSchema != "" {
		tableRef = dbSchema + "." + tableName
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", tableRef, strings.Join(whereParts, " AND "))
	var count int
	err := tx.Get(&count, query, args...)
	return count > 0, err
}

func insertRow(tx *sqlx.Tx, dbType, dbSchema, tableName string, columns []string, row []string) error {
	placeholders := make([]string, len(columns))
	args := make([]any, len(columns))
	for i := range columns {
		if dbType == "oracle" {
			placeholders[i] = fmt.Sprintf(":%d", i+1)
		} else {
			placeholders[i] = "?"
		}
		args[i] = row[i]
	}
	tableRef := tableName
	if dbSchema != "" {
		tableRef = dbSchema + "." + tableName
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableRef, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
	_, err := tx.Exec(query, args...)
	return err
}

func updateRow(tx *sqlx.Tx, dbType, dbSchema, tableName string, columns []string, row []string, primaryKeys []string) error {
	pkSet := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkSet[strings.ToUpper(pk)] = true
	}
	var setParts, whereParts []string
	var setArgs, whereArgs []any
	argIdx := 0
	for i, col := range columns {
		if pkSet[strings.ToUpper(col)] {
			if dbType == "oracle" {
				argIdx++
				whereParts = append(whereParts, fmt.Sprintf("%s = :%d", col, argIdx+len(columns)))
			} else {
				whereParts = append(whereParts, fmt.Sprintf("%s = ?", col))
			}
			whereArgs = append(whereArgs, row[i])
		} else {
			if dbType == "oracle" {
				argIdx++
				setParts = append(setParts, fmt.Sprintf("%s = :%d", col, argIdx))
			} else {
				setParts = append(setParts, fmt.Sprintf("%s = ?", col))
			}
			setArgs = append(setArgs, row[i])
		}
	}
	if len(setParts) == 0 || len(whereParts) == 0 {
		return fmt.Errorf("cannot build update: missing SET or WHERE")
	}
	args := append(setArgs, whereArgs...)
	tableRef := tableName
	if dbSchema != "" {
		tableRef = dbSchema + "." + tableName
	}
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tableRef, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))
	_, err := tx.Exec(query, args...)
	return err
}

func applyRowLimit(sql, driverName string, maxRows int) string {
	upper := strings.ToUpper(sql)
	switch driverName {
	case "oracle":
		if strings.Contains(upper, "ROWNUM") || strings.Contains(upper, "FETCH ") {
			return sql
		}
		return fmt.Sprintf("SELECT * FROM (%s) WHERE ROWNUM <= %d", sql, maxRows)
	default:
		if strings.Contains(upper, " LIMIT ") {
			return sql
		}
		return fmt.Sprintf("%s LIMIT %d", sql, maxRows)
	}
}
