package dialect

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
	"websql/internal/logger"

	"slices"
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
	"sqlite": {
		"listSchema": "SELECT 'main' AS schema_name",
		"listTable":  "SELECT name AS TABLE_NAME, type AS TABLE_TYPE, '' AS table_comment FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' AND ?1 = ?1 ORDER BY name",
		"listColumns": `SELECT ci.name || ' ' || ci.type AS column_name, '' AS COLUMN_COMMENT
			FROM pragma_table_info(?1) AS ci`,
		"listAllColumns": `SELECT m.name AS TABLE_NAME, '' AS column_name, '' AS COLUMN_COMMENT
			FROM sqlite_master m WHERE m.type IN ('table', 'view') AND m.name NOT LIKE 'sqlite_%' AND ?1 = ?1 ORDER BY m.name`,
		"listTableColumns": `SELECT ci.name AS COLUMN_NAME, ci.type AS COLUMN_TYPE,
			CASE WHEN ci."notnull" = 1 THEN 'NO' ELSE 'YES' END AS IS_NULLABLE,
			ci.dflt_value AS COLUMN_DEFAULT,
			'' AS COLUMN_COMMENT,
			CASE WHEN ci.pk > 0 THEN 'PRI' ELSE '' END AS COLUMN_KEY,
			ci.cid AS ORDINAL_POSITION,
			NULL AS CHARACTER_MAXIMUM_LENGTH,
			'' AS EXTRA
			FROM pragma_table_info(?2) AS ci WHERE (?1 = ?1) ORDER BY ci.cid`,
		"ColumnMap":       "SELECT name AS COLUMN_NAME, '' AS column_comment FROM pragma_table_info(?1) WHERE (?2 = ?2)",
		"QueryPrimaryKey": "SELECT name AS column_name FROM pragma_table_info(?2) WHERE pk > 0 AND (?1 = ?1)",
		"QueryColType":    "SELECT name AS column_name, type AS DATA_TYPE FROM pragma_table_info(?2) WHERE (?1 = ?1)",
		"tableOptions":    "SELECT '' AS ENGINE, '' AS TABLE_COLLATION, '' AS TABLE_COMMENT, '' AS ROW_FORMAT, 0 AS AUTO_INCREMENT, '' AS CREATE_OPTIONS WHERE (?1 = ?1) AND (?2 = ?2) LIMIT 1",
		"tableStatistics": "SELECT 0 AS TABLE_ROWS, 0 AS DATA_LENGTH, 0 AS INDEX_LENGTH, 0 AS DATA_FREE, 0 AS AVG_ROW_LENGTH, '' AS CREATE_TIME, '' AS UPDATE_TIME WHERE (?1 = ?1) AND (?2 = ?2) LIMIT 1",
		"listIndexes": `SELECT il.name AS INDEX_NAME, ii.name AS COLUMN_NAME,
			CASE WHEN il.origin = 'pk' THEN 0 ELSE CASE WHEN il."unique" = 1 THEN 0 ELSE 1 END END AS NON_UNIQUE,
			ii.seqno AS SEQ_IN_INDEX,
			'' AS INDEX_TYPE,
			'YES' AS NULLABLE,
			'' AS COMMENT,
			'' AS INDEX_COMMENT
			FROM pragma_index_list(?2) AS il JOIN pragma_index_info(il.name) AS ii
			WHERE (?1 = ?1) ORDER BY il.name, ii.seqno`,
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

var ConvertColHandler = map[string]func(colType *string, val *any, overSign bool) *any{
	"mysql": func(colType *string, val *any, overSign bool) *any {
		var v any
		if b, ok := (*val).([]byte); ok {
			strVal := string(b)
			switch *colType {
			case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "BIGINT":
				iv, err := strconv.ParseInt(strVal, 10, 64)
				logger.PanicErrf("数字解析失败", err)
				if overSign && iv > 9007199254740992 {
					v = fmt.Sprintf("s:%d", iv)
				} else {
					v = iv
				}
			case "DECIMAL", "NUMERIC", "FLOAT", "DOUBLE":
				fv, err := strconv.ParseFloat(strVal, 64)
				logger.PanicErrf("数字解析失败", err)
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
						logger.PrintErrf("NUMBER类型解析失败: %s", err2)
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
					logger.PrintErrf("INTEGER类型解析失败", err)
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
		if t, ok := (*val).(time.Time); ok {
			v = time.Time(t).Format(time.DateTime)
		} else {
			v = *val
		}
		return &v
	},
}

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
			logger.PanicErr(err)
			retVal = f
		case "int", "bigint", "smallint", "mediumint", "tinyint":
			f, err := strconv.ParseFloat(*val, 64)
			logger.PanicErr(err)
			retVal = int64(f)
		case "bit":
			if (*val)[0:2] == "b'" {
				b, err := strconv.ParseBool((*val)[2:3])
				logger.PanicErr(err)
				retVal = b
			} else {
				f, err := strconv.ParseBool(*val)
				logger.PanicErr(err)
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
				logger.PanicErr(err)
				retVal = f
			} else {
				logger.PanicErr(err)
				retVal = i
			}
		case "INTEGER":
			f, err := strconv.ParseInt(*val, 10, 64)
			logger.PanicErr(err)
			retVal = f
		case "TIMESTAMP(6)":
			f, err := time.Parse("2006-01-02 15:04:05", *val)
			logger.PanicErr(err)
			retVal = f
		default:
			var r any = *val
			return &r
		}
		return &retVal
	},
}
