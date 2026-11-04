package audit

import (
	"strings"

	maskpkg "websql/internal/pkg/mask"
)

// maskAuditText 审计文本的统一脱敏入口。
//
// 顺序：先 MaskSQL（单引号字面量 → ***），再对内层洞与普通文本执行文本级脱敏
// （密钥赋值、身份证、手机号、邮箱）。两步均幂等，Record 与落库边界反复调用安全。
func maskAuditText(text string) string {
	if text == "" {
		return text
	}
	return maskpkg.MaskText(MaskSQL(text))
}

// maskSQLMaxLen 审计日志中保留的 SQL 最大字符数（按 rune 计），超出部分截断。
const maskSQLMaxLen = 4000

// maskSQLTruncatedSuffix 超长 SQL 截断后追加的标记。
const maskSQLTruncatedSuffix = "...(truncated)"

// MaskSQL 对准备落库的 SQL 文本做脱敏，用于隐藏字面量中的敏感数据
// （手机号 / 身份证 / 人名等）：
//
//  1. 单引号字符串字面量的内容替换为 ***（保留引号本身），
//     正确处理 ” 转义（如 'O”Brien' 视为一个字面量）；
//  2. 识别 -- / # 行注释与 /* */ 块注释边界：注释区域原样保留，
//     其内部的引号不会被当作字面量起点，注释外的 SQL 结构不受影响；
//  3. 超过 maskSQLMaxLen 个字符时截断并追加 "...(truncated)"。
//
// 纯函数：无副作用、不依赖外部状态，可安全地在多次调用（幂等）与并发场景使用。
func MaskSQL(sql string) string {
	if sql == "" {
		return sql
	}
	return truncateMasked(rewriteStringLiterals(sql, func(string, string) string { return "***" }))
}

// PreviewSQL 为审批面板生成「结构可见、值部分隐藏」的 SQL 预览。
//
// 与 MaskSQL（审计落库全遮）不同：
//   - 字段名、表名、运算符与 WHERE 结构原样保留；
//   - 普通字面量长度 ≤ 8 时完整展示，> 8 时保留首尾各 2 字符（ab***yz）；
//   - 敏感列（password/pwd/token/secret/key 等）的值仍完全替换为 ***。
//
// 原始语句仍只保存在服务端 checkpoint，本函数只用于前端确认面板展示。
func PreviewSQL(sql string) string {
	if sql == "" {
		return sql
	}
	return truncateMasked(rewriteStringLiterals(sql, previewLiteral))
}

// previewLiteral 按列名敏感度与值长度生成展示副本。
func previewLiteral(value, column string) string {
	if value == "" {
		return value
	}
	if isSensitiveColumn(column) {
		return "***"
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		return value
	}
	return string(runes[:2]) + "***" + string(runes[len(runes)-2:])
}

// isSensitiveColumn 判断列名是否属于凭据类字段，这类值即使很短也必须全遮。
func isSensitiveColumn(column string) bool {
	col := strings.ToLower(strings.Trim(strings.TrimSpace(column), "`\"[]"))
	if col == "" {
		return false
	}
	for _, keyword := range []string{"password", "passwd", "pwd", "token", "secret", "key", "credential"} {
		if strings.Contains(col, keyword) {
			return true
		}
	}
	return false
}

// rewriteStringLiterals 将所有注释外的单引号字面量交给 replace 生成展示副本。
// replace 的第二个参数是字面量左侧最近的列名（无法识别时为空串）。
func rewriteStringLiterals(sql string, replace func(value, column string) string) string {
	runes := []rune(sql)
	n := len(runes)
	var sb strings.Builder
	sb.Grow(len(sql))

	for i := 0; i < n; {
		switch {
		case runes[i] == '\'':
			// 字符串字面量：保留引号本身，内容交给 replace 处理
			literalStart := i
			sb.WriteRune('\'')
			i++
			var value strings.Builder
			for i < n {
				if runes[i] == '\'' {
					if i+1 < n && runes[i+1] == '\'' {
						value.WriteRune('\'')
						i += 2 // '' 转义，仍在字面量内部
						continue
					}
					i++ // 字面量闭合
					break
				}
				value.WriteRune(runes[i])
				i++
			}
			sb.WriteString(replace(value.String(), precedingColumn(runes, literalStart)))
			sb.WriteRune('\'')
		case runes[i] == '-' && i+1 < n && runes[i+1] == '-':
			// 行注释：原样保留至行尾
			sb.WriteString("--")
			i += 2
			for i < n && runes[i] != '\n' {
				sb.WriteRune(runes[i])
				i++
			}
		case runes[i] == '#':
			// MySQL 风格行注释：原样保留至行尾
			for i < n && runes[i] != '\n' {
				sb.WriteRune(runes[i])
				i++
			}
		case runes[i] == '/' && i+1 < n && runes[i+1] == '*':
			// 块注释：原样保留（含引号），直到 */ 结束
			sb.WriteString("/*")
			i += 2
			for i < n {
				if runes[i] == '*' && i+1 < n && runes[i+1] == '/' {
					sb.WriteString("*/")
					i += 2
					break
				}
				sb.WriteRune(runes[i])
				i++
			}
		default:
			sb.WriteRune(runes[i])
			i++
		}
	}

	return sb.String()
}

// precedingColumn 从字面量位置向前回溯，尝试识别所属列名。
// 识别失败返回空串（此时按普通值处理）。
func precedingColumn(runes []rune, literalStart int) string {
	i := literalStart - 1
	// 最多回溯 3 个「运算符词」：IN / LIKE / BETWEEN / NOT IN 等
	for attempt := 0; attempt < 3 && i >= 0; attempt++ {
		i = skipSpacesBackward(runes, i)
		// 跳过 = < > ! + - * / % | & ^ ~ 等运算符
		for i >= 0 && strings.ContainsRune("=<>!+-*/%|&^~", runes[i]) {
			i--
		}
		i = skipSpacesBackward(runes, i)
		word, next := readWordBackward(runes, i)
		if word == "" {
			return ""
		}
		if !isOperatorWord(word) {
			return word
		}
		i = next - 1
	}
	return ""
}

func skipSpacesBackward(runes []rune, i int) int {
	for i >= 0 && (runes[i] == ' ' || runes[i] == '\t' || runes[i] == '\n' || runes[i] == '\r') {
		i--
	}
	return i
}

func readWordBackward(runes []rune, i int) (string, int) {
	end := i
	for i >= 0 && isWordRune(runes[i]) {
		i--
	}
	if end < 0 || i == end {
		return "", end
	}
	word := strings.Trim(string(runes[i+1:end+1]), "`\"[]")
	// 仅保留最后一段（schema.table.col → col）
	if idx := strings.LastIndex(word, "."); idx >= 0 {
		word = word[idx+1:]
	}
	return word, i + 1
}

func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		r == '_' || r == '$' || r == '.' || r == '`' || r == '"' || r == '[' || r == ']'
}

func isOperatorWord(word string) bool {
	switch strings.ToLower(word) {
	case "like", "in", "between", "is", "not", "and", "or", "then", "else", "when", "case", "values", "set", "where", "having", "on":
		return true
	}
	return false
}

// truncateMasked 按 rune 截断超长 SQL，避免截断出非法 UTF-8 序列。
func truncateMasked(sql string) string {
	if len(sql) <= maskSQLMaxLen {
		return sql
	}
	runes := []rune(sql)
	if len(runes) <= maskSQLMaxLen {
		return sql
	}
	return string(runes[:maskSQLMaxLen]) + maskSQLTruncatedSuffix
}
