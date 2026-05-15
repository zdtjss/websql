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
		"listSchema":       "select schema_name from information_schema.schemata order by schema_name",
		"listTable":        "select TABLE_NAME,TABLE_TYPE,table_comment from information_schema.tables WHERE table_schema = ? order by TABLE_NAME",
		"listColumns":      "select concat(column_name,'  ', column_type) column_name,COLUMN_COMMENT from information_schema.COLUMNS where TABLE_NAME = ? order by ORDINAL_POSITION",
		"listAllColumns":   "select TABLE_NAME, column_name, COLUMN_COMMENT from information_schema.COLUMNS where table_schema = ? order by TABLE_SCHEMA,TABLE_NAME,ORDINAL_POSITION",
		"listTableColumns": "select COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_COMMENT, COLUMN_KEY, ORDINAL_POSITION, CHARACTER_MAXIMUM_LENGTH from information_schema.COLUMNS where table_schema = ? and table_name = ? order by ORDINAL_POSITION",
		"ColumnMap":        "SELECT COLUMN_NAME,column_comment FROM information_schema.COLUMNS WHERE lower(TABLE_NAME) = ? and lower(table_schema) = ?",
		"QueryPrimaryKey":  "select column_name from information_schema.columns where TABLE_SCHEMA = ? and table_name = ? and column_key = 'PRI'",
		"QueryColType":     "select column_name,DATA_TYPE from information_schema.columns where TABLE_SCHEMA = ? and table_name = ?",
		"tableOptions":     "SELECT ENGINE,TABLE_COLLATION,TABLE_COMMENT,ROW_FORMAT,AUTO_INCREMENT,CREATE_OPTIONS FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
		"tableStatistics":  "SELECT TABLE_ROWS,DATA_LENGTH,INDEX_LENGTH,DATA_FREE,AVG_ROW_LENGTH,CREATE_TIME,UPDATE_TIME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
		"listIndexes":      "SELECT INDEX_NAME,COLUMN_NAME,NON_UNIQUE,SEQ_IN_INDEX,INDEX_TYPE,NULLABLE,COMMENT,INDEX_COMMENT FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME,SEQ_IN_INDEX",
	},
	"oracle": {
		"listSchema": "select SYS_CONTEXT('USERENV','CURRENT_SCHEMA') schema_name from dual order by schema_name",
		"listTable":  "select TABLE_NAME, table_type, COMMENTS table_comment from user_tab_comments where 'notexists' <> :1 order by TABLE_NAME",
		"listColumns": `SELECT B.COLUMN_NAME || ' ' || B.DATA_TYPE as column_name, A.COMMENTS COLUMN_COMMENT
			FROM USER_COL_COMMENTS A left join USER_TAB_COLUMNS B on A.TABLE_NAME = B.TABLE_NAME 
			WHERE a.COLUMN_NAME = b.COLUMN_NAME and A.TABLE_NAME = :1`,
		"listAllColumns": `SELECT a.TABLE_NAME,B.COLUMN_NAME, A.COMMENTS COLUMN_COMMENT
			FROM USER_COL_COMMENTS A left join USER_TAB_COLUMNS B on A.TABLE_NAME = B.TABLE_NAME 
			WHERE a.COLUMN_NAME = b.COLUMN_NAME and 'notexists' <> :1 order by a.TABLE_NAME,b.COLUMN_ID`,
		"listTableColumns": `select a.column_name,case when nullable='Y' then 'YES' else 'NO' end is_nullable, data_type column_type, a.comments COLUMN_COMMENT FROM USER_COL_COMMENTS A left join USER_TAB_COLUMNS B on A.TABLE_NAME = B.TABLE_NAME and a.column_name = b.column_name
			where 'notexists' <> :1 and a.TABLE_NAME = :2`,
		"ColumnMap": `SELECT B.COLUMN_NAME, A.COMMENTS column_comment 
			FROM ALL_COL_COMMENTS A left join ALL_TAB_COLUMNS B on A.TABLE_NAME = B.TABLE_NAME and A.OWNER = B.OWNER
			WHERE a.COLUMN_NAME = b.COLUMN_NAME and A.TABLE_NAME = :1 and A.OWNER = :2`,
		"QueryPrimaryKey": "SELECT b.COLUMN_NAME from user_constraints a left join user_cons_columns b on a.TABLE_NAME = b.TABLE_NAME where a.TABLE_NAME = :1 and CONSTRAINT_TYPE = 'P'",
		"QueryColType":    "select column_name,DATA_TYPE from USER_TAB_COLUMNS where 'notexists' <> :1 and table_name = :2",
		"tableOptions":    "SELECT TABLE_NAME,TABLESPACE_NAME,PCT_FREE,INI_TRANS,LOGGING,COMPRESSION FROM USER_TABLES WHERE TABLE_NAME = :1",
		"tableStatistics": "SELECT NUM_ROWS,BLOCKS,AVG_ROW_LEN,LAST_ANALYZED FROM USER_TABLES WHERE TABLE_NAME = :1",
		"listIndexes":     "SELECT i.INDEX_NAME, ic.COLUMN_NAME, DECODE(i.UNIQUENESS,'UNIQUE',0,1) NON_UNIQUE, ic.COLUMN_POSITION SEQ_IN_INDEX, i.INDEX_TYPE INDEX_TYPE, 'YES' NULLABLE, CHR(32) COMMENTS, CHR(32) INDEX_COMMENT FROM USER_INDEXES i, USER_IND_COLUMNS ic WHERE i.INDEX_NAME = ic.INDEX_NAME AND i.TABLE_NAME = :1 ORDER BY i.INDEX_NAME, ic.COLUMN_POSITION",
	},
}

// 查询页面或导出excel显示的结果数据
// 由数据库查询到的数据不便直接展示
var ConvertColHandler = map[string]func(colType *string, val *any, overSign bool) *any{
	"mysql": func(colType *string, val *any, overSign bool) *any {
		var v any
		//判断是否为 []byte
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
			// 其他类型直接返回值（解引用）
			v = *val
		}
		return &v
	},
	"oracle": func(colType *string, val *any, overSign bool) *any {
		var v any
		if b, ok := (*val).([]byte); ok {
			strVal := string(b)
			switch *colType {
			case "NUMBER":
				i, err := strconv.ParseInt(strVal, 10, 64)
				if err != nil {
					f, err2 := strconv.ParseFloat(strVal, 64)
					if err2 != nil {
						logutils.PrintErrf("NUMBER类型解析失败: %s", err2)
						v = strVal
					} else {
						if overSign && f > 9007199254740992 {
							v = fmt.Sprintf("s:%f", f)
						} else {
							v = f
						}
					}
				} else {
					if overSign && i > 9007199254740992 {
						v = fmt.Sprintf("s:%d", i)
					} else {
						v = i
					}
				}
			case "INTEGER":
				i, err := strconv.ParseInt(strVal, 10, 64)
				if err != nil {
					logutils.PrintErrf("INTEGER类型解析失败", err)
					v = strVal
				} else {
					if overSign && i > 9007199254740992 {
						v = fmt.Sprintf("s:%d", i)
					} else {
						v = i
					}
				}
			default:
				v = string(b)
			}
		} else if t, ok := (*val).(time.Time); ok {
			v = time.Time(t).Format(time.DateTime)
		} else {
			v = *val
		}
		return &v
	}, "sqlite": func(colType *string, val *any, overSign bool) *any {
		var v any
		//判断是否为 []byte
		if t, ok := (*val).(time.Time); ok {
			v = time.Time(t).Format(time.DateTime)
		} else {
			// 其他类型直接返回值（解引用）
			v = *val
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
