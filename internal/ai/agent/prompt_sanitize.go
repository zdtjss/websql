// prompt_sanitize.go — 进入系统提示词的客户端可控字段清洗
//
// 旧版 ExcelData.Columns、上传文件名等字段由客户端提供，直接拼接会形成提示词注入面。
// 这里统一做“控制字符剥离 + 长度限制 + 去重 + 数量上限”，只保留展示需要的安全文本；
// 真实列名始终应由 read_file_data 从服务端解析结果读取。
package agent

import (
	"strings"
	"unicode"
)

const (
	// promptIdentifierMaxRunes 单个列名/文件名的最大字符数。
	promptIdentifierMaxRunes = 128
	// promptMaxColumns 注入提示词的列名数量上限。
	promptMaxColumns = 64
)

// sanitizePromptColumns 清洗客户端传入的列名列表：
// 丢弃空值、剥离换行/控制字符/零宽字符/引号分号，去重并限制数量。
func sanitizePromptColumns(cols []string) []string {
	if len(cols) == 0 {
		return nil
	}
	out := make([]string, 0, len(cols))
	seen := make(map[string]bool, len(cols))
	for _, col := range cols {
		cleaned := sanitizePromptLabel(col, "")
		if cleaned == "" || seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
		if len(out) >= promptMaxColumns {
			break
		}
	}
	return out
}

// sanitizePromptLabel 清洗单个人类可读标识（列名、文件名）。
// 空结果返回 fallback；超长截断到 promptIdentifierMaxRunes。
func sanitizePromptLabel(raw, fallback string) string {
	cleaned := stripPromptUnsafe(raw)
	if cleaned == "" {
		return fallback
	}
	runes := []rune(cleaned)
	if len(runes) > promptIdentifierMaxRunes {
		cleaned = string(runes[:promptIdentifierMaxRunes]) + "…"
	}
	return cleaned
}

// stripPromptUnsafe 移除可能破坏提示词结构的字符：
//   - 所有控制字符（含 \r\n\t）与零宽/双向控制字符 → 空格
//   - 引号、反引号、分号 → 空格（这些字符无法在展示字段中产生合法语义，只会增加注入面）
//
// 最后合并连续空白并 trim，合法列名（字母/数字/下划线/中文/空格/点/括号）保持不变。
func stripPromptUnsafe(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) || isPromptInvisibleRune(r) || strings.ContainsRune("\"'`;", r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// isPromptInvisibleRune 判断零宽空格、双向控制符、BOM 等不可见字符。
func isPromptInvisibleRune(r rune) bool {
	switch {
	case r == '\u200B', r == '\u200C', r == '\u200D', r == '\u200E', r == '\u200F':
		return true
	case r >= '\u202A' && r <= '\u202E':
		return true
	case r >= '\u2060' && r <= '\u206F':
		return true
	case r == '\uFEFF', r == '\uFE0F':
		return true
	default:
		return false
	}
}

// sanitizeLegacyFileType 把旧版 ExcelData.FileType 约束到已知枚举，
// 未知值返回空串（由提示词按通用“数据文件”处理）。
func sanitizeLegacyFileType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "excel":
		return "excel"
	case "csv":
		return "csv"
	case "markdown":
		return "markdown"
	default:
		return ""
	}
}
