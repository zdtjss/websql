package admin

import (
	"database/sql"
	"errors"
	"fmt"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

func listSchema(key string, authorization string) []*Tree {
	schemaName := ""
	dc := GetConn(key, authorization)
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listSchema"])
	logutils.PanicErr(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		tree = append(tree, &Tree{Label: schemaName, Type: TREE_NODE_TYPE_SCHEMA, Data: map[string]any{"dbType": dc.DriverName()}})
	}
	return tree
}

func checkSchemaAccess(connId, schemaName, authorization string) {
	userPower := GetUserPower(authorization)
	param := &PowerCheckParam{
		ConnId:     connId,
		SchemaName: schemaName,
	}
	if !checkPower(userPower, param) {
		logutils.PanicErr(errors.New("无权访问此 Schema"))
	}
}

func listTable(key string, schema, authorization string) []*Tree {
	tableName, tableType, tableComment := "", "", ""
	dc := GetConn(key, authorization)

	checkSchemaAccess(key, schema, authorization)

	tableName, columnName, columnComment := "", "", ""
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	logutils.PanicErr(err)
	tableColumns := make([]map[string]string, 0)
	for row.Next() {
		*&columnComment = ""
		row.Scan(&tableName, &columnName, &columnComment)
		tableColumns = append(tableColumns, map[string]string{"tableName": tableName, "columnName": columnName, "columnComment": columnComment})
	}

	grouped := make(map[string][]Column)

	for _, col := range tableColumns {
		tableName := col["tableName"]
		// 确保 key 存在
		if grouped[tableName] == nil {
			grouped[tableName] = make([]Column, 0)
		}
		// 只保留 columnName 和 columnComment（可选）
		fieldInfo := Column{
			Name:    col["columnName"],
			Comment: col["columnComment"],
		}
		grouped[tableName] = append(grouped[tableName], fieldInfo)
	}

	row, err = dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listTable"], schema)
	logutils.PrintErr(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableType, &tableComment)
		treeNode := &Tree{Label: tableName, Data: map[string]any{"text": tableComment, "columns": grouped[tableName]}, Type: TREE_NODE_TYPE_TABLE}
		if dc.DriverName() == "mysql" || dc.DriverName() == "mariadb" {
			switch tableType {
			case "VIEW":
				treeNode.Type = "view"
			case "BASE TABLE":
				treeNode.Type = "table"
			}
		} else if dc.DriverName() == "oracle" {
			treeNode.Type = strings.ToLower(tableType)
		}
		tree = append(tree, treeNode)
	}
	return tree
}

func checkTableAccess(connId, schemaName, tableName, authorization string) {
	userPower := GetUserPower(authorization)
	param := &PowerCheckParam{
		ConnId:     connId,
		SchemaName: schemaName,
		TableName:  tableName,
	}
	if !checkPower(userPower, param) {
		logutils.PanicErr(errors.New("无权访问此表"))
	}
}

func listColumns(key string, table, authorization string) []*Tree {
	columnName, columnComment := "", ""
	dc := GetConn(key, authorization)
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listColumns"], table)
	logutils.PrintErr(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func checkColumnAccess(connId, schemaName, tableName, columnName, authorization string) {
	userPower := GetUserPower(authorization)
	param := &PowerCheckParam{
		ConnId:     connId,
		SchemaName: schemaName,
		TableName:  tableName,
		ColumnName: columnName,
	}
	if !checkPower(userPower, param) {
		logutils.PanicErr(errors.New("无权访问此字段"))
	}
}

func listAllColumns(key string, schema, authorization string) []*Tree {
	columnName, columnComment := "", ""
	dc := GetConn(key, authorization)
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	logutils.PanicErr(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		*&columnComment = ""
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func listTableColumns(connId, tableName, schema, authorization string) []map[string]any {
	dc := GetConn(connId, authorization)
	rows, err := dc.Queryx(dbutils.SQL_DIALECT[dc.DriverName()]["listTableColumns"], schema, tableName)
	logutils.PanicErr(err)
	return dbutils.GetResultRows(dc.DriverName(), rows)
}

func ListTableFat(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	tables := queryTableInfo(c.Query("connId"), c.Query("schema"), authorization)
	c.JSON(http.StatusOK, tables)
}

func queryTableInfo(key string, schema, authorization string) []*Table {
	tables := make([]*Table, 0)
	dc := GetConn(key, authorization)

	// 根据 schema 是否为空决定查询语句
	var querySQL string
	if schema != "" {
		querySQL = dbutils.SQL_DIALECT[dc.DriverName()]["listTable"]
	} else {
		// 不使用 schema 过滤的查询
		switch dc.DriverName() {
		case "mysql", "mariadb":
			querySQL = "SELECT table_name, table_type, table_comment FROM information_schema.tables WHERE table_schema = DATABASE()"
		case "oracle":
			querySQL = "SELECT table_name, 'TABLE', NULL FROM user_tables"
		case "sqlite":
			querySQL = "SELECT name, 'table', NULL FROM sqlite_master WHERE type='table'"
		default:
			querySQL = dbutils.SQL_DIALECT[dc.DriverName()]["listTable"]
		}
	}

	stmt, err := dc.Prepare(querySQL)
	logutils.PanicErr(err)

	var rs *sql.Rows
	var err2 error
	if schema != "" {
		rs, err2 = stmt.Query(schema)
	} else {
		rs, err2 = stmt.Query()
	}
	logutils.PanicErr(err2)

	var tableName, tableType, tableComment string
	for rs.Next() {
		*&tableComment = ""
		rs.Scan(&tableName, &tableType, &tableComment)
		table := &Table{tableName, tableComment}
		tables = append(tables, table)
	}
	return tables
}

func ConvertCol(dbType, colType *string, val *any, overSign bool) *any {

	return dbutils.ConvertColHandler[*dbType](colType, val, overSign)
}

func ParseVal(dbType, colType *string, val *string) *any {

	return dbutils.ParseValHandler[*dbType](colType, val)
}

func ColumnMap(table, schema string, conn *sqlx.DB) map[string]string {
	columnMap := make(map[string]string)
	stmt, err := conn.Prepare(dbutils.SQL_DIALECT[conn.DriverName()]["ColumnMap"])
	logutils.PrintErr(err)
	table = strings.TrimPrefix(table, schema+".")
	if conn.DriverName() == "oracle" {
		schema = strings.ToUpper(schema)
		table = strings.ToUpper(table)
	}
	rs, err2 := stmt.Query(table, schema)
	logutils.PrintErr(err2)
	var name, comment string
	for rs.Next() {
		*&comment = ""
		rs.Scan(&name, &comment)
		columnMap[name] = comment
	}
	return columnMap
}

func QueryPrimaryKey(schema, table string, tx *sqlx.Tx) ([]string, error) {
	primaryKeys := make([]string, 0)
	stmt, err := tx.Prepare(dbutils.SQL_DIALECT[tx.DriverName()]["QueryPrimaryKey"])
	logutils.PrintErr(err)
	rs, err2 := stmt.Query(schema, table)
	logutils.PrintErr(err2)
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

func QueryColType(schema, table string, tx *sqlx.Tx) map[string]string {
	colTypeMap := make(map[string]string, 0)
	stmt, err := tx.Prepare(dbutils.SQL_DIALECT[tx.DriverName()]["QueryColType"])
	logutils.PrintErr(err)
	rs, err2 := stmt.Query(schema, table)
	logutils.PrintErr(err2)
	var colName, colType string
	for rs.Next() {
		*&colType = ""
		rs.Scan(&colName, &colType)
		colTypeMap[colName] = colType
	}
	return colTypeMap
}

type Table struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

type Column struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

func TableOptions(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	param := ColumnsQuery{}
	c.ShouldBindJSON(&param)
	dc := GetConn(param.ConnId, authorization)
	dialect := dbutils.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := dialect["tableOptions"]
	if !ok {
		utils.WriteJson(c.Writer, map[string]any{})
		return
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, param.TableName)
	} else {
		args = append(args, param.Schema, param.TableName)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	logutils.PanicErr(err)
	data := dbutils.GetResultRows(dc.DriverName(), rows)
	if len(data) > 0 {
		utils.WriteJson(c.Writer, data[0])
	} else {
		utils.WriteJson(c.Writer, map[string]any{})
	}
}

func TableStatistics(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	param := ColumnsQuery{}
	c.ShouldBindJSON(&param)
	dc := GetConn(param.ConnId, authorization)
	dialect := dbutils.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := dialect["tableStatistics"]
	if !ok {
		utils.WriteJson(c.Writer, map[string]any{})
		return
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, param.TableName)
	} else {
		args = append(args, param.Schema, param.TableName)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	logutils.PanicErr(err)
	data := dbutils.GetResultRows(dc.DriverName(), rows)
	if len(data) > 0 {
		utils.WriteJson(c.Writer, data[0])
	} else {
		utils.WriteJson(c.Writer, map[string]any{})
	}
}

func ListIndexes(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	param := ColumnsQuery{}
	c.ShouldBindJSON(&param)
	dc := GetConn(param.ConnId, authorization)
	dialect := dbutils.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := dialect["listIndexes"]
	if !ok {
		utils.WriteJson(c.Writer, []map[string]any{})
		return
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, param.TableName)
	} else {
		args = append(args, param.Schema, param.TableName)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	logutils.PanicErr(err)
	data := dbutils.GetResultRows(dc.DriverName(), rows)
	utils.WriteJson(c.Writer, data)
}
