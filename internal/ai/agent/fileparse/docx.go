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

	text, extractErr := extractDocxText(rc)
	text = normalizeDocxText(text)
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
	}, nil
}

// extractDocxText 流式提取 docx 正文文本。
//
// 规则与常见文本抽取实现一致：
//   - <w:t> 为正文文本（<w:delText> 为删除痕迹，忽略）；
//   - <w:p> 段落结束补换行；<w:br>/<w:cr> 强制换行；<w:tab> 制表符；
//   - 表格：<w:tc> 单元格之间以制表符分隔，<w:tr> 行之间换行。
func extractDocxText(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false // 容忍个别不规范节点，尽量提取可读文本

	var (
		sb       strings.Builder
		inText   bool
		cellSeen bool
		// 提前退出阈值（字节粗估）：CJK 单字符最多 3 字节，4 倍留足余量
		hardLimit = MaxTextChars * 4
	)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return sb.String(), nil
		}
		if err != nil {
			return sb.String(), err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				sb.WriteByte('\t')
			case "br", "cr":
				sb.WriteByte('\n')
			case "tc":
				if cellSeen {
					sb.WriteByte('\t')
				}
				cellSeen = true
			case "tr":
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
				cellSeen = false
			case "noBreakHyphen":
				sb.WriteByte('-')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}

		if sb.Len() > hardLimit {
			return sb.String(), nil
		}
	}
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
