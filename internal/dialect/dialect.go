package dialect

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"websql/internal/logger"

	"slices"

	"golang.org/x/text/encoding/simplifiedchinese"
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
		// listView 列出所有视图
		"listView": "SELECT TABLE_NAME AS VIEW_NAME, VIEW_DEFINITION, CHECK_OPTION, IS_UPDATABLE FROM information_schema.VIEWS WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME",
		// getTableDDL 获取建表语句（MySQL 用 SHOW CREATE TABLE，表名需调用方替换 {table} 占位符）
		"getTableDDL": "SHOW CREATE TABLE `{table}`",
		// getTableComment 获取表注释
		"getTableComment": "SELECT TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
		// getRowCount 获取行数（InnoDB 引擎下 TABLE_ROWS 为估算值）
		"getRowCount": "SELECT TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
		// listProcedures 列出存储过程
		"listProcedures": "SELECT ROUTINE_NAME FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ? AND ROUTINE_TYPE = 'PROCEDURE' ORDER BY ROUTINE_NAME",
		// listTriggers 列出触发器
		"listTriggers": "SELECT TRIGGER_NAME, EVENT_MANIPULATION, EVENT_OBJECT_TABLE, ACTION_TIMING FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ? ORDER BY TRIGGER_NAME",
		// listFunctions 列出函数
		"listFunctions": "SELECT ROUTINE_NAME FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ? AND ROUTINE_TYPE = 'FUNCTION' ORDER BY ROUTINE_NAME",
		// listEvents 列出事件（仅 MySQL/MariaDB 支持事件调度器）
		"listEvents": "SELECT EVENT_NAME, EVENT_TYPE, STATUS FROM information_schema.EVENTS WHERE EVENT_SCHEMA = ? ORDER BY EVENT_NAME",
		// viewDDL 获取视图定义（SHOW CREATE VIEW，name 需调用方替换 {name} 占位符；调用前必须 sanitize 校验）
		"viewDDL": "SHOW CREATE VIEW `{name}`",
		// procedureDDL 获取存储过程定义（SHOW CREATE PROCEDURE，name 需调用方替换 {name} 占位符）
		"procedureDDL": "SHOW CREATE PROCEDURE `{name}`",
		// functionDDL 获取函数定义（SHOW CREATE FUNCTION，name 需调用方替换 {name} 占位符）
		"functionDDL": "SHOW CREATE FUNCTION `{name}`",
		// triggerDDL 获取触发器定义（SHOW CREATE TRIGGER，name 需调用方替换 {name} 占位符）
		"triggerDDL": "SHOW CREATE TRIGGER `{name}`",
		// eventDDL 获取事件定义（SHOW CREATE EVENT，name 需调用方替换 {name} 占位符）
		"eventDDL": "SHOW CREATE EVENT `{name}`",
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
		// listView 列出所有视图（SQLite 通过 sqlite_master 查询 type='view' 的对象）
		"listView": "SELECT name AS VIEW_NAME, sql AS VIEW_DEFINITION FROM sqlite_master WHERE type = 'view' AND name NOT LIKE 'sqlite_%' AND ?1 = ?1 ORDER BY name",
		// getTableDDL 获取建表语句（SQLite 直接查询 sqlite_master.sql 字段）
		"getTableDDL": "SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?1",
		// getTableComment 获取表注释（SQLite 不支持表注释，返回空字符串占位）
		"getTableComment": "SELECT '' AS TABLE_COMMENT WHERE ?1 = ?1",
		// getRowCount 获取行数（SQLite 无元数据表存储行数，需精确 COUNT，表名需调用方替换 {table} 占位符）
		"getRowCount": "SELECT COUNT(*) AS ROW_COUNT FROM \"{table}\"",
		// listProcedures 列出存储过程（SQLite 不支持存储过程，返回空结果集）
		"listProcedures": "SELECT NULL AS PROCEDURE_NAME WHERE 1=0",
		// listTriggers 列出触发器（SQLite 通过 sqlite_master 查询 type='trigger' 的对象）
		"listTriggers": "SELECT name AS TRIGGER_NAME, tbl_name AS TABLE_NAME FROM sqlite_master WHERE type = 'trigger' AND ?1 = ?1 ORDER BY name",
		// listFunctions 列出函数（SQLite 不支持自定义函数，返回空结果集）
		"listFunctions": "SELECT NULL AS FUNCTION_NAME WHERE 1=0",
		// listEvents 列出事件（SQLite 不支持事件调度器，返回空结果集）
		"listEvents": "SELECT NULL AS EVENT_NAME WHERE 1=0",
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
		// listView 列出所有视图（Oracle 通过 USER_VIEWS 数据字典视图查询）
		"listView": "SELECT VIEW_NAME, TEXT AS VIEW_DEFINITION FROM USER_VIEWS WHERE 'notexists' <> :1 ORDER BY VIEW_NAME",
		// getTableDDL 获取建表语句（Oracle 使用 DBMS_METADATA.GET_DDL 包获取完整 DDL）
		"getTableDDL": "SELECT DBMS_METADATA.GET_DDL('TABLE', :1) AS DDL FROM DUAL",
		// getTableComment 获取表注释（Oracle 通过 USER_TAB_COMMENTS 查询）
		"getTableComment": "SELECT COMMENTS FROM USER_TAB_COMMENTS WHERE TABLE_NAME = :1",
		// getRowCount 获取行数（Oracle 通过 USER_TABLES.NUM_ROWS 获取估算值，需先执行 ANALYZE 收集统计信息）
		"getRowCount": "SELECT NUM_ROWS FROM USER_TABLES WHERE TABLE_NAME = :1",
		// listProcedures 列出存储过程（Oracle 通过 USER_OBJECTS 查询 OBJECT_TYPE='PROCEDURE' 的对象）
		"listProcedures": "SELECT OBJECT_NAME FROM USER_OBJECTS WHERE OBJECT_TYPE = 'PROCEDURE' AND 'notexists' <> :1 ORDER BY OBJECT_NAME",
		// listTriggers 列出触发器（Oracle 通过 USER_TRIGGERS 数据字典视图查询）
		"listTriggers": "SELECT TRIGGER_NAME, TRIGGERING_EVENT, TABLE_NAME, TRIGGER_TYPE FROM USER_TRIGGERS WHERE 'notexists' <> :1 ORDER BY TRIGGER_NAME",
		// listFunctions 列出函数（Oracle 通过 USER_OBJECTS 查询 OBJECT_TYPE='FUNCTION' 的对象）
		"listFunctions": "SELECT OBJECT_NAME FROM USER_OBJECTS WHERE OBJECT_TYPE = 'FUNCTION' AND 'notexists' <> :1 ORDER BY OBJECT_NAME",
		// listEvents 列出事件（Oracle 无 MySQL 风格的事件调度器，返回空结果集；调度任务可查 DBA_SCHEDULER_JOBS）
		"listEvents": "SELECT NULL AS EVENT_NAME FROM DUAL WHERE 1=0",
		// viewDDL 获取视图定义（Oracle 通过 USER_VIEWS.TEXT 查询，name 需调用方转为大写）
		"viewDDL": "SELECT TEXT AS DDL FROM USER_VIEWS WHERE VIEW_NAME = :1",
		// procedureDDL 获取存储过程定义（Oracle 通过 USER_SOURCE 按行聚合为完整 DDL，name 需大写）
		"procedureDDL": "SELECT LISTAGG(TEXT, CHR(10)) WITHIN GROUP (ORDER BY LINE) AS DDL FROM USER_SOURCE WHERE TYPE = 'PROCEDURE' AND NAME = :1",
		// functionDDL 获取函数定义（Oracle 通过 USER_SOURCE 按行聚合为完整 DDL，name 需大写）
		"functionDDL": "SELECT LISTAGG(TEXT, CHR(10)) WITHIN GROUP (ORDER BY LINE) AS DDL FROM USER_SOURCE WHERE TYPE = 'FUNCTION' AND NAME = :1",
		// triggerDDL 获取触发器定义（Oracle 通过 USER_TRIGGERS.TRIGGER_BODY 查询，name 需大写）
		"triggerDDL": "SELECT TRIGGER_BODY AS DDL FROM USER_TRIGGERS WHERE TRIGGER_NAME = :1",
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
				if err != nil {
					logger.PrintErrf("MySQL INT解析失败, colType=%s, val=%s", err, *colType, strVal)
					v = strVal
				} else if overSign && iv > 9007199254740992 {
					v = fmt.Sprintf("s:%d", iv)
				} else {
					v = iv
				}
			case "DECIMAL", "NUMERIC", "FLOAT", "DOUBLE":
				fv, err := strconv.ParseFloat(strVal, 64)
				if err != nil {
					logger.PrintErrf("MySQL FLOAT解析失败, colType=%s, val=%s", err, *colType, strVal)
					v = strVal
				} else if overSign && fv > 9007199254740992 {
					v = fmt.Sprintf("s:%f", fv)
				} else {
					v = fv
				}
			case "BOOL", "BOOLEAN":
				if len(b) > 0 {
					v = b[0] != byte(0)
				} else {
					v = false
				}
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
			strVal := fixOracleEncoding(string(b))
			switch *colType {
			case "NUMBER":
				i, err := strconv.ParseInt(strVal, 10, 64)
				if err != nil {
					f, err2 := strconv.ParseFloat(strVal, 64)
					if err2 != nil {
						logger.PrintErrf("Oracle NUMBER类型解析失败: %s", err2)
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
					logger.PrintErrf("Oracle INTEGER类型解析失败", err)
					v = strVal
				} else {
					if overSign && i > 9007199254740992 {
						v = fmt.Sprintf("s:%d", i)
					} else {
						v = i
					}
				}
			default:
				v = strVal
			}
		} else if t, ok := (*val).(time.Time); ok {
			v = time.Time(t).Format(time.DateTime)
		} else if s, ok := (*val).(string); ok {
			v = fixOracleEncoding(s)
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
			if err != nil {
				logger.PrintErrf("MySQL ParseVal FLOAT解析失败", err)
				var r any = *val
				return &r
			}
			retVal = f
		case "int", "bigint", "smallint", "mediumint", "tinyint":
			f, err := strconv.ParseFloat(*val, 64)
			if err != nil {
				logger.PrintErrf("MySQL ParseVal INT解析失败", err)
				var r any = *val
				return &r
			}
			retVal = int64(f)
		case "bit":
			if len(*val) >= 3 && (*val)[0:2] == "b'" {
				b, err := strconv.ParseBool((*val)[2:3])
				if err != nil {
					logger.PrintErrf("MySQL ParseVal BIT解析失败", err)
					var r any = *val
					return &r
				}
				retVal = b
			} else {
				f, err := strconv.ParseBool(*val)
				if err != nil {
					logger.PrintErrf("MySQL ParseVal BOOL解析失败", err)
					var r any = *val
					return &r
				}
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
				if err != nil {
					logger.PrintErrf("Oracle ParseVal NUMBER/FLOAT解析失败", err)
					var r any = *val
					return &r
				}
				retVal = f
			} else if err != nil {
				logger.PrintErrf("Oracle ParseVal NUMBER解析失败", err)
				var r any = *val
				return &r
			} else {
				retVal = i
			}
		case "INTEGER":
			f, err := strconv.ParseInt(*val, 10, 64)
			if err != nil {
				logger.PrintErrf("Oracle ParseVal INTEGER解析失败", err)
				var r any = *val
				return &r
			}
			retVal = f
		case "TIMESTAMP(6)":
			f, err := time.Parse("2006-01-02 15:04:05", *val)
			if err != nil {
				logger.PrintErrf("Oracle ParseVal TIMESTAMP解析失败", err)
				var r any = *val
				return &r
			}
			retVal = f
		default:
			var r any = *val
			return &r
		}
		return &retVal
	},
}

// init 为 mariadb 复制 mysql 的方言定义、列转换器和值解析器。
// MariaDB 与 MySQL 的 information_schema 完全兼容，协议层面也一致，
// 因此方言 SQL 模板和类型处理逻辑可完全复用 MySQL 的实现。
// 这里通过深拷贝 SQL_DIALECT["mysql"] 为独立的 mariadb map，
// 避免运行时对 mysql map 的修改影响 mariadb；而 ConvertColHandler / ParseValHandler
// 为函数类型不可变，可直接共享引用。
func init() {
	if mysqlDialect, ok := SQL_DIALECT["mysql"]; ok {
		mariadbDialect := make(map[string]string, len(mysqlDialect))
		for k, v := range mysqlDialect {
			mariadbDialect[k] = v
		}
		SQL_DIALECT["mariadb"] = mariadbDialect
	}
	if fn, ok := ConvertColHandler["mysql"]; ok {
		ConvertColHandler["mariadb"] = fn
	}
	if fn, ok := ParseValHandler["mysql"]; ok {
		ParseValHandler["mariadb"] = fn
	}
}

// fixOracleEncoding 修复 Oracle 返回的可能存在编码问题的字符串。
// 当 go-ora 驱动未能正确转换字符集时（如服务器使用 ZHS16GBK 等中文字符集），
// 返回的 string 可能包含非法 UTF-8 序列。此函数检测并尝试用 GBK 解码修复。
func fixOracleEncoding(s string) string {
	if s == "" {
		return s
	}
	// 如果已经是合法的 UTF-8，直接返回
	if utf8.ValidString(s) {
		return s
	}
	// 尝试用 GBK 解码（中国 Oracle 数据库最常见的字符集）
	decoded, err := simplifiedchinese.GBK.NewDecoder().String(s)
	if err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	// GBK 失败，尝试 GB18030（GBK 的超集，覆盖更多字符）
	decoded, err = simplifiedchinese.GB18030.NewDecoder().String(s)
	if err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	// 都失败了，返回原始字符串
	return s
}
