package admin

import (
	"database/sql"
	"fmt"
	"go-web/logutils"
	"go-web/utils"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/slices"
)

func listSchema(key uint64, authorization string) []*Tree {
	schemaName := ""
	dc := GetConn(key, authorization)
	row, err := dc.Conn.Query(SQL_DIALECT[dc.DbType]["listSchemaMySQL"])
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
	row, err := dc.Conn.Query(SQL_DIALECT[dc.DbType]["listTableMySQL"], schema)
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
	row, err := dc.Conn.Query(SQL_DIALECT[dc.DbType]["listColumnsMySQL"], table)
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
	row, err := dc.Conn.Query(SQL_DIALECT[dc.DbType]["listAllColumnsMySQL"], schema)
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
	stmt, err := dc.Conn.Prepare(SQL_DIALECT[dc.DbType]["queryTableInfoMySQL"])
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

func ConvertColMySQL(colType string, val any) any {
	var v any
	//判断是否为[]byte
	if b, ok := val.([]byte); ok {
		switch colType {
		case "TINYINT", "SMALLINT", "MEDIUMINT", "INT":
			iv, err := strconv.ParseInt(string(b), 10, 32)
			logutils.Panicf("转换类型失败， %x", err)
			v = int(iv)
		case "BIGINT":
			iv, err := strconv.ParseInt(string(b), 10, 64)
			logutils.Panicf("转换类型失败， %x", err)
			v = iv
		case "FLOAT":
			iv, err := strconv.ParseFloat(string(b), 32)
			logutils.Panicf("转换类型失败， %x", err)
			v = float32(iv)
		case "DOUBLE", "DECIMAL":
			iv, err := strconv.ParseFloat(string(b), 64)
			logutils.Panicf("转换类型失败， %x", err)
			v = iv
		case "BIT":
			v = b[0] == byte(1)
		default:
			v = string(b)
		}
	} else if t, ok := val.(time.Time); ok {
		v = t.Format("2006-01-02 15:04:05")
	} else {
		v = val
	}
	return v
}

func ParseValMySQL(colType string, val string) (retVal any) {
	if slices.Contains([]string{"float", "double", "datetime", "decimal", "int", "bigint", "smallint", "tinyint", "bit"}, colType) && val == "" {
		return nil
	}
	switch colType {
	case "float", "double", "decimal":
		f, err := strconv.ParseFloat(val, 64)
		logutils.Panicln(err)
		retVal = f
	case "int", "bigint", "smallint", "tinyint":
		f, err := strconv.ParseInt(val, 10, 64)
		logutils.Panicln(err)
		retVal = f
	case "bit":
		f, err := strconv.ParseBool(val)
		logutils.Panicln(err)
		retVal = f
	default:
		retVal = val
	}
	return retVal
}

func ColumnMap(table string, connId uint64, authorization string) map[string]string {
	columnMap := make(map[string]string)
	dc := GetConn(connId, authorization)
	stmt, err := dc.Conn.Prepare(SQL_DIALECT[dc.DbType]["ColumnMapMySQL"])
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

func QueryPrimaryKey(schema, table string, tx *sql.Tx) []string {
	primaryKeys := make([]string, 0)
	stmt, err := tx.Prepare("select column_name from information_schema.columns where TABLE_SCHEMA = ? and table_name = ? and column_key = 'PRI'")
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

func QueryColType(schema, table string, tx *sql.Tx) map[string]string {
	colTypeMap := make(map[string]string, 0)
	stmt, err := tx.Prepare("select column_name,DATA_TYPE from information_schema.columns where TABLE_SCHEMA = ? and table_name = ?")
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
