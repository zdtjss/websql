package fileparse

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── CSV ────────────────────────────────────────────────────

func TestParseCSV_UTF8(t *testing.T) {
	content := "姓名,金额\n张三,100\n\n李四,200\n"
	p := writeTemp(t, "a.csv", []byte(content))

	res, err := Parse(context.Background(), Source{Path: p, Ext: ".csv", FileName: "a.csv"})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if !res.IsTabular() || res.TotalRows != 2 {
		t.Fatalf("表格解析结果异常：kind=%s rows=%d", res.Kind, res.TotalRows)
	}
	if strings.Join(res.Columns, ",") != "姓名,金额" {
		t.Errorf("表头解析错误：%v", res.Columns)
	}
	if res.Rows[1][0] != "李四" {
		t.Errorf("空行应被跳过，实际第二行：%v", res.Rows[1])
	}
}

func TestParseCSV_GB18030(t *testing.T) {
	p := writeTemp(t, "gbk.csv", toGB18030(t, "城市,销量\n北京,10\n上海,20\n"))

	res, err := Parse(context.Background(), Source{Path: p, Ext: ".csv"})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if res.Columns[0] != "城市" || res.Rows[0][0] != "北京" {
		t.Errorf("GB18030 内容未正确解码：%v / %v", res.Columns, res.Rows[0])
	}
}

func TestParseCSV_TooFewRows(t *testing.T) {
	p := writeTemp(t, "one.csv", []byte("仅表头\n"))
	if _, err := Parse(context.Background(), Source{Path: p, Ext: ".csv"}); err == nil {
		t.Error("仅表头应报「数据不足」")
	}
}

// ─── XLSX ───────────────────────────────────────────────────

func TestParseXLSX(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.xlsx")
	writeXLSX(t, p, []string{"id", "name"}, [][]string{{"1", "a"}, {"2", "b"}})

	res, err := Parse(context.Background(), Source{Path: p, Ext: ".xlsx"})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if res.TotalRows != 2 || res.Columns[0] != "id" {
		t.Errorf("xlsx 解析结果异常：rows=%d cols=%v", res.TotalRows, res.Columns)
	}
	if res.Impl != "go-xlsx" {
		t.Errorf("实现名应为 go-xlsx，实际 %s", res.Impl)
	}
}

func TestParseXLS_LegacyBinary(t *testing.T) {
	p := writeTemp(t, "old.xls", []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	_, err := Parse(context.Background(), Source{Path: p, Ext: ".xls"})
	if err == nil || !strings.Contains(err.Error(), "另存为 .xlsx") {
		t.Errorf("旧版 .xls 应给出明确指引，实际：%v", err)
	}
}

// ─── 文本 ───────────────────────────────────────────────────

func TestParseText_AndTruncate(t *testing.T) {
	p := writeTemp(t, "a.txt", []byte("第一行\n第二行\n"))
	res, err := Parse(context.Background(), Source{Path: p, Ext: ".txt"})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if res.Kind != KindText || res.TotalChars == 0 || !strings.Contains(res.Text, "第二行") {
		t.Errorf("文本解析结果异常：kind=%s chars=%d", res.Kind, res.TotalChars)
	}

	// 超长文本应截断并给出 Notice
	long := strings.Repeat("字", MaxTextChars+10)
	p2 := writeTemp(t, "long.txt", []byte(long))
	res2, err := Parse(context.Background(), Source{Path: p2, Ext: ".txt"})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len([]rune(res2.Text)) != MaxTextChars {
		t.Errorf("应截断到 %d 字符，实际 %d", MaxTextChars, len([]rune(res2.Text)))
	}
	if res2.Notice == "" {
		t.Error("截断应给出 Notice 说明")
	}
}

// ─── DOCX（Go 兜底实现，直测实现本身） ───────────────────────
//
// 说明：注册表在有 python-docx 时会优先选择 Python 实现，
// 因此这里直接调用 Go 实现（parseDOCX）验证兜底路径的提取质量；
// Python 主路径与降级链由 python_test.go 覆盖。

func TestParseDOCX_GoImplementation(t *testing.T) {
	xmlDoc := docxDocXML(
		[]string{"季度经营分析报告", "第二段内容"},
		[][]string{{"地区", "销售额"}, {"华东", "120"}},
	)
	p := filepath.Join(t.TempDir(), "a.docx")
	writeDOCX(t, p, xmlDoc)

	res, err := parseDOCX(context.Background(), Source{Path: p, Ext: ".docx"})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if res.Kind != KindDoc {
		t.Fatalf("类别应为 doc，实际 %s", res.Kind)
	}
	if !strings.Contains(res.Text, "季度经营分析报告") || !strings.Contains(res.Text, "华东") {
		t.Errorf("正文提取不完整：%q", res.Text)
	}
	if !res.Degraded || res.Impl != "go-docx" {
		t.Errorf("应标记为降级实现：degraded=%v impl=%s", res.Degraded, res.Impl)
	}
	if res.Notice == "" {
		t.Error("降级解析应给出 Notice 说明")
	}
}

// TestParseDOCX_DispatchDegradesToGo 验证降级链：
// 优先实现无法处理该文件（缺少 [Content_Types].xml 的极简 zip）时，
// 自动落到底层 Go 实现并成功提取正文，而不是直接把错误抛给用户。
func TestParseDOCX_DispatchDegradesToGo(t *testing.T) {
	if !pythonModuleAvailable(pythonModuleDocx, &docxModuleProbe) {
		t.Skip("未安装 python-docx，本用例验证的降级链无意义")
	}
	xmlDoc := docxDocXML([]string{"降级链验证内容"}, nil)
	p := filepath.Join(t.TempDir(), "minimal.docx")
	writeDOCX(t, p, xmlDoc)

	res, err := Parse(context.Background(), Source{Path: p, Ext: ".docx", FileName: "minimal.docx"})
	if err != nil {
		t.Fatalf("优先实现失败时应降级成功，实际报错：%v", err)
	}
	if res.Impl != "go-docx" || !res.Degraded {
		t.Errorf("应降级到 go-docx 并标记降级，实际 impl=%s degraded=%v", res.Impl, res.Degraded)
	}
	if !strings.Contains(res.Text, "降级链验证内容") {
		t.Errorf("降级解析应提取到正文，实际：%q", res.Text)
	}
}

func TestParseDOCX_Corrupted(t *testing.T) {
	p := writeTemp(t, "bad.docx", []byte("this is not a zip file"))
	if _, err := Parse(context.Background(), Source{Path: p, Ext: ".docx"}); err == nil {
		t.Error("非 zip 内容应解析失败")
	}
}

func TestParseDOCX_EmptyBody(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.docx")
	writeDOCX(t, p, "")
	res, err := parseDOCX(context.Background(), Source{Path: p, Ext: ".docx"})
	if err != nil {
		t.Fatalf("空正文不应报错：%v", err)
	}
	if strings.TrimSpace(res.Text) != "" {
		t.Errorf("空正文应解析为空字符串，实际 %q", res.Text)
	}
}

func TestParseDOCX_MissingDocumentXML(t *testing.T) {
	p := filepath.Join(t.TempDir(), "no-body.docx")
	// 合法的 zip，但缺少 word/document.xml（结构不完整的 docx）
	writeZipEntries(t, p, map[string]string{"[Content_Types].xml": "<Types/>"})

	if _, err := parseDOCX(context.Background(), Source{Path: p, Ext: ".docx"}); err == nil {
		t.Error("缺少正文条目应解析失败")
	}
}

// ─── 图片 ───────────────────────────────────────────────────

func TestDetectImage_AndParse(t *testing.T) {
	data := pngBytes(t, 8, 6)
	p := writeTemp(t, "a.png", data)

	info, err := DetectImage(p)
	if err != nil {
		t.Fatalf("图片头校验失败：%v", err)
	}
	if info.Width != 8 || info.Height != 6 || info.MIME != "image/png" {
		t.Errorf("图片元信息错误：%+v", info)
	}

	res, err := Parse(context.Background(), Source{Path: p, Ext: ".png", Size: int64(len(data))})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if res.Kind != KindImage || res.Meta["width"] != 8 {
		t.Errorf("图片解析结果异常：kind=%s meta=%v", res.Kind, res.Meta)
	}
}

func TestDetectImage_Invalid(t *testing.T) {
	p := writeTemp(t, "fake.png", []byte("not an image"))
	if _, err := DetectImage(p); err == nil {
		t.Error("非法图片内容应校验失败")
	}
}

// ─── 视频（MP4 元数据） ──────────────────────────────────────

func TestParseVideo_MP4Metadata(t *testing.T) {
	data := mp4Bytes()
	p := writeTemp(t, "a.mp4", data)

	res, err := Parse(context.Background(), Source{Path: p, Ext: ".mp4", Size: int64(len(data))})
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if res.Kind != KindVideo {
		t.Fatalf("类别应为 video，实际 %s", res.Kind)
	}
	if got := res.Meta["duration_sec"]; got != float64(5) {
		t.Errorf("时长应为 5s，实际 %v", got)
	}
	if res.Meta["width"] != 640 || res.Meta["height"] != 360 {
		t.Errorf("分辨率解析错误：%v x %v", res.Meta["width"], res.Meta["height"])
	}
	if res.Meta["codec"] != "avc1" {
		t.Errorf("编码应为 avc1，实际 %v", res.Meta["codec"])
	}
}

func TestParseVideo_NotMP4(t *testing.T) {
	p := writeTemp(t, "a.mp4", []byte("random bytes without ftyp"))
	if _, err := Parse(context.Background(), Source{Path: p, Ext: ".mp4"}); err == nil {
		t.Error("缺少 ftyp 应解析失败")
	}
}

// ─── 文件头嗅探 ──────────────────────────────────────────────

func TestSniffBytes(t *testing.T) {
	cases := []struct {
		ext   string
		head  []byte
		wantE bool
	}{
		{".docx", []byte("PK\x03\x04rest"), false},
		{".docx", []byte("not a zip"), true},
		{".xlsx", []byte("PK\x03\x04rest"), false},
		{".xls", []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, false},
		{".pdf", []byte("%PDF-1.7"), false},
		{".pdf", []byte("PK\x03\x04"), true},
		{".png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, false},
		{".jpg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, false},
		{".jpeg", []byte{0xFF, 0xD8, 0x00}, true},
		{".mp4", append([]byte{0, 0, 0, 0x18}, []byte("ftypisom")...), false},
		{".mp4", []byte("not mp4 content"), true},
		{".txt", []byte("任意文本"), false},
		{".csv", []byte("a,b\n1,2"), false},
	}
	for _, c := range cases {
		err := SniffBytes(c.ext, c.head)
		if c.wantE && err == nil {
			t.Errorf("SniffBytes(%s) 应失败", c.ext)
		}
		if !c.wantE && err != nil {
			t.Errorf("SniffBytes(%s) 应通过，实际：%v", c.ext, err)
		}
	}

	if err := SniffBytes(".png", nil); err == nil {
		t.Error("空内容应被拒绝")
	}
}

func TestSniff_FromDisk(t *testing.T) {
	data := pngBytes(t, 4, 4)
	p := writeTemp(t, "a.png", data)
	if err := Sniff(".png", p); err != nil {
		t.Errorf("磁盘文件嗅探应通过：%v", err)
	}
	if err := Sniff(".png", filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Error("文件不存在应返回错误")
	}
	_ = os.Remove(p)
}
