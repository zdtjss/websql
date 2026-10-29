package fileparse

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

func init() {
	Register("go-text", 0, nil, []string{".txt", ".md", ".markdown"}, ParserFunc(parseText))
}

// parseText 解析纯文本类文件（txt/md/markdown）。
//
// 与既有行为保持一致：Markdown 返回全文（仅受 MaxTextChars 硬上限约束），
// 新增的 txt 走同一实现并叠加编码探测（GBK/UTF-16 常见于 Windows 导出）。
func parseText(_ context.Context, src Source) (*Result, error) {
	raw, err := os.ReadFile(src.Path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败，请重试")
	}

	text, enc := decodeText(raw)
	total := utf8.RuneCountInString(text)

	var notice string
	if total > MaxTextChars {
		text = string([]rune(text)[:MaxTextChars])
		notice = appendNotice(notice, fmt.Sprintf("文档过长，仅保留前 %d 字符（原文共 %d 字符）", MaxTextChars, total))
	}
	if enc != "utf-8" && enc != "utf-8-bom" {
		notice = appendNotice(notice, fmt.Sprintf("文件检测为 %s 编码，已自动转换为 UTF-8", enc))
	}

	var sections []Section
	if src.Ext == ".md" || src.Ext == ".markdown" {
		sections = extractMarkdownSections(text)
	}
	return &Result{
		Kind:       KindText,
		Text:       text,
		TotalChars: total,
		Notice:     notice,
		Sections:   sections,
	}, nil
}

// extractMarkdownSections 按 ATX 标题（# ~ ######）提取章节与字符偏移。
// 偏移使用 rune 计数，与 read_file_data 的字符窗口语义一致。
func extractMarkdownSections(text string) []Section {
	lines := strings.SplitAfter(text, "\n")
	type rawSection struct {
		title string
		level int
		start int
	}
	raw := make([]rawSection, 0, 8)
	offset := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			if level > 0 && level <= 6 && level < len(trimmed) && (trimmed[level] == ' ' || trimmed[level] == '\t') {
				raw = append(raw, rawSection{title: strings.TrimSpace(trimmed[level:]), level: level, start: offset})
			}
		}
		offset += utf8.RuneCountInString(line)
	}
	if len(raw) == 0 {
		return nil
	}
	sections := make([]Section, 0, len(raw))
	for i, r := range raw {
		end := offset
		if i+1 < len(raw) {
			end = raw[i+1].start
		}
		sections = append(sections, Section{Title: r.title, Level: r.level, StartChar: r.start, EndChar: end})
	}
	return sections
}

// appendNotice 追加说明文案（分号分隔）
func appendNotice(existing, add string) string {
	if add == "" {
		return existing
	}
	if existing == "" {
		return add
	}
	return existing + "；" + add
}
