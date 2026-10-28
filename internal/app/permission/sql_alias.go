package permission

import (
	"strings"
	"unicode"
)

// 方言别名解析（保守实现：无法确定来源时不猜测）。
//
// 目标：把 SELECT 中的列（含别名、表达式、JOIN）解析回真实来源表，
// 使 `SELECT b.id FROM a JOIN b ON ...` 不能借用 a.id 的授权。
// 解析失败（派生表、未知别名、多表裸列）时返回来源不明，由调用方 fail-closed。

// TableRefInfo FROM/JOIN 子句中的表引用。
type TableRefInfo struct {
	Schema string
	Table  string
	Alias  string
}

// sqlClauseStopWords 不能作为隐式别名的关键字。
var sqlClauseStopWords = map[string]bool{
	"ON": true, "USING": true, "WHERE": true, "GROUP": true, "ORDER": true, "HAVING": true,
	"LIMIT": true, "OFFSET": true, "UNION": true, "INTERSECT": true, "EXCEPT": true,
	"LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true, "CROSS": true, "FULL": true,
	"JOIN": true, "NATURAL": true, "STRAIGHT_JOIN": true, "FOR": true, "LOCK": true,
	"SET": true, "VALUES": true, "RETURNING": true, "WINDOW": true, "QUALIFY": true,
	"CONNECT": true, "START": true, "FETCH": true, "PROCEDURE": true,
}

// ExtractTableAliases 解析 FROM/JOIN 子句中的「别名 / 表名 → 真实表名」映射。
//
// 映射键统一大写；对于 schema.table 形式，表名本身也会作为键登记
// （自连接场景下别名优先）。无法解析的别名不会登记，由调用方按来源不明处理。
func ExtractTableAliases(sql string) map[string]string {
	out := make(map[string]string)
	for _, ref := range ExtractTableRefs(sql) {
		if ref.Alias != "" && ref.Table != "" {
			out[strings.ToUpper(ref.Alias)] = ref.Table
		}
		if ref.Table != "" {
			out[strings.ToUpper(ref.Table)] = ref.Table
			if ref.Schema != "" {
				out[strings.ToUpper(ref.Schema+"."+ref.Table)] = ref.Table
			}
		}
	}
	return out
}

// ExtractTableRefs 解析 FROM/JOIN 中的表引用（含逗号连接与派生表）。
func ExtractTableRefs(sql string) []TableRefInfo {
	masked := maskLiteralsAndComments(sql)
	upper := strings.ToUpper(masked)
	var refs []TableRefInfo

	for i := 0; i < len(upper); i++ {
		after := keywordEnd(upper, i, "FROM")
		if after < 0 {
			after = keywordEnd(upper, i, "JOIN")
		}
		if after < 0 {
			continue
		}
		pos := after
		for {
			ref, next, ok := parseTableRef(masked, pos)
			if !ok {
				break
			}
			if ref.Table != "" && !strings.EqualFold(ref.Table, "DUAL") &&
				!strings.EqualFold(ref.Table, "SYSDUMMY1") {
				refs = append(refs, ref)
			}
			pos = next
			k := skipSpaces(masked, pos)
			if k < len(masked) && masked[k] == ',' {
				pos = k + 1
				continue
			}
			break
		}
		i = pos - 1
	}
	return refs
}

// keywordEnd 若 kw 从 i 开始且前后为词边界，返回关键字结束位置；否则 -1。
func keywordEnd(upper string, i int, kw string) int {
	if i+len(kw) > len(upper) || upper[i:i+len(kw)] != kw {
		return -1
	}
	if i > 0 && isIdentChar(upper[i-1]) {
		return -1
	}
	if i+len(kw) < len(upper) && isIdentChar(upper[i+len(kw)]) {
		return -1
	}
	return i + len(kw)
}

func skipSpaces(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// parseTableRef 从 pos 开始解析一个表引用，返回到下一个待扫描位置。
func parseTableRef(s string, pos int) (TableRefInfo, int, bool) {
	pos = skipSpaces(s, pos)
	if pos >= len(s) {
		return TableRefInfo{}, pos, false
	}
	// 派生表：跳过括号并登记其别名（列来源无法映射，交由调用方判断）
	if s[pos] == '(' {
		next, ok := skipBalancedParens(s, pos)
		if !ok {
			return TableRefInfo{}, next, false
		}
		alias, after := parseAlias(s, next)
		return TableRefInfo{Alias: alias}, after, true
	}

	ident, next := readIdentifierToken(s, pos)
	if ident == "" {
		return TableRefInfo{}, pos, false
	}
	parts := splitDottedIdentifier(ident)
	ref := TableRefInfo{}
	switch len(parts) {
	case 1:
		ref.Table = parts[0]
	default:
		ref.Schema = parts[len(parts)-2]
		ref.Table = parts[len(parts)-1]
	}
	alias, after := parseAlias(s, next)
	ref.Alias = alias
	return ref, after, true
}

func skipBalancedParens(s string, pos int) (int, bool) {
	depth := 0
	for i := pos; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return len(s), false
}

// parseAlias 解析表引用后的别名（支持 AS、隐式别名与引号标识符）。
func parseAlias(s string, pos int) (string, int) {
	p := skipSpaces(s, pos)
	if p >= len(s) {
		return "", p
	}
	if p+2 <= len(s) && strings.EqualFold(s[p:p+2], "AS") &&
		(p+2 >= len(s) || !isIdentChar(s[p+2])) {
		p = skipSpaces(s, p+2)
	}
	ident, next := readIdentifierToken(s, p)
	if ident == "" {
		return "", p
	}
	if sqlClauseStopWords[strings.ToUpper(ident)] {
		return "", p
	}
	return ident, next
}

// readIdentifierToken 读取一个（可能带引号、可能带点号）的标识符。
func readIdentifierToken(s string, pos int) (string, int) {
	var sb strings.Builder
	i := pos
	for i < len(s) {
		c := s[i]
		switch {
		case c == '`':
			end := strings.IndexByte(s[i+1:], '`')
			if end < 0 {
				return "", pos
			}
			sb.WriteString(s[i+1 : i+1+end])
			i = i + 1 + end + 1
			continue
		case c == '"':
			end := strings.IndexByte(s[i+1:], '"')
			if end < 0 {
				return "", pos
			}
			sb.WriteString(s[i+1 : i+1+end])
			i = i + 1 + end + 1
			continue
		case c == '[':
			end := strings.IndexByte(s[i+1:], ']')
			if end < 0 {
				return "", pos
			}
			sb.WriteString(s[i+1 : i+1+end])
			i = i + 1 + end + 1
			continue
		case isIdentCharByte(c) || c == '.' || c == '$' || c == '#' || c == '@':
			sb.WriteByte(c)
			i++
			continue
		}
		break
	}
	if sb.Len() == 0 {
		return "", pos
	}
	return sb.String(), i
}

func isIdentCharByte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
}

// maskLiteralsAndComments 把字符串字面量与注释替换为空格（保持长度与下标对齐），
// 同时保留引号标识符（"x" / `x` / [x]），供别名解析使用。
func maskLiteralsAndComments(sql string) string {
	b := []byte(sql)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '\'':
			j := i + 1
			for j < len(b) {
				if b[j] == '\'' {
					if j+1 < len(b) && b[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			for k := i; k < j && k < len(b); k++ {
				b[k] = ' '
			}
			i = j - 1
		case b[i] == '-' && i+1 < len(b) && b[i+1] == '-':
			j := i
			for j < len(b) && b[j] != '\n' {
				b[j] = ' '
				j++
			}
			i = j - 1
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			j := i
			for j < len(b) {
				if b[j] == '*' && j+1 < len(b) && b[j+1] == '/' {
					j += 2
					break
				}
				b[j] = ' '
				j++
			}
			for k := i; k < j && k < len(b); k++ {
				b[k] = ' '
			}
			i = j - 1
		}
	}
	return string(b)
}

// ResolveSelectColumns 在 ExtractSelectColumns 基础上解析真实来源表。
//
// complete=false 表示存在无法确定来源的列（多表裸列、未知别名、派生表）。
// 调用方只在涉及列级受限表时才需要依赖 complete。
func ResolveSelectColumns(sql string) (cols []SelectColumn, complete bool) {
	cols = ExtractSelectColumns(sql)
	aliases := ExtractTableAliases(sql)
	refs := ExtractTableRefs(sql)

	tableSet := make(map[string]bool)
	tableSchema := make(map[string]string)
	for _, r := range refs {
		if r.Table != "" {
			tableSet[r.Table] = true
			if r.Schema != "" {
				if existing, ok := tableSchema[r.Table]; ok && existing != r.Schema {
					delete(tableSchema, r.Table) // 同名表跨 Schema，无法唯一确定
				} else {
					tableSchema[r.Table] = r.Schema
				}
			}
		}
	}
	singleTable := ""
	if len(tableSet) == 1 {
		for t := range tableSet {
			singleTable = t
		}
	}

	complete = true
	for i := range cols {
		c := &cols[i]
		if c.IsStar {
			if c.TableAlias != "" {
				if t, ok := aliases[strings.ToUpper(c.TableAlias)]; ok {
					c.TableName = t
					c.TableSchema = tableSchema[t]
				} else {
					complete = false
				}
			}
			continue
		}
		if c.TableAlias != "" {
			if t, ok := aliases[strings.ToUpper(c.TableAlias)]; ok {
				c.TableName = t
				c.TableSchema = tableSchema[t]
				continue
			}
			complete = false
			continue
		}
		// 裸列：仅单表场景可确定来源
		if singleTable != "" {
			c.TableName = singleTable
			c.TableSchema = tableSchema[singleTable]
		} else {
			complete = false
		}
	}
	return cols, complete
}

// noResourceBareKeywords 无参日期/常量函数（不访问任何表）。
var noResourceBareKeywords = map[string]bool{
	"CURRENT_DATE":      true,
	"CURRENT_TIME":      true,
	"CURRENT_TIMESTAMP": true,
	"SYSDATE":           true,
	"SYSTIMESTAMP":      true,
	"LOCALTIME":         true,
	"LOCALTIMESTAMP":    true,
	"NOW":               true,
	"PI":                true,
	"RAND":              true,
}

// isConstantExpression 判断是否为纯常量表达式（算术/拼接，无标识符引用）。
//
// 先剥离字符串字面量，剩余部分不允许出现字母或标识符字符。
func isConstantExpression(expr string) bool {
	var b strings.Builder
	inString := false
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if c == '\'' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		b.WriteByte(c)
	}
	for _, r := range b.String() {
		if unicode.IsLetter(r) || r == '_' || r == '$' || r == '#' || r == '@' {
			return false
		}
	}
	return strings.TrimSpace(b.String()) != ""
}

// IsVerifiedNoResourceQuery 判断是否为「已证明不访问任何数据资源」的查询。
//
// 只接受 SELECT + 常量表达式列表（可带 FROM DUAL / SYSIBM.SYSDUMMY1），
// 不允许子查询、CTE、标识符引用与函数参数中的列引用。
// 该白名单用于「提取不到表名」的受限用户路径：不能用「没提取到表名」代替。
func IsVerifiedNoResourceQuery(sql string) bool {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sql), ";"))
	if body == "" {
		return false
	}
	upper := strings.ToUpper(body)
	if !strings.HasPrefix(upper, "SELECT") {
		return false
	}
	// 出现第二个 SELECT 说明含子查询；CTE 一律不认定为无资源查询
	if strings.Contains(upper[6:], "SELECT") || strings.Contains(upper, "WITH") {
		return false
	}
	if idx := findTopLevelKeyword(upper, "FROM"); idx >= 0 {
		tail := strings.TrimSpace(upper[idx+4:])
		tail = strings.TrimSpace(strings.TrimSuffix(tail, ";"))
		switch tail {
		case "DUAL", "SYSIBM.SYSDUMMY1":
		default:
			return false
		}
	}
	sel := strings.TrimSpace(extractSelectBody(body))
	if sel == "" {
		return false
	}
	for _, part := range splitByCommaRespectParens(sel) {
		expr := strings.TrimSpace(removeAlias(part))
		if expr == "" {
			continue
		}
		if isConstant(expr) || isConstantExpression(expr) || noResourceBareKeywords[strings.ToUpper(expr)] {
			continue
		}
		if isFunctionCall(expr) {
			start := strings.Index(expr, "(")
			end := strings.LastIndex(expr, ")")
			if start < 0 || end <= start {
				return false
			}
			args := strings.TrimSpace(expr[start+1 : end])
			if args == "" || args == "*" {
				continue
			}
			for _, arg := range splitByCommaRespectParens(args) {
				arg = strings.TrimSpace(arg)
				if arg == "" || isConstant(arg) {
					continue
				}
				return false
			}
			continue
		}
		return false
	}
	return true
}
