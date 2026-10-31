// ddlfilter.go — DDL 过滤逻辑从 permmw.go 拆分而来。
//
// 本文件存放对 get_table_schema 返回的 DDL 进行列级过滤的函数：
//   - filterDDLByScope：根据 PermissionScope 移除用户无权访问的列定义，
//     保留 CREATE TABLE 头、约束、索引、引擎等结构性语句。
package agent

import (
	"log"
	"regexp"
	"strings"
)

func filterDDLByScope(ddl string, tables []string, scope *PermissionScope) string {
	lines := strings.Split(ddl, "\n")
	var filtered []string
	columnDefRegex := regexp.MustCompile("(?i)^\\s+[`\"\\[]?(\\w+)[`\"\\]]?\\s+")
	// 支持 schema 限定名与引号/方括号：Oracle 的 DBMS_METADATA.GET_DDL 返回
	// `CREATE TABLE "HR"."EMPLOYEES" (...)`，旧正则会把 HR 当成表名。
	createTableRegex := regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?((?:[`\"\\[\\]\\w]+\\.)*[`\"\\[\\]\\w]+)")

	currentTable := ""
	removedColCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperTrimmed := strings.ToUpper(trimmed)

		if strings.HasPrefix(upperTrimmed, "CREATE ") {
			if match := createTableRegex.FindStringSubmatch(line); len(match) >= 2 {
				// 只取限定名的最后一段（表名），schema 不能当表名参与权限判定。
				parts := strings.Split(match[1], ".")
				currentTable = trimIdent(parts[len(parts)-1])
			} else {
				currentTable = ""
			}
			filtered = append(filtered, line)
			continue
		}

		if strings.HasPrefix(upperTrimmed, ")") ||
			strings.HasPrefix(upperTrimmed, "PRIMARY KEY") ||
			strings.HasPrefix(upperTrimmed, "KEY ") ||
			strings.HasPrefix(upperTrimmed, "INDEX ") ||
			strings.HasPrefix(upperTrimmed, "UNIQUE ") ||
			strings.HasPrefix(upperTrimmed, "CONSTRAINT ") ||
			strings.HasPrefix(upperTrimmed, "ENGINE") ||
			strings.HasPrefix(upperTrimmed, "DEFAULT CHARSET") ||
			strings.HasPrefix(upperTrimmed, "COMMENT") ||
			strings.HasPrefix(upperTrimmed, "AUTO_INCREMENT") ||
			trimmed == "" || trimmed == ";" {
			filtered = append(filtered, line)
			continue
		}

		match := columnDefRegex.FindStringSubmatch(line)
		if len(match) >= 2 {
			colName := match[1]
			if currentTable != "" {
				accessLevel := scope.GetTableAccessLevel(currentTable)
				if accessLevel == "full" {
					filtered = append(filtered, line)
				} else if accessLevel == "column" {
					if scope.IsColumnAllowed(currentTable, colName) {
						filtered = append(filtered, line)
					} else {
						removedColCount++
					}
				} else {
					removedColCount++
				}
			} else {
				// 无法确定当前表：非 full 权限下一律删除列定义（fail-closed）。
				if scope.SkipChecks() || scope.AllSchemasFull {
					filtered = append(filtered, line)
				} else {
					removedColCount++
				}
			}
		} else {
			filtered = append(filtered, line)
		}
	}

	if removedColCount > 0 {
		log.Printf("[PermScope:DDLFilter] DDL列过滤 - user=%s, 移除列数=%d\n", scope.UserID, removedColCount)
	}

	return strings.Join(filtered, "\n")
}
