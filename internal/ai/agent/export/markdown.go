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
	reMarkdownBold      = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reMarkdownItalic    = regexp.MustCompile(`\*(.+?)\*`)
	reMarkdownCode      = regexp.MustCompile("`(.+?)`")
	reMarkdownLink      = regexp.MustCompile(`\[(.+?)\]\(.+?\)`)
	reBlockquote        = regexp.MustCompile(`(?m)^>\s?`)
	reHorizontalRule    = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
	reLatexDisplayMath  = regexp.MustCompile(`\$\$(.+?)\$\$`)
	reLatexInlineMath   = regexp.MustCompile(`\$([^\$]+?)\$`)
	reLatexRightArrow   = regexp.MustCompile(`\\rightarrow|\$\$?\\rightarrow\$\$?`)
	reLatexLeftArrow    = regexp.MustCompile(`\\leftarrow|\$\$?\\leftarrow\$\$?`)
	reLatexRightarrow   = regexp.MustCompile(`\\Rightarrow|\$\$?\\Rightarrow\$\$?`)
	reLatexLeftarrow    = regexp.MustCompile(`\\Leftarrow|\$\$?\\Leftarrow\$\$?`)
	reLatexTo           = regexp.MustCompile(`\\to\b`)
	reLatexGt           = regexp.MustCompile(`\\gt\b`)
	reLatexLt           = regexp.MustCompile(`\\lt\b`)
	reLatexGe           = regexp.MustCompile(`\\ge\b|\\geqslant\b|\\geq\b`)
	reLatexLe           = regexp.MustCompile(`\\le\b|\\leqslant\b|\\leq\b`)
	reLatexNe           = regexp.MustCompile(`\\ne\b|\\neq\b`)
	reLatexSum          = regexp.MustCompile(`\\sum\b`)
	reLatexProd         = regexp.MustCompile(`\\prod\b`)
	reLatexAlpha        = regexp.MustCompile(`\\alpha\b`)
	reLatexBeta         = regexp.MustCompile(`\\beta\b`)
	reLatexGamma        = regexp.MustCompile(`\\gamma\b`)
	reLatexDelta        = regexp.MustCompile(`\\delta\b`)
	reLatexEpsilon      = regexp.MustCompile(`\\epsilon\b`)
	reLatexTheta        = regexp.MustCompile(`\\theta\b`)
	reLatexPi           = regexp.MustCompile(`\\pi\b`)
	reLatexPhi          = regexp.MustCompile(`\\phi\b`)
	reLatexOmega        = regexp.MustCompile(`\\omega\b`)
	reLatexLdots        = regexp.MustCompile(`\\ldots\b|\\cdots\b|\\vdots\b|\\ddots\b`)
	reLatexHat          = regexp.MustCompile(`\\hat\{`)
	reLatexBar          = regexp.MustCompile(`\\bar\{`)
	reLatexTilde        = regexp.MustCompile(`\\tilde\{`)
	reLatexFrac         = regexp.MustCompile(`\\frac\{`)
	reLatexMbox         = regexp.MustCompile(`\\mbox\{`)
	reLatexText         = regexp.MustCompile(`\\text\{`)
	reLatexBraces       = regexp.MustCompile(`[\{\}]`)
)

func StripMarkdownFormatting(s string) string {
	s = reLatexRightArrow.ReplaceAllString(s, "→")
	s = reLatexLeftArrow.ReplaceAllString(s, "←")
	s = reLatexRightarrow.ReplaceAllString(s, "⇒")
	s = reLatexLeftarrow.ReplaceAllString(s, "⇐")
	s = reLatexTo.ReplaceAllString(s, "→")
	s = reLatexGt.ReplaceAllString(s, ">")
	s = reLatexLt.ReplaceAllString(s, "<")
	s = reLatexGe.ReplaceAllString(s, "≥")
	s = reLatexLe.ReplaceAllString(s, "≤")
	s = reLatexNe.ReplaceAllString(s, "≠")
	s = reLatexSum.ReplaceAllString(s, "∑")
	s = reLatexProd.ReplaceAllString(s, "∏")
	s = reLatexAlpha.ReplaceAllString(s, "α")
	s = reLatexBeta.ReplaceAllString(s, "β")
	s = reLatexGamma.ReplaceAllString(s, "γ")
	s = reLatexDelta.ReplaceAllString(s, "δ")
	s = reLatexEpsilon.ReplaceAllString(s, "ε")
	s = reLatexTheta.ReplaceAllString(s, "θ")
	s = reLatexPi.ReplaceAllString(s, "π")
	s = reLatexPhi.ReplaceAllString(s, "φ")
	s = reLatexOmega.ReplaceAllString(s, "ω")
	s = reLatexLdots.ReplaceAllString(s, "...")
	s = reLatexFrac.ReplaceAllString(s, "")
	s = reLatexHat.ReplaceAllString(s, "")
	s = reLatexBar.ReplaceAllString(s, "")
	s = reLatexTilde.ReplaceAllString(s, "")
	s = reLatexMbox.ReplaceAllString(s, "")
	s = reLatexText.ReplaceAllString(s, "")
	s = reLatexBraces.ReplaceAllString(s, "")
	s = reLatexDisplayMath.ReplaceAllString(s, "$1")
	s = reLatexInlineMath.ReplaceAllString(s, "$1")
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