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
	tokens := topLevelTokens(sql)
	for idx, t := range tokens {
		switch t.text {
		case "LIMIT":
			return true
		case "FETCH":
			// FETCH FIRST n ROWS ONLY / FETCH NEXT n ROWS ONLY
			if idx+1 < len(tokens) && (tokens[idx+1].text == "FIRST" || tokens[idx+1].text == "NEXT") {
				return true
			}
		case "TOP":
			// SELECT TOP n ...（MSSQL）；排除 UPDATE ... TOP / 列名为 top 的情况
			if idx == 1 && tokens[0].text == "SELECT" {
				return true
			}
		}
	}
	return false
}

// HasTopLevelRowNumLimit 判断最外层是否已有 ROWNUM 上界谓词。
// 只认能限制结果行数的写法（ROWNUM <= n、< n、= n、BETWEEN），
// ROWNUM > 0 / >= 1 之类不会限制行数，不能据此跳过服务端上限；
// 子查询里的 ROWNUM 同样不算。
func HasTopLevelRowNumLimit(sql string) bool {
	for _, t := range topLevelTokens(sql) {
		if t.text != "ROWNUM" || t.end >= len(sql) {
			continue
		}
		rest := strings.TrimLeft(sql[t.end:], " \t\r\n")
		if rest == "" {
			continue
		}
		if strings.HasPrefix(rest, "<") || strings.HasPrefix(rest, "=") {
			return true
		}
		if len(rest) >= 7 && strings.EqualFold(rest[:7], "BETWEEN") {
			return true
		}
	}
	return false
}

// sqlToken 是词法级扫描得到的最外层 token（引号、注释、子查询内的内容不计）。
type sqlToken struct {
	text string // 已转大写
	end  int    // 结束字节位置（不含）
}

// topLevelTokens 提取最外层 token，跳过字符串、引号标识符与注释。
// 与 HasTopLevelLimit 共用同一套词法规则，避免「子查询 LIMIT/ROWNUM」误判。
func topLevelTokens(sql string) []sqlToken {
	n := len(sql)
	if n == 0 {
		return nil
	}

	depth := 0
	tokens := make([]sqlToken, 0, 16)

	var word strings.Builder
	flushWord := func(end int) {
		if word.Len() == 0 {
			return
		}
		if depth == 0 {
			tokens = append(tokens, sqlToken{
				text: strings.ToUpper(word.String()),
				end:  end,
			})
		}
		word.Reset()
	}

	for i := 0; i < n; i++ {
		c := sql[i]
		switch {
		// 字符串与引号标识符：整体跳过，内部关键字不参与判断
		case c == '\'' || c == '"' || c == '`':
			flushWord(i)
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
			flushWord(i)
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = n
			} else {
				i += end + 3
			}
		// 行注释：-- 与 #
		case c == '-' && i+1 < n && sql[i+1] == '-':
			flushWord(i)
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				i = n
			} else {
				i += end
			}
		case c == '#':
			flushWord(i)
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				i = n
			} else {
				i += end
			}
		case c == '(':
			flushWord(i)
			depth++
		case c == ')':
			flushWord(i)
			if depth > 0 {
				depth--
			}
		case isWordByte(c) || c >= 0x80:
			word.WriteByte(c)
		default:
			flushWord(i)
		}
	}
	flushWord(n)

	return tokens
}
