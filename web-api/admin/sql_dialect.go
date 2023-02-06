package admin

var SQL_DIALECT = map[string]map[string]string{
	"mysql": {
		"listSchema":      "select schema_name from information_schema.schemata",
		"listTable":       "select TABLE_NAME,table_comment from information_schema.tables WHERE table_schema = ?",
		"listColumns":     "select concat(column_name,'  ', column_type) column_name,COLUMN_COMMENT from information_schema.COLUMNS where TABLE_NAME = ? order by ORDINAL_POSITION",
		"listAllColumns":  "select column_name, COLUMN_COMMENT from information_schema.COLUMNS where table_schema = ?",
		"queryTableInfo":  "SELECT TABLE_NAME,table_comment FROM information_schema.tables WHERE table_schema = ?",
		"ColumnMap":       "SELECT COLUMN_NAME,column_comment FROM information_schema.COLUMNS WHERE TABLE_NAME = ?",
		"QueryPrimaryKey": "select column_name from information_schema.columns where TABLE_SCHEMA = ? and table_name = ? and column_key = 'PRI'",
		"QueryColType":    "select column_name,DATA_TYPE from information_schema.columns where TABLE_SCHEMA = ? and table_name = ?",
	},
}
