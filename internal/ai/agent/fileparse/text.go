package fileparse

import (
	"context"
	"fmt"
	"os"
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

	return &Result{
		Kind:       KindText,
		Text:       text,
		TotalChars: total,
		Notice:     notice,
	}, nil
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
