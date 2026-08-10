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

// ContainsMultipleStatements 检测 SQL 是否包含多条语句（分号分隔）。
//
// 在剥离注释和字符串字面量后，检查是否存在真正的语句分隔符分号。
// 尾部的单个分号不算多语句（如 "SELECT 1;"），但中间出现分号则判定为多语句。
//
// 此函数用于安全校验：防止通过 "SELECT 1; DROP TABLE x" 这类多语句注入绕过
// query_data 的语句类型白名单。
//
// 注意：字符串内的分号（如 WHERE name = 'a;b'）和注释中的分号不会误判。
func ContainsMultipleStatements(sql string) bool {
	// 扫描策略：跳过字符串字面量和注释，只检测裸分号的位置。
	// 找到第一个裸分号后，检查其后（跳过空白）是否还有非空内容。
	inSingle, inDouble := false, false
	i := 0
	for i < len(sql) {
		c := sql[i]
		if inSingle {
			if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i += 2 // 转义 ''
					continue
				}
				inSingle = false
			}
			i++
			continue
		}
		if inDouble {
			if c == '"' {
				inDouble = false
			}
			i++
			continue
		}
		// 进入字符串
		if c == '\'' {
			inSingle = true
			i++
			continue
		}
		if c == '"' {
			inDouble = true
			i++
			continue
		}
		// 块注释跳过
		if c == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			idx := strings.Index(sql[i+2:], "*/")
			if idx == -1 {
				return false // 未闭合块注释 → 无后续内容
			}
			i += idx + 4
			continue
		}
		// 行注释跳过
		if (c == '-' && i+1 < len(sql) && sql[i+1] == '-') || c == '#' {
			idx := strings.Index(sql[i:], "\n")
			if idx == -1 {
				return false // 行注释直到结尾 → 无后续内容
			}
			i += idx + 1
			continue
		}
		// 发现裸分号
		if c == ';' {
			// 检查分号后面是否还有有效 SQL 内容（忽略空白、注释）
			rest := strings.TrimSpace(sql[i+1:])
			if rest == "" || rest == ";" {
				return false // 仅尾部分号或连续分号后无内容
			}
			// 递归去注释检查剩余内容是否为空
			stripped := StripSQLComments(rest)
			trimmed := strings.TrimRight(strings.TrimSpace(stripped), ";")
			trimmed = strings.TrimSpace(trimmed)
			return trimmed != ""
		}
		i++
	}
	return false
}
