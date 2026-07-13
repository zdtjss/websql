package sqlguard

import (
	"fmt"
	"regexp"
	"strings"
)

// 危险 SQL 模式黑名单（所有校验共用）
var dangerousPatterns = []string{
	"DROP TABLE", "DROP DATABASE", "TRUNCATE",
	"GRANT", "REVOKE", "CREATE USER", "ALTER USER", "DROP USER",
	"SHUTDOWN", "LOAD DATA", "INTO OUTFILE", "INTO DUMPFILE",
}

// whitespaceRe 用于把 SQL 中的连续空白（含 tab/换行）压缩为单空格，
// 让前缀匹配能容忍 "DELETE   FROM" / "UPDATE\tcol" 这类异常但合法的写法。
var whitespaceRe = regexp.MustCompile(`\s+`)

// normalize 把 SQL trim + 转大写 + 压缩空白，便于做前缀/包含匹配
func normalize(sql string) string {
	s := strings.TrimSpace(sql)
	s = whitespaceRe.ReplaceAllString(s, " ")
	return strings.ToUpper(s)
}

// ValidateSchemaSQL 校验 Schema 变更 SQL
// 仅允许 ALTER TABLE / CREATE INDEX / DROP INDEX
func ValidateSchemaSQL(sql string) error {
	upper := normalize(sql)

	allowedPrefixes := []string{
		"ALTER TABLE", "CREATE INDEX", "DROP INDEX",
	}
	matched := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			matched = true
			break
		}
	}
	if !matched {
		preview := upper
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		return fmt.Errorf("不允许执行的SQL类型: %s (仅允许 ALTER TABLE / CREATE INDEX / DROP INDEX)", preview)
	}

	return checkDangerous(upper)
}

// ValidateDataSQL 校验数据操作 SQL
// 仅允许 INSERT / UPDATE / DELETE
func ValidateDataSQL(sql string) error {
	upper := normalize(sql)

	allowedPrefixes := []string{"INSERT INTO", "UPDATE", "DELETE FROM"}
	matched := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			matched = true
			break
		}
	}
	if !matched {
		preview := upper
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		return fmt.Errorf("不允许执行的SQL类型: %s (仅允许 INSERT/UPDATE/DELETE)", preview)
	}

	return checkDangerous(upper)
}

// ValidateDDL 校验 DDL SQL（用于 modeler 正向工程、backup 恢复）
// 允许 CREATE / ALTER / DROP（表/索引/视图），禁止危险操作
func ValidateDDL(sql string) error {
	upper := normalize(sql)

	allowedPrefixes := []string{
		"CREATE TABLE", "CREATE INDEX", "CREATE VIEW", "CREATE SEQUENCE",
		"ALTER TABLE", "ALTER INDEX", "ALTER VIEW",
		"DROP TABLE", "DROP INDEX", "DROP VIEW", "DROP SEQUENCE",
	}
	matched := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			matched = true
			break
		}
	}
	if !matched {
		preview := upper
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		return fmt.Errorf("不允许执行的DDL类型: %s (仅允许 CREATE/ALTER/DROP TABLE/INDEX/VIEW)", preview)
	}

	// DROP TABLE 允许但标记为危险，其他危险操作禁止
	strictDangerous := []string{
		"DROP DATABASE", "TRUNCATE",
		"GRANT", "REVOKE", "CREATE USER", "ALTER USER", "DROP USER",
		"SHUTDOWN", "LOAD DATA", "INTO OUTFILE", "INTO DUMPFILE",
	}
	for _, d := range strictDangerous {
		if strings.Contains(upper, d) {
			return fmt.Errorf("SQL包含危险操作: %s", d)
		}
	}

	return nil
}

// dmlDangerousPatterns 是 DML 场景下的危险模式黑名单。
// 不含 DROP TABLE/TRUNCATE 等 DDL 关键词，避免对含这些词的合法 DML 误报。
var dmlDangerousPatterns = []string{
	"INTO OUTFILE", "INTO DUMPFILE",
	"LOAD_FILE(", "LOAD DATA",
	"GRANT", "REVOKE", "CREATE USER", "ALTER USER", "DROP USER",
	"SHUTDOWN",
}

// ValidateDML 校验 DML SQL（用于备份恢复中的 INSERT/UPDATE/DELETE 语句）
// 允许 INSERT/UPDATE/DELETE，拦截文件写入、加载文件等危险操作
func ValidateDML(sql string) error {
	upper := normalize(sql)

	allowedPrefixes := []string{"INSERT INTO", "UPDATE", "DELETE FROM"}
	matched := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			matched = true
			break
		}
	}
	if !matched {
		preview := upper
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		return fmt.Errorf("不允许执行的DML类型: %s (仅允许 INSERT/UPDATE/DELETE)", preview)
	}

	for _, d := range dmlDangerousPatterns {
		if strings.Contains(upper, d) {
			return fmt.Errorf("DML包含危险操作: %s", d)
		}
	}

	return nil
}

// IsDML 判断 SQL 语句是否为 DML（INSERT/UPDATE/DELETE）
func IsDML(sql string) bool {
	upper := normalize(sql)
	return strings.HasPrefix(upper, "INSERT INTO") ||
		strings.HasPrefix(upper, "UPDATE") ||
		strings.HasPrefix(upper, "DELETE FROM")
}

// checkDangerous 检查危险模式
func checkDangerous(upper string) error {
	for _, d := range dangerousPatterns {
		if strings.Contains(upper, d) {
			return fmt.Errorf("SQL包含危险操作: %s", d)
		}
	}
	return nil
}
