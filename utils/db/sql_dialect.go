package db

import (
	"bytes"
	"fmt"
	"go-web/logutils"
	"strconv"
	"strings"
	"time"

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
var ConvertColHandler = map[string]func(colType *string, val *any, overSign bool) *any{
	"mysql": func(colType *string, val *any, overSign bool) *any {
		var v any
		//判断是否为[]byte
		if b, ok := (*val).([]byte); ok {
			strVal := string(b)
			switch *colType {
			case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "BIGINT":
				iv, err := strconv.ParseInt(strVal, 10, 64)
				logutils.PanicErrf("数字解析失败", err)
				// js 数字类型上限大小
				if overSign && iv > 9007199254740992 {
					v = fmt.Sprintf("s:%d", iv)
				} else {
					v = iv
				}
			case "DECIMAL", "NUMERIC", "FLOAT", "DOUBLE":
				fv, err := strconv.ParseFloat(strVal, 64)
				logutils.PanicErrf("数字解析失败", err)
				// js 数字类型上限大小
				if overSign && fv > 9007199254740992 {
					v = fmt.Sprintf("s:%f", fv)
				} else {
					v = fv
				}
			case "BOOL", "BOOLEAN":
				v = b[0] != byte(0)
			case "BIT":
				ba := bytes.NewBufferString("")
				for idx := range b {
					ba.WriteString(strconv.FormatUint(uint64(b[idx]), 2))
				}
				if len(b) > 1 {
					v = "b'" + strings.TrimLeft(ba.String(), "0") + "'"
				} else {
					v = "b'" + ba.String() + "'"
				}
			default:
				v = strVal
			}
		} else if t, ok := (*val).(time.Time); ok {
			v = t.Format(time.DateTime)
		} else {
			return val
		}
		return &v
	},
	"oracle": func(colType *string, val *any, overSign bool) *any {
		var v any
		//判断是否为[]byte
		if b, ok := (*val).([]byte); ok {
			strVal := string(b)
			switch *colType {
			case "NUMBER":
				i, err := strconv.ParseInt(strVal, 10, 64)
				if err != nil && strings.Contains(err.Error(), "invalid syntax") {
					f, err := strconv.ParseFloat(strVal, 64)
					logutils.PanicErr(err)
					// js 数字类型上限大小
					if overSign && f > 9007199254740992 {
						v = fmt.Sprintf("s:%f", f)
					} else {
						v = f
					}
				} else {
					logutils.PanicErr(err)
					// js 数字类型上限大小
					if overSign && i > 9007199254740992 {
						v = fmt.Sprintf("s:%d", i)
					} else {
						v = i
					}
				}
			case "INTEGER":
				i, err := strconv.ParseInt(strVal, 10, 64)
				logutils.PanicErr(err)
				// js 数字类型上限大小
				if overSign && i > 9007199254740992 {
					v = fmt.Sprintf("s:%d", i)
				} else {
					v = i
				}
			default:
				v = string(b)
			}
		} else if t, ok := (*val).(time.Time); ok {
			v = time.Time(t).Format(time.DateTime)
		} else {
			return val
		}
		return &v
	}, "sqlite": func(colType *string, val *any, overSign bool) *any {
		var v any
		//判断是否为[]byte
		if t, ok := (*val).(time.Time); ok {
			v = time.Time(t).Format(time.DateTime)
		} else {
			v = val
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
		case "float", "double", "decimal", "numeric":
			f, err := strconv.ParseFloat(*val, 64)
			logutils.PanicErr(err)
			retVal = f
		case "int", "bigint", "smallint", "mediumint", "tinyint":
			f, err := strconv.ParseFloat(*val, 64)
			logutils.PanicErr(err)
			retVal = int64(f)
		case "bit":
			if (*val)[0:2] == "b'" {
				b, err := strconv.ParseBool((*val)[2:3])
				logutils.PanicErr(err)
				retVal = b
			} else {
				f, err := strconv.ParseBool(*val)
				logutils.PanicErr(err)
				retVal = f
			}
		default:
			var r any = *val
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
		case "NUMBER", "FLOAT":
			i, err := strconv.ParseInt(*val, 10, 64)
			if err != nil && strings.Contains(err.Error(), "invalid syntax") {
				f, err := strconv.ParseFloat(*val, 64)
				logutils.PanicErr(err)
				retVal = f
			} else {
				logutils.PanicErr(err)
				retVal = i
			}
		case "INTEGER":
			f, err := strconv.ParseInt(*val, 10, 64)
			logutils.PanicErr(err)
			retVal = f
		case "TIMESTAMP(6)":
			f, err := time.Parse("2006-01-02 15:04:05", *val)
			logutils.PanicErr(err)
			retVal = f
		default:
			var r any = *val
			return &r
		}
		return &retVal
	},
}
