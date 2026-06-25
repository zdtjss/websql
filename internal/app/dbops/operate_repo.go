package dbops

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"websql/internal/app/conn"
	"websql/internal/database"
	"websql/internal/dialect"
	"websql/internal/logger"

	"github.com/jmoiron/sqlx"
)

// OperateRepo 定义数据库操作数据访问接口，所有 SQL 查询均在此实现。
// 注意：由于 dbops 操作的是用户动态数据库连接，所有方法接受 *sqlx.DB（或 *sqlx.Tx）参数，
// 而不使用构造函数传入的 db 字段（该字段保留用于模式一致性及未来扩展）。
type OperateRepo interface {
	ListSchemas(dc *sqlx.DB) []string
	ListTables(dc *sqlx.DB, schema string) []*tableRawRow
	ListAllColumnsForTable(dc *sqlx.DB, schema string) []map[string]string
	ListAllColumnsRaw(dc *sqlx.DB, schema string) []*columnRawRow
	ListColumnsForTable(dc *sqlx.DB, table string) []*columnRawRow
	ListTableColumnsRaw(dc *sqlx.DB, schema, table string) ([]map[string]any, error)
	QueryTables(dc *sqlx.DB, schema string) []*conn.Table
	ColumnMap(dc *sqlx.DB, table, schema string) map[string]string
	QueryPrimaryKeyWithTx(tx *sqlx.Tx, schema, table string) ([]string, error)
	QueryPrimaryKey(dc *sqlx.DB, schema, table string) []string
	GetTableOptions(dc *sqlx.DB, schema, table string) ([]map[string]any, error)
	GetTableStatistics(dc *sqlx.DB, schema, table string) ([]map[string]any, error)
	ListIndexes(dc *sqlx.DB, schema, table string) ([]map[string]any, error)
	GetCurrentSchema(dc *sqlx.DB) string
	GetCurrentSchemaForFat(dc *sqlx.DB) string
	// ListObjects 列出 schema 下指定类型的数据库对象（view/procedure/function/trigger/event/table）
	ListObjects(dc *sqlx.DB, schema, objType string) ([]map[string]any, error)
	// GetObjectDDL 获取指定对象的 DDL 定义文本（schema/name 已由 service 层 sanitize 校验）
	GetObjectDDL(dc *sqlx.DB, schema, name, objType string) (string, error)
}

type operateRepo struct {
	db *sqlx.DB
}

// NewOperateRepo 创建 OperateRepo 实例
func NewOperateRepo(db *sqlx.DB) OperateRepo {
	return &operateRepo{db: db}
}

func (r *operateRepo) ListSchemas(dc *sqlx.DB) []string {
	schemas := make([]string, 0)
	dialectMap, ok := dialect.SQL_DIALECT[dc.DriverName()]
	if !ok {
		log.Printf("[ListSchema] 不支持的数据库驱动: %s", dc.DriverName())
		return schemas
	}
	listSchemaSQL, ok := dialectMap["listSchema"]
	if !ok {
		log.Printf("[ListSchema] 缺少 listSchema SQL: %s", dc.DriverName())
		return schemas
	}
	row, err := dc.Query(listSchemaSQL)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return schemas
	}
	var schemaName string
	for row.Next() {
		row.Scan(&schemaName)
		schemas = append(schemas, schemaName)
	}
	return schemas
}

func (r *operateRepo) ListAllColumnsForTable(dc *sqlx.DB, schema string) []map[string]string {
	tableColumns := make([]map[string]string, 0)
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return tableColumns
	}
	var tableName2, columnName, columnComment string
	for row.Next() {
		columnComment = ""
		row.Scan(&tableName2, &columnName, &columnComment)
		tableColumns = append(tableColumns, map[string]string{"tableName": tableName2, "columnName": columnName, "columnComment": columnComment})
	}
	return tableColumns
}

func (r *operateRepo) ListTables(dc *sqlx.DB, schema string) []*tableRawRow {
	tables := make([]*tableRawRow, 0)
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listTable"], schema)
	logger.PrintErr(err)
	for row.Next() {
		var t tableRawRow
		row.Scan(&t.Name, &t.Type, &t.Comment)
		tables = append(tables, &t)
	}
	return tables
}

func (r *operateRepo) ListAllColumnsRaw(dc *sqlx.DB, schema string) []*columnRawRow {
	columns := make([]*columnRawRow, 0)
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return columns
	}
	var columnName, columnComment string
	for row.Next() {
		columnComment = ""
		row.Scan(&columnName, &columnComment)
		columns = append(columns, &columnRawRow{Name: columnName, Comment: columnComment})
	}
	return columns
}

func (r *operateRepo) ListColumnsForTable(dc *sqlx.DB, table string) []*columnRawRow {
	columns := make([]*columnRawRow, 0)
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listColumns"], table)
	logger.PrintErr(err)
	for row.Next() {
		var c columnRawRow
		row.Scan(&c.Name, &c.Comment)
		columns = append(columns, &c)
	}
	return columns
}

func (r *operateRepo) ListTableColumnsRaw(dc *sqlx.DB, schema, table string) ([]map[string]any, error) {
	rows, err := dc.Queryx(dialect.SQL_DIALECT[dc.DriverName()]["listTableColumns"], schema, table)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return nil, err
	}
	result, err := database.GetResultRows(dc.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取列信息失败", err)
		return nil, err
	}
	return result, nil
}

func (r *operateRepo) QueryTables(dc *sqlx.DB, schema string) []*conn.Table {
	tables := make([]*conn.Table, 0)

	var querySQL string
	if schema != "" {
		querySQL = dialect.SQL_DIALECT[dc.DriverName()]["listTable"]
	} else {
		switch dc.DriverName() {
		case "mysql", "mariadb":
			querySQL = "SELECT table_name, table_type, table_comment FROM information_schema.tables WHERE table_schema = DATABASE()"
		case "oracle":
			querySQL = "SELECT table_name, table_type, NULL FROM user_tab_comments UNION ALL SELECT view_name, 'VIEW', NULL FROM user_views ORDER BY table_name"
		case "sqlite":
			querySQL = "SELECT name, type, NULL FROM sqlite_master WHERE type IN ('table','view')"
		default:
			querySQL = dialect.SQL_DIALECT[dc.DriverName()]["listTable"]
		}
	}

	stmt, err := dc.Prepare(querySQL)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return tables
	}

	var rs *sql.Rows
	var err2 error
	if schema != "" {
		if dc.DriverName() == "sqlite" {
			rs, err2 = stmt.Query()
		} else {
			rs, err2 = stmt.Query(schema)
		}
	} else {
		rs, err2 = stmt.Query()
	}
	if err2 != nil {
		log.Printf("查询失败: %v", err2)
		return tables
	}

	var tableName, tableType, tableComment string
	for rs.Next() {
		tableComment = ""
		rs.Scan(&tableName, &tableType, &tableComment)
		table := &conn.Table{Name: tableName, Comment: tableComment}
		tables = append(tables, table)
	}
	return tables
}

func (r *operateRepo) ColumnMap(dc *sqlx.DB, table, schema string) map[string]string {
	columnMap := make(map[string]string)
	stmt, err := dc.Prepare(dialect.SQL_DIALECT[dc.DriverName()]["ColumnMap"])
	logger.PrintErr(err)
	table = strings.TrimPrefix(table, schema+".")
	if dc.DriverName() == "oracle" {
		schema = strings.ToUpper(schema)
		table = strings.ToUpper(table)
	}
	rs, err2 := stmt.Query(table, schema)
	logger.PrintErr(err2)
	var name, comment string
	for rs.Next() {
		comment = ""
		rs.Scan(&name, &comment)
		columnMap[name] = comment
	}
	return columnMap
}

func (r *operateRepo) QueryPrimaryKeyWithTx(tx *sqlx.Tx, schema, table string) ([]string, error) {
	primaryKeys := make([]string, 0)
	stmt, err := tx.Prepare(dialect.SQL_DIALECT[tx.DriverName()]["QueryPrimaryKey"])
	logger.PrintErr(err)
	rs, err2 := stmt.Query(schema, table)
	logger.PrintErr(err2)
	var name string
	for rs.Next() {
		rs.Scan(&name)
		primaryKeys = append(primaryKeys, name)
	}
	if len(primaryKeys) == 0 {
		msg := fmt.Sprintf("%s 没有主键", table)
		log.Println(msg)
		tx.Rollback()
		return nil, errors.New(msg)
	}
	return primaryKeys, nil
}

func (r *operateRepo) QueryPrimaryKey(dc *sqlx.DB, schema, table string) []string {
	primaryKeys := make([]string, 0)
	stmt, err := dc.Prepare(dialect.SQL_DIALECT[dc.DriverName()]["QueryPrimaryKey"])
	if err != nil {
		return primaryKeys
	}
	schemaVal := schema
	tableVal := table
	if dc.DriverName() == "oracle" {
		schemaVal = strings.ToUpper(schema)
		tableVal = strings.ToUpper(table)
	}
	rs, err2 := stmt.Query(schemaVal, tableVal)
	if err2 != nil {
		return primaryKeys
	}
	var name string
	for rs.Next() {
		rs.Scan(&name)
		primaryKeys = append(primaryKeys, name)
	}
	return primaryKeys
}

func (r *operateRepo) GetTableOptions(dc *sqlx.DB, schema, table string) ([]map[string]any, error) {
	d := dialect.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := d["tableOptions"]
	if !ok {
		return nil, nil
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, table)
	} else {
		args = append(args, schema, table)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return nil, errors.New("操作失败")
	}
	data, err := database.GetResultRows(dc.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取表详情失败", err)
		return nil, err
	}
	return data, nil
}

func (r *operateRepo) GetTableStatistics(dc *sqlx.DB, schema, table string) ([]map[string]any, error) {
	d := dialect.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := d["tableStatistics"]
	if !ok {
		return nil, nil
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, table)
	} else {
		args = append(args, schema, table)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return nil, errors.New("操作失败")
	}
	data, err := database.GetResultRows(dc.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取表统计信息失败", err)
		return nil, err
	}
	return data, nil
}

func (r *operateRepo) ListIndexes(dc *sqlx.DB, schema, table string) ([]map[string]any, error) {
	d := dialect.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := d["listIndexes"]
	if !ok {
		return nil, nil
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, table)
	} else {
		args = append(args, schema, table)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	if err != nil {
		log.Printf("查询失败: %v", err)
		return nil, errors.New("操作失败")
	}
	data, err := database.GetResultRows(dc.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取索引信息失败", err)
		return nil, err
	}
	return data, nil
}

func (r *operateRepo) GetCurrentSchema(dc *sqlx.DB) string {
	var schema string
	switch dc.DriverName() {
	case "mysql", "mariadb":
		row := dc.QueryRow("SELECT DATABASE()")
		row.Scan(&schema)
	case "oracle":
		row := dc.QueryRow("SELECT USER FROM DUAL")
		row.Scan(&schema)
		schema = strings.ToUpper(schema)
	case "sqlite":
		schema = "main"
	default:
		schema = ""
	}
	return schema
}

func (r *operateRepo) GetCurrentSchemaForFat(dc *sqlx.DB) string {
	var schema string
	switch dc.DriverName() {
	case "mysql", "mariadb":
		dc.Get(&schema, "SELECT DATABASE()")
	case "oracle":
		dc.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
	case "sqlite":
		schema = "main"
	}
	return schema
}

// objectListTmplKey 将对象类型映射到 dialect 中对应的列表查询模板 key
func objectListTmplKey(objType string) (string, bool) {
	switch objType {
	case "table":
		return "listTable", true
	case "view":
		return "listView", true
	case "procedure":
		return "listProcedures", true
	case "function":
		return "listFunctions", true
	case "trigger":
		return "listTriggers", true
	case "event":
		return "listEvents", true
	}
	return "", false
}

// objectDDLTmplKey 将对象类型映射到 dialect 中对应的 DDL 查询模板 key
func objectDDLTmplKey(objType string) (string, bool) {
	switch objType {
	case "table":
		return "getTableDDL", true
	case "view":
		return "viewDDL", true
	case "procedure":
		return "procedureDDL", true
	case "function":
		return "functionDDL", true
	case "trigger":
		return "triggerDDL", true
	case "event":
		return "eventDDL", true
	}
	return "", false
}

// ListObjects 列出 schema 下指定类型的数据库对象
func (r *operateRepo) ListObjects(dc *sqlx.DB, schema, objType string) ([]map[string]any, error) {
	dbType := dc.DriverName()
	d, ok := dialect.SQL_DIALECT[dbType]
	if !ok {
		return nil, fmt.Errorf("不支持的数据库类型: %s", dbType)
	}
	tmplKey, ok := objectListTmplKey(objType)
	if !ok {
		return nil, fmt.Errorf("不支持的对象类型: %s", objType)
	}
	sqlStr, ok := d[tmplKey]
	if !ok {
		// 该数据库方言未定义此对象类型的查询模板（如 SQLite 无存储过程），返回空列表
		return []map[string]any{}, nil
	}

	rows, err := dc.Queryx(sqlStr, schema)
	if err != nil {
		log.Printf("[ListObjects] 查询失败 - dbType=%s, objType=%s, err=%v", dbType, objType, err)
		return nil, errors.New("查询对象列表失败")
	}
	defer rows.Close()

	data, err := database.GetResultRows(dbType, rows)
	if err != nil {
		logger.PrintErrf("[ListObjects] 读取结果失败", err)
		return nil, errors.New("读取对象列表失败")
	}
	if data == nil {
		return []map[string]any{}, nil
	}
	return data, nil
}

// GetObjectDDL 获取指定对象的 DDL 定义文本。
// 对于 SHOW CREATE 系列（MySQL/MariaDB），模板含 {name} 占位符，需字符串替换（name 已由 service 层 sanitize 校验）；
// 对于参数化查询（SQLite/Oracle），使用占位符传参；Oracle 的标识符需转为大写。
func (r *operateRepo) GetObjectDDL(dc *sqlx.DB, schema, name, objType string) (string, error) {
	dbType := dc.DriverName()
	d, ok := dialect.SQL_DIALECT[dbType]
	if !ok {
		return "", fmt.Errorf("不支持的数据库类型: %s", dbType)
	}
	tmplKey, ok := objectDDLTmplKey(objType)
	if !ok {
		return "", fmt.Errorf("不支持的对象类型: %s", objType)
	}
	sqlStr, ok := d[tmplKey]
	if !ok {
		// 该数据库方言不支持此对象类型的 DDL 获取（如 SQLite 无存储过程）
		return "", fmt.Errorf("%s 不支持 %s", dbType, objTypeZH(objType))
	}

	var rows *sqlx.Rows
	var err error
	if strings.Contains(sqlStr, "{name}") || strings.Contains(sqlStr, "{table}") {
		// SHOW CREATE 系列：name 已由 service 层通过 sanitize.ValidateIdentifier 校验为合法标识符，
		// 此处直接替换占位符是安全的（仅允许字母/数字/下划线/$）
		finalSQL := strings.ReplaceAll(sqlStr, "{name}", name)
		finalSQL = strings.ReplaceAll(finalSQL, "{table}", name)
		rows, err = dc.Queryx(finalSQL)
	} else {
		// 参数化查询：Oracle 标识符默认大写存储
		arg := name
		if dbType == "oracle" {
			arg = strings.ToUpper(name)
		}
		rows, err = dc.Queryx(sqlStr, arg)
	}
	if err != nil {
		log.Printf("[GetObjectDDL] 查询失败 - dbType=%s, objType=%s, name=%s, err=%v", dbType, objType, name, err)
		return "", errors.New("查询对象定义失败")
	}
	defer rows.Close()

	data, err := database.GetResultRows(dbType, rows)
	if err != nil || len(data) == 0 {
		if err != nil {
			logger.PrintErrf("[GetObjectDDL] 读取结果失败", err)
		}
		return "", errors.New("未获取到对象定义")
	}
	return extractDDLText(data[0]), nil
}

// extractDDLText 从结果行中提取 DDL 文本。
// 不同数据库/对象返回的列名不一致（如 "Create View"、DDL、TEXT、SQL），策略：
// 优先取列名包含 "create" 的列；否则取所有字符串值中最长的一条作为 DDL。
func extractDDLText(row map[string]any) string {
	var createVal string
	var longest string
	for k, v := range row {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if len(s) > len(longest) {
			longest = s
		}
		if createVal == "" && strings.Contains(strings.ToLower(k), "create") {
			createVal = s
		}
	}
	if createVal != "" {
		return createVal
	}
	if longest != "" {
		return longest
	}
	return "-- 没有可用的定义"
}

// objTypeZH 返回对象类型的中文描述，用于错误提示
func objTypeZH(objType string) string {
	switch objType {
	case "table":
		return "表"
	case "view":
		return "视图"
	case "procedure":
		return "存储过程"
	case "function":
		return "函数"
	case "trigger":
		return "触发器"
	case "event":
		return "事件"
	}
	return objType
}
