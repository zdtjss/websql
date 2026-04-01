package agent

import (
	"regexp"
	"strings"
)

// dangerPatterns 匹配需要用户确认的危险 SQL 关键字。
var dangerPatterns = regexp.MustCompile(
	`(?i)^\s*(ALTER|UPDATE|DELETE|DROP|TRUNCATE)\s`,
)

// ClassifySQL 判断 SQL 的危险等级。
func ClassifySQL(sql string) DangerLevel {
	// 可能包含多条语句，逐条检查
	for _, stmt := range strings.Split(sql, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if dangerPatterns.MatchString(stmt) {
			return DangerConfirm
		}
	}
	return DangerNone
}

// ExtractSQLFromResponse 从 AI 回复中提取 SQL（去除 markdown 代码块标记）。
func ExtractSQLFromResponse(content string) string {
	content = strings.TrimSpace(content)
	// 去除 ```sql ... ``` 包裹
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) >= 2 {
			// 去掉首行 ```sql 和末行 ```
			start := 1
			end := len(lines)
			if strings.TrimSpace(lines[end-1]) == "```" {
				end--
			}
			content = strings.Join(lines[start:end], "\n")
		}
	}
	return strings.TrimSpace(content)
}
