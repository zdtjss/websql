package admin

import (
	"fmt"
	"go-web/logutils"
	"go-web/utils"
	"log"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
)

func listSchema(key uint64, authorization string) []*Tree {
	schemaName := ""
	dc := GetConn(key, authorization)
	row, err := dc.Query(SQL_DIALECT[dc.DriverName()]["listSchema"])
	logutils.Panicln(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		tree = append(tree, &Tree{Label: schemaName, Type: TREE_NODE_TYPE_SCHEMA})
	}
	return tree
}

func listTable(key uint64, schema, authorization string) []*Tree {
	tableName, tableComment := "", ""
	dc := GetConn(key, authorization)
	params := make([]any, 0)
	if dc.DriverName() == "mysql" {
		params = append(params, schema)
	}
	row, err := dc.Query(SQL_DIALECT[dc.DriverName()]["listTable"], params...)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableComment)
		tree = append(tree, &Tree{Label: tableName, Data: map[string]any{"text": tableComment}, Type: TREE_NODE_TYPE_TABLE})
	}
	return tree
}

func listColumns(key uint64, table, authorization string) []*Tree {
	columnName, columnComment := "", ""
	dc := GetConn(key, authorization)
	row, err := dc.Query(SQL_DIALECT[dc.DriverName()]["listColumns"], table)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func listAllColumns(key uint64, schema, authorization string) []*Tree {
	columnName, columnComment := "", ""
	dc := GetConn(key, authorization)
	row, err := dc.Query(SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func ListTableFat(w http.ResponseWriter, r *http.Request) {
	authorization := r.Header.Get("Authorization")
	r.ParseForm()
	tables := queryTableInfo(utils.AtoUint64(r.FormValue("connId")), r.Form.Get("schema"), authorization)
	utils.WriteJson(w, tables)
}

func queryTableInfo(key uint64, schema, authorization string) []*Table {
	tables := make([]*Table, 0)
	dc := GetConn(key, authorization)
	stmt, err := dc.Prepare(SQL_DIALECT[dc.DriverName()]["listSchema"])
	logutils.Println(err)
	rs, err2 := stmt.Query(schema)
	logutils.Println(err2)
	var name, comment string
	for rs.Next() {
		rs.Scan(&name, &comment)
		table := &Table{name, comment}
		tables = append(tables, table)
	}
	return tables
}

func ConvertCol(dbType, colType string, val any) any {

	return ConvertColHandler[dbType](colType, val)
}

func ParseVal(dbType, colType string, val string) (retVal any) {

	return ParseValHandler[dbType](colType, val)
}

func ColumnMap(table string, connId uint64, authorization string) map[string]string {
	columnMap := make(map[string]string)
	dc := GetConn(connId, authorization)
	stmt, err := dc.Prepare(SQL_DIALECT[dc.DriverName()]["ColumnMap"])
	logutils.Println(err)
	rs, err2 := stmt.Query(table[strings.Index(table, ".")+1:])
	logutils.Println(err2)
	var name, comment string
	for rs.Next() {
		rs.Scan(&name, &comment)
		columnMap[name] = comment
	}
	return columnMap
}

func QueryPrimaryKey(schema, table string, tx *sqlx.Tx) []string {
	primaryKeys := make([]string, 0)
	stmt, err := tx.Prepare(SQL_DIALECT[tx.DriverName()]["QueryPrimaryKey"])
	logutils.Println(err)
	rs, err2 := stmt.Query(schema, table)
	logutils.Println(err2)
	var name string
	for rs.Next() {
		rs.Scan(&name)
		primaryKeys = append(primaryKeys, name)
	}
	if len(primaryKeys) == 0 {
		msg := fmt.Sprintf("%s 没有主键", table)
		log.Println(msg)
		panic(msg)
	}
	return primaryKeys
}

func QueryColType(schema, table string, tx *sqlx.Tx) map[string]string {
	colTypeMap := make(map[string]string, 0)
	stmt, err := tx.Prepare(SQL_DIALECT[tx.DriverName()]["QueryColType"])
	logutils.Println(err)
	rs, err2 := stmt.Query(schema, table)
	logutils.Println(err2)
	var colName, colType string
	for rs.Next() {
		rs.Scan(&colName, &colType)
		colTypeMap[colName] = colType
	}
	return colTypeMap
}

type Table struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}
