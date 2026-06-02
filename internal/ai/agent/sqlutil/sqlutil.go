// Package sqlutil 提供跨 agent 与 export 包共享的 SQL 文本处理工具。
//
// 背景（EINO_DEEP_ANALYSIS §10.1）：项目早期在 middleware.go、export/types.go
// 等多处分别实现 StripSQLComments，行为不一致——middleware 版本只去前导注释，
// export 版本按行过滤；导致 PermissionMiddleware 与 Query 工具对同一份 SQL
// 看到不同的表名集合。
//
// 修复策略：把所有"剥离 SQL 注释"的实现统一到本包，并暴露为唯一的公开 API。
// 任何需要"先剥离注释再解析表名/SQL 类型"的代码都应调用 sqlutil.StripSQLComments。
package sqlutil

import "strings"

// StripSQLComments 是项目内**唯一**的 SQL 注释剥离实现。
//
// 正确处理：
//   - 行注释：-- 直至换行（Postgres/Oracle/SQL Server/MySQL）
//   - 行注释：#  直至换行（MySQL）
//   - 块注释：/* ... */  可以跨行，可以内联
//   - 字符串字面量内的 '--' / '#' / '/*' 不视为注释（防止误剥离 'a--b'）
//   - 单引号字符串内可转义：'a''b' 表示字面量 a'b
//   - 双引号标识符内不视为注释：col"name
//
// 替换了以下两处历史实现：
//   - middleware.go 原 stripSQLComments（只去前导注释）
//   - export/types.go 原 StripSQLComments（按行过滤，无法处理 /* ... */ 块注释和字符串内的 --）
//
// 算法复杂度 O(n)，单次扫描，使用 Builder 避免额外分配。
func StripSQLComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	inSingle, inDouble := false, false
	i := 0
	for i < len(sql) {
		c := sql[i]
		if inSingle {
			b.WriteByte(c)
			if c == '\'' {
				// 检查转义：SQL 标准下 a''b 表示字面 a'b
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
		// 进入字符串
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
		// 块注释
		if c == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			idx := strings.Index(sql[i+2:], "*/")
			if idx == -1 {
				// 未闭合的块注释视为剩余全部是注释
				return strings.TrimSpace(b.String())
			}
			// 块注释 → 替换为空格，避免 'SELECT/**/id' 被解析成 'SELECTid'
			b.WriteByte(' ')
			i += idx + 4
			continue
		}
		// 行注释 -- / #（注意 # 不是 MySQL 标准但项目兼容）
		if (c == '-' && i+1 < len(sql) && sql[i+1] == '-') || c == '#' {
			idx := strings.Index(sql[i:], "\n")
			if idx == -1 {
				// 行注释直到末尾
				return strings.TrimSpace(b.String())
			}
			i += idx
			continue
		}
		// '--' 中第一个 '-' 后跟着的字符：若不是 '-' 仍要写回
		b.WriteByte(c)
		i++
	}
	return strings.TrimSpace(b.String())
}
