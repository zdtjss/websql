package export

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

var groupColumnNames = map[string]bool{
	"表名": true, "表名称": true, "table_name": true, "TABLE_NAME": true,
}

const (
	DocxPrimaryColor = "1A237E"
	DocxAccentColor  = "00BCD4"
	DocxSuccessColor = "4CAF50"
	DocxWarningColor = "FF9800"
	DocxLightGray    = "757575"
	DocxBgColor      = "F5F7FA"
)

func GenerateDocx(qr *QueryResult, title string, chartImagePaths []string, outputPath string) error {
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
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
</Types>`
	writeZipEntry(zw, "[Content_Types].xml", ct)

	writeZipEntry(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)

	docRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
  <Relationship Id="rIdNumbering" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`
	for i, rid := range imageRIDs {
		docRels += fmt.Sprintf(`
  <Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/chart_%d.png"/>`, rid, i)
	}
	docRels += `
</Relationships>`
	writeZipEntry(zw, "word/_rels/document.xml.rels", docRels)

	var body strings.Builder
	body.WriteString(docxDocumentHeader())
	body.WriteString(DocxCoverPageEnhanced(title, "专业数据分析报告"))
	body.WriteString(docxSectionBreak())

	body.WriteString(docxHeadingEnhanced("执行摘要", 1))
	body.WriteString(docxParagraphEnhanced(
		fmt.Sprintf("本报告基于数据库查询生成，共包含 %d 条记录、%d 个字段。生成时间：%s。报告通过多维度统计分析，深入挖掘数据价值，为决策提供数据支撑。",
			len(qr.Data), len(qr.Columns), time.Now().Format("2006年01月02日 15:04:05")),
		false, 22, ""))
	body.WriteString(docxSpacing(8))

	if hasImages {
		body.WriteString(docxHeadingEnhanced("数据可视化", 2))
		body.WriteString(docxParagraphEnhanced("以下图表直观展示了数据的关键指标趋势与对比情况，帮助快速洞察数据特征。", false, 20, ""))
		body.WriteString(docxSpacing(6))
		for i, rid := range imageRIDs {
			body.WriteString(docxParagraph("", false, 8, ""))
			body.WriteString(docxImage(rid, 5400000, 3240000))
			body.WriteString(docxParagraph(
				fmt.Sprintf("图 %d. 数据指标可视化分析", i+1),
				false, 18, "center"))
			body.WriteString(docxSpacing(4))
		}
	}

	body.WriteString(docxPageBreak())
	body.WriteString(docxHeadingEnhanced("统计摘要", 2))
	body.WriteString(docxParagraphEnhanced("对数据集的核心统计指标进行汇总，覆盖整体规模、数值分布等关键维度。", false, 20, ""))

	var statsItems []string
	statsItems = append(statsItems, fmt.Sprintf("总记录数：**%d** 条", len(qr.Data)))
	statsItems = append(statsItems, fmt.Sprintf("数据列数：**%d** 列", len(qr.Columns)))

	numericCols := DetectNumericCols(qr)
	if len(numericCols) > 0 {
		statsItems = append(statsItems, fmt.Sprintf("数值字段：**%d** 个（%s）", len(numericCols), strings.Join(numericCols, "、")))
		for _, col := range numericCols {
			min, max, avg, count := CalcNumericStats(qr, col)
			if count > 0 && count > 1 {
				statsItems = append(statsItems, fmt.Sprintf("`%s` — 最小值：%.2f，最大值：%.2f，平均值：%.2f，总计：%.2f", col, min, max, avg, avg*float64(count)))
			}
		}
	}
	body.WriteString(docxBulletList(statsItems))
	body.WriteString(docxSpacing(8))

	body.WriteString(docxHeadingEnhanced("核心发现", 2))
	var insights []string
	for _, col := range numericCols {
		_, max, avg, count := CalcNumericStats(qr, col)
		if count > 1 {
			insights = append(insights, fmt.Sprintf("`%s` 的平均值为 **%.2f**，峰值为 **%.2f**，表明该指标存在较大波动空间", col, avg, max))
			if len(insights) >= 5 {
				break
			}
		}
	}
	if len(insights) == 0 {
		insights = append(insights, "针对现有字段的分析显示数据质量良好，建议进一步细化分析维度")
	}
	body.WriteString(docxBulletList(insights))
	body.WriteString(docxSpacing(8))

	if len(qr.Data) > 0 {
		body.WriteString(docxPageBreak())
		body.WriteString(docxHeadingEnhanced("数据明细", 2))

		groupCol := ""
		for _, c := range qr.Columns {
			if groupColumnNames[c] {
				groupCol = c
				break
			}
		}

		if groupCol != "" {
			body.WriteString(docxParagraphEnhanced(
				fmt.Sprintf("共 %d 条记录，按「%s」分组展示。", len(qr.Data), groupCol), false, 20, ""))
			body.WriteString(docxSpacing(4))

			type groupEntry struct {
				name string
				rows []map[string]any
			}
			var groups []groupEntry
			groupIndex := map[string]int{}
			for _, row := range qr.Data {
				gk := fmt.Sprintf("%v", row[groupCol])
				if idx, ok := groupIndex[gk]; ok {
					groups[idx].rows = append(groups[idx].rows, row)
				} else {
					groupIndex[gk] = len(groups)
					groups = append(groups, groupEntry{name: gk, rows: []map[string]any{row}})
				}
			}

			otherCols := make([]string, 0, len(qr.Columns)-1)
			for _, c := range qr.Columns {
				if c != groupCol {
					otherCols = append(otherCols, c)
				}
			}

			for _, g := range groups {
				body.WriteString(docxHeadingEnhanced(
					fmt.Sprintf("%s: %s（%d 条）", groupCol, g.name, len(g.rows)), 3))
				subQr := &QueryResult{Columns: otherCols, Data: g.rows}
				body.WriteString(docxTableForColumns(subQr, otherCols))
				body.WriteString(docxSpacing(4))
			}
		} else {
			body.WriteString(docxParagraphEnhanced(
				fmt.Sprintf("以下表格展示了数据集的前 %d 条记录，包含所有字段的完整信息。", minInt(len(qr.Data), 8000)), false, 20, ""))
			body.WriteString(docxSpacing(4))
			body.WriteString(docxTableEnhanced(qr))
			body.WriteString(docxSpacing(8))
		}
	}

	body.WriteString(docxPageBreak())
	body.WriteString(docxHeadingEnhanced("字段说明", 2))
	body.WriteString(docxParagraphEnhanced("对各字段的数据类型和含义进行说明，便于理解数据结构和后续分析。", false, 20, ""))
	body.WriteString(docxSpacing(4))
	var colItems []string
	for i, col := range qr.Columns {
		isNum := false
		for _, nc := range numericCols {
			if nc == col {
				isNum = true
				break
			}
		}
		typeTag := "文本类型"
		typeColor := DocxLightGray
		if isNum {
			typeTag = "数值类型"
			typeColor = DocxSuccessColor
		}
		colItems = append(colItems, fmt.Sprintf("**`%s`** — `%s`（第 %d 列）", col, typeTag, i+1))
		_ = typeColor
	}
	body.WriteString(docxBulletList(colItems))

	body.WriteString(docxParagraph("", false, 16, ""))
	body.WriteString(docxDecoLine(DocxAccentColor))
	body.WriteString(docxParagraph("", false, 10, ""))
	body.WriteString(docxParagraphEnhanced(
		"* 本报告由 WebSQL AI 智能数据分析平台自动生成，数据来源为实时数据库查询，分析结果仅供参考。",
		false, 18, "center"))
	body.WriteString(docxParagraphEnhanced(
		fmt.Sprintf("* 报告生成时间：%s  |  版本：v2.0", time.Now().Format("2006-01-02 15:04:05")),
		false, 18, "center"))

	body.WriteString(docxSectionBreakFooter())
	writeZipEntry(zw, "word/styles.xml", docxStylesXML())
	writeZipEntry(zw, "word/numbering.xml", docxNumberingXML())
	writeZipEntry(zw, "word/document.xml", body.String())

	if hasImages {
		for i, path := range chartImagePaths {
			_ = writeZipFile(zw, fmt.Sprintf("word/media/chart_%d.png", i), path)
		}
	}

	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func DocxCoverPageEnhanced(title, subtitle string) string {
	var sb strings.Builder

	for i := 0; i < 8; i++ {
		sb.WriteString(docxParagraph("", false, 24, ""))
	}

	sb.WriteString(docxDecoLine(DocxPrimaryColor))
	sb.WriteString(docxParagraph("", false, 8, ""))

	sb.WriteString(docxParagraph(title, true, 44, "center"))
	sb.WriteString(docxParagraph("", false, 12, ""))

	sb.WriteString(docxParagraph(subtitle, false, 28, "center"))
	sb.WriteString(docxParagraph("", false, 16, ""))

	sb.WriteString(docxDecoLine(DocxAccentColor))
	sb.WriteString(docxParagraph("", false, 16, ""))

	sb.WriteString(docxParagraph(chr(9472)+" WebSQL AI \u00b7 智能数据分析平台 "+chr(9472), false, 18, "center"))
	sb.WriteString(docxParagraph(
		fmt.Sprintf("生成日期：%s", time.Now().Format("2006-01-02")),
		false, 18, "center"))
	sb.WriteString(docxParagraph(
		fmt.Sprintf("文档编号：WS-REPORT-%s", time.Now().Format("20060102")),
		false, 16, "center"))
	return sb.String()
}

func chr(r rune) string {
	return string(r)
}

func docxHeadingEnhanced(text string, level int) string {
	fontSize := 28
	styleId := "Heading1"
	switch level {
	case 1:
		fontSize = 32
		styleId = "Heading1"
	case 2:
		fontSize = 26
		styleId = "Heading2"
	case 3:
		fontSize = 22
		styleId = "Heading3"
	}
	var sb strings.Builder

	sb.WriteString(docxParagraph("", false, 10, ""))

	sb.WriteString("<w:p>")
	sb.WriteString("<w:pPr>")
	fmt.Fprintf(&sb, `<w:pStyle w:val="%s"/>`, styleId)
	sb.WriteString(`<w:spacing w:before="300" w:after="120"/>`)
	sb.WriteString("</w:pPr>")

	sb.WriteString("<w:r>")
	sb.WriteString("<w:rPr>")
	fmt.Fprintf(&sb, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, fontSize, fontSize)
	barColor := DocxPrimaryColor
	if level == 2 {
		barColor = DocxAccentColor
	}
	sb.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, barColor))
	sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
	sb.WriteString("</w:rPr>")
	sb.WriteString("<w:t xml:space=\"preserve\">")
	sb.WriteString(xmlEscape(text))
	sb.WriteString("</w:t>")
	sb.WriteString("</w:r>")
	sb.WriteString("</w:p>\n")

	if level == 1 {
		sb.WriteString(docxDecoLine(DocxAccentColor))
	}
	sb.WriteString(docxParagraph("", false, 4, ""))

	return sb.String()
}

func docxParagraphEnhanced(text string, bold bool, fontSize int, align string) string {
	return docxRichParagraph(text, bold, fontSize, align, false)
}

func docxSpacing(points int) string {
	return docxParagraph("", false, points, "")
}

func docxPageBreak() string {
	return `<w:p><w:r><w:br w:type="page"/></w:r></w:p>`
}

func docxTableEnhanced(qr *QueryResult) string {
	return docxTableForColumns(qr, qr.Columns)
}

func docxTableForColumns(qr *QueryResult, cols []string) string {
	var sb strings.Builder

	numCols := len(cols)

	sb.WriteString(`<w:tbl>
<w:tblPr>
  <w:tblStyle w:val="TableGrid"/>
  <w:tblW w:w="5000" w:type="pct"/>
  <w:jc w:val="center"/>
  <w:tblLayout w:type="autofit"/>
  <w:tblBorders>
    <w:top w:val="single" w:sz="6" w:space="0" w:color="BDBDBD"/>
    <w:left w:val="single" w:sz="6" w:space="0" w:color="BDBDBD"/>
    <w:bottom w:val="single" w:sz="6" w:space="0" w:color="BDBDBD"/>
    <w:right w:val="single" w:sz="6" w:space="0" w:color="BDBDBD"/>
    <w:insideH w:val="single" w:sz="4" w:space="0" w:color="E0E0E0"/>
    <w:insideV w:val="single" w:sz="4" w:space="0" w:color="E0E0E0"/>
  </w:tblBorders>
</w:tblPr>
`)

	sb.WriteString(`<w:tblGrid>`)
	for i := 0; i < numCols; i++ {
		sb.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, 9000/numCols))
	}
	sb.WriteString(`</w:tblGrid>`)

	sb.WriteString("<w:tr>")
	for i := 0; i < numCols; i++ {
		sb.WriteString(`<w:tc><w:tcPr>
  <w:shd w:val="clear" w:color="auto" w:fill="1A237E"/>
  <w:tcW w:w="1000" w:type="pct"/>
</w:tcPr>`)
		sb.WriteString("<w:p><w:pPr><w:jc w:val=\"center\"/><w:spacing w:before=\"40\" w:after=\"40\"/></w:pPr>")
		sb.WriteString("<w:r><w:rPr><w:b/><w:bCs/>")
		sb.WriteString(`<w:sz w:val="20"/><w:szCs w:val="20"/><w:color w:val="FFFFFF"/>`)
		sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
		sb.WriteString("</w:rPr><w:t xml:space=\"preserve\">")
		sb.WriteString(xmlEscape(cols[i]))
		sb.WriteString("</w:t></w:r></w:p></w:tc>")
	}
	sb.WriteString("</w:tr>\n")

	maxRows := len(qr.Data)
	if maxRows > 8000 {
		maxRows = 8000
	}
	for i := 0; i < maxRows; i++ {
		row := qr.Data[i]
		fillColor := "FFFFFF"
		if i%2 == 1 {
			fillColor = "F0F4FF"
		}
		sb.WriteString("<w:tr>")
		for j := 0; j < numCols; j++ {
			sb.WriteString(fmt.Sprintf(`<w:tc><w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="%s"/></w:tcPr>`, fillColor))
			sb.WriteString("<w:p><w:pPr><w:spacing w:before=\"20\" w:after=\"20\"/></w:pPr>")
			sb.WriteString("<w:r><w:rPr>")
			sb.WriteString(`<w:sz w:val="20"/><w:szCs w:val="20"/>`)
			sb.WriteString(`<w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>`)
			sb.WriteString("</w:rPr><w:t xml:space=\"preserve\">")
			if v, ok := row[cols[j]]; ok {
				sb.WriteString(xmlEscape(fmt.Sprintf("%v", v)))
			}
			sb.WriteString("</w:t></w:r></w:p></w:tc>")
		}
		sb.WriteString("</w:tr>\n")
	}

	if len(qr.Data) > 8000 {
		sb.WriteString("<w:tr><w:tc>")
		sb.WriteString("<w:tcPr><w:gridSpan w:val=\"" + fmt.Sprintf("%d", numCols) + "\"/><w:shd w:val=\"clear\" w:color=\"auto\" w:fill=\"FFF3E0\"/></w:tcPr>")
		sb.WriteString(fmt.Sprintf("<w:p><w:pPr><w:jc w:val=\"center\"/></w:pPr><w:r><w:rPr><w:i/><w:sz w:val=\"20\"/><w:color w:val=\"E65100\"/></w:rPr><w:t>... 共 %d 行，仅显示前 8000 行</w:t></w:r></w:p>", len(qr.Data)))
		sb.WriteString("</w:tc></w:tr>\n")
	}

	sb.WriteString("</w:tbl>\n")
	return sb.String()
}

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

func docxStylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:docDefaults>
    <w:rPrDefault>
      <w:rPr>
        <w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>
        <w:sz w:val="22"/>
      </w:rPr>
    </w:rPrDefault>
    <w:pPrDefault>
      <w:pPr>
        <w:spacing w:after="160" w:line="259" w:lineRule="auto"/>
      </w:pPr>
    </w:pPrDefault>
  </w:docDefaults>
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:keepNext/>
      <w:keepLines/>
      <w:spacing w:before="480" w:after="120"/>
      <w:outlineLvl w:val="0"/>
    </w:pPr>
    <w:rPr>
      <w:b/><w:bCs/>
      <w:sz w:val="32"/><w:szCs w:val="32"/>
      <w:color w:val="1A237E"/>
      <w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>
    </w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading2">
    <w:name w:val="heading 2"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:keepNext/>
      <w:keepLines/>
      <w:spacing w:before="360" w:after="120"/>
      <w:outlineLvl w:val="1"/>
    </w:pPr>
    <w:rPr>
      <w:b/><w:bCs/>
      <w:sz w:val="26"/><w:szCs w:val="26"/>
      <w:color w:val="00BCD4"/>
      <w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>
    </w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading3">
    <w:name w:val="heading 3"/>
    <w:basedOn w:val="Normal"/>
    <w:next w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:keepNext/>
      <w:keepLines/>
      <w:spacing w:before="240" w:after="80"/>
      <w:outlineLvl w:val="2"/>
    </w:pPr>
    <w:rPr>
      <w:b/><w:bCs/>
      <w:sz w:val="22"/><w:szCs w:val="22"/>
      <w:color w:val="1E4D8C"/>
      <w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>
    </w:rPr>
  </w:style>
  <w:style w:type="table" w:styleId="TableGrid">
    <w:name w:val="Table Grid"/>
    <w:basedOn w:val="NormalTable"/>
    <w:tblPr>
      <w:tblBorders>
        <w:top w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:left w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:bottom w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:right w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:insideH w:val="single" w:sz="4" w:space="0" w:color="auto"/>
        <w:insideV w:val="single" w:sz="4" w:space="0" w:color="auto"/>
      </w:tblBorders>
    </w:tblPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="ListBullet">
    <w:name w:val="List Bullet"/>
    <w:basedOn w:val="Normal"/>
    <w:qFormat/>
    <w:pPr>
      <w:spacing w:before="40" w:after="40"/>
      <w:ind w:left="720" w:hanging="360"/>
    </w:pPr>
    <w:rPr>
      <w:sz w:val="20"/><w:szCs w:val="20"/>
    </w:rPr>
  </w:style>
</w:styles>`
}

func docxNumberingXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:abstractNum w:abstractNumId="0">
    <w:nsid w:val="2DDA5878"/>
    <w:multiLevelType w:val="hybridMultilevel"/>
    <w:lvl w:ilvl="0">
      <w:start w:val="1"/>
      <w:numFmt w:val="bullet"/>
      <w:lvlText w:val="&#x25B8;"/>
      <w:lvlJc w:val="left"/>
      <w:pPr>
        <w:ind w:left="720" w:hanging="360"/>
      </w:pPr>
      <w:rPr>
        <w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>
      </w:rPr>
    </w:lvl>
    <w:lvl w:ilvl="1">
      <w:start w:val="1"/>
      <w:numFmt w:val="bullet"/>
      <w:lvlText w:val="&#x25E6;"/>
      <w:lvlJc w:val="left"/>
      <w:pPr>
        <w:ind w:left="1440" w:hanging="360"/>
      </w:pPr>
      <w:rPr>
        <w:rFonts w:ascii="Microsoft YaHei" w:hAnsi="Microsoft YaHei" w:eastAsia="Microsoft YaHei"/>
      </w:rPr>
    </w:lvl>
  </w:abstractNum>
  <w:num w:numId="1">
    <w:abstractNumId w:val="0"/>
  </w:num>
</w:numbering>`
}

func docxSectionBreakFooter() string {
	return `  <w:sectPr>
    <w:pgSz w:w="11906" w:h="16838"/>
    <w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720"/>
  </w:sectPr>
</w:body>
</w:document>`
}

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

		if runes[i] == '$' {
			delim := 1
			if i+1 < len(runes) && runes[i+1] == '$' {
				delim = 2
			}
			if buf.Len() > 0 {
				current.text = buf.String()
				segments = append(segments, current)
				buf.Reset()
			}
			closeDelim := "$"
			if delim == 2 {
				closeDelim = "$$"
			}
			i += delim
			end := findStr(runes[i:], closeDelim)
			if end >= 0 {
				mathContent := string(runes[i : i+end])
				current.text = StripMarkdownFormatting(mathContent)
				segments = append(segments, current)
				i += end + delim
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

func docxBulletList(items []string) string {
	var sb strings.Builder
	for _, item := range items {
		sb.WriteString("<w:p>")
		sb.WriteString("<w:pPr>")
		sb.WriteString(`<w:pStyle w:val="ListBullet"/>`)
		sb.WriteString(`<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr>`)
		sb.WriteString(`<w:spacing w:before="40" w:after="40"/>`)
		sb.WriteString("</w:pPr>")
		sb.WriteString(docxRichTextRuns(item, false, 20))
		sb.WriteString("</w:p>\n")
	}
	return sb.String()
}

func docxCodeBlock(code string) string {
	var sb strings.Builder
	sb.WriteString(docxParagraph("", false, 4, ""))
	for _, line := range strings.Split(code, "\n") {
		sb.WriteString("<w:p>")
		sb.WriteString("<w:pPr>")
		sb.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="F5F5F5"/>`)
		sb.WriteString(`<w:spacing w:before="20" w:after="20"/>`)
		sb.WriteString(`<w:ind w:left="360"/>`)
		sb.WriteString("</w:pPr>")
		sb.WriteString("<w:r>")
		sb.WriteString("<w:rPr>")
		sb.WriteString(`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:eastAsia="Microsoft YaHei"/>`)
		sb.WriteString(`<w:sz w:val="18"/><w:szCs w:val="18"/>`)
		sb.WriteString("</w:rPr>")
		sb.WriteString("<w:t xml:space=\"preserve\">")
		sb.WriteString(xmlEscape(line))
		sb.WriteString("</w:t>")
		sb.WriteString("</w:r>")
		sb.WriteString("</w:p>\n")
	}
	sb.WriteString(docxParagraph("", false, 4, ""))
	return sb.String()
}

func docxDecoLine(color string) string {
	var sb strings.Builder
	sb.WriteString(`<w:p><w:pPr><w:jc w:val="center"/><w:pBdr><w:bottom w:val="single" w:sz="12" w:space="1"`)
	sb.WriteString(fmt.Sprintf(` w:color="%s"/>`, color))
	sb.WriteString(`</w:pBdr></w:pPr></w:p>`)
	return sb.String()
}

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

func GenerateDocxFromContent(content, title, outputPath string) error {
	blocks := ParseMarkdownBlocks(content)

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
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
</Types>`)

	writeZipEntry(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)

	writeZipEntry(zw, "word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
  <Relationship Id="rIdNumbering" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
</Relationships>`)

	var body strings.Builder
	body.WriteString(docxDocumentHeader())

	body.WriteString(DocxCoverPageEnhanced(title, "专业数据分析报告"))
	body.WriteString(docxSectionBreak())

	for _, block := range blocks {
		switch block.Type {
		case "h1":
			body.WriteString(docxHeadingEnhanced(StripMarkdownFormatting(block.Content), 1))
		case "h2":
			body.WriteString(docxHeadingEnhanced(StripMarkdownFormatting(block.Content), 2))
		case "h3":
			body.WriteString(docxHeadingEnhanced(StripMarkdownFormatting(block.Content), 3))
		case "paragraph":
			body.WriteString(docxRichParagraph(block.Content, false, 22, "", true))
			body.WriteString(docxSpacing(4))
		case "list":
			for _, item := range strings.Split(block.Content, "\n") {
				body.WriteString(docxRichParagraph("\u25B8 "+item, false, 22, "", true))
			}
			body.WriteString(docxSpacing(4))
		case "code":
			body.WriteString(docxCodeBlock(block.Content))
		case "table":
			body.WriteString(docxMarkdownTable(block.Content))
			body.WriteString(docxSpacing(8))
		}
	}

	body.WriteString(docxSectionBreakFooter())
	writeZipEntry(zw, "word/styles.xml", docxStylesXML())
	writeZipEntry(zw, "word/numbering.xml", docxNumberingXML())
	writeZipEntry(zw, "word/document.xml", body.String())

	return nil
}

func docxMarkdownTable(mdTable string) string {
	lines := strings.Split(mdTable, "\n")
	var dataLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || IsTableSeparator(trimmed) {
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
			cells[i] = strings.TrimSpace(StripMarkdownFormatting(cells[i]))
		}
		rows = append(rows, cells)
	}

	if len(rows) == 0 {
		return ""
	}

	cols := rows[0]
	qr := &QueryResult{
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
	return docxTableEnhanced(qr)
}
