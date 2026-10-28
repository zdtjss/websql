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
	return truncateMasked(maskStringLiterals(sql))
}

// maskStringLiterals 将注释外的单引号字符串字面量内容替换为 ***。
func maskStringLiterals(sql string) string {
	runes := []rune(sql)
	n := len(runes)
	var sb strings.Builder
	sb.Grow(len(sql))

	for i := 0; i < n; {
		switch {
		case runes[i] == '\'':
			// 字符串字面量：保留引号本身，内容统一替换为 ***
			sb.WriteRune('\'')
			i++
			for i < n {
				if runes[i] == '\'' {
					if i+1 < n && runes[i+1] == '\'' {
						i += 2 // '' 转义，仍在字面量内部
						continue
					}
					i++ // 字面量闭合
					break
				}
				i++
			}
			sb.WriteString("***")
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
