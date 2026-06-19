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
	columnDefRegex := regexp.MustCompile("(?i)^\\s+[`\"']?(\\w+)[`\"']?\\s+")
	createTableRegex := regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[`\"']?(\\w+)[`\"']?")

	currentTable := ""
	removedColCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperTrimmed := strings.ToUpper(trimmed)

		if strings.HasPrefix(upperTrimmed, "CREATE ") {
			if match := createTableRegex.FindStringSubmatch(line); len(match) >= 2 {
				currentTable = match[1]
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
				filtered = append(filtered, line)
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
