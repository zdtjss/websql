package admin

import (
	"errors"
	"fmt"
	"go-web/logutils"
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

func listTable(key string, schema, authorization string) []*Tree {
	tableName, tableComment := "", ""
	dc := GetConn(key, authorization)
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listTable"], schema)
	logutils.PrintErr(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableComment)
		tree = append(tree, &Tree{Label: tableName, Data: map[string]any{"text": tableComment}, Type: TREE_NODE_TYPE_TABLE})
	}
	return tree
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

func listAllColumns(key string, schema, authorization string) []*Tree {
	columnName, columnComment := "", ""
	dc := GetConn(key, authorization)
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	logutils.PrintErr(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		*&columnComment = ""
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func ListTableFat(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	tables := queryTableInfo(c.Query("connId"), c.Query("schema"), authorization)
	c.JSON(http.StatusOK, tables)
}

func queryTableInfo(key string, schema, authorization string) []*Table {
	tables := make([]*Table, 0)
	dc := GetConn(key, authorization)
	stmt, err := dc.Prepare(dbutils.SQL_DIALECT[dc.DriverName()]["listTable"])
	logutils.PrintErr(err)
	rs, err2 := stmt.Query(schema)
	logutils.PrintErr(err2)
	var name, comment string
	for rs.Next() {
		*&comment = ""
		rs.Scan(&name, &comment)
		table := &Table{name, comment}
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
	rs, err2 := stmt.Query(table[strings.Index(table, ".")+1:], schema)
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
