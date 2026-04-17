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
	allSchemas := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		allSchemas = append(allSchemas, &Tree{Label: schemaName, Type: TREE_NODE_TYPE_SCHEMA, Data: map[string]any{"dbType": dc.DriverName()}})
	}
	// 权限过滤：只返回用户有权限访问的 schema
	return filterSchemasByPermission(allSchemas, key, authorization)
}

// filterSchemasByPermission 根据用户权限过滤 schema 列表
func filterSchemasByPermission(schemas []*Tree, connId, authorization string) []*Tree {
	userPower := GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		return schemas
	}
	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return []*Tree{}
	}
	allowedSchemas := make(map[string]bool)
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			// conn 级权限 → 全部 schema 可用（无论是否有下级配置）
			return schemas
		case "schema":
			if p.SchemaName != nil {
				allowedSchemas[*p.SchemaName] = true
			}
		case "table":
			if p.SchemaName != nil {
				allowedSchemas[*p.SchemaName] = true
			}
		case "column":
			if p.SchemaName != nil {
				allowedSchemas[*p.SchemaName] = true
			}
		}
	}
	filtered := make([]*Tree, 0)
	for _, s := range schemas {
		if allowedSchemas[s.Label] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func checkSchemaAccess(connId, schemaName, authorization string) {
	userPower := GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		logutils.PanicErr(errors.New("无权访问此 Schema"))
		return
	}
	// 使用 powerDetails 进行更精确的层级检查
	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		logutils.PanicErr(errors.New("无权访问此 Schema"))
		return
	}
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			return // conn 级权限，可以访问所有 schema
		case "schema":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				return
			}
		case "table":
			// 有 table 级权限意味着可以访问其所属 schema
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				return
			}
		case "column":
			// 有 column 级权限意味着可以访问其所属 schema
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				return
			}
		}
	}
	logutils.PanicErr(errors.New("无权访问此 Schema"))
}

func listTable(key string, schema, authorization string) []*Tree {
	tableName, tableType, tableComment := "", "", ""
	dc := GetConn(key, authorization)

	checkSchemaAccess(key, schema, authorization)

	tableName2, columnName, columnComment := "", "", ""
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	logutils.PanicErr(err)
	tableColumns := make([]map[string]string, 0)
	for row.Next() {
		*&columnComment = ""
		row.Scan(&tableName2, &columnName, &columnComment)
		tableColumns = append(tableColumns, map[string]string{"tableName": tableName2, "columnName": columnName, "columnComment": columnComment})
	}

	grouped := make(map[string][]Column)

	for _, col := range tableColumns {
		tn := col["tableName"]
		if grouped[tn] == nil {
			grouped[tn] = make([]Column, 0)
		}
		fieldInfo := Column{
			Name:    col["columnName"],
			Comment: col["columnComment"],
		}
		grouped[tn] = append(grouped[tn], fieldInfo)
	}

	row, err = dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listTable"], schema)
	logutils.PrintErr(err)
	allTables := make([]*Tree, 0)
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
		allTables = append(allTables, treeNode)
	}
	// 权限过滤：只返回用户有权限访问的表
	return filterTreeTablesByPermission(allTables, key, schema, authorization)
}

// filterTreeTablesByPermission 根据用户权限过滤树中的表列表
func filterTreeTablesByPermission(tables []*Tree, connId, schema, authorization string) []*Tree {
	userPower := GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		return tables
	}
	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return []*Tree{}
	}
	allowedTables := make(map[string]bool)
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			// conn 级权限 → 全部表可用
			return tables
		case "schema":
			if p.SchemaName != nil && *p.SchemaName == schema {
				// schema 级权限 → 该 schema 下全部表可用
				return tables
			}
		case "table":
			if p.SchemaName != nil && *p.SchemaName == schema && p.TableName != nil {
				allowedTables[*p.TableName] = true
			}
		case "column":
			if p.SchemaName != nil && *p.SchemaName == schema && p.TableName != nil {
				allowedTables[*p.TableName] = true
			}
		}
	}
	filtered := make([]*Tree, 0)
	for _, t := range tables {
		if allowedTables[t.Label] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func checkTableAccess(connId, schemaName, tableName, authorization string) {
	userPower := GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		logutils.PanicErr(errors.New("无权访问此表"))
		return
	}
	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		logutils.PanicErr(errors.New("无权访问此表"))
		return
	}
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			return // conn 级权限 → 全部表可用
		case "schema":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				return // schema 级权限 → 该 schema 下全部表可用
			}
		case "table":
			if p.SchemaName != nil && *p.SchemaName == schemaName && p.TableName != nil && *p.TableName == tableName {
				return
			}
		case "column":
			if p.SchemaName != nil && *p.SchemaName == schemaName && p.TableName != nil && *p.TableName == tableName {
				return
			}
		}
	}
	logutils.PanicErr(errors.New("无权访问此表"))
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
