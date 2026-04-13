package agentv2

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"

	"github.com/jmoiron/sqlx"
)

// ──────────────────────────────────────────────
// Tool 输入/输出结构体
// ──────────────────────────────────────────────

type QueryInput struct {
	SQL string `json:"sql" jsonschema:"required" jsonschema_description:"要执行的 SELECT SQL 语句"`
}
type QueryOutput struct {
	Columns []string         `json:"columns"`
	Data    []map[string]any `json:"data"`
	Count   int              `json:"count"`
}
type ExecInput struct {
	SQL string `json:"sql" jsonschema:"required" jsonschema_description:"要执行的写操作 SQL 语句"`
}
type ExecOutput struct {
	AffectedRows int64  `json:"affectedRows"`
	Message      string `json:"message"`
}
type SchemaInput struct {
	Tables []string `json:"tables" jsonschema:"required" jsonschema_description:"要查询结构的表名列表"`
}
type SchemaOutput struct {
	Schema string `json:"schema"`
}
type ExportInput struct {
	SQL      string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出数据的 SELECT SQL"`
	FileName string `json:"fileName" jsonschema_description:"导出文件名（不含扩展名）"`
}
type ExportOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
}

type CurrentDateTimeInput struct {
}

type CurrentDateTimeOutput struct {
	DateTime string `json:"dateTime"`
}

// ──────────────────────────────────────────────
// 数据库连接
// ──────────────────────────────────────────────

func getConn(connId string) (*sqlx.DB, string) {
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connId)
	if err != nil || len(cfgList) == 0 {
		return nil, ""
	}
	cfg := &cfgList[0]
	pwd := ""
	if cfg.Pwd != nil {
		pwd = utils.AESDecode(*cfg.Pwd)
	}
	name := ""
	if cfg.Name != nil {
		name = *cfg.Name
	}
	user := ""
	if cfg.User != nil {
		user = *cfg.User
	}
	url := ""
	if cfg.Url != nil {
		url = *cfg.Url
	}
	conn := config.GetConn(&config.DBParam{
		Id: cfg.Id, Name: name, DbType: cfg.DbType,
		User: user, Pwd: pwd, Url: url,
	})
	return conn, cfg.DbType
}

// ──────────────────────────────────────────────
// Tool 实现
// ──────────────────────────────────────────────

// 获取当前日期时间的tool
func GetCurrentDateTime() func(ctx context.Context, input *CurrentDateTimeInput) (*CurrentDateTimeOutput, error) {
	return func(ctx context.Context, input *CurrentDateTimeInput) (*CurrentDateTimeOutput, error) {
		return &CurrentDateTimeOutput{DateTime: time.Now().Format("2006-01-02 15:09:05")}, nil
	}
}

func NewQueryFunc(connId string) func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
	return func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
		log.Printf("[Tool:query_data] sql=%s\n", input.SQL)
		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}
		sql := strings.TrimSpace(input.SQL)
		upper := strings.ToUpper(sql)
		if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "SHOW") &&
			!strings.HasPrefix(upper, "DESCRIBE") && !strings.HasPrefix(upper, "EXPLAIN") &&
			!strings.HasPrefix(upper, "WITH") {
			return nil, fmt.Errorf("query_data 仅支持 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH  语句")
		}
		rows, err := conn.Queryx(sql)
		if err != nil {
			log.Printf("[Tool:query_data] 查询失败 - err=%v\n", err)
			return nil, fmt.Errorf("查询失败：%w", err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("获取列信息失败：%w", err)
		}
		data := dbutils.GetResultRows(conn.DriverName(), rows)
		log.Printf("[Tool:query_data] 成功 - columns=%d, rows=%d\n", len(cols), len(data))
		return &QueryOutput{Columns: cols, Data: data, Count: len(data)}, nil
	}
}

func NewExecFunc(connId string) func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
	return func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
		log.Printf("[Tool:exec_sql] sql=%s\n", input.SQL)
		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}
		sql := strings.TrimSpace(input.SQL)
		if !strings.Contains(sql, "-- CONFIRMED:") {
			for line := range strings.SplitSeq(sql, ";") {
				if isDangerousSQL(line) {
					return nil, &DangerousSQLError{SQL: sql}
				}
			}
			// return nil, fmt.Errorf("此操作需要用户确认")
		}
		var actualLines []string
		for line := range strings.SplitSeq(sql, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "-- CONFIRMED:") {
				actualLines = append(actualLines, line)
			}
		}
		actualSQL := strings.TrimSpace(strings.Join(actualLines, "\n"))
		if actualSQL == "" {
			return nil, fmt.Errorf("SQL 不能为空")
		}
		result, err := conn.Exec(actualSQL)
		if err != nil {
			log.Printf("[Tool:exec_sql] 执行失败 - err=%v\n", err)
			return nil, fmt.Errorf("执行失败：%w", err)
		}
		affected, _ := result.RowsAffected()
		log.Printf("[Tool:exec_sql] 成功 - affectedRows=%d\n", affected)
		return &ExecOutput{AffectedRows: affected, Message: fmt.Sprintf("执行成功，影响 %d 行", affected)}, nil
	}
}

func NewSchemaFunc(connId, dbType, dbSchema string) func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
	return func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
		log.Printf("[Tool:get_table_schema] tables=%v\n", input.Tables)
		conn, actualDBType := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}
		var sb strings.Builder
		for _, table := range input.Tables {
			var schemaSQL string
			switch actualDBType {
			case "mysql", "mariadb":
				schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`", table)
			case "sqlite":
				schemaSQL = fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='table' AND name='%s'", table)
			case "oracle":
				schemaSQL = fmt.Sprintf("SELECT DBMS_METADATA.GET_DDL('TABLE', '%s') FROM DUAL", strings.ToUpper(table))
			default:
				schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`", table)
			}
			rows, err := conn.Query(schemaSQL)
			if err != nil {
				logutils.PrintErr(fmt.Errorf("获取表结构失败 %s: %w", table, err))
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

// Export tool functions are defined in export_tools.go

// ──────────────────────────────────────────────
// 数据导入工具（从后端暂存的 Excel 文件读取全量数据）
// ──────────────────────────────────────────────

type ImportDataInput struct {
	FileID    string            `json:"fileId" jsonschema:"required" jsonschema_description:"后端返回的上传文件 ID"`
	TableName string            `json:"tableName" jsonschema:"required" jsonschema_description:"目标表名"`
	Mapping   map[string]string `json:"mapping" jsonschema:"required" jsonschema_description:"字段映射：key=Excel列名, value=数据库字段名。所有Excel列都必须映射到表中实际存在的字段。"`
	Mode      string            `json:"mode" jsonschema_description:"导入模式: insert（仅插入）或 upsert（有主键则更新无则插入）。默认 insert"`
}

type ImportDataOutput struct {
	Message      string `json:"message"`
	InsertedRows int    `json:"insertedRows"`
	UpdatedRows  int    `json:"updatedRows"`
}

// NewImportDataFunc 数据导入工具 — 从后端暂存区读取 Excel 全量数据，按字段映射导入目标表
func NewImportDataFunc(connID, dbType, dbSchema string) func(ctx context.Context, input *ImportDataInput) (*ImportDataOutput, error) {
	return func(ctx context.Context, input *ImportDataInput) (*ImportDataOutput, error) {
		log.Printf("[Tool:import_data] fileId=%s, table=%s, mapping=%v, mode=%s\n",
			input.FileID, input.TableName, input.Mapping, input.Mode)

		conn, _ := getConn(connID)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connID)
		}
		if input.FileID == "" {
			return nil, fmt.Errorf("fileId 不能为空，请先上传 Excel 文件")
		}
		if len(input.Mapping) == 0 {
			return nil, fmt.Errorf("字段映射不能为空")
		}

		mode := strings.ToLower(input.Mode)
		if mode == "" {
			mode = "insert"
		}

		// 从暂存区读取 Excel 文件
		upload, err := GetUploadedFile(input.FileID)
		if err != nil {
			return nil, err
		}

		// 验证表结构
		tableColumns, err := getTableColumns(conn, dbType, dbSchema, input.TableName)
		if err != nil {
			return nil, fmt.Errorf("获取表 %s 的列信息失败：%w", input.TableName, err)
		}
		tableColSet := make(map[string]bool)
		for _, c := range tableColumns {
			tableColSet[strings.ToUpper(c)] = true
		}

		// 构建映射索引
		excelColIdx := make(map[string]int)
		for i, c := range upload.Columns {
			excelColIdx[c] = i
		}

		var dbColumns []string
		var excelIndices []int
		for excelCol, dbCol := range input.Mapping {
			idx, ok := excelColIdx[excelCol]
			if !ok {
				return nil, fmt.Errorf("Excel 列 '%s' 在上传文件中不存在，可用列：%s", excelCol, strings.Join(upload.Columns, ", "))
			}
			if !tableColSet[strings.ToUpper(dbCol)] {
				return nil, fmt.Errorf("字段 '%s' 在表 '%s' 中不存在", dbCol, input.TableName)
			}
			dbColumns = append(dbColumns, dbCol)
			excelIndices = append(excelIndices, idx)
		}

		// 获取主键
		primaryKeys, _ := getPrimaryKeys(conn, dbType, dbSchema, input.TableName)

		tx, err := conn.Beginx()
		if err != nil {
			return nil, fmt.Errorf("开启事务失败：%w", err)
		}
		defer tx.Rollback()

		insertedRows, updatedRows := 0, 0
		for _, excelRow := range upload.Data {
			row := make([]string, len(dbColumns))
			for i, idx := range excelIndices {
				if idx < len(excelRow) {
					row[i] = excelRow[idx]
				}
			}
			if mode == "upsert" && len(primaryKeys) > 0 {
				exists, err := checkRowExists(tx, dbType, dbSchema, input.TableName, dbColumns, row, primaryKeys)
				if err != nil {
					return nil, fmt.Errorf("检查数据是否存在失败：%w", err)
				}
				if exists {
					if err := updateRow(tx, dbType, dbSchema, input.TableName, dbColumns, row, primaryKeys); err != nil {
						return nil, fmt.Errorf("更新数据失败：%w", err)
					}
					updatedRows++
				} else {
					if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
						return nil, fmt.Errorf("插入数据失败：%w", err)
					}
					insertedRows++
				}
			} else {
				if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
					return nil, fmt.Errorf("插入数据失败：%w", err)
				}
				insertedRows++
			}
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("提交事务失败：%w", err)
		}

		RemoveUploadedFile(input.FileID)

		msg := fmt.Sprintf("导入完成：插入 %d 行", insertedRows)
		if updatedRows > 0 {
			msg += fmt.Sprintf("，更新 %d 行", updatedRows)
		}
		log.Printf("[Tool:import_data] %s\n", msg)
		return &ImportDataOutput{Message: msg, InsertedRows: insertedRows, UpdatedRows: updatedRows}, nil
	}
}

// ──────────────────────────────────────────────
// 数据库辅助函数
// ──────────────────────────────────────────────

func getTableColumns(conn *sqlx.DB, dbType, dbSchema, tableName string) ([]string, error) {
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
		args = []any{dbSchema, tableName}
	case "oracle":
		query = "SELECT COLUMN_NAME FROM ALL_TAB_COLUMNS WHERE OWNER = :1 AND TABLE_NAME = :2 ORDER BY COLUMN_ID"
		args = []any{strings.ToUpper(dbSchema), strings.ToUpper(tableName)}
	case "sqlite":
		query = fmt.Sprintf("PRAGMA table_info('%s')", tableName)
	default:
		query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
		args = []any{dbSchema, tableName}
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
		query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = ? AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
		args = []any{dbSchema, tableName}
	case "oracle":
		query = `SELECT cols.COLUMN_NAME FROM ALL_CONSTRAINTS cons JOIN ALL_CONS_COLUMNS cols ON cons.CONSTRAINT_NAME = cols.CONSTRAINT_NAME AND cons.OWNER = cols.OWNER WHERE cons.CONSTRAINT_TYPE = 'P' AND cons.OWNER = :1 AND cons.TABLE_NAME = :2 ORDER BY cols.POSITION`
		args = []any{strings.ToUpper(dbSchema), strings.ToUpper(tableName)}
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
		query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = ? AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
		args = []any{dbSchema, tableName}
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
	for _, pk := range primaryKeys {
		for i, col := range columns {
			if strings.EqualFold(col, pk) {
				whereParts = append(whereParts, fmt.Sprintf("%s = ?", pk))
				args = append(args, row[i])
				break
			}
		}
	}
	if len(whereParts) == 0 {
		return false, nil
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s WHERE %s", dbSchema, tableName, strings.Join(whereParts, " AND "))
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
	query := fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES (%s)",
		dbSchema, tableName, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
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
	for i, col := range columns {
		if pkSet[strings.ToUpper(col)] {
			whereParts = append(whereParts, fmt.Sprintf("%s = ?", col))
			whereArgs = append(whereArgs, row[i])
		} else {
			setParts = append(setParts, fmt.Sprintf("%s = ?", col))
			setArgs = append(setArgs, row[i])
		}
	}
	if len(setParts) == 0 || len(whereParts) == 0 {
		return fmt.Errorf("无法构建更新语句：缺少更新字段或主键条件")
	}
	args := append(setArgs, whereArgs...)
	query := fmt.Sprintf("UPDATE %s.%s SET %s WHERE %s",
		dbSchema, tableName, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))
	_, err := tx.Exec(query, args...)
	return err
}
