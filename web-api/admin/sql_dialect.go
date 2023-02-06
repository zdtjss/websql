package admin

import (
	"go-web/logutils"
	"strconv"
	"time"

	"golang.org/x/exp/slices"
)

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

// 查询页面或导出excel显示的结果数据
// 由数据库查询到的数据不便直接展示
var ConvertColHandler = map[string]func(colType string, val any) any{
	"mysql": func(colType string, val any) any {
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
	},
}

// 通常是导入数据时将excel中数据转成数据库类型
var ParseValHandler = map[string]func(colType string, val string) any{
	"mysql": func(colType string, val string) any {
		if slices.Contains([]string{"float", "double", "datetime", "decimal", "int", "bigint", "smallint", "tinyint", "bit"}, colType) && val == "" {
			return nil
		}
		var retVal any
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
	},
}
