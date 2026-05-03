// PPTX 生成器 — 直接构建 Office Open XML
//
// PPTX 文件本质是 ZIP 包。为确保 PowerPoint 能正常打开（不提示修复），
// 必须包含完整的 Content_Types、theme、docProps 等必要文件。
//
// 设计理念：
//   - 使用统一的品牌色系 (Navy #1A237E + Cyan #00BCD4)
//   - 标题页：深色渐变背景 + 装饰元素
//   - 内容页：顶部色条 + 清晰标题区 + 内容区 + 右下页码
//   - 表格：深色表头 + 交替行色 + 细边框
//   - 数据亮点：卡片式展示
package agentv2

import (
	"archive/zip"
	"fmt"
	"os"
	"strings"
	"time"
)

func generatePptx(qr *queryResult, title string, chartPaths []string, outputPath string) (int, error) {
	f, err := createFile(outputPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	hasChart := len(chartPaths) > 0
	slideCount := 3
	if len(qr.Data) > 5 {
		slideCount = 4
	}
	if hasChart {
		slideCount++
	}

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

	writeZipEntry(zw, "ppt/slides/slide1.xml", pptxTitleSlide(title))
	writeZipEntry(zw, "ppt/slides/_rels/slide1.xml.rels", pptxSlideRelsWithImage(hasChart, 0))

	currentSlide := 2

	if hasChart {
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", currentSlide), pptxChartSlide(title, currentSlide))
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", currentSlide), pptxSlideRelsWithImage(true, 0))
		currentSlide++
	}

	writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", currentSlide), pptxSummarySlide(qr))
	writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", currentSlide), pptxSlideRels())
	currentSlide++

	writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", currentSlide), pptxTableSlide(qr))
	writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", currentSlide), pptxSlideRels())
	currentSlide++

	if slideCount > currentSlide-1 {
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", currentSlide), pptxHighlightSlide(qr))
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", currentSlide), pptxSlideRels())
	}

	if hasChart {
		_ = writeZipFile(zw, "ppt/media/chart_0.png", chartPaths[0])
	}

	return slideCount, nil
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
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="WebSQL Theme">
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
      <a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface=""/></a:majorFont>
      <a:minorFont><a:latin typeface="Calibri"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface=""/></a:minorFont>
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
  <p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="F8F9FA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
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

func pptxChartSlide(title string, pageNum int) string {
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
		pptxColorBar(5, 0, 0, 12192000, 60000, "00BCD4"),
		pptxTextBox(2, 800000, 300000, 10500000, 700000, title+" · 数据洞察", 2400, true, "1A237E"),
		pptxChartImage(10),
		pptxSlideNumber(20, pageNum),
	)
}

func pptxChartImage(id int) string {
	return fmt.Sprintf(`<p:pic>
  <p:nvPicPr><p:cNvPr id="%d" name="Chart"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr>
  <p:blipFill><a:blip r:embed="rIdImg"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>
  <p:spPr><a:xfrm><a:off x="800000" y="1200000"/><a:ext cx="10500000" cy="5200000"/></a:xfrm>
  <a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>
</p:pic>`, id)
}

func pptxTitleSlide(title string) string {
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
</p:spTree></p:cSld>
</p:sld>`,
		pptxTitleDecoLine(2, 1400000, 2800000, 400000, 4, "00BCD4"),
		pptxTextBox(3, 1400000, 2000000, 9400000, 2000000, title, 4000, true, "FFFFFF"),
		pptxTextBox(4, 1400000, 3900000, 9400000, 500000, "数据分析演示报告", 2000, false, "80CBC4"),
		pptxTextBox(5, 1400000, 4500000, 9400000, 400000, "生成时间："+now, 1400, false, "78909C"),
	)
}

func pptxSummarySlide(qr *queryResult) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("总记录数：%d 条", len(qr.Data)))
	lines = append(lines, fmt.Sprintf("数据列数：%d 列", len(qr.Columns)))
	lines = append(lines, "")

	numericCols := detectNumericCols(qr)
	if len(numericCols) > 0 {
		lines = append(lines, "数值字段：")
		for _, nc := range numericCols {
			lines = append(lines, fmt.Sprintf("  \u2022 %s", nc))
		}
		lines = append(lines, "")
	}

	lines = append(lines, "完整字段列表：")
	for i, col := range qr.Columns {
		if i >= 20 {
			lines = append(lines, fmt.Sprintf("  ... 共 %d 列，仅显示前 20 列", len(qr.Columns)))
			break
		}
		marker := "  \u25CB"
		for _, nc := range numericCols {
			if nc == col {
				marker = "  \u25CF"
				break
			}
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, col))
	}

	content := strings.Join(lines, "\n")

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
		pptxColorBar(5, 0, 0, 12192000, 60000, "00BCD4"),
		pptxTextBox(2, 800000, 300000, 10500000, 700000, "数据摘要", 2800, true, "1A237E"),
		pptxContentTextBox(3, 800000, 1200000, 10500000, 5200000, content),
		pptxSlideNumber(20, 2),
	)
}

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
  <p:bg><p:bgPr><a:solidFill><a:srgbClr val="F8F9FA"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>
  <p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
`)

	sb.WriteString(pptxColorBar(5, 0, 0, 12192000, 60000, "00BCD4"))
	sb.WriteString(pptxTextBox(2, 800000, 200000, 10500000, 600000, "数据明细", 2400, true, "1A237E"))

	numCols := len(qr.Columns)
	if numCols > 8 {
		numCols = 8
	}
	numRows := maxRows + 1

	tableWidth := int64(10500000)
	colWidth := tableWidth / int64(numCols)
	rowHeight := int64(310000)
	tableCy := int64(numRows) * rowHeight

	sb.WriteString(fmt.Sprintf(`<p:graphicFrame>
  <p:nvGraphicFramePr><p:cNvPr id="10" name="Table"/><p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr><p:nvPr/></p:nvGraphicFramePr>
  <p:xfrm><a:off x="800000" y="950000"/><a:ext cx="%d" cy="%d"/></p:xfrm>
  <a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">
  <a:tbl>
    <a:tblPr firstRow="1" bandRow="0">
      <a:tableStyleId>{5940675A-B579-460E-94D1-54222C63F5DA}</a:tableStyleId>
    </a:tblPr>
    <a:tblGrid>`, tableWidth, tableCy))

	for i := 0; i < numCols; i++ {
		fmt.Fprintf(&sb, `<a:gridCol w="%d"/>`, colWidth)
	}
	sb.WriteString("</a:tblGrid>\n")

	sb.WriteString("<a:tr h=\"360000\">")
	for i := 0; i < numCols; i++ {
		fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr lIns="40000" rIns="40000" tIns="20000" bIns="20000"/><a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="zh-CN" sz="1100" b="1"><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="1A237E"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(qr.Columns[i]))
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
			if len([]rune(val)) > 25 {
				val = string([]rune(val)[:25]) + "\u2026"
			}
			fillColor := "FFFFFF"
			if r%2 == 1 {
				fillColor = "EDEFF7"
			}
			fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr lIns="40000" rIns="40000" tIns="15000" bIns="15000"/><a:p><a:r><a:rPr lang="zh-CN" sz="1000"><a:solidFill><a:srgbClr val="212121"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(val), fillColor)
		}
		sb.WriteString("</a:tr>\n")
	}

	sb.WriteString("</a:tbl></a:graphicData></a:graphic></p:graphicFrame>\n")

	if len(qr.Data) > 15 {
		hint := fmt.Sprintf("\u203b \u5171 %d \u884c\u6570\u636e\uff0c\u4ee5\u4e0a\u4ec5\u5c55\u793a\u524d 15 \u884c", len(qr.Data))
		sb.WriteString(pptxTextBox(21, 800000, tableCy+1100000, tableWidth, 400000, hint, 1200, false, "757575"))
	}

	sb.WriteString(pptxSlideNumber(22, 3))

	sb.WriteString("</p:spTree></p:cSld>\n</p:sld>")
	return sb.String()
}

func pptxHighlightSlide(qr *queryResult) string {
	var highlights []string
	highlights = append(highlights, fmt.Sprintf("\u25B6 \u6570\u636e\u96c6\u5305\u542b %d \u6761\u8bb0\u5f55\uff0c%d \u4e2a\u5b57\u6bb5", len(qr.Data), len(qr.Columns)))

	for _, col := range qr.Columns {
		if len(qr.Data) == 0 {
			break
		}
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
				highlights = append(highlights, fmt.Sprintf("\u25B6 %s\uff1a\u6700\u5c0f %.2f  \u6700\u5927 %.2f  \u5e73\u5747 %.2f", col, min, max, avg))
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
		pptxColorBar(5, 0, 0, 12192000, 60000, "00BCD4"),
		pptxTextBox(2, 800000, 300000, 10500000, 700000, "\u6570\u636e\u4eae\u70b9", 2800, true, "1A237E"),
		pptxContentTextBox(3, 800000, 1200000, 10500000, 5200000, content),
		pptxSlideNumber(20, 4),
	)
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
		fmt.Fprintf(&paragraphs, `<a:p><a:r><a:rPr lang="zh-CN" sz="%d"%s>%s</a:rPr><a:t>%s</a:t></a:r></a:p>`, fontSize, boldAttr, colorXml, xmlEscape(line))
	}

	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="TextBox%d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>
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
			fmt.Fprintf(&paragraphs, `<a:p><a:pPr marL="0" indent="0"><a:spcBef><a:spcPts val="600"/></a:spcBef></a:pPr><a:endParaRPr lang="zh-CN" sz="1400"><a:solidFill><a:srgbClr val="424242"/></a:solidFill></a:endParaRPr></a:p>`)
			continue
		}

		isBold := strings.HasPrefix(strings.TrimSpace(line), "\u25CF") || strings.HasPrefix(strings.TrimSpace(line), "\u25B6")
		boldAttr := ""
		if isBold {
			boldAttr = ` b="1"`
		}

		fmt.Fprintf(&paragraphs, `<a:p><a:r><a:rPr lang="zh-CN" sz="1400"%s><a:solidFill><a:srgbClr val="424242"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p>`, boldAttr, xmlEscape(line))
	}

	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="TextBox%d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>
  <p:txBody><a:bodyPr wrap="square" rtlCol="0" lIns="60000" rIns="60000"/>%s</p:txBody>
</p:sp>
`, id, id, x, y, cx, cy, paragraphs.String())
}

func pptxColorBar(id int, x, y, cx, cy int64, color string) string {
	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="Bar%d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr>
</p:sp>
`, id, id, x, y, cx, cy, color)
}

func pptxTitleDecoLine(id int, x, y, cx, cy int64, color string) string {
	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="DecoLine%d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr>
</p:sp>
`, id, id, x, y, cx, cy, color)
}

func pptxSlideNumber(id int, pageNum int) string {
	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="PageNum"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="11000000" y="6500000"/><a:ext cx="1000000" cy="250000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>
  <p:txBody><a:bodyPr wrap="square" rtlCol="0"/><a:p><a:pPr algn="r"/><a:r><a:rPr lang="en-US" sz="1000"><a:solidFill><a:srgbClr val="BDBDBD"/></a:solidFill></a:rPr><a:t>%d</a:t></a:r></a:p></p:txBody>
</p:sp>
`, id, pageNum)
}

func detectNumericCols(qr *queryResult) []string {
	if len(qr.Data) == 0 {
		return nil
	}
	var numeric []string
	for _, col := range qr.Columns {
		if _, err := toFloat64(qr.Data[0][col]); err == nil {
			numeric = append(numeric, col)
		}
	}
	return numeric
}

func createFile(path string) (*os.File, error) {
	return os.Create(path)
}

type slideSection struct {
	Title  string
	Blocks []mdBlock
}

func generatePptxFromContent(content, title, outputPath string) (int, error) {
	blocks := parseMarkdownBlocks(content)

	var sections []slideSection
	var current slideSection

	for _, block := range blocks {
		if block.Type == "h1" || block.Type == "h2" {
			if current.Title != "" || len(current.Blocks) > 0 {
				sections = append(sections, current)
			}
			current = slideSection{Title: stripMarkdownFormatting(block.Content)}
		} else {
			current.Blocks = append(current.Blocks, block)
		}
	}
	if current.Title != "" || len(current.Blocks) > 0 {
		sections = append(sections, current)
	}

	if len(sections) == 0 {
		sections = append(sections, slideSection{Title: title, Blocks: blocks})
	}

	slideCount := len(sections) + 1

	f, err := createFile(outputPath)
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

	writeZipEntry(zw, "ppt/slides/slide1.xml", pptxTitleSlide(title))
	writeZipEntry(zw, "ppt/slides/_rels/slide1.xml.rels", pptxSlideRels())

	for i, sec := range sections {
		slideNum := i + 2
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/slide%d.xml", slideNum), pptxContentSlide(sec.Title, sec.Blocks, slideNum))
		writeZipEntry(zw, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", slideNum), pptxSlideRels())
	}

	return slideCount, nil
}

func pptxContentSlide(slideTitle string, blocks []mdBlock, pageNum int) string {
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
			textLines = append(textLines, "\u25B8 "+stripMarkdownFormatting(block.Content))
		case "paragraph":
			textLines = append(textLines, stripMarkdownFormatting(block.Content))
		case "list":
			for _, item := range strings.Split(block.Content, "\n") {
				textLines = append(textLines, "\u2022 "+stripMarkdownFormatting(item))
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
	tableX := int64(800000)
	tableW := int64(10500000)
	textX := int64(800000)

	var shapes strings.Builder
	shapes.WriteString(pptxColorBar(5, 0, 0, 12192000, 60000, "00BCD4"))
	shapes.WriteString(pptxTextBox(2, 800000, 300000, 10500000, 700000, slideTitle, 2800, true, "1A237E"))

	if len(tables) > 0 {
		curY := int64(1100000)
		for _, tb := range tables {
			posXML := strings.Replace(tb.shapeXML, `a:off x="0" y="0"`, fmt.Sprintf(`a:off x="%d" y="%d"`, tableX, curY), 1)
			shapes.WriteString(posXML)
			curY += tb.height + 200000
		}
		if content != "" {
			remainingH := int64(6858000) - curY - 200000
			if remainingH < 300000 {
				remainingH = 300000
			}
			shapes.WriteString(pptxContentTextBox(200, textX, curY, tableW, remainingH, content))
		}
	} else {
		shapes.WriteString(pptxContentTextBox(3, 800000, 1200000, 10500000, 5200000, content))
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
		if trimmed == "" || isTableSeparator(trimmed) {
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
			cells[i] = strings.TrimSpace(stripMarkdownFormatting(cells[i]))
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
		fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr lIns="40000" rIns="40000" tIns="20000" bIns="20000"/><a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="zh-CN" sz="1100" b="1"><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="1A237E"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(cols[i]))
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
				fillColor = "EDEFF7"
			}
			fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr lIns="40000" rIns="40000" tIns="15000" bIns="15000"/><a:p><a:r><a:rPr lang="zh-CN" sz="1000"><a:solidFill><a:srgbClr val="212121"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(val), fillColor)
		}
		sb.WriteString("</a:tr>\n")
	}

	sb.WriteString("</a:tbl></a:graphicData></a:graphic></p:graphicFrame>\n")

	if len(rows) > 12 {
		hint := fmt.Sprintf("\u203b \u5171 %d \u884c\uff0c\u4ec5\u5c55\u793a\u524d 12 \u884c", len(rows))
		sb.WriteString(pptxTextBox(idStart+50, 800000, tableH+1100000, 10500000, 300000, hint, 1000, false, "757575"))
		tableH += 500000
	}

	return sb.String(), tableH
}
