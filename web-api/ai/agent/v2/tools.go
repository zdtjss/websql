package agentv2

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"

	"github.com/jmoiron/sqlx"
)

// isValidTableName 验证表名是否合法，防止 SQL 注入
// 只允许字母、数字、下划线、点号（schema.table）和反引号
var validTableNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_` + "`" + `.]+$`)

func isValidTableName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return false
	}
	return validTableNameRegex.MatchString(name)
}

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

// getConn 获取数据库连接和类型。
// 注意：每次调用都会查询管理库，高频场景下可考虑缓存。
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

// ──────────────────────────────────────────────
// Tool 实现
// ──────────────────────────────────────────────

// GetCurrentDateTime 获取当前日期时间的 tool
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
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}
		sql := strings.TrimSpace(input.SQL)
		// 先去除 SQL 注释，防止注释绕过类型检测
		stripped := stripSQLComments(sql)
		upper := strings.ToUpper(stripped)
		if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "SHOW") &&
			!strings.HasPrefix(upper, "DESCRIBE") && !strings.HasPrefix(upper, "EXPLAIN") &&
			!strings.HasPrefix(upper, "WITH") {
			return nil, fmt.Errorf("query_data 仅支持 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 语句")
		}
		// WITH CTE 可能包含写操作（如 WITH deleted AS (DELETE ...) SELECT ...），需要额外检查
		if strings.HasPrefix(upper, "WITH") {
			// 检查 CTE 体内是否包含写操作关键字
			cteUpper := strings.ToUpper(stripped)
			writeKeywords := []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "TRUNCATE ", "ALTER ", "CREATE ", "REPLACE ", "MERGE "}
			for _, kw := range writeKeywords {
				if strings.Contains(cteUpper, kw) {
					return nil, fmt.Errorf("query_data 不允许在 WITH 语句中包含写操作（%s），请使用 exec_sql 工具", strings.TrimSpace(kw))
				}
			}
		}

		// 防止大结果集：如果是 SELECT/WITH 且没有行数限制，按数据库方言自动添加
		if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") {
			sql = applyRowLimit(sql, conn.DriverName(), 2000)
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
		if sql == "" {
			return nil, fmt.Errorf("SQL 不能为空")
		}

		// 所有写操作都必须经过 SQLSecurityMiddleware 拦截 → 前端确认 → handleConfirmedExec 执行。
		// 这里做最后一道防线：如果 SQL 是危险操作，直接触发拦截。
		for _, line := range strings.Split(sql, ";") {
			line = strings.TrimSpace(line)
			if line != "" && isDangerousSQL(line) {
				return nil, &DangerousSQLError{SQL: sql}
			}
		}

		result, err := conn.Exec(sql)
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
			// 验证表名，防止 SQL 注入
			if !isValidTableName(table) {
				logutils.PrintErr(fmt.Errorf("无效的表名：%s", table))
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
	Mapping   map[string]string `json:"mapping" jsonschema_description:"字段映射：key=Excel列名, value=数据库字段名。如果不提供，后端会自动按列名匹配（大小写不敏感、去除空格和下划线差异）。"`
	Mode      string            `json:"mode" jsonschema_description:"导入模式: insert（仅插入）或 upsert（有主键则更新无则插入）。默认 insert"`
}

type ImportDataOutput struct {
	Message      string `json:"message"`
	InsertedRows int    `json:"insertedRows"`
	UpdatedRows  int    `json:"updatedRows"`
}

// NewImportDataFunc 数据导入工具 — 从后端暂存区读取 Excel 全量数据，按字段映射导入目标表
// 当 AI 不提供 mapping 时，后端自动按列名匹配（大小写不敏感、忽略空格和下划线差异）。
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

		mode := strings.ToLower(input.Mode)
		if mode == "" {
			mode = "insert"
		}

		// 从暂存区读取 Excel 文件
		upload, err := GetUploadedFile(input.FileID)
		if err != nil {
			return nil, err
		}

		// 获取目标表的实际列名
		tableColumns, err := getTableColumns(conn, dbType, dbSchema, input.TableName)
		if err != nil {
			return nil, fmt.Errorf("获取表 %s 的列信息失败：%w", input.TableName, err)
		}
		if len(tableColumns) == 0 {
			return nil, fmt.Errorf("表 %s 不存在或没有列", input.TableName)
		}

		// 构建最终映射：excelColIndex -> dbColumnName
		// 优先使用 AI 提供的 mapping，否则自动匹配
		finalMapping, err := buildFinalMapping(upload.Columns, tableColumns, input.Mapping)
		if err != nil {
			return nil, err
		}

		if len(finalMapping) == 0 {
			return nil, fmt.Errorf("没有任何 Excel 列能匹配到表 %s 的字段。\nExcel 列：%s\n表字段：%s",
				input.TableName, strings.Join(upload.Columns, ", "), strings.Join(tableColumns, ", "))
		}

		// 提取有序的 dbColumns 和对应的 excelIndices
		var dbColumns []string
		var excelIndices []int
		for excelIdx, dbCol := range finalMapping {
			dbColumns = append(dbColumns, dbCol)
			excelIndices = append(excelIndices, excelIdx)
		}

		// 获取主键
		primaryKeys, _ := getPrimaryKeys(conn, dbType, dbSchema, input.TableName)

		tx, err := conn.Beginx()
		if err != nil {
			return nil, fmt.Errorf("开启事务失败：%w", err)
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
					return nil, fmt.Errorf("第 %d 行检查数据是否存在失败：%w", rowNum+2, err)
				}
				if exists {
					if err := updateRow(tx, dbType, dbSchema, input.TableName, dbColumns, row, primaryKeys); err != nil {
						return nil, fmt.Errorf("第 %d 行更新数据失败：%w", rowNum+2, err)
					}
					updatedRows++
				} else {
					if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
						return nil, fmt.Errorf("第 %d 行插入数据失败：%w", rowNum+2, err)
					}
					insertedRows++
				}
			} else {
				if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
					return nil, fmt.Errorf("第 %d 行插入数据失败：%w", rowNum+2, err)
				}
				insertedRows++
			}
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("提交事务失败：%w", err)
		}

		RemoveUploadedFile(input.FileID)

		// 构建详细的映射说明
		var mappingDesc strings.Builder
		for i, idx := range excelIndices {
			if i > 0 {
				mappingDesc.WriteString(", ")
			}
			fmt.Fprintf(&mappingDesc, "%s→%s", upload.Columns[idx], dbColumns[i])
		}

		msg := fmt.Sprintf("导入完成：插入 %d 行", insertedRows)
		if updatedRows > 0 {
			msg += fmt.Sprintf("，更新 %d 行", updatedRows)
		}
		msg += fmt.Sprintf("。字段映射：%s", mappingDesc.String())
		log.Printf("[Tool:import_data] %s\n", msg)
		return &ImportDataOutput{Message: msg, InsertedRows: insertedRows, UpdatedRows: updatedRows}, nil
	}
}

// normalizeColName 标准化列名用于模糊匹配：转小写、去除空格、下划线、连字符
func normalizeColName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "")
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}

// buildFinalMapping 构建最终的 excelColumnIndex → dbColumnName 映射。
//
// 策略：
//  1. 如果 AI 提供了 mapping，先尝试使用它（对 key 和 value 都做模糊匹配以容错）
//  2. 对于 AI mapping 中未匹配到的列，以及 AI 未提供 mapping 的情况，自动按列名匹配
//  3. 自动匹配规则：精确匹配 > 大小写不敏感匹配 > 标准化匹配（去空格/下划线/连字符）
func buildFinalMapping(excelColumns, tableColumns []string, aiMapping map[string]string) (map[int]string, error) {
	result := make(map[int]string)      // excelIndex -> dbColumn
	usedDBCols := make(map[string]bool) // 已使用的数据库列（大写）

	// 构建 Excel 列名索引（精确 + 标准化）
	excelByExact := make(map[string]int) // exactName -> index
	excelByNorm := make(map[string]int)  // normalizedName -> index
	for i, c := range excelColumns {
		excelByExact[c] = i
		excelByNorm[normalizeColName(c)] = i
	}

	// 构建数据库列名索引（精确 + 标准化 → 原始名）
	dbByUpper := make(map[string]string) // UPPER(name) -> originalName
	dbByNorm := make(map[string]string)  // normalizedName -> originalName
	for _, c := range tableColumns {
		dbByUpper[strings.ToUpper(c)] = c
		dbByNorm[normalizeColName(c)] = c
	}

	// 第一步：如果 AI 提供了 mapping，尝试使用（带容错）
	if len(aiMapping) > 0 {
		for aiExcelCol, aiDBCol := range aiMapping {
			// 查找 Excel 列索引（精确 → 大小写不敏感 → 标准化）
			excelIdx := -1
			if idx, ok := excelByExact[aiExcelCol]; ok {
				excelIdx = idx
			} else if idx, ok := excelByNorm[normalizeColName(aiExcelCol)]; ok {
				excelIdx = idx
			}
			if excelIdx == -1 {
				log.Printf("[import_data] AI mapping 中的 Excel 列 '%s' 未找到，跳过\n", aiExcelCol)
				continue
			}

			// 查找数据库列名（精确 → 大小写不敏感 → 标准化）
			dbCol := ""
			if orig, ok := dbByUpper[strings.ToUpper(aiDBCol)]; ok {
				dbCol = orig
			} else if orig, ok := dbByNorm[normalizeColName(aiDBCol)]; ok {
				dbCol = orig
			}
			if dbCol == "" {
				log.Printf("[import_data] AI mapping 中的数据库字段 '%s' 未找到，跳过\n", aiDBCol)
				continue
			}

			if _, exists := result[excelIdx]; exists {
				continue // 该 Excel 列已映射
			}
			if usedDBCols[strings.ToUpper(dbCol)] {
				continue // 该数据库列已被使用
			}

			result[excelIdx] = dbCol
			usedDBCols[strings.ToUpper(dbCol)] = true
		}
	}

	// 第二步：对未映射的 Excel 列，自动按列名匹配
	for i, excelCol := range excelColumns {
		if _, exists := result[i]; exists {
			continue // 已通过 AI mapping 映射
		}

		// 精确匹配
		if orig, ok := dbByUpper[strings.ToUpper(excelCol)]; ok && !usedDBCols[strings.ToUpper(orig)] {
			result[i] = orig
			usedDBCols[strings.ToUpper(orig)] = true
			continue
		}

		// 标准化匹配
		norm := normalizeColName(excelCol)
		if orig, ok := dbByNorm[norm]; ok && !usedDBCols[strings.ToUpper(orig)] {
			result[i] = orig
			usedDBCols[strings.ToUpper(orig)] = true
			continue
		}
	}

	return result, nil
}

// ──────────────────────────────────────────────
// 数据库辅助函数
// ──────────────────────────────────────────────

func getTableColumns(conn *sqlx.DB, dbType, dbSchema, tableName string) ([]string, error) {
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			// schema 为空时，使用当前连接的默认 schema
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
	log.Printf("[getTableColumns] dbType=%s, schema=%s, table=%s, columns=%v\n", dbType, dbSchema, tableName, cols)
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
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableRef, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
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
		return fmt.Errorf("无法构建更新语句：缺少更新字段或主键条件")
	}
	args := append(setArgs, whereArgs...)
	tableRef := tableName
	if dbSchema != "" {
		tableRef = dbSchema + "." + tableName
	}
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		tableRef, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))
	_, err := tx.Exec(query, args...)
	return err
}

// applyRowLimit 根据数据库方言为 SQL 添加行数限制，防止大结果集。
// 如果 SQL 已包含对应的限制语法则不做修改。
func applyRowLimit(sql, driverName string, maxRows int) string {
	upper := strings.ToUpper(sql)
	switch driverName {
	case "oracle":
		// Oracle 使用 ROWNUM 或 FETCH FIRST（12c+）
		if strings.Contains(upper, "ROWNUM") || strings.Contains(upper, "FETCH ") {
			return sql
		}
		return fmt.Sprintf("SELECT * FROM (%s) WHERE ROWNUM <= %d", sql, maxRows)
	case "sqlite", "mysql", "mariadb":
		if strings.Contains(upper, " LIMIT ") {
			return sql
		}
		return fmt.Sprintf("%s LIMIT %d", sql, maxRows)
	default:
		// 默认使用 LIMIT（MySQL/MariaDB/SQLite/PostgreSQL 等）
		if strings.Contains(upper, " LIMIT ") {
			return sql
		}
		return fmt.Sprintf("%s LIMIT %d", sql, maxRows)
	}
}
