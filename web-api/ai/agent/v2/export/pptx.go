package export

import (
	"archive/zip"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	PptxPrimaryColor = "1A237E"
	PptxAccentColor  = "00BCD4"
	PptxBgColor      = "F8F9FA"
	PptxDarkBg       = "0D1B3E"
	PptxGradientMid  = "1A237E"
	PptxGradientEnd  = "283593"
)

func GeneratePptx(qr *QueryResult, title string, chartPaths []string, outputPath string) (int, error) {
	f, err := os.Create(outputPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	hasChart := len(chartPaths) > 0
	hasHighlights := len(qr.Data) > 5
	slideCount := 3 + boolInt(hasChart) + boolInt(hasHighlights)

	writeZipEntry(zw, "[Content_Types].xml", pptxContentTypes(slideCount))
	writeZipEntry(zw, "_rels/.rels", pptxTopRels())
	writeZipEntry(zw, "docProps/app.xml", pptxAppXml(slideCount))
	writeZipEntry(zw, "docProps/core.xml", pptxCoreXml(title))
	writeZipEntry(zw, "ppt/presentation.xml", pptxPresentation(slideCount))
	writeZipEntry(zw, "ppt/_rels/presentation.xml.rels", pptxPresentationRels(slideCount))
	writeZipEntry(zw, "ppt/theme/theme1.xml", pptxTheme())
	writeZipEntry(zw, "ppt/slideMasters/slideMaster1.xml", pptxSlideMaster())
	writeZipEntry(zw, "ppt/slideMasters/_rels/slideMaster1.xml.rels", pptxSlideMasterRels())
	writeZipEntry(zw, "ppt/slideLayouts/slideLayout1.xml", pptxSlideLayout())
	writeZipEntry(zw, "ppt/slideLayouts/_rels/slideLayout1.xml.rels", pptxSlideLayoutRels())

	writeZipEntry(zw, "ppt/slides/slide1.xml", pptxTitleSlideEnhanced(title))
	writeZipEntry(zw, "ppt/slides/_rels/slide1.xml.rels", pptxSlideRels())

	currentSlide := 2

	if hasChart {
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", currentSlide), pptxChartSlideEnhanced(title, currentSlide))
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", currentSlide), pptxSlideRelsWithImage(true, 0))
		currentSlide++
	}

	writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", currentSlide), pptxSummarySlideEnhanced(qr, currentSlide))
	writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", currentSlide), pptxSlideRels())
	currentSlide++

	writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", currentSlide), pptxTableSlideEnhanced(qr, currentSlide))
	writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", currentSlide), pptxSlideRels())
	currentSlide++

	if hasHighlights {
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", currentSlide), pptxHighlightSlideEnhanced(qr, currentSlide))
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", currentSlide), pptxSlideRels())
	}

	if hasChart {
		_ = writeZipFile(zw, "ppt/media/chart_0.png", chartPaths[0])
	}

	return slideCount, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func pptxTitleSlideEnhanced(title string) string {
	now := time.Now().Format("2006-01-02 15:04")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr>
    <a:gradFill rotWithShape="1"><a:gsLst>
      <a:gs pos="0"><a:srgbClr val="0D1B3E"/></a:gs>
      <a:gs pos="40000"><a:srgbClr val="1A237E"/></a:gs>
      <a:gs pos="100000"><a:srgbClr val="283593"/></a:gs>
    </a:gsLst><a:lin ang="5400000" scaled="0"/></a:gradFill><a:effectLst/>
  </p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
  %s
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxShape(2, 1400000, 2800000, 600000, 60000, PptxAccentColor),
		pptxShape(3, 1400000, 2880000, 60000, 600000, PptxAccentColor),
		pptxTextBox(4, 1500000, 2000000, 9200000, 2000000, title, 4200, true, "FFFFFF"),
		pptxTextBox(5, 1500000, 3900000, 9200000, 500000, "专业数据分析演示", 2200, false, "80CBC4"),
		pptxTextBox(6, 1500000, 4600000, 9200000, 500000, "\u25C6 生成时间："+now+"  \u25C6 平台：WebSQL AI 智能分析", 1400, false, "90A4AE"),
	)
}

func pptxChartSlideEnhanced(title string, pageNum int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="F8F9FA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxColorBarEnhanced(5, 0, 0, 12192000, 80000, PptxAccentColor),
		pptxTextBox(2, 1000000, 300000, 10200000, 800000, title+" \u00b7 数据洞察", 2600, true, PptxPrimaryColor),
		pptxChartImage(10),
		pptxSlideNumber(20, pageNum),
	)
}

func pptxSummarySlideEnhanced(qr *QueryResult, pageNum int) string {
	var sections []string

	sections = append(sections, fmt.Sprintf("\u25B8 数据集规模：%d 条记录，%d 个字段", len(qr.Data), len(qr.Columns)))

	numericCols := DetectNumericCols(qr)
	if len(numericCols) > 0 {
		sections = append(sections, "\u25B8 数值字段："+strings.Join(numericCols, "、"))
		for _, nc := range numericCols {
			min, max, avg, count := CalcNumericStats(qr, nc)
			if count > 0 {
				sections = append(sections, fmt.Sprintf("  \u25E6 %s：均值 %.2f，范围 [%.2f, %.2f]", nc, avg, min, max))
			}
			if len(sections) >= 12 {
				break
			}
		}
	}

	sections = append(sections, "")
	sections = append(sections, "\u25B8 字段列表：")
	for i, col := range qr.Columns {
		if i >= 15 {
			sections = append(sections, fmt.Sprintf("  \u25E6 ... 共 %d 列", len(qr.Columns)))
			break
		}
		marker := "\u25CB"
		for _, nc := range numericCols {
			if nc == col {
				marker = "\u25CF"
				break
			}
		}
		sections = append(sections, fmt.Sprintf("  %s %s", marker, col))
	}

	content := strings.Join(sections, "\n")

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="F8F9FA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxColorBarEnhanced(5, 0, 0, 12192000, 80000, PptxAccentColor),
		pptxTextBox(2, 1000000, 300000, 10200000, 800000, "数据摘要", 3000, true, PptxPrimaryColor),
		pptxContentTextBox(3, 1000000, 1300000, 10200000, 5100000, content),
		pptxSlideNumber(20, pageNum),
	)
}

func pptxTableSlideEnhanced(qr *QueryResult, pageNum int) string {
	maxRows := len(qr.Data)
	if maxRows > 15 {
		maxRows = 15
	}

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="F8F9FA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
`)

	sb.WriteString(pptxColorBarEnhanced(5, 0, 0, 12192000, 80000, PptxAccentColor))
	sb.WriteString(pptxTextBox(2, 1000000, 250000, 10200000, 700000, "数据明细", 2600, true, PptxPrimaryColor))

	numCols := len(qr.Columns)
	if numCols > 8 {
		numCols = 8
	}
	numRows := maxRows + 1

	tableWidth := int64(10500000)
	colWidth := tableWidth / int64(numCols)
	rowHeight := int64(330000)
	tableCy := int64(numRows) * rowHeight

	sb.WriteString(fmt.Sprintf(`<p:graphicFrame>
  <p:nvGraphicFramePr><p:cNvPr id="10" name="Table"/><p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr><p:nvPr/></p:nvGraphicFramePr>
  <p:xfrm><a:off x="1000000" y="1100000"/><a:ext cx="%d" cy="%d"/></p:xfrm>
  <a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">
  <a:tbl>
    <a:tblPr firstRow="1" bandRow="0"/>
    <a:tblGrid>`, tableWidth, tableCy))

	for i := 0; i < numCols; i++ {
		fmt.Fprintf(&sb, `<a:gridCol w="%d"/>`, colWidth)
	}
	sb.WriteString("</a:tblGrid>\n")

	sb.WriteString("<a:tr h=\"380000\">")
	for i := 0; i < numCols; i++ {
		fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr lIns="50000" rIns="50000" tIns="25000" bIns="25000"/><a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="zh-CN" sz="1100" b="1"><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:latin typeface="Microsoft YaHei"/><a:ea typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="1A237E"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(qr.Columns[i]))
	}
	sb.WriteString("</a:tr>\n")

	for r := 0; r < maxRows; r++ {
		row := qr.Data[r]
		sb.WriteString(fmt.Sprintf("<a:tr h=\"%d\">", rowHeight))
		for c := 0; c < numCols; c++ {
			val := ""
			if v, ok := row[qr.Columns[c]]; ok {
				val = fmt.Sprintf("%v", v)
			}
			if len([]rune(val)) > 28 {
				val = string([]rune(val)[:28]) + "\u2026"
			}
			fillColor := "FFFFFF"
			if r%2 == 1 {
				fillColor = "F0F4FF"
			}
			fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr lIns="50000" rIns="50000" tIns="18000" bIns="18000"/><a:p><a:r><a:rPr lang="zh-CN" sz="1050"><a:solidFill><a:srgbClr val="212121"/></a:solidFill><a:latin typeface="Microsoft YaHei"/><a:ea typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(val), fillColor)
		}
		sb.WriteString("</a:tr>\n")
	}

	sb.WriteString("</a:tbl></a:graphicData></a:graphic></p:graphicFrame>\n")

	if len(qr.Data) > 15 {
		hint := fmt.Sprintf("\u203B \u5171 %d \u884C\u6570\u636E\uFF0C\u4EE5\u4E0A\u4EC5\u5C55\u793A\u524D 15 \u884C", len(qr.Data))
		sb.WriteString(pptxTextBox(21, 1000000, tableCy+1300000, tableWidth, 350000, hint, 1200, false, "757575"))
	}

	sb.WriteString(pptxSlideNumber(22, pageNum))
	sb.WriteString("</p:spTree></p:cSld>\n</p:sld>")
	return sb.String()
}

func pptxHighlightSlideEnhanced(qr *QueryResult, pageNum int) string {
	var highlights []string
	highlights = append(highlights, fmt.Sprintf("\u25B6 数据集包含 %d 条记录，%d 个字段，数据完整度良好", len(qr.Data), len(qr.Columns)))

	numericCols := DetectNumericCols(qr)
	for _, col := range numericCols {
		_, max, avg, count := CalcNumericStats(qr, col)
		if count > 1 {
			highlights = append(highlights, fmt.Sprintf("\u25B6 %s — 均值：%.2f，峰值：%.2f", col, avg, max))
			if len(highlights) >= 8 {
				break
			}
		}
	}

	content := strings.Join(highlights, "\n\n")

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="F8F9FA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxColorBarEnhanced(5, 0, 0, 12192000, 80000, PptxAccentColor),
		pptxTextBox(2, 1000000, 300000, 10200000, 800000, "数据亮点", 3000, true, PptxPrimaryColor),
		pptxContentTextBox(3, 1000000, 1300000, 10200000, 5100000, content),
		pptxSlideNumber(20, pageNum),
	)
}

func pptxTopRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>
</Relationships>`
}

func pptxAppXml(slides int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">
  <Application>WebSQL AI</Application>
  <Slides>%d</Slides>
  <ScaleCrop>false</ScaleCrop>
  <LinksUpToDate>false</LinksUpToDate>
  <SharedDoc>false</SharedDoc>
  <HyperlinksChanged>false</HyperlinksChanged>
</Properties>`, slides)
}

func pptxCoreXml(title string) string {
	now := time.Now().Format("2006-01-02T15:04:05Z")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
                   xmlns:dc="http://purl.org/dc/elements/1.1/"
                   xmlns:dcterms="http://purl.org/dc/terms/"
                   xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <dc:title>%s</dc:title>
  <dc:creator>WebSQL AI</dc:creator>
  <dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created>
  <dcterms:modified xsi:type="dcterms:W3CDTF">%s</dcterms:modified>
</cp:coreProperties>`, xmlEscape(title), now, now)
}

func pptxTheme() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="WebSQL Professional">
  <a:themeElements>
    <a:clrScheme name="WebSQL">
      <a:dk1><a:srgbClr val="1A237E"/></a:dk1>
      <a:lt1><a:srgbClr val="FFFFFF"/></a:lt1>
      <a:dk2><a:srgbClr val="283593"/></a:dk2>
      <a:lt2><a:srgbClr val="F5F7FA"/></a:lt2>
      <a:accent1><a:srgbClr val="00BCD4"/></a:accent1>
      <a:accent2><a:srgbClr val="4CAF50"/></a:accent2>
      <a:accent3><a:srgbClr val="FF9800"/></a:accent3>
      <a:accent4><a:srgbClr val="E91E63"/></a:accent4>
      <a:accent5><a:srgbClr val="5B9BD5"/></a:accent5>
      <a:accent6><a:srgbClr val="1A237E"/></a:accent6>
      <a:hlink><a:srgbClr val="00BCD4"/></a:hlink>
      <a:folHlink><a:srgbClr val="9C27B0"/></a:folHlink>
    </a:clrScheme>
    <a:fontScheme name="WebSQL">
      <a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface="Microsoft YaHei"/></a:majorFont>
      <a:minorFont><a:latin typeface="Calibri"/><a:ea typeface="Microsoft YaHei"/></a:minorFont>
    </a:fontScheme>
    <a:fmtScheme name="WebSQL">
      <a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst>
      <a:lnStyleLst><a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="12700"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="19050"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst>
      <a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>
      <a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst>
    </a:fmtScheme>
  </a:themeElements>
</a:theme>`
}

func pptxContentTypes(slides int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Default Extension="png" ContentType="image/png"/>
  <Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>
  <Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>
  <Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>
  <Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
  <Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>`)
	for i := 1; i <= slides; i++ {
		fmt.Fprintf(&sb, `
  <Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, i)
	}
	sb.WriteString(`
</Types>`)
	return sb.String()
}

func pptxPresentation(slides int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
                xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
                xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rIdMaster"/></p:sldMasterIdLst>
  <p:sldIdLst>`)
	for i := 1; i <= slides; i++ {
		fmt.Fprintf(&sb, `<p:sldId id="%d" r:id="rIdSlide%d"/>`, 255+i, i)
	}
	sb.WriteString(`</p:sldIdLst>
  <p:sldSz cx="12192000" cy="6858000"/>
  <p:notesSz cx="6858000" cy="9144000"/>
</p:presentation>`)
	return sb.String()
}

func pptxPresentationRels(slides int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdMaster" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>
  <Relationship Id="rIdTheme" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/>`)
	for i := 1; i <= slides; i++ {
		fmt.Fprintf(&sb, `
  <Relationship Id="rIdSlide%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, i, i)
	}
	sb.WriteString(`
</Relationships>`)
	return sb.String()
}

func pptxSlideMaster() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldMaster xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
             xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
             xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:cSld name="Blank">
    <p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
    <p:spTree>
      <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
      <p:grpSpPr/>
      <p:sp>
        <p:nvSpPr><p:cNvPr id="2" name="Title Placeholder 1"/><p:cNvSpPr txBox="1"/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
        <p:spPr><a:xfrm><a:off x="457200" y="274638"/><a:ext cx="8229600" cy="1371600"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>
        <p:style><a:lnStyleLst><a:ln w="0"><a:solidFill><a:schemeClr val="accent1"/></a:solidFill></a:ln></a:lnStyleLst></p:style>
        <p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US" sz="4400" dirty="0"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mj-lt"/><a:ea typeface="+mj-ea"/></a:rPr><a:t>Click to add title</a:t></a:r></a:p></p:txBody>
      </p:sp>
      <p:sp>
        <p:nvSpPr><p:cNvPr id="3" name="Content Placeholder 2"/><p:cNvSpPr txBox="1"/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr>
        <p:spPr><a:xfrm><a:off x="457200" y="1828800"/><a:ext cx="8229600" cy="4572000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>
        <p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US" sz="2400" dirty="0"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill></a:endParaRPr></a:p></p:txBody>
      </p:sp>
    </p:spTree>
  </p:cSld>
  <p:txStyles>
    <a:txDefRPr lang="en-US" sz="1800" dirty="0">
      <a:solidFill><a:schemeClr val="tx1"/></a:solidFill>
      <a:latin typeface="+mn-lt"/>
      <a:ea typeface="+mn-ea"/>
    </a:txDefRPr>
    <a:txBodyStyle>
      <a:lstStyle>
        <a:lvl1pPr marL="0" algn="l" defTabSz="914400" rtl="0" eaLnBrk="1" latinLnBrk="0" hangPunct="1">
          <a:spcBef><a:spcPts val="0"/></a:spcBef>
          <a:spcAft><a:spcPts val="0"/></a:spcAft>
          <a:defRPr sz="1800" dirty="0">
            <a:solidFill><a:schemeClr val="tx1"/></a:solidFill>
            <a:latin typeface="+mn-lt"/>
            <a:ea typeface="+mn-ea"/>
          </a:defRPr>
        </a:lvl1pPr>
      </a:lstStyle>
    </a:txBodyStyle>
  </p:txStyles>
  <p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>
  <p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rIdLayout"/></p:sldLayoutIdLst>
</p:sldMaster>`
}

func pptxSlideMasterRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdLayout" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
  <Relationship Id="rIdTheme" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/>
</Relationships>`
}

func pptxSlideLayout() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
             xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
             xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
             type="blank" showMasterSp="0">
  <p:cSld name="Blank">
    <p:spTree>
      <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
      <p:grpSpPr/>
      <p:sp>
        <p:nvSpPr><p:cNvPr id="2" name="Title Placeholder 1"/><p:cNvSpPr txBox="1"/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
        <p:spPr><a:xfrm><a:off x="457200" y="274638"/><a:ext cx="8229600" cy="1371600"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>
        <p:style><a:lnStyleLst><a:ln w="0"><a:solidFill><a:schemeClr val="accent1"/></a:solidFill></a:ln></a:lnStyleLst></p:style>
        <p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US" sz="4400" dirty="0"/></a:p></p:txBody>
      </p:sp>
      <p:sp>
        <p:nvSpPr><p:cNvPr id="3" name="Content Placeholder 2"/><p:cNvSpPr txBox="1"/><p:nvPr><p:ph idx="1"/></p:nvPr></p:nvSpPr>
        <p:spPr><a:xfrm><a:off x="457200" y="1828800"/><a:ext cx="8229600" cy="4572000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>
        <p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US" sz="2400" dirty="0"/></a:p></p:txBody>
      </p:sp>
    </p:spTree>
  </p:cSld>
  <p:clrMapOvr><a:overrideClrMapping bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/></p:clrMapOvr>
</p:sldLayout>`
}

func pptxSlideLayoutRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdMaster" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/>
</Relationships>`
}

func pptxSlideRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdLayout" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
</Relationships>`
}

func pptxSlideRelsWithImage(hasImage bool, imageIdx int) string {
	if !hasImage {
		return pptxSlideRels()
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdLayout" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
  <Relationship Id="rIdImg" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/chart_%d.png"/>
</Relationships>`, imageIdx)
}

func pptxTextBox(id int, x, y, cx, cy int64, text string, fontSize int, bold bool, fontColor string) string {
	boldAttr := ""
	if bold {
		boldAttr = ` b="1"`
	}
	colorXml := ""
	if fontColor != "" {
		colorXml = fmt.Sprintf(`<a:solidFill><a:srgbClr val="%s"/></a:solidFill>`, fontColor)
	}
	lines := strings.Split(text, "\n")
	var paragraphs strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&paragraphs, `<a:p><a:r><a:rPr lang="zh-CN" sz="%d"%s>%s<a:latin typeface="Microsoft YaHei"/><a:ea typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r></a:p>`, fontSize, boldAttr, colorXml, xmlEscape(line))
	}
	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="T%d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>
  <p:txBody><a:bodyPr wrap="square" rtlCol="0" lIns="40000" rIns="40000"/>%s</p:txBody>
</p:sp>
`, id, id, x, y, cx, cy, paragraphs.String())
}

func pptxContentTextBox(id int, x, y, cx, cy int64, text string) string {
	lines := strings.Split(text, "\n")
	var paragraphs strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			fmt.Fprintf(&paragraphs, `<a:p><a:pPr marL="0" indent="0"><a:spcBef><a:spcPts val="800"/></a:spcBef></a:pPr><a:endParaRPr lang="zh-CN" sz="1400"><a:solidFill><a:srgbClr val="424242"/></a:solidFill></a:endParaRPr></a:p>`)
			continue
		}
		isBold := strings.HasPrefix(strings.TrimSpace(line), "\u25CF") || strings.HasPrefix(strings.TrimSpace(line), "\u25B6") || strings.HasPrefix(strings.TrimSpace(line), "\u25B8")
		boldAttr := ""
		highlightColor := "424242"
		if isBold {
			boldAttr = ` b="1"`
			highlightColor = PptxPrimaryColor
		}
		fmt.Fprintf(&paragraphs, `<a:p><a:r><a:rPr lang="zh-CN" sz="1500"%s><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="Microsoft YaHei"/><a:ea typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r></a:p>`, boldAttr, highlightColor, xmlEscape(line))
	}
	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="CT%d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>
  <p:txBody><a:bodyPr wrap="square" rtlCol="0" lIns="80000" rIns="80000"/>%s</p:txBody>
</p:sp>
`, id, id, x, y, cx, cy, paragraphs.String())
}

func pptxShape(id int, x, y, cx, cy int64, color string) string {
	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="Shape%d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr>
</p:sp>
`, id, id, x, y, cx, cy, color)
}

func pptxColorBarEnhanced(id int, x, y, cx, cy int64, color string) string {
	return pptxShape(id, x, y, cx, cy, color)
}

func pptxChartImage(id int) string {
	return fmt.Sprintf(`<p:pic>
  <p:nvPicPr><p:cNvPr id="%d" name="Chart"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr>
  <p:blipFill><a:blip r:embed="rIdImg"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>
  <p:spPr><a:xfrm><a:off x="1000000" y="1400000"/><a:ext cx="10200000" cy="4900000"/></a:xfrm>
  <a:prstGeom prst="rect"><a:avLst/></a:prstGeom>
  <a:effectLst><a:outerShdw blurRad="50000" dist="20000" dir="5400000"><a:srgbClr val="000000"><a:alpha val="15000"/></a:srgbClr></a:outerShdw></a:effectLst></p:spPr>
</p:pic>`, id)
}

func pptxSlideNumber(id int, pageNum int) string {
	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="PN"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="11000000" y="6500000"/><a:ext cx="1000000" cy="280000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>
  <p:txBody><a:bodyPr wrap="square" rtlCol="0"/><a:p><a:pPr algn="r"/><a:r><a:rPr lang="en-US" sz="1050"><a:solidFill><a:srgbClr val="BDBDBD"/></a:solidFill></a:rPr><a:t>%d / %d</a:t></a:r></a:p></p:txBody>
</p:sp>
`, id, pageNum, pageNum)
}

func GeneratePptxFromContent(content, title, outputPath string) (int, error) {
	blocks := ParseMarkdownBlocks(content)

	var sections []SlideSection
	var current SlideSection

	for _, block := range blocks {
		if block.Type == "h1" || block.Type == "h2" {
			if current.Title != "" || len(current.Blocks) > 0 {
				sections = append(sections, current)
			}
			current = SlideSection{Title: StripMarkdownFormatting(block.Content)}
		} else {
			current.Blocks = append(current.Blocks, block)
		}
	}
	if current.Title != "" || len(current.Blocks) > 0 {
		sections = append(sections, current)
	}

	if len(sections) == 0 {
		sections = append(sections, SlideSection{Title: title, Blocks: blocks})
	}

	slideCount := len(sections) + 1

	f, err := os.Create(outputPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	writeZipEntry(zw, "[Content_Types].xml", pptxContentTypes(slideCount))
	writeZipEntry(zw, "_rels/.rels", pptxTopRels())
	writeZipEntry(zw, "docProps/app.xml", pptxAppXml(slideCount))
	writeZipEntry(zw, "docProps/core.xml", pptxCoreXml(title))
	writeZipEntry(zw, "ppt/presentation.xml", pptxPresentation(slideCount))
	writeZipEntry(zw, "ppt/_rels/presentation.xml.rels", pptxPresentationRels(slideCount))
	writeZipEntry(zw, "ppt/theme/theme1.xml", pptxTheme())
	writeZipEntry(zw, "ppt/slideMasters/slideMaster1.xml", pptxSlideMaster())
	writeZipEntry(zw, "ppt/slideMasters/_rels/slideMaster1.xml.rels", pptxSlideMasterRels())
	writeZipEntry(zw, "ppt/slideLayouts/slideLayout1.xml", pptxSlideLayout())
	writeZipEntry(zw, "ppt/slideLayouts/_rels/slideLayout1.xml.rels", pptxSlideLayoutRels())

	writeZipEntry(zw, "ppt/slides/slide1.xml", pptxTitleSlideEnhanced(title))
	writeZipEntry(zw, "ppt/slides/_rels/slide1.xml.rels", pptxSlideRels())

	for i, sec := range sections {
		slideNum := i + 2
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", slideNum), pptxContentSlide(sec.Title, sec.Blocks, slideNum))
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", slideNum), pptxSlideRels())
	}

	return slideCount, nil
}

func pptxContentSlide(slideTitle string, blocks []MdBlock, pageNum int) string {
	var textLines []string
	type tableBlock struct {
		mdTable  string
		shapeXML string
		height   int64
	}
	var tables []tableBlock

	for _, block := range blocks {
		if block.Type == "table" {
			shapeXML, h := pptxContentTableXML(block.Content, len(tables)+10)
			tables = append(tables, tableBlock{mdTable: block.Content, shapeXML: shapeXML, height: h})
			continue
		}
		switch block.Type {
		case "h3":
			textLines = append(textLines, "\u25B8 "+StripMarkdownFormatting(block.Content))
		case "paragraph":
			textLines = append(textLines, StripMarkdownFormatting(block.Content))
		case "list":
			for _, item := range strings.Split(block.Content, "\n") {
				textLines = append(textLines, "\u2022 "+StripMarkdownFormatting(item))
			}
		case "code":
			maxShow := 8
			codeLines := strings.Split(block.Content, "\n")
			if len(codeLines) < maxShow {
				maxShow = len(codeLines)
			}
			for j := 0; j < maxShow; j++ {
				if len([]rune(codeLines[j])) > 60 {
					codeLines[j] = string([]rune(codeLines[j])[:60]) + "\u2026"
				}
				textLines = append(textLines, "  "+codeLines[j])
			}
			if len(codeLines) > maxShow {
				textLines = append(textLines, fmt.Sprintf("  ... \u5171 %d \u884c", len(codeLines)))
			}
		case "mermaid":
			textLines = append(textLines, "[Mermaid \u56fe\u8868]")
			mermaidLines := strings.Split(block.Content, "\n")
			for j := 0; j < len(mermaidLines) && j < 8; j++ {
				textLines = append(textLines, "  "+mermaidLines[j])
			}
			if len(mermaidLines) > 8 {
				textLines = append(textLines, fmt.Sprintf("  ... \u5171 %d \u884c", len(mermaidLines)))
			}
		}
	}

	content := strings.Join(textLines, "\n")
	if content == "" && len(tables) == 0 {
		content = slideTitle
	}

	pageId := pageNum + 100
	tableX := int64(1000000)
	tableW := int64(10200000)
	textX := int64(1000000)

	var shapes strings.Builder
	shapes.WriteString(pptxColorBarEnhanced(5, 0, 0, 12192000, 80000, PptxAccentColor))
	shapes.WriteString(pptxTextBox(2, 1000000, 300000, 10200000, 800000, slideTitle, 3000, true, PptxPrimaryColor))

	if len(tables) > 0 {
		curY := int64(1200000)
		for _, tb := range tables {
			posXML := strings.Replace(tb.shapeXML, `a:off x="0" y="0"`, fmt.Sprintf(`a:off x="%d" y="%d"`, tableX, curY), 1)
			shapes.WriteString(posXML)
			curY += tb.height + 200000
		}
		if content != "" {
			remainingH := int64(6858000) - curY - 300000
			if remainingH < 300000 {
				remainingH = 300000
			}
			shapes.WriteString(pptxContentTextBox(200, textX, curY, tableW, remainingH, content))
		}
	} else {
		shapes.WriteString(pptxContentTextBox(3, 1000000, 1300000, 10200000, 5100000, content))
	}

	shapes.WriteString(pptxSlideNumber(pageId, pageNum))

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="F8F9FA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
%s
</p:spTree></p:cSld>
</p:sld>`, shapes.String())
}

func pptxContentTableXML(mdTable string, idStart int) (string, int64) {
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
		return "", 0
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
		return "", 0
	}

	cols := rows[0]
	numCols := len(cols)
	if numCols > 8 {
		numCols = 8
	}
	numRows := len(rows)
	if numRows > 12 {
		numRows = 12
	}

	colWidth := int64(10500000) / int64(numCols)
	rowHeight := int64(310000)
	tableH := int64(numRows)*rowHeight + 50000

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<p:graphicFrame>
  <p:nvGraphicFramePr><p:cNvPr id="%d" name="Table"/><p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr><p:nvPr/></p:nvGraphicFramePr>
  <p:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></p:xfrm>
  <a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">
  <a:tbl>
    <a:tblPr firstRow="1" bandRow="0"/>
    <a:tblGrid>`, idStart, int64(10500000), tableH))

	for i := 0; i < numCols; i++ {
		fmt.Fprintf(&sb, `<a:gridCol w="%d"/>`, colWidth)
	}
	sb.WriteString("</a:tblGrid>\n")

	sb.WriteString("<a:tr h=\"360000\">")
	for i := 0; i < numCols && i < len(cols); i++ {
		fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr lIns="50000" rIns="50000" tIns="20000" bIns="20000"/><a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="zh-CN" sz="1100" b="1"><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:latin typeface="Microsoft YaHei"/><a:ea typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="1A237E"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(cols[i]))
	}
	sb.WriteString("</a:tr>\n")

	for r := 1; r < numRows; r++ {
		row := rows[r]
		sb.WriteString(fmt.Sprintf("<a:tr h=\"%d\">", rowHeight))
		for c := 0; c < numCols; c++ {
			val := ""
			if c < len(row) {
				val = row[c]
			}
			if len([]rune(val)) > 25 {
				val = string([]rune(val)[:25]) + "\u2026"
			}
			fillColor := "FFFFFF"
			if r%2 == 0 {
				fillColor = "F0F4FF"
			}
			fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr lIns="50000" rIns="50000" tIns="15000" bIns="15000"/><a:p><a:r><a:rPr lang="zh-CN" sz="1000"><a:solidFill><a:srgbClr val="212121"/></a:solidFill><a:latin typeface="Microsoft YaHei"/><a:ea typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(val), fillColor)
		}
		sb.WriteString("</a:tr>\n")
	}

	sb.WriteString("</a:tbl></a:graphicData></a:graphic></p:graphicFrame>\n")

	if len(rows) > 12 {
		hint := fmt.Sprintf("\u203B \u5171 %d \u884c\uff0c\u4ec5\u5c55\u793a\u524d 12 \u884c", len(rows))
		residualH := tableH + 1100000
		sb.WriteString(pptxTextBox(idStart+50, 1000000, residualH, 10200000, 300000, hint, 1050, false, "757575"))
		tableH += 500000
	}

	return sb.String(), tableH
}
