package fileparse

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func init() {
	// 内置 Go 解析器：优先级 0（无 Python 环境时的兜底实现）。
	// Python 增强实现（python-docx）在 python.go 中以更高优先级注册，
	// 可用时自动覆盖本实现。
	Register("go-docx", 0, nil, []string{".docx"}, ParserFunc(parseDOCX))
}

// parseDOCX 使用标准库解析 docx 正文（zip + word/document.xml）。
//
// 这是「降级实现」：能可靠提取段落与表格中的纯文本，但不保留样式、
// 图片、批注等结构；Python 环境可用时优先走 python-docx（见 python.go）。
func parseDOCX(_ context.Context, src Source) (*Result, error) {
	zr, err := zip.OpenReader(src.Path)
	if err != nil {
		return nil, fmt.Errorf("docx 文件损坏或不是有效的 OOXML 文档")
	}
	defer zr.Close()

	if len(zr.File) > MaxZipEntries {
		return nil, fmt.Errorf("文件结构异常（内部条目过多），已拒绝解析")
	}

	var (
		docFile      *zip.File
		uncompressed int64
	)
	for _, f := range zr.File {
		uncompressed += int64(f.UncompressedSize64)
		if uncompressed > MaxDecompressBytes {
			return nil, fmt.Errorf("文件解压后体积过大，已拒绝解析")
		}
		if f.Name == "word/document.xml" {
			docFile = f
		}
	}
	if docFile == nil {
		return nil, fmt.Errorf("docx 缺少正文内容（未找到 word/document.xml）")
	}

	rc, err := docFile.Open()
	if err != nil {
		return nil, fmt.Errorf("读取文档正文失败，请重试")
	}
	defer rc.Close()

	text, sections, extractErr := extractDocxText(rc)
	text = normalizeDocxText(text)
	sections = relocateSections(text, sections)
	truncated := false
	total := utf8.RuneCountInString(text)
	if total > MaxTextChars {
		text = string([]rune(text)[:MaxTextChars])
		truncated = true
	}

	var notice string
	if extractErr != nil && text == "" {
		return nil, fmt.Errorf("解析 docx 正文失败，文件可能已损坏")
	}
	if extractErr != nil {
		notice = appendNotice(notice, "文档部分内容解析失败，已忽略异常节点")
	}
	if truncated {
		notice = appendNotice(notice, fmt.Sprintf("文档过长，仅保留前 %d 字符（原文共 %d 字符）", MaxTextChars, total))
	}
	notice = appendNotice(notice, "由内置解析器提取纯文本（当前环境未启用 Python 增强解析），不包含样式/图片信息")

	return &Result{
		Kind:       KindDoc,
		Text:       text,
		TotalChars: total,
		Impl:       "go-docx",
		Degraded:   true,
		Notice:     notice,
		Sections:   sections,
	}, nil
}

// extractDocxText 流式提取 docx 正文文本。
//
// 规则与常见文本抽取实现一致：
//   - <w:t> 为正文文本（<w:delText> 为删除痕迹，忽略）；
//   - <w:p> 段落结束补换行；<w:br>/<w:cr> 强制换行；<w:tab> 制表符；
//   - 表格：<w:tc> 单元格之间以制表符分隔，<w:tr> 行之间换行。
func extractDocxText(r io.Reader) (string, []Section, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false // 容忍个别不规范节点，尽量提取可读文本

	var (
		sb         strings.Builder
		inText     bool
		cellSeen   bool
		runes      int
		sections   []Section
		inPara     bool
		paraStart  int
		paraText   strings.Builder
		headingLvl int
		// 提前退出阈值（字节粗估）：CJK 单字符最多 3 字节，4 倍留足余量
		hardLimit = MaxTextChars * 4
	)
	writeByte := func(b byte) {
		sb.WriteByte(b)
		runes++
	}
	writeText := func(s string) {
		sb.WriteString(s)
		runes += utf8.RuneCountInString(s)
		if inPara {
			paraText.WriteString(s)
		}
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return sb.String(), finalizeSections(sections, runes), nil
		}
		if err != nil {
			return sb.String(), finalizeSections(sections, runes), err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inPara = true
				paraStart = runes
				paraText.Reset()
				headingLvl = 0
			case "pStyle":
				for _, attr := range t.Attr {
					if attr.Name.Local == "val" {
						headingLvl = docxHeadingLevel(attr.Value)
					}
				}
			case "t":
				inText = true
			case "tab":
				writeByte('\t')
			case "br", "cr":
				writeByte('\n')
			case "tc":
				if cellSeen {
					writeByte('\t')
				}
				cellSeen = true
			case "tr":
				if sb.Len() > 0 {
					writeByte('\n')
				}
				cellSeen = false
			case "noBreakHyphen":
				writeByte('-')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if inPara && headingLvl > 0 {
					if title := strings.TrimSpace(paraText.String()); title != "" {
						sections = append(sections, Section{Title: title, Level: headingLvl, StartChar: paraStart})
					}
				}
				inPara = false
				if sb.Len() > 0 {
					writeByte('\n')
				}
			}
		case xml.CharData:
			if inText {
				writeText(string(t))
			}
		}

		if sb.Len() > hardLimit {
			return sb.String(), finalizeSections(sections, runes), nil
		}
	}
}

// finalizeSections 补齐章节结束偏移。
func finalizeSections(sections []Section, total int) []Section {
	for i := range sections {
		if i+1 < len(sections) {
			sections[i].EndChar = sections[i+1].StartChar
		} else {
			sections[i].EndChar = total
		}
	}
	return sections
}

// docxHeadingLevel 从 pStyle 值（Heading 1/标题 1）解析标题级别。
func docxHeadingLevel(style string) int {
	lower := strings.ToLower(strings.TrimSpace(style))
	idx := strings.Index(lower, "heading")
	if idx < 0 {
		idx = strings.Index(lower, "标题")
	}
	if idx < 0 {
		return 0
	}
	level := 0
	for i := idx; i < len(lower); i++ {
		if lower[i] >= '0' && lower[i] <= '9' {
			level = level*10 + int(lower[i]-'0')
		} else if level > 0 {
			break
		}
	}
	if level < 1 || level > 6 {
		return 0
	}
	return level
}

// relocateSections 在归一化后的文本中重新定位章节，修正空白折叠造成的偏移。
func relocateSections(text string, sections []Section) []Section {
	if len(sections) == 0 {
		return nil
	}
	searchFrom := 0
	out := make([]Section, 0, len(sections))
	for _, section := range sections {
		idx := strings.Index(text[searchFrom:], section.Title)
		if idx < 0 {
			continue
		}
		byteStart := searchFrom + idx
		section.StartChar = utf8.RuneCountInString(text[:byteStart])
		out = append(out, section)
		searchFrom = byteStart + len(section.Title)
	}
	return finalizeSections(out, utf8.RuneCountInString(text))
}

// normalizeDocxText 归一化文本：统一换行、去除行尾空白、折叠连续空行。
func normalizeDocxText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	prevEmpty := true
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		if strings.TrimSpace(ln) == "" {
			if prevEmpty {
				continue
			}
			prevEmpty = true
			out = append(out, "")
			continue
		}
		prevEmpty = false
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
