package admin

import (
	"go-web/logutils"
	"go-web/utils"
	"net/http"
)

func listSchema_mysql(key uint64, authorization string) []*Tree {
	schemaName := ""
	row, err := GetConn(key, authorization).Query("select schema_name from information_schema.schemata")
	logutils.Panicln(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		tree = append(tree, &Tree{Label: schemaName, Type: TREE_NODE_TYPE_SCHEMA})
	}
	return tree
}

func listTable_mysql(key uint64, schema, authorization string) []*Tree {
	tableName, tableComment := "", ""
	row, err := GetConn(key, authorization).Query("select TABLE_NAME,table_comment from information_schema.tables WHERE table_schema = ?", schema)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableComment)
		tree = append(tree, &Tree{Label: tableName, Data: map[string]any{"text": tableComment}, Type: TREE_NODE_TYPE_TABLE})
	}
	return tree
}

func listColumns_mysql(key uint64, table, authorization string) []*Tree {
	columnName, columnComment := "", ""
	row, err := GetConn(key, authorization).Query("select concat(column_name,'  ', column_type) column_name,COLUMN_COMMENT from information_schema.COLUMNS where TABLE_NAME = ? order by ORDINAL_POSITION", table)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func listAllColumns_mysql(key uint64, schema, authorization string) []*Tree {
	columnName, columnComment := "", ""
	row, err := GetConn(key, authorization).Query("select column_name, COLUMN_COMMENT from information_schema.COLUMNS where table_schema = ?", schema)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func ListTableFat_mysql(w http.ResponseWriter, r *http.Request) {
	authorization := r.Header.Get("Authorization")
	r.ParseForm()
	tables := queryTableInfo_mysql(utils.AtoUint64(r.FormValue("connId")), r.Form.Get("schema"), authorization)
	utils.WriteJson(w, tables)
}

func queryTableInfo_mysql(key uint64, schema, authorization string) []*Table {
	tables := make([]*Table, 0)
	stmt, err := GetConn(key, authorization).Prepare("SELECT TABLE_NAME,table_comment FROM information_schema.tables WHERE table_schema = ?")
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

type Table struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}
