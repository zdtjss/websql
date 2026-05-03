// DOCX 生成器 — 直接构建 Office Open XML
//
// DOCX 文件本质是 ZIP 包，包含以下核心 XML：
//   - [Content_Types].xml  — 内容类型声明
//   - _rels/.rels          — 顶层关系
//   - word/document.xml    — 文档主体
//   - word/_rels/document.xml.rels — 文档关系（图片等）
//
// 设计理念：
//   - 封面页：居中标题 + 装饰线 + 副标题 + 日期
//   - 正文：适当页边距，清晰的标题层次
//   - 表格：深色表头 + 交替行色 + 细边框
//   - 富文本：保留 Markdown 中的粗体、斜体、行内代码
//   - 统一品牌色系 (Navy #1A237E + Cyan #00BCD4)
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

func generateDocx(qr *queryResult, title string, chartImagePaths []string, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	hasImages := len(chartImagePaths) > 0
	var imageRIDs []string
	for i := range chartImagePaths {
		imageRIDs = append(imageRIDs, fmt.Sprintf("rIdImg%d", i+10))
	}

	ct := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>`
	if hasImages {
		ct += `
  <Default Extension="png" ContentType="image/png"/>`
	}
	ct += `
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`
	writeZipEntry(zw, "[Content_Types].xml", ct)

	writeZipEntry(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)

	docRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	for i, rid := range imageRIDs {
		docRels += fmt.Sprintf(`
  <Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/chart_%d.png"/>`, rid, i)
	}
	docRels += `
</Relationships>`
	writeZipEntry(zw, "word/_rels/document.xml.rels", docRels)

	var body strings.Builder
	body.WriteString(docxDocumentHeader())

	coverPageTitle := title
	body.WriteString(docxCoverPage(coverPageTitle, "数据分析报告"))
	body.WriteString(docxSectionBreak())

	body.WriteString(docxHeading("执行摘要", 1))
	body.WriteString(docxParagraph(
		fmt.Sprintf("本报告基于数据库查询生成，共包含 %d 条记录、%d 个字段。生成时间：%s。",
			len(qr.Data), len(qr.Columns), time.Now().Format("2006-01-02 15:04:05")),
		false, 22, ""))

	if hasImages {
		body.WriteString(docxHeading("数据可视化", 2))
		for i, rid := range imageRIDs {
			if i > 0 {
				body.WriteString(docxParagraph("", false, 12, ""))
			}
			body.WriteString(docxImage(rid, 5400000, 3240000))
			body.WriteString(docxParagraph(
				fmt.Sprintf("图 %d. 数据指标可视化分析", i+1),
				false, 18, "center"))
		}
		body.WriteString(docxParagraph("", false, 12, ""))
	}

	body.WriteString(docxHeading("统计摘要", 2))

	var statsItems []string
	statsItems = append(statsItems, fmt.Sprintf("总记录数：**%d** 条", len(qr.Data)))
	statsItems = append(statsItems, fmt.Sprintf("数据列数：**%d** 列", len(qr.Columns)))

	numericCols := detectNumericCols(qr)
	if len(numericCols) > 0 {
		statsItems = append(statsItems, fmt.Sprintf("数值字段：**%d** 个（%s）", len(numericCols), strings.Join(numericCols, "、")))

		for _, col := range numericCols {
			min, max, avg, count := calcNumericStats(qr, col)
			if count > 0 && count > 1 {
				statsItems = append(statsItems, fmt.Sprintf("`%s` — 最小值：%.2f，最大值：%.2f，平均值：%.2f，总计：%.2f", col, min, max, avg, avg*float64(count)))
			}
		}
	}

	statsItems = append(statsItems, "")
	statsItems = append(statsItems, "**核心发现**:")
	for _, col := range numericCols {
		_, max, avg, count := calcNumericStats(qr, col)
		if count > 1 {
			statsItems = append(statsItems, fmt.Sprintf("· `%s` 均值为 %.2f，峰值为 %.2f", col, avg, max))
			if len(statsItems) > 8 {
				break
			}
		}
	}

	body.WriteString(docxBulletList(statsItems))
	body.WriteString(docxParagraph("", false, 12, ""))

	if len(qr.Data) > 0 {
		body.WriteString(docxHeading("数据明细", 2))
		body.WriteString(docxParagraph("", false, 12, ""))
		body.WriteString(docxTable(qr))
		body.WriteString(docxParagraph("", false, 12, ""))
	}

	body.WriteString(docxHeading("字段说明", 2))
	var colItems []string
	for i, col := range qr.Columns {
		isNum := false
		for _, nc := range numericCols {
			if nc == col {
				isNum = true
				break
			}
		}
		typeTag := "（文本）"
		if isNum {
			typeTag = "（数值）"
		}
		colItems = append(colItems, fmt.Sprintf("**%d. `%s`** %s", i+1, col, typeTag))
	}
	body.WriteString(docxBulletList(colItems))

	body.WriteString(docxParagraph("", false, 16, ""))
	body.WriteString(docxDecoLine("00BCD4"))
	body.WriteString(docxParagraph("", false, 12, ""))
	body.WriteString(docxParagraph(
		"* 本报告由 WebSQL AI 智能数据分析平台自动生成，数据来源为实时数据库查询。",
		false, 18, "center"))
	body.WriteString(docxParagraph(
		fmt.Sprintf("* 报告生成时间：%s", time.Now().Format("2006-01-02 15:04:05")),
		false, 18, "center"))

	body.WriteString(docxSectionBreakFooter())
	writeZipEntry(zw, "word/document.xml", body.String())

	if hasImages {
		for i, path := range chartImagePaths {
			_ = writeZipFile(zw, fmt.Sprintf("word/media/chart_%d.png", i), path)
		}
	}

	return nil
}

func calcNumericStats(qr *queryResult, col string) (min, max, avg float64, count int) {
	min = 1e18
	max = -1e18
	var sum float64
	for _, row := range qr.Data {
		if v, err := toFloat64(row[col]); err == nil {
			sum += v
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
			count++
		}
	}
	if count > 0 {
		avg = sum / float64(count)
	}
	return
}

// ──────────────────────────────────────────────
// DOCX XML 构建 — 文档结构
// ──────────────────────────────────────────────

func docxDocumentHeader() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
            xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
            xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
            xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
<w:body>
`
}

func docxSectionBreak() string {
	return `  <w:p>
    <w:pPr>
      <w:sectPr>
        <w:pgSz w:w="11906" w:h="16838"/>
        <w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720"/>
      </w:sectPr>
    </w:pPr>
  </w:p>
`
}

func docxSectionBreakFooter() string {
	return `  <w:sectPr>
    <w:pgSz w:w="11906" w:h="16838"/>
    <w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720"/>
    <w:footerReference r:id="rId99" w:type="default"/>
  </w:sectPr>
</w:body>
</w:document>`
}

func docxCoverPage(title, subtitle string) string {
	var sb strings.Builder

	// 顶部留白
	for i := 0; i < 6; i++ {
		sb.WriteString(docxParagraph("", false, 24, ""))
	}

	// 装饰线
	sb.WriteString(docxDecoLine("1A237E"))

	// 主标题
	sb.WriteString(docxParagraph(title, true, 40, "center"))
	sb.WriteString(docxParagraph("", false, 16, ""))

	// 副标题
	sb.WriteString(docxParagraph(subtitle, false, 26, "center"))
	sb.WriteString(docxParagraph("", false, 20, ""))

	// 装饰线
	sb.WriteString(docxDecoLine("00BCD4"))

	// 生成信息
	sb.WriteString(docxParagraph("", false, 20, ""))
	sb.WriteString(docxParagraph("WebSQL AI · 智能数据分析平台", false, 20, "center"))
	sb.WriteString(docxParagraph(
		fmt.Sprintf("生成日期：%s", time.Now().Format("2006-01-02")),
		false, 20, "center"))

	return sb.String()
}

// ──────────────────────────────────────────────
// DOCX XML 构建 — 段落
// ──────────────────────────────────────────────

func docxParagraph(text string, bold bool, fontSize int, align string) string {
	return docxRichParagraph(text, bold, fontSize, align, false)
}

func docxRichParagraph(text string, bold bool, fontSize int, align string, rich bool) string {
	var sb strings.Builder
	sb.WriteString("<w:p>")

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

	if !rich {
		sb.WriteString(docxRun(text, bold, fontSize, ""))
	} else {
		sb.WriteString(docxRichTextRuns(text, bold, fontSize))
	}

	sb.WriteString("</w:p>\n")
	return sb.String()
}

func docxRun(text string, bold bool, fontSize int, color string) string {
	var sb strings.Builder
	sb.WriteString("<w:r>")
	sb.WriteString("<w:rPr>")
	if bold {
		sb.WriteString("<w:b/><w:bCs/>")
	}
	if fontSize > 0 {
		fmt.Fprintf(&sb, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, fontSize, fontSize)
	}
	if color != "" {
		fmt.Fprintf(&sb, `<w:color w:val="%s"/>`, color)
	}
	sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
	sb.WriteString("</w:rPr>")
	sb.WriteString("<w:t xml:space=\"preserve\">")
	sb.WriteString(xmlEscape(text))
	sb.WriteString("</w:t>")
	sb.WriteString("</w:r>")
	return sb.String()
}

func docxRichTextRuns(text string, defaultBold bool, defaultFontSize int) string {
	var sb strings.Builder

	segments := parseInlineMarkdown(text, defaultBold, defaultFontSize)

	for _, seg := range segments {
		if seg.text == "" {
			continue
		}
		sb.WriteString("<w:r>")
		sb.WriteString("<w:rPr>")
		if seg.bold {
			sb.WriteString("<w:b/><w:bCs/>")
		}
		if seg.italic {
			sb.WriteString("<w:i/><w:iCs/>")
		}
		fontSize := seg.fontSize
		if fontSize <= 0 {
			fontSize = 22
		}
		fmt.Fprintf(&sb, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, fontSize, fontSize)
		if seg.color != "" {
			fmt.Fprintf(&sb, `<w:color w:val="%s"/>`, seg.color)
		}
		if seg.isCode {
			sb.WriteString(`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:eastAsia="Microsoft YaHei"/>`)
			sb.WriteString(`<w:highlight w:val="lightGray"/>`)
		} else {
			sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
		}
		sb.WriteString("</w:rPr>")
		sb.WriteString("<w:t xml:space=\"preserve\">")
		sb.WriteString(xmlEscape(seg.text))
		sb.WriteString("</w:t>")
		sb.WriteString("</w:r>")
	}
	return sb.String()
}

type runSegment struct {
	text     string
	bold     bool
	italic   bool
	isCode   bool
	fontSize int
	color    string
}

func parseInlineMarkdown(text string, defaultBold bool, defaultFontSize int) []runSegment {
	var segments []runSegment
	current := runSegment{bold: defaultBold, fontSize: defaultFontSize}
	var buf strings.Builder

	runes := []rune(text)
	i := 0
	for i < len(runes) {
		if i+1 < len(runes) && runes[i] == '\\' && (runes[i+1] == '*' || runes[i+1] == '_' || runes[i+1] == '`' || runes[i+1] == '\\') {
			buf.WriteRune(runes[i+1])
			i += 2
			continue
		}

		if i+1 < len(runes) && runes[i] == '*' && runes[i+1] == '*' {
			if buf.Len() > 0 {
				current.text = buf.String()
				segments = append(segments, current)
				buf.Reset()
			}
			current = runSegment{bold: true, fontSize: defaultFontSize}
			i += 2
			end := findStr(runes[i:], "**")
			if end >= 0 {
				current.text = string(runes[i : i+end])
				segments = append(segments, current)
				i += end + 2
				current = runSegment{bold: defaultBold, fontSize: defaultFontSize}
			}
			continue
		}

		if runes[i] == '*' {
			if buf.Len() > 0 {
				current.text = buf.String()
				segments = append(segments, current)
				buf.Reset()
			}
			current = runSegment{italic: true, bold: defaultBold, fontSize: defaultFontSize}
			i++
			end := findStr(runes[i:], "*")
			if end >= 0 {
				current.text = string(runes[i : i+end])
				segments = append(segments, current)
				i += end + 1
				current = runSegment{bold: defaultBold, fontSize: defaultFontSize}
			}
			continue
		}

		if runes[i] == '`' {
			if buf.Len() > 0 {
				current.text = buf.String()
				segments = append(segments, current)
				buf.Reset()
			}
			current = runSegment{isCode: true, fontSize: defaultFontSize}
			i++
			end := findStr(runes[i:], "`")
			if end >= 0 {
				current.text = string(runes[i : i+end])
				segments = append(segments, current)
				i += end + 1
				current = runSegment{bold: defaultBold, fontSize: defaultFontSize}
			}
			continue
		}

		buf.WriteRune(runes[i])
		i++
	}

	if buf.Len() > 0 {
		current.text = buf.String()
		segments = append(segments, current)
	}

	return segments
}

func findStr(runes []rune, s string) int {
	sRunes := []rune(s)
	for i := 0; i <= len(runes)-len(sRunes); i++ {
		match := true
		for j := 0; j < len(sRunes); j++ {
			if runes[i+j] != sRunes[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// ──────────────────────────────────────────────
// DOCX XML 构建 — 标题
// ──────────────────────────────────────────────

func docxHeading(text string, level int) string {
	fontSize := 28
	switch level {
	case 1:
		fontSize = 30
	case 2:
		fontSize = 26
	case 3:
		fontSize = 22
	}
	var sb strings.Builder

	// 标题上方间距
	sb.WriteString(docxParagraph("", false, 8, ""))

	// 左侧色条 + 标题
	sb.WriteString("<w:p>")
	sb.WriteString("<w:pPr><w:spacing w:before=\"200\" w:after=\"100\"/></w:pPr>")

	// 色条标记
	sb.WriteString("<w:r>")
	sb.WriteString("<w:rPr>")
	fmt.Fprintf(&sb, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, fontSize, fontSize)
	barColor := "1A237E"
	if level == 2 {
		barColor = "00BCD4"
	}
	sb.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, barColor))
	sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
	sb.WriteString("</w:rPr>")
	if level == 1 {
		sb.WriteString("<w:t xml:space=\"preserve\">\u2503 </w:t>")
	} else {
		sb.WriteString("<w:t xml:space=\"preserve\">\u2502 </w:t>")
	}
	sb.WriteString("</w:r>")

	// 标题文本
	sb.WriteString("<w:r>")
	sb.WriteString("<w:rPr><w:b/><w:bCs/>")
	fmt.Fprintf(&sb, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, fontSize, fontSize)
	sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
	sb.WriteString("</w:rPr>")
	sb.WriteString("<w:t xml:space=\"preserve\">")
	sb.WriteString(xmlEscape(text))
	sb.WriteString("</w:t>")
	sb.WriteString("</w:r>")
	sb.WriteString("</w:p>\n")

	sb.WriteString(docxParagraph("", false, 6, ""))

	return sb.String()
}

// ──────────────────────────────────────────────
// DOCX XML 构建 — 表格
// ──────────────────────────────────────────────

func docxTable(qr *queryResult) string {
	var sb strings.Builder

	sb.WriteString(`<w:tbl>
<w:tblPr>
  <w:tblStyle w:val="TableGrid"/>
  <w:tblW w:w="5000" w:type="pct"/>
  <w:tblBorders>
    <w:top w:val="single" w:sz="4" w:space="0" w:color="BDBDBD"/>
    <w:left w:val="single" w:sz="4" w:space="0" w:color="BDBDBD"/>
    <w:bottom w:val="single" w:sz="4" w:space="0" w:color="BDBDBD"/>
    <w:right w:val="single" w:sz="4" w:space="0" w:color="BDBDBD"/>
    <w:insideH w:val="single" w:sz="4" w:space="0" w:color="E0E0E0"/>
    <w:insideV w:val="single" w:sz="4" w:space="0" w:color="E0E0E0"/>
  </w:tblBorders>
</w:tblPr>
`)

	// 表头行
	sb.WriteString("<w:tr>")
	for _, col := range qr.Columns {
		sb.WriteString(`<w:tc><w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="1A237E"/></w:tcPr>`)
		sb.WriteString("<w:p><w:pPr><w:jc w:val=\"center\"/></w:pPr>")
		sb.WriteString("<w:r><w:rPr><w:b/><w:bCs/>")
		sb.WriteString(`<w:sz w:val="20"/><w:szCs w:val="20"/><w:color w:val="FFFFFF"/>`)
		sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
		sb.WriteString("</w:rPr><w:t xml:space=\"preserve\">")
		sb.WriteString(xmlEscape(col))
		sb.WriteString("</w:t></w:r></w:p></w:tc>")
	}
	sb.WriteString("</w:tr>\n")

	// 数据行（最多 500 行）
	maxRows := len(qr.Data)
	if maxRows > 500 {
		maxRows = 500
	}
	for i := 0; i < maxRows; i++ {
		row := qr.Data[i]
		fillColor := "FFFFFF"
		if i%2 == 1 {
			fillColor = "F5F7FA"
		}
		sb.WriteString("<w:tr>")
		for _, col := range qr.Columns {
			fmt.Fprintf(&sb, `<w:tc><w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr>`, fillColor)
			sb.WriteString("<w:p>")
			sb.WriteString("<w:r><w:rPr>")
			sb.WriteString(`<w:sz w:val="20"/><w:szCs w:val="20"/>`)
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
		sb.WriteString("<w:tcPr><w:gridSpan w:val=\"" + fmt.Sprintf("%d", len(qr.Columns)) + "\"/></w:tcPr>")
		sb.WriteString(fmt.Sprintf("<w:p><w:pPr><w:jc w:val=\"center\"/></w:pPr><w:r><w:rPr><w:i/><w:sz w:val=\"20\"/><w:color w:val=\"757575\"/></w:rPr><w:t>... 共 %d 行，仅显示前 500 行</w:t></w:r></w:p>", len(qr.Data)))
		sb.WriteString("</w:tc></w:tr>\n")
	}

	sb.WriteString("</w:tbl>\n")
	return sb.String()
}

// ──────────────────────────────────────────────
// DOCX XML 构建 — 列表 & 装饰
// ──────────────────────────────────────────────

func docxBulletList(items []string) string {
	var sb strings.Builder
	for _, item := range items {
		sb.WriteString(docxRichParagraph("\u2022 "+item, false, 20, "", true))
	}
	return sb.String()
}

func docxDecoLine(color string) string {
	var sb strings.Builder
	sb.WriteString(`<w:p><w:pPr><w:jc w:val="center"/><w:pBdr><w:bottom w:val="single" w:sz="12" w:space="1"`)
	sb.WriteString(fmt.Sprintf(` w:color="%s"/>`, color))
	sb.WriteString(`</w:pBdr></w:pPr></w:p>`)
	return sb.String()
}

// ──────────────────────────────────────────────
// DOCX XML 构建 — 图片
// ──────────────────────────────────────────────

func docxImage(rID string, cx, cy int64) string {
	return fmt.Sprintf(`<w:p><w:pPr><w:jc w:val="center"/></w:pPr>
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
</w:p>`, cx, cy, rID, cx, cy)
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
	body.WriteString(docxDocumentHeader())

	// 封面
	body.WriteString(docxCoverPage(title, "数据分析报告"))
	body.WriteString(docxSectionBreak())

	// 正文内容
	for _, block := range blocks {
		switch block.Type {
		case "h1":
			body.WriteString(docxHeading(stripMarkdownFormatting(block.Content), 1))
		case "h2":
			body.WriteString(docxHeading(stripMarkdownFormatting(block.Content), 2))
		case "h3":
			body.WriteString(docxHeading(stripMarkdownFormatting(block.Content), 3))
		case "paragraph":
			body.WriteString(docxRichParagraph(block.Content, false, 22, "", true))
		case "list":
			for _, item := range strings.Split(block.Content, "\n") {
				body.WriteString(docxRichParagraph("\u2022 "+item, false, 22, "", true))
			}
		case "code":
			body.WriteString(docxParagraph("", false, 10, ""))
			for _, line := range strings.Split(block.Content, "\n") {
				body.WriteString(docxRichParagraph("  "+line, false, 18, "", false))
			}
			body.WriteString(docxParagraph("", false, 10, ""))
		case "table":
			body.WriteString(docxMarkdownTable(block.Content))
			body.WriteString(docxParagraph("", false, 10, ""))
		}
	}

	body.WriteString(docxSectionBreakFooter())
	writeZipEntry(zw, "word/document.xml", body.String())

	return nil
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
