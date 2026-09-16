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
// 仅允许 ALTER TABLE / CREATE TABLE / CREATE INDEX / DROP INDEX
// （CREATE TABLE 用于结构同步中的整表新增）
func ValidateSchemaSQL(sql string) error {
	upper := normalize(sql)

	allowedPrefixes := []string{
		"ALTER TABLE", "CREATE TABLE", "CREATE INDEX", "DROP INDEX",
	}
	matched := false
	isCreateTable := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			matched = true
			if prefix == "CREATE TABLE" {
				isCreateTable = true
			}
			break
		}
	}
	if !matched {
		preview := upper
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		return fmt.Errorf("不允许执行的SQL类型: %s (仅允许 ALTER TABLE / CREATE TABLE / CREATE INDEX / DROP INDEX)", preview)
	}

	if isCreateTable {
		// 建表语句中的列注释/默认值可能合法包含 DROP TABLE 等字样
		// （如 COMMENT='...DROP TABLE...'），改用严格黑名单避免误拦
		return checkStrictDangerous(upper)
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

// ValidateNoFileSideEffects 仅执行「文件/权限副作用」危险模式检查，不校验语句类型。
//
// 用途：AI Agent 的 exec_sql 工具已在上层做语句类型白名单（DDL/DML 分别走 ValidateDDL/
// ValidateDML），此处对**所有**语句统一拦截读写文件、加载数据、权限变更等副作用写法
// （SELECT ... INTO OUTFILE、LOAD DATA、LOAD_FILE、GRANT 等），确保 AI 路径与
// 同步/备份/建模复用同一份黑名单，避免规则漂移。
func ValidateNoFileSideEffects(sql string) error {
	upper := normalize(sql)
	for _, d := range dmlDangerousPatterns {
		if strings.Contains(upper, d) {
			return fmt.Errorf("SQL包含危险操作: %s", d)
		}
	}
	return nil
}

// ──────────────────────────────────────────────
// SQL 语句分类（词级判定）
//
// 背景：项目历史实现中，SQL 类型判定存在「关键字+空格」前缀匹配（如
// strings.HasPrefix(upper, "DELETE ")）与「不剥离注释」两处缺陷：
//   - `DELETE\tFROM t`（制表符）、`DELETE\nFROM t`（换行）不命中前缀匹配；
//   - `WITH/*c*/x AS (...) DELETE FROM t`、`DELETE/*c*/FROM t` 因注释
//     插在关键字中间而无法识别首关键字，导致上层按「查询」处理并放行写入。
//
// 本组函数是项目内唯一的 SQL 语句分类实现（AI 路径的 sqlutil 与经典路径的
// permission 均委托到这里），供权限校验、执行分支、审计分级共用，避免规则漂移。
// ─────────────────────────────────────────────

// writeKeywords 写操作关键字集合（词级匹配，大小写不敏感）。
// 与 AI 路径 sqlutil 保持同一份口径。
var writeKeywords = map[string]bool{
	"DROP": true, "TRUNCATE": true, "DELETE": true, "ALTER": true,
	"CREATE": true, "REPLACE": true, "INSERT": true, "UPDATE": true,
	"MERGE": true,
}

// readOnlyKeywords 明确只读的首关键字白名单。
// 白名单之外的一切语句（含 SET/USE/PRAGMA/事务控制等）在权限层按「需要修改权限」
// 处理（fail-closed），避免解析器无法识别时被静默放行。
var readOnlyKeywords = map[string]bool{
	"SELECT": true, "SHOW": true, "DESCRIBE": true, "DESC": true,
	"EXPLAIN": true, "VALUES": true, "TABLE": true,
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentRune(r rune) bool {
	return r == '_' || r == '$' ||
		(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// StripComments 剥离 SQL 注释，是项目内唯一的注释剥离实现。
//
// 正确处理：
//   - 行注释：-- 直至换行；#  直至换行（MySQL）
//   - 块注释：/* ... */（可跨行、可内联，替换为空格避免 "SELECT/**/id" 粘连）
//   - 字符串字面量内的 '--' / '#' / '/*' 不视为注释
//   - 单引号字符串内 '' 转义、双引号标识符内不视为注释
func StripComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	inSingle, inDouble := false, false
	i := 0
	for i < len(sql) {
		c := sql[i]
		if inSingle {
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					b.WriteByte('\'')
					i += 2
					continue
				}
				inSingle = false
			}
			i++
			continue
		}
		if inDouble {
			b.WriteByte(c)
			if c == '"' {
				inDouble = false
			}
			i++
			continue
		}
		if c == '\'' {
			inSingle = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == '"' {
			inDouble = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			idx := strings.Index(sql[i+2:], "*/")
			if idx == -1 {
				return strings.TrimSpace(b.String())
			}
			b.WriteByte(' ')
			i += idx + 4
			continue
		}
		if (c == '-' && i+1 < len(sql) && sql[i+1] == '-') || c == '#' {
			idx := strings.Index(sql[i:], "\n")
			if idx == -1 {
				return strings.TrimSpace(b.String())
			}
			i += idx
			continue
		}
		b.WriteByte(c)
		i++
	}
	return strings.TrimSpace(b.String())
}

// FirstKeyword 返回剥离注释后的首个关键字（跳过前导空白/标点，统一大写）。
// 无关键字时返回空串。
func FirstKeyword(sql string) string {
	stripped := StripComments(strings.TrimSpace(sql))
	i := 0
	for i < len(stripped) && !isIdentByte(stripped[i]) {
		i++
	}
	j := i
	for j < len(stripped) && isIdentByte(stripped[j]) {
		j++
	}
	return strings.ToUpper(stripped[i:j])
}

// HasWriteKeyword 判断剥离注释后的语句中是否出现写操作关键字（词级整句扫描）。
// 用于识别 CTE 包裹的写操作（首关键字为 WITH）。
func HasWriteKeyword(sql string) bool {
	stripped := StripComments(sql)
	for _, tok := range strings.FieldsFunc(stripped, func(r rune) bool { return !isIdentRune(r) }) {
		if writeKeywords[strings.ToUpper(tok)] {
			return true
		}
	}
	return false
}

// WriteKind 返回语句的「有效写操作关键字」：
//   - 普通语句：首关键字（如 DELETE FROM t → DELETE）
//   - CTE 语句：整句词级扫描到的第一个写关键字（如 WITH x AS (...) DELETE FROM t → DELETE）
//   - 非写语句：返回首关键字或空串
func WriteKind(sql string) string {
	kw := FirstKeyword(sql)
	if kw != "WITH" {
		return kw
	}
	stripped := StripComments(sql)
	for _, tok := range strings.FieldsFunc(stripped, func(r rune) bool { return !isIdentRune(r) }) {
		up := strings.ToUpper(tok)
		if writeKeywords[up] {
			return up
		}
	}
	return kw
}

// IsWriteStatement 判断语句是否为写操作（含 CTE 包裹的写操作）。
func IsWriteStatement(sql string) bool {
	return writeKeywords[WriteKind(sql)]
}

// IsReadOnlyStatement 判断语句是否「明确只读」（fail-closed 白名单）：
// 首关键字属于 SELECT/SHOW/DESCRIBE/DESC/EXPLAIN/VALUES/TABLE，
// 或首关键字为 WITH 且整句不含写关键字。
// 返回 false 的语句在权限层一律要求「数据修改权限」。
func IsReadOnlyStatement(sql string) bool {
	kw := FirstKeyword(sql)
	if kw == "WITH" {
		return !HasWriteKeyword(sql)
	}
	return readOnlyKeywords[kw]
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

// checkStrictDangerous 检查严格危险模式（不含 DROP TABLE/TRUNCATE）。
// 用于 CREATE TABLE 场景：建表语句的列注释/默认值可能合法包含这些关键词，
// 而文件写入、权限变更等真正危险的操作仍会被拦截。
func checkStrictDangerous(upper string) error {
	strict := []string{
		"DROP DATABASE",
		"GRANT", "REVOKE", "CREATE USER", "ALTER USER", "DROP USER",
		"SHUTDOWN", "LOAD DATA", "INTO OUTFILE", "INTO DUMPFILE",
	}
	for _, d := range strict {
		if strings.Contains(upper, d) {
			return fmt.Errorf("SQL包含危险操作: %s", d)
		}
	}
	return nil
}
