// PPTX 生成器 — 直接构建 Office Open XML
//
// PPTX 文件本质是 ZIP 包。为确保 PowerPoint 能正常打开（不提示修复），
// 必须包含完整的 Content_Types、theme、docProps 等必要文件。
//
// 生成策略：
//
//	Slide 1 — 标题页（标题 + 副标题 + 生成时间）
//	Slide 2 — 数据摘要（列名、行数、统计）
//	Slide 3 — 数据表格（前 15 行预览）
//	Slide 4 — 关键数据亮点（如果数据量足够）
package agentv2

import (
	"archive/zip"
	"fmt"
	"os"
	"strings"
	"time"
)

// generatePptx 生成 PPTX 文件，返回幻灯片数量
func generatePptx(qr *queryResult, title, outputPath string) (int, error) {
	f, err := createFile(outputPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	slideCount := 3
	if len(qr.Data) > 5 {
		slideCount = 4 // 增加数据亮点页
	}

	// [Content_Types].xml
	writeZipEntry(zw, "[Content_Types].xml", pptxContentTypes(slideCount))

	// _rels/.rels
	writeZipEntry(zw, "_rels/.rels", pptxTopRels())

	// docProps/app.xml
	writeZipEntry(zw, "docProps/app.xml", pptxAppXml(slideCount))

	// docProps/core.xml
	writeZipEntry(zw, "docProps/core.xml", pptxCoreXml(title))

	// ppt/presentation.xml
	writeZipEntry(zw, "ppt/presentation.xml", pptxPresentation(slideCount))

	// ppt/_rels/presentation.xml.rels
	writeZipEntry(zw, "ppt/_rels/presentation.xml.rels", pptxPresentationRels(slideCount))

	// ppt/theme/theme1.xml (必须)
	writeZipEntry(zw, "ppt/theme/theme1.xml", pptxTheme())

	// Slide Master & Layout
	writeZipEntry(zw, "ppt/slideMasters/slideMaster1.xml", pptxSlideMaster())
	writeZipEntry(zw, "ppt/slideMasters/_rels/slideMaster1.xml.rels", pptxSlideMasterRels())
	writeZipEntry(zw, "ppt/slideLayouts/slideLayout1.xml", pptxSlideLayout())
	writeZipEntry(zw, "ppt/slideLayouts/_rels/slideLayout1.xml.rels", pptxSlideLayoutRels())

	// Slide 1 — 标题页
	writeZipEntry(zw, "ppt/slides/slide1.xml", pptxTitleSlide(title))
	writeZipEntry(zw, "ppt/slides/_rels/slide1.xml.rels", pptxSlideRels())

	// Slide 2 — 数据摘要
	writeZipEntry(zw, "ppt/slides/slide2.xml", pptxSummarySlide(qr))
	writeZipEntry(zw, "ppt/slides/_rels/slide2.xml.rels", pptxSlideRels())

	// Slide 3 — 数据表格
	writeZipEntry(zw, "ppt/slides/slide3.xml", pptxTableSlide(qr))
	writeZipEntry(zw, "ppt/slides/_rels/slide3.xml.rels", pptxSlideRels())

	// Slide 4 — 数据亮点（可选）
	if slideCount >= 4 {
		writeZipEntry(zw, "ppt/slides/slide4.xml", pptxHighlightSlide(qr))
		writeZipEntry(zw, "ppt/slides/_rels/slide4.xml.rels", pptxSlideRels())
	}

	return slideCount, nil
}

// ──────────────────────────────────────────────
// 顶层关系和属性文件
// ──────────────────────────────────────────────

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

// ──────────────────────────────────────────────
// Theme (必须包含，否则 PowerPoint 会提示修复)
// ──────────────────────────────────────────────

func pptxTheme() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Office Theme">
  <a:themeElements>
    <a:clrScheme name="Office">
      <a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1>
      <a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>
      <a:dk2><a:srgbClr val="44546A"/></a:dk2>
      <a:lt2><a:srgbClr val="E7E6E6"/></a:lt2>
      <a:accent1><a:srgbClr val="4472C4"/></a:accent1>
      <a:accent2><a:srgbClr val="ED7D31"/></a:accent2>
      <a:accent3><a:srgbClr val="A5A5A5"/></a:accent3>
      <a:accent4><a:srgbClr val="FFC000"/></a:accent4>
      <a:accent5><a:srgbClr val="5B9BD5"/></a:accent5>
      <a:accent6><a:srgbClr val="70AD47"/></a:accent6>
      <a:hlink><a:srgbClr val="0563C1"/></a:hlink>
      <a:folHlink><a:srgbClr val="954F72"/></a:folHlink>
    </a:clrScheme>
    <a:fontScheme name="Office">
      <a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface=""/></a:majorFont>
      <a:minorFont><a:latin typeface="Calibri"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface=""/></a:minorFont>
    </a:fontScheme>
    <a:fmtScheme name="Office">
      <a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst>
      <a:lnStyleLst><a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="12700"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="19050"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst>
      <a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>
      <a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst>
    </a:fmtScheme>
  </a:themeElements>
</a:theme>`
}

// ──────────────────────────────────────────────
// PPTX XML 模板
// ──────────────────────────────────────────────

func pptxContentTypes(slides int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
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
  <p:cSld><p:bg><p:bgPr><a:solidFill><a:schemeClr val="bg1"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/></p:spTree></p:cSld>
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
             type="blank">
  <p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/></p:spTree></p:cSld>
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

// ──────────────────────────────────────────────
// 幻灯片内容
// ──────────────────────────────────────────────

// pptxTitleSlide 标题页 — 带渐变背景和副标题
func pptxTitleSlide(title string) string {
	now := time.Now().Format("2006-01-02 15:04")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr><a:gradFill><a:gsLst>
    <a:gs pos="0"><a:srgbClr val="1A237E"/></a:gs>
    <a:gs pos="100000"><a:srgbClr val="283593"/></a:gs>
  </a:gsLst><a:lin ang="5400000" scaled="1"/></a:gradFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxTextBox(2, 600000, 1800000, 10800000, 1800000, title, 4000, true, "FFFFFF"),
		pptxTextBox(3, 600000, 3800000, 10800000, 600000, "数据分析报告", 2000, false, "B0BEC5"),
		pptxTextBox(4, 600000, 4600000, 10800000, 500000, "生成时间："+now, 1400, false, "78909C"),
	)
}

// pptxSummarySlide 数据摘要页
func pptxSummarySlide(qr *queryResult) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("📊 总记录数：%d 条", len(qr.Data)))
	lines = append(lines, fmt.Sprintf("📋 数据列数：%d 列", len(qr.Columns)))
	lines = append(lines, "")
	lines = append(lines, "字段列表：")
	for i, col := range qr.Columns {
		if i >= 20 {
			lines = append(lines, fmt.Sprintf("  ... 共 %d 列，仅显示前 20 列", len(qr.Columns)))
			break
		}
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1, col))
	}

	content := strings.Join(lines, "\n")

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="FAFAFA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxColorBar(5, 0, 0, 12192000, 80000, "3F51B5"),
		pptxTextBox(2, 600000, 300000, 10800000, 800000, "数据摘要", 2800, true, "212121"),
		pptxTextBox(3, 600000, 1300000, 10800000, 5000000, content, 1600, false, "424242"),
	)
}

// pptxTableSlide 数据表格页（前 15 行）
func pptxTableSlide(qr *queryResult) string {
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
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="FAFAFA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
`)

	// 顶部色条
	sb.WriteString(pptxColorBar(5, 0, 0, 12192000, 80000, "3F51B5"))

	// 标题
	sb.WriteString(pptxTextBox(2, 600000, 200000, 10800000, 600000, "数据预览", 2400, true, "212121"))

	// 表格
	numCols := len(qr.Columns)
	if numCols > 10 {
		numCols = 10
	}
	numRows := maxRows + 1

	colWidth := int64(10800000 / numCols)
	rowHeight := int64(300000)

	sb.WriteString(fmt.Sprintf(`<p:graphicFrame>
  <p:nvGraphicFramePr><p:cNvPr id="10" name="Table"/><p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr><p:nvPr/></p:nvGraphicFramePr>
  <p:xfrm><a:off x="600000" y="900000"/><a:ext cx="10800000" cy="%d"/></p:xfrm>
  <a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">
  <a:tbl>
    <a:tblPr firstRow="1" bandRow="1"><a:noFill/></a:tblPr>
    <a:tblGrid>`, int64(numRows)*rowHeight))

	for i := 0; i < numCols; i++ {
		fmt.Fprintf(&sb, `<a:gridCol w="%d"/>`, colWidth)
	}
	sb.WriteString("</a:tblGrid>\n")

	// Header row
	sb.WriteString("<a:tr h=\"350000\">")
	for i := 0; i < numCols; i++ {
		fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:rPr lang="zh-CN" sz="1100" b="1" dirty="0"><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="3F51B5"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(qr.Columns[i]))
	}
	sb.WriteString("</a:tr>\n")

	// Data rows
	for r := 0; r < maxRows; r++ {
		row := qr.Data[r]
		sb.WriteString(fmt.Sprintf("<a:tr h=\"%d\">", rowHeight))
		for c := 0; c < numCols; c++ {
			val := ""
			if v, ok := row[qr.Columns[c]]; ok {
				val = fmt.Sprintf("%v", v)
			}
			if len([]rune(val)) > 20 {
				val = string([]rune(val)[:20]) + "…"
			}
			fillColor := "FFFFFF"
			if r%2 == 1 {
				fillColor = "E8EAF6"
			}
			fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:rPr lang="zh-CN" sz="1000" dirty="0"/><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(val), fillColor)
		}
		sb.WriteString("</a:tr>\n")
	}

	sb.WriteString("</a:tbl></a:graphicData></a:graphic></p:graphicFrame>\n")

	if len(qr.Data) > 15 {
		hint := fmt.Sprintf("共 %d 行，仅展示前 15 行", len(qr.Data))
		sb.WriteString(pptxTextBox(20, 600000, 6200000, 10800000, 400000, hint, 1200, false, "757575"))
	}

	sb.WriteString("</p:spTree></p:cSld>\n</p:sld>")
	return sb.String()
}

// pptxHighlightSlide 数据亮点页
func pptxHighlightSlide(qr *queryResult) string {
	var highlights []string
	highlights = append(highlights, fmt.Sprintf("数据集包含 %d 条记录，%d 个字段", len(qr.Data), len(qr.Columns)))

	// 尝试找到数值列并计算简单统计
	for _, col := range qr.Columns {
		if len(qr.Data) == 0 {
			break
		}
		// 尝试将第一个值转为数字
		firstVal := qr.Data[0][col]
		if _, err := toFloat64(firstVal); err == nil {
			var sum, min, max float64
			min = 1e18
			max = -1e18
			count := 0
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
				avg := sum / float64(count)
				highlights = append(highlights, fmt.Sprintf("字段 [%s]：最小值 %.2f，最大值 %.2f，平均值 %.2f", col, min, max, avg))
				if len(highlights) >= 6 {
					break
				}
			}
		}
	}

	content := strings.Join(highlights, "\n\n")

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld>
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="FAFAFA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxColorBar(5, 0, 0, 12192000, 80000, "3F51B5"),
		pptxTextBox(2, 600000, 300000, 10800000, 800000, "数据亮点", 2800, true, "212121"),
		pptxTextBox(3, 600000, 1300000, 10800000, 5000000, content, 1600, false, "424242"),
	)
}

// pptxTextBox 生成文本框 shape XML（支持自定义颜色）
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
		fmt.Fprintf(&paragraphs, `<a:p><a:r><a:rPr lang="zh-CN" sz="%d"%s dirty="0">%s</a:rPr><a:t>%s</a:t></a:r></a:p>`, fontSize, boldAttr, colorXml, xmlEscape(line))
	}

	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="TextBox%d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>
  <p:txBody><a:bodyPr wrap="square" rtlCol="0"/>%s</p:txBody>
</p:sp>
`, id, id, x, y, cx, cy, paragraphs.String())
}

// pptxColorBar 生成顶部装饰色条
func pptxColorBar(id int, x, y, cx, cy int64, color string) string {
	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="Bar%d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr>
</p:sp>
`, id, id, x, y, cx, cy, color)
}

func createFile(path string) (*os.File, error) {
	return os.Create(path)
}
