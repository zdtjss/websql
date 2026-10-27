package fileparse

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"github.com/xuri/excelize/v2"
)

// 测试夹具构造辅助：全部在运行期生成，避免仓库内提交二进制样本。

// writeTemp 把字节写入临时文件并返回路径
func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}
	return p
}

// toGB18030 把 UTF-8 文本编码为 GB18030（模拟 Windows 导出文件）
func toGB18030(t *testing.T, s string) []byte {
	t.Helper()
	out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(s))
	if err != nil {
		t.Fatalf("构造 GB18030 文本失败：%v", err)
	}
	return out
}

// writeXLSX 生成一个含表头与数据行的 xlsx
func writeXLSX(t *testing.T, path string, headers []string, rows [][]string) {
	t.Helper()
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	for r, row := range rows {
		for i, v := range row {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("生成 xlsx 失败：%v", err)
	}
	_ = f.Close()
}

// docxDocXML 构造 word/document.xml：段落 + 表格
func docxDocXML(paragraphs []string, tableRows [][]string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paragraphs {
		sb.WriteString(`<w:p><w:r><w:t>` + p + `</w:t></w:r></w:p>`)
	}
	if len(tableRows) > 0 {
		sb.WriteString(`<w:tbl>`)
		for _, row := range tableRows {
			sb.WriteString(`<w:tr>`)
			for _, cell := range row {
				sb.WriteString(`<w:tc><w:p><w:r><w:t>` + cell + `</w:t></w:r></w:p></w:tc>`)
			}
			sb.WriteString(`</w:tr>`)
		}
		sb.WriteString(`</w:tbl>`)
	}
	sb.WriteString(`</w:body></w:document>`)
	return sb.String()
}

// writeDOCX 生成一个最小可用的 docx（zip + word/document.xml）
func writeDOCX(t *testing.T, path, documentXML string) {
	t.Helper()
	writeZipEntries(t, path, map[string]string{"word/document.xml": documentXML})
}

// writeZipEntries 生成包含指定条目的 zip 文件（用于构造边界夹具）
func writeZipEntries(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 zip 失败：%v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("创建 zip 条目失败：%v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("写入 zip 内容失败：%v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败：%v", err)
	}
}

// pngBytes 生成指定尺寸的 PNG 字节
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	tmp := filepath.Join(t.TempDir(), "img.png")
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatalf("创建 png 失败：%v", err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("编码 png 失败：%v", err)
	}
	f.Close()
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("读取 png 失败：%v", err)
	}
	return data
}

// pdfBytes 构造一个含单页文本的最小 PDF（xref 偏移按实际写入字节计算）。
//
// 用于离线验证 PDF 解析链路，不引入额外的 PDF 生成依赖。
func pdfBytes(t *testing.T, text string) []byte {
	t.Helper()

	var buf bytes.Buffer
	offsets := make([]int, 0, 6)
	writeObj := func(num int, body string) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}

	buf.WriteString("%PDF-1.4\n")
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
		"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>")
	stream := fmt.Sprintf("BT /F1 18 Tf 72 700 Td (%s) Tj ET", text)
	writeObj(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	writeObj(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(offsets)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(offsets)+1, xrefOffset)

	return buf.Bytes()
}

// mp4Bytes 构造最小 MP4：ftyp + moov(mvhd + trak(tkhd + mdia/minf/stbl/stsd))
func mp4Bytes() []byte {
	box := func(typ string, payload []byte) []byte {
		buf := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(buf[0:4], uint32(8+len(payload)))
		copy(buf[4:8], typ)
		copy(buf[8:], payload)
		return buf
	}

	ftyp := box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2"))

	// mvhd v0：version/flags(4) + creation(4) + modification(4) + timescale(4) + duration(4)
	mvhdPayload := make([]byte, 20)
	binary.BigEndian.PutUint32(mvhdPayload[12:16], 1000) // timescale
	binary.BigEndian.PutUint32(mvhdPayload[16:20], 5000) // duration → 5s
	mvhd := box("mvhd", mvhdPayload)

	// tkhd v0：宽高为末尾 8 字节（16.16 定点）
	tkhdPayload := make([]byte, 84)
	binary.BigEndian.PutUint32(tkhdPayload[76:80], uint32(640)<<16)
	binary.BigEndian.PutUint32(tkhdPayload[80:84], uint32(360)<<16)
	tkhd := box("tkhd", tkhdPayload)

	// stsd：version/flags(4) + entry_count(4) + entry(size(4)+fourcc(4))
	stsdPayload := make([]byte, 16)
	binary.BigEndian.PutUint32(stsdPayload[4:8], 1)
	binary.BigEndian.PutUint32(stsdPayload[8:12], 12)
	copy(stsdPayload[12:16], "avc1")
	stsd := box("stsd", stsdPayload)

	stbl := box("stbl", stsd)
	minf := box("minf", stbl)
	mdia := box("mdia", minf)
	trak := box("trak", append(append([]byte{}, tkhd...), mdia...))
	moov := box("moov", append(append([]byte{}, mvhd...), trak...))

	return append(ftyp, moov...)
}
