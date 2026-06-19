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
