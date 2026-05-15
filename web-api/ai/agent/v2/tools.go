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

	"github.com/jmoiron/sqlx"
)

var validTableNameRegex = regexp.MustCompile("^[a-zA-Z0-9_`\".]+$")

var dangerousTableNamePatterns = []string{
	";", "--", "/*", "*/", "'", "\\",
	"xp_", "sp_", "0x", "char(", "exec(",
	"union", "select", "insert", "update", "delete",
	"drop", "alter", "create", "truncate", "exec",
}

func isValidTableName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || !validTableNameRegex.MatchString(name) {
		return false
	}
	lower := strings.ToLower(name)
	for _, pattern := range dangerousTableNamePatterns {
		if strings.Contains(lower, pattern) {
			return false
		}
	}
	return true
}

func safeQuoteTableName(name string) string {
	if !isValidTableName(name) {
		return ""
	}
	cleaned := strings.ReplaceAll(name, "`", "")
	cleaned = strings.ReplaceAll(cleaned, `"`, "")
	cleaned = strings.ReplaceAll(cleaned, ".", "")
	return "'" + strings.ReplaceAll(cleaned, "'", "''") + "'"
}

type QueryInput struct {
	SQL    string `json:"sql" jsonschema:"required"`
	ConnID string `json:"connId,omitempty"`
}
type QueryOutput struct {
	Columns []string         `json:"columns"`
	Data    []map[string]any `json:"data"`
	Count   int              `json:"count"`
}
type ExecInput struct {
	SQL    string `json:"sql" jsonschema:"required"`
	ConnID string `json:"connId,omitempty"`
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
type CurrentDateInfoInput struct{}
type CurrentDateInfoOutput struct {
	Date     string `json:"date"`
	Weekday  string `json:"weekday"`
	DateTime string `json:"dateTime"`
}

func GetConn(connId string) (*sqlx.DB, string) {
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

func GetCurrentDateInfo() func(ctx context.Context, input *CurrentDateInfoInput) (*CurrentDateInfoOutput, error) {
	return func(ctx context.Context, input *CurrentDateInfoInput) (*CurrentDateInfoOutput, error) {
		now := time.Now()
		weekdayNames := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
		weekday := weekdayNames[now.Weekday()]
		return &CurrentDateInfoOutput{
			Date:     now.Format("2006-01-02"),
			Weekday:  weekday,
			DateTime: now.Format("2006-01-02 15:04:05"),
		}, nil
	}
}

func buildConnLookup(schemas []SchemaRef) map[string]string {
	connLookup := make(map[string]string)
	for _, s := range schemas {
		if s.ConnID != "" && s.Schema != "" {
			key := strings.ToUpper(s.Schema)
			if existing, ok := connLookup[key]; ok && existing != s.ConnID {
				logutils.PrintErr(fmt.Errorf("schema name collision: %q 同时存在于连接 %s 和 %s，将使用后者",
					s.Schema, existing, s.ConnID))
			}
			connLookup[key] = s.ConnID
		}
	}
	return connLookup
}

// buildSchemaNames 返回所有 schema 名的切片，用于错误提示
func buildSchemaNames(schemas []SchemaRef) []string {
	seen := make(map[string]bool)
	var names []string
	for _, s := range schemas {
		if s.Schema != "" && !seen[s.Schema] {
			seen[s.Schema] = true
			names = append(names, s.Schema)
		}
	}
	return names
}

func resolveConnID(defaultConnID, connID string, connLookup map[string]string) string {
	if connID == "" {
		return defaultConnID
	}
	if schemaConnID, ok := connLookup[strings.ToUpper(connID)]; ok {
		return schemaConnID
	}
	return connID
}

func NewQueryFunc(connId string, schemas []SchemaRef) func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
	connLookup := buildConnLookup(schemas)
	schemaNames := buildSchemaNames(schemas)
	return func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
		log.Printf("[Tool:query_data] sql=%s connId=%s\n", input.SQL, input.ConnID)
		targetConnID := resolveConnID(connId, input.ConnID, connLookup)
		conn, _ := GetConn(targetConnID)
		if conn == nil {
			msg := fmt.Sprintf("db conn not found: %s", targetConnID)
			if len(schemaNames) > 0 {
				msg += fmt.Sprintf("。可用 schema 名：%v（作为 connId 传入），默认连接ID：%s", schemaNames, connId)
			}
			return nil, fmt.Errorf("%s", msg)
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
		queryCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		rows, err := conn.QueryxContext(queryCtx, sql)
		if err != nil {
			if input.ConnID == "" && targetConnID == connId {
				if altConnID, altConn := tryAlternativeConn(sql, connLookup, connId); altConn != nil {
					log.Printf("[Tool:query_data] 默认连接查询失败，自动路由到连接 %s（schema=%s）\n", altConnID, input.ConnID)
					altQueryCtx, altCancel := context.WithTimeout(ctx, 60*time.Second)
					defer altCancel()
					if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") {
						sql = applyRowLimit(sql, altConn.DriverName(), 2000)
					}
					altRows, altErr := altConn.QueryxContext(altQueryCtx, sql)
					if altErr == nil {
						defer altRows.Close()
						cols, _ := altRows.Columns()
						data := dbutils.GetResultRows(altConn.DriverName(), altRows)
						return &QueryOutput{Columns: cols, Data: data, Count: len(data)}, nil
					}
					log.Printf("[Tool:query_data] 自动路由查询也失败: %v，返回原始错误\n", altErr)
				}
			}
			return nil, fmt.Errorf("query failed: %w", err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		data := dbutils.GetResultRows(conn.DriverName(), rows)
		return &QueryOutput{Columns: cols, Data: data, Count: len(data)}, nil
	}
}

func tryAlternativeConn(sql string, connLookup map[string]string, defaultConnID string) (string, *sqlx.DB) {
	schema := extractSchemaFromSQL(sql)
	if schema == "" {
		return "", nil
	}
	altConnID, ok := connLookup[strings.ToUpper(schema)]
	if !ok || altConnID == defaultConnID {
		return "", nil
	}
	altConn, _ := GetConn(altConnID)
	if altConn == nil {
		return "", nil
	}
	return altConnID, altConn
}

func extractSchemaFromSQL(sql string) string {
	re := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+(?:` + "`" + `([^` + "`" + `]+)` + "`" + `|\"([^\"]+)\"|\[([^\]]+)\]|(\w+))\s*\.\s*(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\"[^\"]+\"|\[[^\]]+\]|\w+)`)
	matches := re.FindStringSubmatch(sql)
	if len(matches) > 1 {
		for i := 1; i < len(matches); i++ {
			if matches[i] != "" {
				return matches[i]
			}
		}
	}
	return ""
}

// ExecAuditCtx holds context for audit logging within exec_sql tool
type ExecAuditCtx struct {
	ConnID    string
	UserID    string
	UserName  string
	SessionID string
}

func NewExecFunc(connId string, schemas []SchemaRef, auditCtx *ExecAuditCtx) func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
	connLookup := buildConnLookup(schemas)
	schemaNames := buildSchemaNames(schemas)
	return func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
		log.Printf("[Tool:exec_sql] sql=%s connId=%s\n", input.SQL, input.ConnID)
		sql := strings.TrimSpace(input.SQL)
		if sql == "" {
			return nil, fmt.Errorf("SQL is empty")
		}

		targetConnID := resolveConnID(connId, input.ConnID, connLookup)
		isDefaultConn := targetConnID == connId
		if !isDefaultConn && input.ConnID != "" {
			log.Printf("[Tool:exec_sql] 非默认连接操作：原始connId=%s → 目标连接=%s（默认=%s）", input.ConnID, targetConnID, connId)
		}
		conn, _ := GetConn(targetConnID)
		if conn == nil {
			msg := fmt.Sprintf("db conn not found: %s（原始输入 connId=%s，解析后=%s）", targetConnID, input.ConnID, targetConnID)
			if len(schemaNames) > 0 {
				msg += fmt.Sprintf("。可用 schema 名：%v（作为 connId 传入），默认连接ID：%s", schemaNames, connId)
			}
			return nil, fmt.Errorf("%s", msg)
		}

		// 审计日志
		auditID := utils.RandomStr()
		sqlType := detectSQLType(sql)
		riskLevel := detectRiskLevel(sql)

		result, err := conn.ExecContext(ctx, sql)
		if err != nil {
			if auditCtx != nil {
				InsertSQLAudit(auditID, auditCtx.UserID, auditCtx.UserName, targetConnID, auditCtx.SessionID, sql, sqlType, riskLevel, "failed", 0, err.Error())
			}
			return nil, fmt.Errorf("exec failed on conn=%s: %w", targetConnID, err)
		}
		affected, _ := result.RowsAffected()
		if auditCtx != nil {
			InsertSQLAudit(auditID, auditCtx.UserID, auditCtx.UserName, targetConnID, auditCtx.SessionID, sql, sqlType, riskLevel, "success", int(affected), "")
		}
		msg := fmt.Sprintf("ok, %d rows affected（连接：%s）", affected, targetConnID)
		log.Printf("[Tool:exec_sql] ok - %s\n", msg)
		return &ExecOutput{AffectedRows: affected, Message: msg}, nil
	}
}

func NewSchemaFunc(connId, dbType, dbSchema string, schemas []SchemaRef) func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
	schemaConnMap := buildConnLookup(schemas)
	schemaNames := buildSchemaNames(schemas)

	return func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
		log.Printf("[Tool:get_table_schema] tables=%v\n", input.Tables)

		getTableConn := func(table string) (*sqlx.DB, string, string) {
			schemaName, _ := splitSchemaTable(table, dbSchema)
			if schemaName != "" && schemaName != dbSchema {
				if targetConnID, ok := schemaConnMap[strings.ToUpper(schemaName)]; ok {
					if c, t := GetConn(targetConnID); c != nil {
						return c, t, targetConnID
					}
				}
			}
			c, t := GetConn(connId)
			return c, t, connId
		}

		schemaCtx, schemaCancel := context.WithTimeout(ctx, 30*time.Second)
		defer schemaCancel()

		var sb strings.Builder
		for _, table := range input.Tables {
			if !isValidTableName(table) {
				logutils.PrintErr(fmt.Errorf("invalid table name: %s", table))
				continue
			}
			schemaName, tableName := splitSchemaTable(table, dbSchema)
			tableConn, actualDBType, _ := getTableConn(table)

			if tableConn == nil {
				msg := fmt.Sprintf("db conn not found for table: %s", table)
				if len(schemaNames) > 0 {
					msg += fmt.Sprintf("。可用 schema 名：%v", schemaNames)
				}
				logutils.PrintErr(fmt.Errorf("%s", msg))
				continue
			}

			var schemaSQL string
			switch actualDBType {
			case "mysql", "mariadb":
				if schemaName != "" {
					schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", schemaName, tableName)
				} else {
					schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`", tableName)
				}
			case "sqlite":
				schemaSQL = "SELECT sql FROM sqlite_master WHERE type='table' AND name=?"
			case "oracle":
				if schemaName != "" && !strings.EqualFold(schemaName, dbSchema) {
					schemaSQL = fmt.Sprintf("SELECT DBMS_METADATA.GET_DDL('TABLE', '%s', '%s') FROM DUAL",
						strings.ToUpper(tableName), strings.ToUpper(schemaName))
				} else {
					schemaSQL = fmt.Sprintf("SELECT DBMS_METADATA.GET_DDL('TABLE', '%s') FROM DUAL",
						strings.ToUpper(tableName))
				}
			default:
				if schemaName != "" {
					schemaSQL = fmt.Sprintf("SHOW CREATE TABLE \"%s\".\"%s\"", schemaName, tableName)
				} else {
					schemaSQL = fmt.Sprintf("SHOW CREATE TABLE \"%s\"", tableName)
				}
			}
			var rows *sqlx.Rows
			var err error
			switch actualDBType {
			case "sqlite":
				rows, err = tableConn.QueryxContext(schemaCtx, schemaSQL, table)
			default:
				rows, err = tableConn.QueryxContext(schemaCtx, schemaSQL)
			}
			if err != nil {
				logutils.PrintErr(fmt.Errorf("get schema failed %s: %w", table, err))
				sb.WriteString(fallbackColumnInfo(tableConn, actualDBType, schemaName, tableName))
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
	fmt.Fprintf(&sb, "-- Table: %s", table)
	if dbSchema != "" && !strings.EqualFold(dbSchema, table) {
		fmt.Fprintf(&sb, " (schema: %s)", dbSchema)
	}
	sb.WriteString("\n")
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		query = "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
		args = []any{dbSchema, table}
	case "sqlite":
		quoted := safeQuoteTableName(table)
		if quoted == "" {
			return sb.String()
		}
		query = "PRAGMA table_info(" + quoted + ")"
	case "oracle":
		if !isValidTableName(table) {
			return sb.String()
		}
		if dbSchema != "" {
			query = fmt.Sprintf("SELECT COLUMN_NAME, DATA_TYPE, DATA_LENGTH, NULLABLE FROM ALL_TAB_COLUMNS WHERE OWNER = '%s' AND TABLE_NAME = '%s' ORDER BY COLUMN_ID",
				strings.ToUpper(dbSchema), strings.ToUpper(table))
		} else {
			query = fmt.Sprintf("SELECT COLUMN_NAME, DATA_TYPE, DATA_LENGTH, NULLABLE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = '%s' ORDER BY COLUMN_ID",
				strings.ToUpper(table))
		}
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
		log.Printf("[Tool:import_data] fileId=%s, table=%s, mode=%s\n", input.FileID, input.TableName, input.Mode)
		conn, _ := GetConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("db conn not found: %s", connID)
		}
		if input.FileID == "" {
			return nil, fmt.Errorf("fileId is required")
		}
		if !isValidTableName(input.TableName) {
			return nil, fmt.Errorf("invalid table name: %s", input.TableName)
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

		if mode == "insert" {
			// 批量插入：每 200 行一批，使用 prepared statement
			batchSize := 200
			tableRef := quoteTableRef(dbType, dbSchema, input.TableName)
			quotedCols := make([]string, len(dbColumns))
			for i, col := range dbColumns {
				quotedCols[i] = quoteIdent(dbType, col)
			}
			colList := strings.Join(quotedCols, ", ")

			for batchStart := 0; batchStart < len(upload.Data); batchStart += batchSize {
				batchEnd := batchStart + batchSize
				if batchEnd > len(upload.Data) {
					batchEnd = len(upload.Data)
				}
				batch := upload.Data[batchStart:batchEnd]

				// 构建多行 VALUES
				var valueParts []string
				var allArgs []any
				for _, excelRow := range batch {
					row := make([]string, len(dbColumns))
					for i, idx := range excelIndices {
						if idx < len(excelRow) {
							row[i] = excelRow[idx]
						}
					}
					placeholders := make([]string, len(dbColumns))
					for i := range dbColumns {
						if dbType == "oracle" {
							placeholders[i] = fmt.Sprintf(":%d", len(allArgs)+i+1)
						} else {
							placeholders[i] = "?"
						}
						allArgs = append(allArgs, row[i])
					}
					valueParts = append(valueParts, "("+strings.Join(placeholders, ", ")+")")
				}

				if dbType == "oracle" {
					// Oracle 不支持多行 VALUES，逐行插入
					for _, excelRow := range batch {
						row := make([]string, len(dbColumns))
						for i, idx := range excelIndices {
							if idx < len(excelRow) {
								row[i] = excelRow[idx]
							}
						}
						if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
							return nil, fmt.Errorf("row %d insert failed: %w", batchStart+insertedRows+2, err)
						}
						insertedRows++
					}
				} else {
					query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", tableRef, colList, strings.Join(valueParts, ", "))
					if _, err := tx.Exec(query, allArgs...); err != nil {
						return nil, fmt.Errorf("batch insert failed at row %d: %w", batchStart+2, err)
					}
					insertedRows += len(batch)
				}
			}
		} else {
			// upsert 模式：逐行处理
			for rowNum, excelRow := range upload.Data {
				row := make([]string, len(dbColumns))
				for i, idx := range excelIndices {
					if idx < len(excelRow) {
						row[i] = excelRow[idx]
					}
				}
				if len(primaryKeys) > 0 {
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
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit failed: %w", err)
		}
		RemoveUploadedFile(input.FileID)

		// 审计日志
		auditID := utils.RandomStr()
		auditSQL := fmt.Sprintf("IMPORT INTO %s (%d rows, mode=%s)", input.TableName, insertedRows+updatedRows, mode)
		InsertSQLAudit(auditID, "", "", connID, "", auditSQL, "IMPORT", "medium", "success", insertedRows+updatedRows, "")

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
		log.Printf("[Tool:import_data] done - %s\n", msg)
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
		quoted := safeQuoteTableName(tableName)
		if quoted == "" {
			return nil, fmt.Errorf("invalid table name: %s", tableName)
		}
		query = "PRAGMA table_info(" + quoted + ")"
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
		quoted := safeQuoteTableName(tableName)
		if quoted == "" {
			return nil, fmt.Errorf("invalid table name: %s", tableName)
		}
		query = "PRAGMA table_info(" + quoted + ")"
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
					whereParts = append(whereParts, fmt.Sprintf("%s = :%d", quoteIdent(dbType, pk), argIdx))
				} else {
					whereParts = append(whereParts, fmt.Sprintf("%s = ?", quoteIdent(dbType, pk)))
				}
				args = append(args, row[i])
				break
			}
		}
	}
	if len(whereParts) == 0 {
		return false, nil
	}
	tableRef := quoteTableRef(dbType, dbSchema, tableName)
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", tableRef, strings.Join(whereParts, " AND "))
	var count int
	err := tx.Get(&count, query, args...)
	return count > 0, err
}

func insertRow(tx *sqlx.Tx, dbType, dbSchema, tableName string, columns []string, row []string) error {
	placeholders := make([]string, len(columns))
	quotedCols := make([]string, len(columns))
	args := make([]any, len(columns))
	for i := range columns {
		quotedCols[i] = quoteIdent(dbType, columns[i])
		if dbType == "oracle" {
			placeholders[i] = fmt.Sprintf(":%d", i+1)
		} else {
			placeholders[i] = "?"
		}
		args[i] = row[i]
	}
	tableRef := quoteTableRef(dbType, dbSchema, tableName)
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableRef, strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))
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
		qCol := quoteIdent(dbType, col)
		if pkSet[strings.ToUpper(col)] {
			if dbType == "oracle" {
				argIdx++
				whereParts = append(whereParts, fmt.Sprintf("%s = :%d", qCol, argIdx+len(columns)))
			} else {
				whereParts = append(whereParts, fmt.Sprintf("%s = ?", qCol))
			}
			whereArgs = append(whereArgs, row[i])
		} else {
			if dbType == "oracle" {
				argIdx++
				setParts = append(setParts, fmt.Sprintf("%s = :%d", qCol, argIdx))
			} else {
				setParts = append(setParts, fmt.Sprintf("%s = ?", qCol))
			}
			setArgs = append(setArgs, row[i])
		}
	}
	if len(setParts) == 0 || len(whereParts) == 0 {
		return fmt.Errorf("cannot build update: missing SET or WHERE")
	}
	args := append(setArgs, whereArgs...)
	tableRef := quoteTableRef(dbType, dbSchema, tableName)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tableRef, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))
	_, err := tx.Exec(query, args...)
	return err
}

// quoteIdent 根据数据库类型对标识符加引号，防止保留字冲突
func quoteIdent(dbType, name string) string {
	switch dbType {
	case "oracle":
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	case "postgresql", "postgres":
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	case "sqlserver", "mssql":
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default: // mysql, mariadb, sqlite
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
}

// quoteTableRef 构建带 schema 的表引用
func splitSchemaTable(table, dbSchema string) (schema, tableName string) {
	schema = dbSchema
	tableName = table
	dotIdx := strings.IndexByte(table, '.')
	if dotIdx > 0 && dotIdx < len(table)-1 {
		schema = table[:dotIdx]
		tableName = table[dotIdx+1:]
	}
	return
}

func quoteTableRef(dbType, dbSchema, tableName string) string {
	if dbSchema != "" {
		return quoteIdent(dbType, dbSchema) + "." + quoteIdent(dbType, tableName)
	}
	return quoteIdent(dbType, tableName)
}

func applyRowLimit(sql, driverName string, maxRows int) string {
	trimmed := strings.TrimRight(sql, "; \t\r\n")
	upper := strings.ToUpper(trimmed)
	switch driverName {
	case "oracle":
		if strings.Contains(upper, "ROWNUM") ||
			strings.Contains(upper, "FETCH NEXT") ||
			strings.Contains(upper, "FETCH FIRST") {
			return sql
		}
		return fmt.Sprintf("SELECT * FROM (%s) WHERE ROWNUM <= %d", trimmed, maxRows)
	default:
		if reLimit.MatchString(upper) {
			return sql
		}
		return fmt.Sprintf("%s LIMIT %d", trimmed, maxRows)
	}
}

var reLimit = regexp.MustCompile(`(?i)\bLIMIT\s+\d+`)
