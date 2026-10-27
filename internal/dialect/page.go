package dialect

import "strings"

// 分页包装。
//
// 现有执行路径（internal/app/sql/exec.go 的 page）只追加一个 LIMIT，
// 无法表达 offset，前端也就无法做「继续加载」。
// 这里集中实现方言分页，供 /api/query/page 使用。
// 后续方言注册表重构（Dialect 接口）时，这两个函数会成为接口方法。

// SupportsPaging 判断该方言是否支持按 offset 分页。
func SupportsPaging(dbType string) bool {
	switch dbType {
	case "mysql", "mariadb", "sqlite", "sqlite3":
		return true
	case "oracle":
		return true
	}
	return false
}

// WrapPage 给 SELECT 语句加上 limit/offset。
//
//	MySQL/MariaDB/SQLite: sql LIMIT ? OFFSET ?
//	Oracle 12c 以下兼容写法: SELECT * FROM (SELECT a.*, ROWNUM rn FROM (sql) a WHERE ROWNUM <= ?) WHERE rn > ?
//
// 返回包装后的 SQL 与参数（按出现顺序）。
func WrapPage(dbType, sql string, limit, offset int) (string, []any) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sql), ";"))
	switch dbType {
	case "mysql", "mariadb", "sqlite", "sqlite3":
		return trimmed + " LIMIT ? OFFSET ?", []any{limit, offset}
	case "oracle":
		// ROWNUM 必须在子查询里先限制上界，再在外层按 rn 过滤下界
		return "SELECT * FROM (SELECT a.*, ROWNUM rn FROM (" + trimmed + ") a WHERE ROWNUM <= ?) WHERE rn > ?",
			[]any{offset + limit, offset}
	}
	return trimmed, nil
}
