package sqlguard

import "strings"

// 本文件解决缺陷 D6：
// 执行 SQL 时若需追加行数上限，原实现用字符串字面量判断结果集是否已带 LIMIT
// （exec.go 的 checkContains 只匹配 " limit " / "\nlimit\n"），
// 对 `SELECT ...\nLIMIT 10`、`SELECT ... LIMIT 10;` 等常见写法全部漏判，
// 于是又追加一次 `limit ?`，变成 `... LIMIT 10 limit ?` → 语法错误。
// 这里改为词法级判断：跳过字符串、引号标识符与注释，只认「顶层」的 LIMIT/FETCH/TOP。

// HasTopLevelLimit 判断 SQL 最外层是否已经带有行数限制。
// 已有限制时不得再追加，否则会生成双重 LIMIT 导致语法错误。
// 覆盖 LIMIT n、FETCH FIRST/NEXT n ROWS、SELECT TOP n。
//
// 注意：子查询内的 LIMIT 不算（它不限制外层结果集），此时返回 false，
// 调用方仍会追加外层上限——这与「宁可多限制一次，也不要漏判」的取舍一致。
func HasTopLevelLimit(sql string) bool {
	n := len(sql)
	if n == 0 {
		return false
	}

	depth := 0
	words := make([]string, 0, 16)

	var word strings.Builder
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		if depth == 0 {
			words = append(words, strings.ToUpper(word.String()))
		}
		word.Reset()
	}

	for i := 0; i < n; i++ {
		c := sql[i]
		switch {
		// 字符串与引号标识符：整体跳过，内部关键字不参与判断
		case c == '\'' || c == '"' || c == '`':
			flushWord()
			quote := c
			i++
			for i < n {
				if sql[i] == '\\' && quote != '`' {
					i += 2
					continue
				}
				if sql[i] == quote {
					if i+1 < n && sql[i+1] == quote { // '' / "" / `` 转义
						i += 2
						continue
					}
					break
				}
				i++
			}
		// 块注释
		case c == '/' && i+1 < n && sql[i+1] == '*':
			flushWord()
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = n
			} else {
				i += end + 3
			}
		// 行注释：-- 与 #
		case c == '-' && i+1 < n && sql[i+1] == '-':
			flushWord()
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				i = n
			} else {
				i += end
			}
		case c == '#':
			flushWord()
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				i = n
			} else {
				i += end
			}
		case c == '(':
			flushWord()
			depth++
		case c == ')':
			flushWord()
			if depth > 0 {
				depth--
			}
		case isWordByte(c) || c >= 0x80:
			word.WriteByte(c)
		default:
			flushWord()
		}
	}
	flushWord()

	for idx, w := range words {
		switch w {
		case "LIMIT":
			return true
		case "FETCH":
			// FETCH FIRST n ROWS ONLY / FETCH NEXT n ROWS ONLY
			if idx+1 < len(words) && (words[idx+1] == "FIRST" || words[idx+1] == "NEXT") {
				return true
			}
		case "TOP":
			// SELECT TOP n ...（MSSQL）；排除 UPDATE ... TOP / 列名为 top 的情况
			if idx == 1 && words[0] == "SELECT" {
				return true
			}
		}
	}
	return false
}
