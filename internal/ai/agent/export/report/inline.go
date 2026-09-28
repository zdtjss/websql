package report

import (
	"strings"
	"unicode/utf8"
)

// Span 行内片段
type Span struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
	Strike bool
	Link   string // 仅允许 http/https/mailto 与站内 /exports/ 链接
}

// ParseInline 解析行内 Markdown：**粗体** *斜体* `代码` ~~删除线~~ [文本](链接)，支持反斜杠转义。
// 渲染器只接收结构化片段，从而统一转义，杜绝原始 HTML/XML 注入。
func ParseInline(text string) []Span {
	var out []Span
	parseInlineInto(text, Span{}, &out)
	return mergeSpans(out)
}

// PlainText 去除行内标记后的纯文本
func PlainText(text string) string {
	var sb strings.Builder
	for _, s := range ParseInline(text) {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

func parseInlineInto(text string, style Span, out *[]Span) {
	var buf strings.Builder
	emit := func() {
		if buf.Len() > 0 {
			s := style
			s.Text = buf.String()
			*out = append(*out, s)
			buf.Reset()
		}
	}
	i := 0
	for i < len(text) {
		c := text[i]
		switch {
		case c == '\\' && i+1 < len(text) && strings.IndexByte("\\`*_{}[]()#+-.!~|>", text[i+1]) >= 0:
			buf.WriteByte(text[i+1])
			i += 2
			continue
		case c == '`':
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				emit()
				s := style
				s.Code = true
				s.Text = text[i+1 : i+1+end]
				*out = append(*out, s)
				i += end + 2
				continue
			}
		case c == '*' && strings.HasPrefix(text[i:], "**"):
			if end := findClosing(text, i+2, "**"); end > i+2 {
				emit()
				s := style
				s.Bold = true
				parseInlineInto(text[i+2:end], s, out)
				i = end + 2
				continue
			}
		case c == '~' && strings.HasPrefix(text[i:], "~~"):
			if end := findClosing(text, i+2, "~~"); end > i+2 {
				emit()
				s := style
				s.Strike = true
				parseInlineInto(text[i+2:end], s, out)
				i = end + 2
				continue
			}
		case c == '*':
			// 斜体：*后不能是空白，闭合 * 前不能是空白
			if i+1 < len(text) && text[i+1] != ' ' {
				if end := findClosing(text, i+1, "*"); end > i+1 && text[end-1] != ' ' {
					emit()
					s := style
					s.Italic = true
					parseInlineInto(text[i+1:end], s, out)
					i = end + 1
					continue
				}
			}
		case c == '[':
			if label, url, n, ok := parseLink(text[i:]); ok {
				emit()
				s := style
				if safe := SafeURL(url); safe != "" {
					s.Link = safe
				}
				parseInlineInto(label, s, out)
				i += n
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		buf.WriteString(text[i : i+size])
		i += size
	}
	emit()
}

// findClosing 从 start 开始查找未转义的闭合标记
func findClosing(text string, start int, marker string) int {
	for i := start; i+len(marker) <= len(text); i++ {
		if text[i] == '\\' {
			i++
			continue
		}
		if text[i] == '`' {
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				i += end + 1
				continue
			}
		}
		if strings.HasPrefix(text[i:], marker) {
			// 单个 * 不能匹配 ** 的一部分
			if marker == "*" && strings.HasPrefix(text[i:], "**") {
				i++
				continue
			}
			return i
		}
	}
	return -1
}

// parseLink 解析 [label](url)，返回消耗的字节数
func parseLink(s string) (label, url string, n int, ok bool) {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				if i+1 < len(s) && s[i+1] == '(' {
					end := strings.IndexByte(s[i+2:], ')')
					if end < 0 {
						return "", "", 0, false
					}
					url = strings.TrimSpace(s[i+2 : i+2+end])
					if sp := strings.IndexAny(url, " \t"); sp > 0 { // 去掉 "title"
						url = url[:sp]
					}
					return s[1:i], url, i + 3 + end, true
				}
				return "", "", 0, false
			}
		}
	}
	return "", "", 0, false
}

// SafeURL 仅放行 http/https/mailto 与站内导出链接；javascript:/data: 等一律丢弃
func SafeURL(u string) string {
	u = strings.TrimSpace(u)
	lower := strings.ToLower(u)
	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "mailto:"):
		return u
	case strings.HasPrefix(u, "/exports/"), strings.HasPrefix(u, "#"):
		return u
	}
	return ""
}

func mergeSpans(in []Span) []Span {
	out := make([]Span, 0, len(in))
	for _, s := range in {
		if s.Text == "" {
			continue
		}
		if n := len(out); n > 0 {
			p := out[n-1]
			if p.Bold == s.Bold && p.Italic == s.Italic && p.Code == s.Code && p.Strike == s.Strike && p.Link == s.Link {
				out[n-1].Text += s.Text
				continue
			}
		}
		out = append(out, s)
	}
	return out
}
