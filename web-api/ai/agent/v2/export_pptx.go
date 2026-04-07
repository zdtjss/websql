// PPTX 生成器 — 直接构建 Office Open XML
//
// PPTX 文件本质是 ZIP 包，包含：
//   - [Content_Types].xml
//   - _rels/.rels
//   - ppt/presentation.xml          — 演示文稿主体
//   - ppt/_rels/presentation.xml.rels
//   - ppt/slides/slide1.xml         — 幻灯片
//   - ppt/slides/_rels/slide1.xml.rels
//   - ppt/slideLayouts/slideLayout1.xml
//   - ppt/slideMasters/slideMaster1.xml
//
// 生成策略：
//
//	Slide 1 — 标题页（标题 + 生成时间）
//	Slide 2 — 数据摘要（列名、行数、统计）
//	Slide 3 — 数据表格（前 15 行预览）
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

	// [Content_Types].xml
	writeZipEntry(zw, "[Content_Types].xml", pptxContentTypes(slideCount))

	// _rels/.rels
	writeZipEntry(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
</Relationships>`)

	// ppt/presentation.xml
	writeZipEntry(zw, "ppt/presentation.xml", pptxPresentation(slideCount))

	// ppt/_rels/presentation.xml.rels
	writeZipEntry(zw, "ppt/_rels/presentation.xml.rels", pptxPresentationRels(slideCount))

	// Slide Master & Layout (minimal)
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

	return slideCount, nil
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
  <Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>
  <Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>`)
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
  <Relationship Id="rIdMaster" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>`)
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
  <p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/></p:spTree></p:cSld>
  <p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rIdLayout"/></p:sldLayoutIdLst>
</p:sldMaster>`
}

func pptxSlideMasterRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdLayout" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
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

// pptxTitleSlide 标题页
func pptxTitleSlide(title string) string {
	now := time.Now().Format("2006-01-02 15:04")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld><p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxTextBox(2, 1500000, 2000000, 9000000, 1500000, title, 3600, true),
		pptxTextBox(3, 1500000, 3800000, 9000000, 800000, now, 1800, false),
	)
}

// pptxSummarySlide 数据摘要页
func pptxSummarySlide(qr *queryResult) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("总记录数：%d", len(qr.Data)))
	lines = append(lines, fmt.Sprintf("列数：%d", len(qr.Columns)))
	lines = append(lines, fmt.Sprintf("列名：%s", strings.Join(qr.Columns, ", ")))

	content := strings.Join(lines, "\n")

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld><p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
  %s
  %s
</p:spTree></p:cSld>
</p:sld>`,
		pptxTextBox(2, 600000, 400000, 10800000, 800000, "数据摘要", 2800, true),
		pptxTextBox(3, 600000, 1500000, 10800000, 4500000, content, 1600, false),
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
<p:cSld><p:spTree>
  <p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>
  <p:grpSpPr/>
`)

	// 标题
	sb.WriteString(pptxTextBox(2, 600000, 200000, 10800000, 600000, "数据预览", 2400, true))

	// 表格
	numCols := len(qr.Columns)
	if numCols > 10 {
		numCols = 10 // 限制列数避免溢出
	}
	numRows := maxRows + 1 // +1 for header

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
		fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:rPr lang="zh-CN" sz="1100" b="1"/><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="3F51B5"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(qr.Columns[i]))
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
			fmt.Fprintf(&sb, `<a:tc><a:txBody><a:bodyPr/><a:p><a:r><a:rPr lang="zh-CN" sz="1000"/><a:t>%s</a:t></a:r></a:p></a:txBody><a:tcPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, xmlEscape(val), fillColor)
		}
		sb.WriteString("</a:tr>\n")
	}

	sb.WriteString("</a:tbl></a:graphicData></a:graphic></p:graphicFrame>\n")

	// 行数提示
	if len(qr.Data) > 15 {
		hint := fmt.Sprintf("共 %d 行，仅展示前 15 行", len(qr.Data))
		sb.WriteString(pptxTextBox(20, 600000, 6200000, 10800000, 400000, hint, 1200, false))
	}

	sb.WriteString("</p:spTree></p:cSld>\n</p:sld>")
	return sb.String()
}

// pptxTextBox 生成文本框 shape XML
func pptxTextBox(id int, x, y, cx, cy int64, text string, fontSize int, bold bool) string {
	boldAttr := ""
	if bold {
		boldAttr = ` b="1"`
	}

	// 处理多行文本
	lines := strings.Split(text, "\n")
	var paragraphs strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&paragraphs, `<a:p><a:r><a:rPr lang="zh-CN" sz="%d"%s/><a:t>%s</a:t></a:r></a:p>`, fontSize, boldAttr, xmlEscape(line))
	}

	return fmt.Sprintf(`<p:sp>
  <p:nvSpPr><p:cNvPr id="%d" name="TextBox%d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>
  <p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>
  <p:txBody><a:bodyPr wrap="square" rtlCol="0"/>%s</p:txBody>
</p:sp>
`, id, id, x, y, cx, cy, paragraphs.String())
}

func createFile(path string) (*os.File, error) {
	return os.Create(path)
}
