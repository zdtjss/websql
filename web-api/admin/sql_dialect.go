package admin

import (
	"bytes"
	"go-web/logutils"
	"strconv"
	"time"

	go_ora "github.com/sijms/go-ora/v2"
	"golang.org/x/exp/slices"
)

var SQL_DIALECT = map[string]map[string]string{
	"mysql": {
		"listSchema":      "select schema_name from information_schema.schemata",
		"listTable":       "select TABLE_NAME,table_comment from information_schema.tables WHERE table_schema = ?",
		"listColumns":     "select concat(column_name,'  ', column_type) column_name,COLUMN_COMMENT from information_schema.COLUMNS where TABLE_NAME = ? order by ORDINAL_POSITION",
		"listAllColumns":  "select column_name, COLUMN_COMMENT from information_schema.COLUMNS where table_schema = ?",
		"ColumnMap":       "SELECT COLUMN_NAME,column_comment FROM information_schema.COLUMNS WHERE TABLE_NAME = ?",
		"QueryPrimaryKey": "select column_name from information_schema.columns where TABLE_SCHEMA = ? and table_name = ? and column_key = 'PRI'",
		"QueryColType":    "select column_name,DATA_TYPE from information_schema.columns where TABLE_SCHEMA = ? and table_name = ?",
	},
	"oracle": {
		"listSchema": "select SYS_CONTEXT('USERENV','CURRENT_SCHEMA') schema_name from dual",
		"listTable":  "select TABLE_NAME, COMMENTS table_comment from user_tab_comments where 'notexists' <> :1",
		"listColumns": `SELECT B.COLUMN_NAME || ' ' || B.DATA_TYPE as column_name, A.COMMENTS COLUMN_COMMENT
			FROM USER_COL_COMMENTS A left join USER_TAB_COLUMNS B on A.TABLE_NAME = B.TABLE_NAME 
			WHERE a.COLUMN_NAME = b.COLUMN_NAME and A.TABLE_NAME = :1`,
		"listAllColumns": `SELECT B.COLUMN_NAME, A.COMMENTS COLUMN_COMMENT
			FROM USER_COL_COMMENTS A left join USER_TAB_COLUMNS B on A.TABLE_NAME = B.TABLE_NAME 
			WHERE a.COLUMN_NAME = b.COLUMN_NAME`,
		"ColumnMap": `SELECT B.COLUMN_NAME, A.COMMENTS column_comment 
			FROM USER_COL_COMMENTS A left join USER_TAB_COLUMNS B on A.TABLE_NAME = B.TABLE_NAME 
			WHERE a.COLUMN_NAME = b.COLUMN_NAME and A.TABLE_NAME = :1`,
		"QueryPrimaryKey": "SELECT b.COLUMN_NAME from user_constraints a left join user_cons_columns b on a.TABLE_NAME = b.TABLE_NAME where a.TABLE_NAME = :1 and CONSTRAINT_TYPE = 'P'",
		"QueryColType":    "select column_name,DATA_TYPE from USER_TAB_COLUMNS where 'notexists' <> :1 and table_name = :2",
	},
}

// 查询页面或导出excel显示的结果数据
// 由数据库查询到的数据不便直接展示
var ConvertColHandler = map[string]func(colType *string, val *any) *any{
	"mysql": func(colType *string, val *any) *any {
		var v any
		//判断是否为[]byte
		if b, ok := (*val).([]byte); ok {
			switch *colType {
			case "BIT":
				ba := bytes.NewBufferString("")
				for _, bc := range b {
					if bc != byte(0) {
						ba.WriteString(strconv.FormatUint(uint64(bc), 2))
					}
				}
				v = "b'" + ba.String() + "'"
			default:
				v = string(b)
			}
		} else if t, ok := (*val).(time.Time); ok {
			v = t.Format("2006-01-02 15:04:05")
		} else {
			return val
		}
		return &v
	},
	"oracle": func(colType *string, val *any) *any {
		var v any
		//判断是否为[]byte
		if b, ok := (*val).([]byte); ok {
			switch *colType {
			default:
				v = string(b)
			}
		} else if t, ok := (*val).(go_ora.TimeStamp); ok {
			v = time.Time(t).Format("2006-01-02 15:04:05")
		} else {
			return val
		}
		return &v
	},
}

// 通常是导入数据时将excel中数据转成数据库类型
var ParseValHandler = map[string]func(colType *string, val *string) *any{
	"mysql": func(colType *string, val *string) *any {
		if slices.Contains([]string{"float", "double", "datetime", "decimal", "int", "bigint", "smallint", "tinyint", "bit"}, *colType) && (*val) == "" {
			var empty any
			return &empty
		}
		var retVal any
		switch *colType {
		case "float", "double", "decimal":
			f, err := strconv.ParseFloat(*val, 64)
			logutils.PanicErr(err)
			retVal = f
		case "int", "bigint", "smallint", "tinyint":
			f, err := strconv.ParseFloat(*val, 64)
			logutils.PanicErr(err)
			retVal = int64(f)
		case "bit":
			if (*val)[0:1] == "b" {
				retVal = *val
			} else {
				f, err := strconv.ParseInt(*val, 2, 8)
				logutils.PanicErr(err)
				retVal = f
			}
		default:
			var r any
			r = *val
			return &r
		}
		return &retVal
	},
	"oracle": func(colType *string, val *string) *any {
		if slices.Contains([]string{"NUMBER", "TIMESTAMP(6)"}, *colType) && (*val) == "" {
			return nil
		}
		var retVal any
		switch *colType {
		case "NUMBER":
			f, err := strconv.ParseFloat(*val, 64)
			logutils.PanicErr(err)
			retVal = f
		case "INTEGER":
			f, err := strconv.ParseInt(*val, 10, 64)
			logutils.PanicErr(err)
			retVal = f
		case "TIMESTAMP(6)":
			f, err := time.Parse("2006-01-02 15:04:05", *val)
			logutils.PanicErr(err)
			retVal = f
		default:
			retVal = val
		}
		return &retVal
	},
}
