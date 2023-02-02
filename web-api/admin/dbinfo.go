package admin

import (
	"go-web/logutils"
)

func listSchema(key uint64) []*Tree {
	schemaName := ""
	row, err := GetConn(key).Query("select schema_name from information_schema.schemata")
	logutils.Panicln(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		tree = append(tree, &Tree{Label: schemaName, Type: TREE_NODE_TYPE_SCHEMA})
	}
	return tree
}

func listTable(key uint64, schema string) []*Tree {
	tableName, tableComment := "", ""
	row, err := GetConn(key).Query("select TABLE_NAME,table_comment from information_schema.tables WHERE table_schema = ?", schema)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableComment)
		tree = append(tree, &Tree{Label: tableName, Data: map[string]any{"text": tableComment}, Type: TREE_NODE_TYPE_TABLE})
	}
	return tree
}

func listColumns(key uint64, table string) []*Tree {
	columnName, columnComment := "", ""
	row, err := GetConn(key).Query("select concat(column_name,'  ', column_type) column_name,COLUMN_COMMENT from information_schema.COLUMNS where TABLE_NAME = ? order by ORDINAL_POSITION", table)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func listAllColumns(key uint64, schema string) []*Tree {
	columnName, columnComment := "", ""
	row, err := GetConn(key).Query("select column_name, COLUMN_COMMENT from information_schema.COLUMNS where table_schema = ?", schema)
	logutils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}
