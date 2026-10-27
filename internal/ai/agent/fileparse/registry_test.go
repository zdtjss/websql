package fileparse

import (
	"context"
	"strings"
	"testing"
)

// ─── 扩展名矩阵 ──────────────────────────────────────────────

func TestKindOfExt(t *testing.T) {
	cases := map[string]Kind{
		".xlsx": KindTable, ".xls": KindTable, ".csv": KindTable,
		".txt": KindText, ".md": KindText, ".markdown": KindText,
		".docx": KindDoc, ".pdf": KindDoc,
		".jpg": KindImage, ".jpeg": KindImage, ".png": KindImage,
		".mp4": KindVideo,
	}
	for ext, want := range cases {
		got, ok := KindOfExt(ext)
		if !ok || got != want {
			t.Errorf("KindOfExt(%q) = %v/%v，期望 %v", ext, got, ok, want)
		}
	}
	if _, ok := KindOfExt(".exe"); ok {
		t.Error(".exe 不应在支持矩阵内")
	}
	// 大小写与无点形式都应归一化
	if k, ok := KindOfExt("DNX"); ok {
		_ = k
		t.Error("未知扩展名不应被识别")
	}
	if k, ok := KindOfExt("PDF"); !ok || k != KindDoc {
		t.Errorf("大写扩展名应归一化识别，实际 %v/%v", k, ok)
	}
}

func TestAllowedSize(t *testing.T) {
	if got := AllowedSize(".png"); got != imageMaxSize {
		t.Errorf("图片上限应为 %d，实际 %d", imageMaxSize, got)
	}
	if got := AllowedSize(".docx"); got != defaultMaxSize {
		t.Errorf("docx 上限应为 %d，实际 %d", defaultMaxSize, got)
	}
	if got := AllowedSize(".md"); got != 0 {
		t.Errorf("Markdown 应不限大小（保持既有行为），实际 %d", got)
	}
}

func TestSupportedExts(t *testing.T) {
	exts := SupportedExts()
	if len(exts) < 9 {
		t.Fatalf("受支持扩展名数量异常：%d", len(exts))
	}
	for i := 1; i < len(exts); i++ {
		if exts[i-1] >= exts[i] {
			t.Fatalf("扩展名列表应稳定有序：%v", exts)
		}
	}
}

// ─── 注册表：优先级 / 降级 / 能力描述 ────────────────────────

func TestRegistry_PriorityAndDegrade(t *testing.T) {
	ext := ".prio"
	Register("low", 0, nil, []string{ext}, ParserFunc(func(context.Context, Source) (*Result, error) {
		return &Result{Text: "low"}, nil
	}))
	Register("high", 100, nil, []string{ext}, ParserFunc(func(context.Context, Source) (*Result, error) {
		return &Result{Text: "high"}, nil
	}))

	src := Source{Path: "dummy", Ext: ext}
	res, err := Parse(context.Background(), src)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if res.Text != "high" {
		t.Errorf("应使用高优先级实现，实际 %q（impl=%s）", res.Text, res.Impl)
	}

	// 高优先级不可用 → 自动降级
	unavailableExt := ".degrade"
	available := false
	Register("fallback", 0, nil, []string{unavailableExt}, ParserFunc(func(context.Context, Source) (*Result, error) {
		return &Result{Text: "fallback", Degraded: true}, nil
	}))
	Register("preferred", 100, func() bool { return available }, []string{unavailableExt}, ParserFunc(func(context.Context, Source) (*Result, error) {
		return &Result{Text: "preferred"}, nil
	}))

	res, err = Parse(context.Background(), Source{Path: "dummy", Ext: unavailableExt})
	if err != nil {
		t.Fatalf("降级解析失败：%v", err)
	}
	if res.Text != "fallback" || !res.Degraded {
		t.Errorf("应降级到兜底实现，实际 %q/degraded=%v", res.Text, res.Degraded)
	}

	available = true
	res, err = Parse(context.Background(), Source{Path: "dummy", Ext: unavailableExt})
	if err != nil {
		t.Fatalf("优先实现可用时解析失败：%v", err)
	}
	if res.Text != "preferred" {
		t.Errorf("优先实现可用时应使用之，实际 %q", res.Text)
	}
}

func TestRegistry_NoAvailableImpl(t *testing.T) {
	ext := ".none"
	Register("never", 0, func() bool { return false }, []string{ext}, ParserFunc(func(context.Context, Source) (*Result, error) {
		return &Result{}, nil
	}))

	if _, err := Parse(context.Background(), Source{Path: "dummy", Ext: ext}); err == nil {
		t.Error("无可用实现时应返回错误")
	}
	if IsSupported(ext) {
		t.Error("无可用实现时 IsSupported 应为 false")
	}
}

func TestDescribe_UnknownExt(t *testing.T) {
	kind, impl, ok := Describe(".nope")
	if ok || impl != "none" || kind != "" {
		t.Errorf("未知扩展名应返回 none，实际 kind=%q impl=%q ok=%v", kind, impl, ok)
	}
}

// ─── 解析入口的健壮性 ────────────────────────────────────────

func TestParse_PanicRecovered(t *testing.T) {
	ext := ".panic"
	Register("panicky", 0, nil, []string{ext}, ParserFunc(func(context.Context, Source) (*Result, error) {
		panic("boom")
	}))

	_, err := Parse(context.Background(), Source{Path: "dummy", Ext: ext, FileName: "x"})
	if err == nil {
		t.Fatal("解析器 panic 应被恢复并转为错误")
	}
	if !strings.Contains(err.Error(), "解析文件失败") {
		t.Errorf("错误文案应可读，实际 %q", err.Error())
	}
}

func TestParse_NilResult(t *testing.T) {
	ext := ".nilres"
	Register("nilres", 0, nil, []string{ext}, ParserFunc(func(context.Context, Source) (*Result, error) {
		return nil, nil
	}))
	if _, err := Parse(context.Background(), Source{Path: "dummy", Ext: ext}); err == nil {
		t.Error("解析器返回空结果时应报错")
	}
}

func TestParseCached(t *testing.T) {
	ext := ".cached"
	calls := 0
	Register("cached", 0, nil, []string{ext}, ParserFunc(func(context.Context, Source) (*Result, error) {
		calls++
		return &Result{Text: "cached"}, nil
	}))

	src := Source{Path: "dummy", Ext: ext}
	if _, hit, err := ParseCached(context.Background(), "k1", src); err != nil || hit {
		t.Fatalf("首次解析应未命中缓存：hit=%v err=%v", hit, err)
	}
	res, hit, err := ParseCached(context.Background(), "k1", src)
	if err != nil || !hit {
		t.Fatalf("二次解析应命中缓存：hit=%v err=%v", hit, err)
	}
	if res.Text != "cached" || calls != 1 {
		t.Errorf("缓存未生效：calls=%d res=%q", calls, res.Text)
	}

	Purge("k1")
	if _, hit, _ := ParseCached(context.Background(), "k1", src); hit {
		t.Error("Purge 后不应命中缓存")
	}
}

// ─── 编码探测 ────────────────────────────────────────────────

func TestDecodeText_Encodings(t *testing.T) {
	utf8BOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte("中文内容")...)
	if got, enc := decodeText(utf8BOM); got != "中文内容" || enc != "utf-8-bom" {
		t.Errorf("UTF-8 BOM 解码失败：%q/%s", got, enc)
	}

	gbk := toGB18030(t, "销售数据统计")
	got, enc := decodeText(gbk)
	if got != "销售数据统计" {
		t.Errorf("GB18030 解码失败：%q（enc=%s）", got, enc)
	}
	if enc != "gb18030" {
		t.Errorf("应识别为 gb18030，实际 %s", enc)
	}

	got, enc = decodeText([]byte("plain ascii"))
	if got != "plain ascii" || enc != "utf-8" {
		t.Errorf("纯 ASCII 解码失败：%q/%s", got, enc)
	}

	// UTF-16LE（带 BOM）
	utf16le := []byte{0xFF, 0xFE}
	for _, r := range "你好" {
		utf16le = append(utf16le, byte(r), byte(r>>8))
	}
	if got, enc := decodeText(utf16le); got != "你好" || enc != "utf-16le" {
		t.Errorf("UTF-16LE 解码失败：%q/%s", got, enc)
	}
}
