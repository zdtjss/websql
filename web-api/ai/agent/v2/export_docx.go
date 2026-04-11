// DOCX 生成器 — 直接构建 Office Open XML
//
// DOCX 文件本质是 ZIP 包，包含以下核心 XML：
//   - [Content_Types].xml  — 内容类型声明
//   - _rels/.rels          — 顶层关系
//   - word/document.xml    — 文档主体
//   - word/_rels/document.xml.rels — 文档关系（图片等）
//
// 这种方式不依赖第三方 docx 库，完全可控，易于维护和扩展。
package agentv2

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// generateDocx 生成 DOCX 文件
func generateDocx(qr *queryResult, title, chartImagePath, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	hasImage := chartImagePath != ""
	imageRID := "rId10"

	// 1. [Content_Types].xml
	ct := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>`
	if hasImage {
		ct += `
  <Default Extension="png" ContentType="image/png"/>`
	}
	ct += `
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`
	writeZipEntry(zw, "[Content_Types].xml", ct)

	// 2. _rels/.rels
	writeZipEntry(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)

	// 3. word/_rels/document.xml.rels
	docRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	if hasImage {
		docRels += fmt.Sprintf(`
  <Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/chart.png"/>`, imageRID)
	}
	docRels += `
</Relationships>`
	writeZipEntry(zw, "word/_rels/document.xml.rels", docRels)

	// 4. word/document.xml — 文档主体
	var body strings.Builder
	body.WriteString(docxHeader())

	// 标题
	body.WriteString(docxParagraph(title, true, 32, "center"))
	body.WriteString(docxParagraph("", false, 22, ""))

	// 生成时间
	body.WriteString(docxParagraph(
		fmt.Sprintf("生成时间：%s", time.Now().Format("2006-01-02 15:04:05")),
		false, 18, ""))
	body.WriteString(docxParagraph("", false, 22, ""))

	// 图表（如果有）
	if hasImage {
		body.WriteString(docxParagraph("数据图表", true, 24, ""))
		body.WriteString(docxParagraph("", false, 22, ""))
		body.WriteString(docxImage(imageRID, 5400000, 3240000)) // ~15cm x 9cm
		body.WriteString(docxParagraph("", false, 22, ""))
	}

	// 数据表格
	if len(qr.Data) > 0 {
		body.WriteString(docxParagraph("数据明细", true, 24, ""))
		body.WriteString(docxParagraph("", false, 22, ""))
		body.WriteString(docxTable(qr))
		body.WriteString(docxParagraph("", false, 22, ""))
	}

	// 统计信息
	body.WriteString(docxParagraph("统计信息", true, 24, ""))
	body.WriteString(docxParagraph("", false, 22, ""))
	body.WriteString(docxParagraph(fmt.Sprintf("总记录数：%d", len(qr.Data)), false, 22, ""))
	body.WriteString(docxParagraph(fmt.Sprintf("列数：%d", len(qr.Columns)), false, 22, ""))

	body.WriteString(docxFooter())
	writeZipEntry(zw, "word/document.xml", body.String())

	// 5. 嵌入图片
	if hasImage {
		if err := writeZipFile(zw, "word/media/chart.png", chartImagePath); err != nil {
			// 图片写入失败不影响文档
		}
	}

	return nil
}

// ──────────────────────────────────────────────
// XML 构建辅助
// ──────────────────────────────────────────────

func docxHeader() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
            xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
            xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
            xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
<w:body>
`
}

func docxFooter() string {
	return `</w:body>
</w:document>`
}

// docxParagraph 生成段落 XML
func docxParagraph(text string, bold bool, fontSize int, align string) string {
	var sb strings.Builder
	sb.WriteString("<w:p>")

	// 段落属性
	if align != "" {
		sb.WriteString("<w:pPr>")
		switch align {
		case "center":
			sb.WriteString(`<w:jc w:val="center"/>`)
		case "right":
			sb.WriteString(`<w:jc w:val="right"/>`)
		}
		sb.WriteString("</w:pPr>")
	}

	sb.WriteString("<w:r>")

	// 运行属性
	sb.WriteString("<w:rPr>")
	if bold {
		sb.WriteString("<w:b/>")
	}
	if fontSize > 0 {
		fmt.Fprintf(&sb, `<w:sz w:val="%d"/>`, fontSize)
		fmt.Fprintf(&sb, `<w:szCs w:val="%d"/>`, fontSize)
	}
	sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
	sb.WriteString("</w:rPr>")

	sb.WriteString("<w:t xml:space=\"preserve\">")
	sb.WriteString(xmlEscape(text))
	sb.WriteString("</w:t>")
	sb.WriteString("</w:r>")
	sb.WriteString("</w:p>\n")
	return sb.String()
}

// docxTable 生成表格 XML
func docxTable(qr *queryResult) string {
	var sb strings.Builder

	sb.WriteString(`<w:tbl>
<w:tblPr>
  <w:tblStyle w:val="TableGrid"/>
  <w:tblW w:w="5000" w:type="pct"/>
  <w:tblBorders>
    <w:top w:val="single" w:sz="4" w:space="0" w:color="999999"/>
    <w:left w:val="single" w:sz="4" w:space="0" w:color="999999"/>
    <w:bottom w:val="single" w:sz="4" w:space="0" w:color="999999"/>
    <w:right w:val="single" w:sz="4" w:space="0" w:color="999999"/>
    <w:insideH w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>
    <w:insideV w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>
  </w:tblBorders>
</w:tblPr>
`)

	// 表头行
	sb.WriteString("<w:tr>")
	for _, col := range qr.Columns {
		sb.WriteString(`<w:tc><w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="E8EAF6"/></w:tcPr>`)
		sb.WriteString("<w:p><w:r><w:rPr><w:b/><w:sz w:val=\"18\"/><w:szCs w:val=\"18\"/>")
		sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
		sb.WriteString("</w:rPr><w:t>")
		sb.WriteString(xmlEscape(col))
		sb.WriteString("</w:t></w:r></w:p></w:tc>")
	}
	sb.WriteString("</w:tr>\n")

	// 数据行（最多 500 行，避免文件过大）
	maxRows := len(qr.Data)
	if maxRows > 500 {
		maxRows = 500
	}
	for i := 0; i < maxRows; i++ {
		row := qr.Data[i]
		sb.WriteString("<w:tr>")
		for _, col := range qr.Columns {
			sb.WriteString("<w:tc><w:p><w:r><w:rPr><w:sz w:val=\"18\"/><w:szCs w:val=\"18\"/>")
			sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
			sb.WriteString("</w:rPr><w:t xml:space=\"preserve\">")
			if v, ok := row[col]; ok {
				sb.WriteString(xmlEscape(fmt.Sprintf("%v", v)))
			}
			sb.WriteString("</w:t></w:r></w:p></w:tc>")
		}
		sb.WriteString("</w:tr>\n")
	}

	if len(qr.Data) > 500 {
		sb.WriteString("<w:tr><w:tc>")
		sb.WriteString(fmt.Sprintf("<w:p><w:r><w:rPr><w:i/><w:sz w:val=\"18\"/></w:rPr><w:t>... 共 %d 行，仅显示前 500 行</w:t></w:r></w:p>", len(qr.Data)))
		sb.WriteString("</w:tc></w:tr>\n")
	}

	sb.WriteString("</w:tbl>\n")
	return sb.String()
}

// docxImage 生成内嵌图片 XML
func docxImage(rID string, cx, cy int64) string {
	return fmt.Sprintf(`<w:p>
<w:r>
  <w:drawing>
    <wp:inline distT="0" distB="0" distL="0" distR="0">
      <wp:extent cx="%d" cy="%d"/>
      <wp:effectExtent l="0" t="0" r="0" b="0"/>
      <wp:docPr id="1" name="Chart"/>
      <wp:cNvGraphicFramePr>
        <a:graphicFrameLocks xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" noChangeAspect="1"/>
      </wp:cNvGraphicFramePr>
      <a:graphic>
        <a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">
          <pic:pic>
            <pic:nvPicPr>
              <pic:cNvPr id="1" name="chart.png"/>
              <pic:cNvPicPr/>
            </pic:nvPicPr>
            <pic:blipFill>
              <a:blip r:embed="%s"/>
              <a:stretch><a:fillRect/></a:stretch>
            </pic:blipFill>
            <pic:spPr>
              <a:xfrm>
                <a:off x="0" y="0"/>
                <a:ext cx="%d" cy="%d"/>
              </a:xfrm>
              <a:prstGeom prst="rect"><a:avLst/></a:prstGeom>
            </pic:spPr>
          </pic:pic>
        </a:graphicData>
      </a:graphic>
    </wp:inline>
  </w:drawing>
</w:r>
</w:p>
`, cx, cy, rID, cx, cy)
}

// ──────────────────────────────────────────────
// ZIP 写入辅助
// ──────────────────────────────────────────────

func writeZipEntry(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(content))
	return err
}

func writeZipFile(zw *zip.Writer, zipPath, diskPath string) error {
	src, err := os.Open(diskPath)
	if err != nil {
		return err
	}
	defer src.Close()

	w, err := zw.Create(zipPath)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ──────────────────────────────────────────────
// 基于 Markdown 内容生成 DOCX
// ──────────────────────────────────────────────

func generateDocxFromContent(content, title, outputPath string) error {
	blocks := parseMarkdownBlocks(content)

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	writeZipEntry(zw, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)

	writeZipEntry(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)

	writeZipEntry(zw, "word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`)

	var body strings.Builder
	body.WriteString(docxHeader())
	body.WriteString(docxParagraph(title, true, 32, "center"))
	body.WriteString(docxParagraph("", false, 22, ""))
	body.WriteString(docxParagraph(
		fmt.Sprintf("生成时间：%s", time.Now().Format("2006-01-02 15:04:05")),
		false, 18, ""))
	body.WriteString(docxParagraph("", false, 22, ""))

	for _, block := range blocks {
		switch block.Type {
		case "h1":
			body.WriteString(docxParagraph(stripMarkdownFormatting(block.Content), true, 28, ""))
		case "h2":
			body.WriteString(docxParagraph(stripMarkdownFormatting(block.Content), true, 24, ""))
		case "h3":
			body.WriteString(docxParagraph(stripMarkdownFormatting(block.Content), true, 20, ""))
		case "paragraph":
			body.WriteString(docxParagraph(stripMarkdownFormatting(block.Content), false, 22, ""))
		case "list":
			for _, item := range strings.Split(block.Content, "\n") {
				body.WriteString(docxParagraph("\u2022 "+stripMarkdownFormatting(item), false, 22, ""))
			}
		case "code":
			body.WriteString(docxParagraph("", false, 12, ""))
			for _, line := range strings.Split(block.Content, "\n") {
				body.WriteString(docxCodeParagraph(line))
			}
			body.WriteString(docxParagraph("", false, 12, ""))
		case "mermaid":
			body.WriteString(docxParagraph("Mermaid 图表", true, 20, ""))
			for _, line := range strings.Split(block.Content, "\n") {
				body.WriteString(docxCodeParagraph(line))
			}
			body.WriteString(docxParagraph("", false, 12, ""))
		case "table":
			body.WriteString(docxMarkdownTable(block.Content))
			body.WriteString(docxParagraph("", false, 12, ""))
		}
	}

	body.WriteString(docxFooter())
	writeZipEntry(zw, "word/document.xml", body.String())

	return nil
}

func docxCodeParagraph(text string) string {
	var sb strings.Builder
	sb.WriteString("<w:p>")
	sb.WriteString("<w:pPr><w:shd w:val=\"clear\" w:color=\"auto\" w:fill=\"F5F5F5\"/></w:pPr>")
	sb.WriteString("<w:r>")
	sb.WriteString("<w:rPr>")
	sb.WriteString(`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:eastAsia="Microsoft YaHei"/>`)
	sb.WriteString(`<w:sz w:val="18"/><w:szCs w:val="18"/>`)
	sb.WriteString("</w:rPr>")
	sb.WriteString("<w:t xml:space=\"preserve\">")
	sb.WriteString(xmlEscape(text))
	sb.WriteString("</w:t>")
	sb.WriteString("</w:r>")
	sb.WriteString("</w:p>\n")
	return sb.String()
}

func docxMarkdownTable(mdTable string) string {
	lines := strings.Split(mdTable, "\n")
	var dataLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isTableSeparator(trimmed) {
			continue
		}
		dataLines = append(dataLines, trimmed)
	}
	if len(dataLines) == 0 {
		return ""
	}

	var rows [][]string
	for _, line := range dataLines {
		line = strings.Trim(line, "|")
		cells := strings.Split(line, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(stripMarkdownFormatting(cells[i]))
		}
		rows = append(rows, cells)
	}

	if len(rows) == 0 {
		return ""
	}

	cols := rows[0]
	qr := &queryResult{
		Columns: cols,
		Data:    make([]map[string]any, 0, len(rows)-1),
	}
	for _, row := range rows[1:] {
		m := make(map[string]any)
		for i, col := range cols {
			if i < len(row) {
				m[col] = row[i]
			} else {
				m[col] = ""
			}
		}
		qr.Data = append(qr.Data, m)
	}
	return docxTable(qr)
}
