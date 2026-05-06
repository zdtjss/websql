package export

import (
	"regexp"
	"strings"
)

type MdBlock struct {
	Type    string
	Content string
	Lang    string
}

func ParseMarkdownBlocks(content string) []MdBlock {
	var blocks []MdBlock
	lines := strings.Split(content, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimPrefix(trimmed, "```")
			var codeLines []string
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				codeLines = append(codeLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			blockType := "code"
			if strings.TrimSpace(lang) == "mermaid" {
				blockType = "mermaid"
			}
			blocks = append(blocks, MdBlock{
				Type:    blockType,
				Content: strings.Join(codeLines, "\n"),
				Lang:    strings.TrimSpace(lang),
			})
			continue
		}

		if strings.HasPrefix(trimmed, "### ") {
			blocks = append(blocks, MdBlock{Type: "h3", Content: strings.TrimPrefix(trimmed, "### ")})
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			blocks = append(blocks, MdBlock{Type: "h2", Content: strings.TrimPrefix(trimmed, "## ")})
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			blocks = append(blocks, MdBlock{Type: "h1", Content: strings.TrimPrefix(trimmed, "# ")})
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "|") {
			var tableLines []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				tableLines = append(tableLines, lines[i])
				i++
			}
			blocks = append(blocks, MdBlock{Type: "table", Content: strings.Join(tableLines, "\n")})
			continue
		}

		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			var items []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if strings.HasPrefix(t, "- ") {
					items = append(items, strings.TrimPrefix(t, "- "))
					i++
				} else if strings.HasPrefix(t, "* ") {
					items = append(items, strings.TrimPrefix(t, "* "))
					i++
				} else if t == "" {
					i++
					break
				} else {
					break
				}
			}
			blocks = append(blocks, MdBlock{Type: "list", Content: strings.Join(items, "\n")})
			continue
		}

		if matched, _ := regexp.MatchString(`^[-*_]{3,}\s*$`, trimmed); matched {
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "> ") {
			var items []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if strings.HasPrefix(t, "> ") {
					items = append(items, strings.TrimPrefix(t, "> "))
					i++
				} else if t == "" {
					i++
					break
				} else {
					break
				}
			}
			blocks = append(blocks, MdBlock{Type: "paragraph", Content: strings.Join(items, "\n")})
			continue
		}

		if strings.HasPrefix(trimmed, "1. ") || strings.HasPrefix(trimmed, "1) ") {
			var items []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if matched, _ := regexp.MatchString(`^\d+[.)]\s`, t); matched {
					re := regexp.MustCompile(`^\d+[.)]\s*`)
					items = append(items, re.ReplaceAllString(t, ""))
					i++
				} else if t == "" {
					i++
					break
				} else {
					break
				}
			}
			blocks = append(blocks, MdBlock{Type: "list", Content: strings.Join(items, "\n")})
			continue
		}

		var paraLines []string
		for i < len(lines) {
			t := strings.TrimSpace(lines[i])
			if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "```") ||
				strings.HasPrefix(t, "|") || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") ||
				strings.HasPrefix(t, "> ") {
				break
			}
			if matched, _ := regexp.MatchString(`^\d+[.)]\s`, t); matched {
				break
			}
			paraLines = append(paraLines, lines[i])
			i++
		}
		if len(paraLines) > 0 {
			blocks = append(blocks, MdBlock{Type: "paragraph", Content: strings.Join(paraLines, "\n")})
		}
	}
	return blocks
}

var (
	reMarkdownBold    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reMarkdownItalic  = regexp.MustCompile(`\*(.+?)\*`)
	reMarkdownCode    = regexp.MustCompile("`(.+?)`")
	reMarkdownLink    = regexp.MustCompile(`\[(.+?)\]\(.+?\)`)
	reBlockquote      = regexp.MustCompile(`(?m)^>\s?`)
	reHorizontalRule  = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
)

func StripMarkdownFormatting(s string) string {
	s = reMarkdownBold.ReplaceAllString(s, "$1")
	s = reMarkdownItalic.ReplaceAllString(s, "$1")
	s = reMarkdownCode.ReplaceAllString(s, "$1")
	s = reMarkdownLink.ReplaceAllString(s, "$1")
	s = reBlockquote.ReplaceAllString(s, "")
	s = reHorizontalRule.ReplaceAllString(s, "")
	return s
}

func IsTableSeparator(line string) bool {
	trimmed := strings.ReplaceAll(strings.TrimSpace(line), "|", "")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return false
	}
	for _, c := range trimmed {
		if c != '-' && c != ':' && c != ' ' {
			return false
		}
	}
	return true
}

type SlideSection struct {
	Title  string
	Blocks []MdBlock
}
