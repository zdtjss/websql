package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"websql/internal/ai/agent/fileparse"
	"websql/internal/ai/agent/uploadstore"
	admin "websql/internal/app/admin"
	system "websql/internal/app/system"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// 上传接口层单测：覆盖「校验顺序 / 不解析内容 / 归属校验 / 能力校验 / 内容读取」。

// withTempUploadStore 把全局暂存库替换为临时目录，避免测试污染仓库 data 目录。
func withTempUploadStore(t *testing.T) {
	t.Helper()
	old := globalUploadStore
	globalUploadStore = uploadstore.NewStore(t.TempDir(), uploadMaxAge, uploadCleanSec, maxUploadEntries)
	t.Cleanup(func() {
		globalUploadStore.Close()
		globalUploadStore = old
	})
}

// newUploadTestRouter 构造带身份注入的测试路由（X-Test-User 指定当前用户）。
func newUploadTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/ai/agent")
	g.Use(func(c *gin.Context) {
		userID := c.GetHeader("X-Test-User")
		if userID == "" {
			userID = "u1"
		}
		c.Set("currentUser", &admin.User{Id: userID})
		c.Next()
	})
	g.POST("/uploadFile", HandleUploadFile)
	g.POST("/uploadFile/delete", HandleDeleteUploadFile)
	g.GET("/uploadFile/content", HandleUploadFileContent)
	g.GET("/fileSupport", HandleFileSupport)
	return r
}

// multipartFile 构造 multipart 请求
func multipartFile(t *testing.T, field, filename string, content []byte, extra map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	for k, v := range extra {
		_ = w.WriteField(k, v)
	}
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("构造 multipart 失败：%v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("写入 multipart 内容失败：%v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败：%v", err)
	}
	return buf, w.FormDataContentType()
}

// doUpload 发起上传请求并返回响应与解析后的 data
func doUpload(t *testing.T, r *gin.Engine, user, filename string, content []byte, extra map[string]string) (*httptest.ResponseRecorder, map[string]any, string) {
	t.Helper()
	body, ct := multipartFile(t, "file", filename, content, extra)
	req := httptest.NewRequest(http.MethodPost, "/ai/agent/uploadFile", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Test-User", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var parsed struct {
		Code int            `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w, parsed.Data, parsed.Msg
}

func pngFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{B: 255, A: 255})
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, img); err != nil {
		t.Fatalf("生成 PNG 失败：%v", err)
	}
	return buf.Bytes()
}

// writeDocxFixture 生成极简 docx 夹具（仅 word/document.xml）
func writeDocxFixture(t *testing.T, path string, paragraphs []string) {
	t.Helper()

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paragraphs {
		sb.WriteString(`<w:p><w:r><w:t>` + p + `</w:t></w:r></w:p>`)
	}
	sb.WriteString(`</w:body></w:document>`)

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 docx 夹具失败：%v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("创建 zip 条目失败：%v", err)
	}
	if _, err := w.Write([]byte(sb.String())); err != nil {
		t.Fatalf("写入 docx 内容失败：%v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败：%v", err)
	}
}

// writeXLSXFixture 生成 xlsx 测试夹具
func writeXLSXFixture(t *testing.T, path string, headers []string, rows [][]string) {
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

// ─── 上传：成功路径 + 延迟解析 ───────────────────────────────

func TestHandleUploadFile_TextThenContentThenDelete(t *testing.T) {
	withTempUploadStore(t)
	r := newUploadTestRouter()

	_, data, msg := doUpload(t, r, "u1", "note.txt", []byte("hello 世界"), nil)
	if data == nil {
		t.Fatalf("上传应成功，msg=%s", msg)
	}
	fileID, _ := data["fileId"].(string)
	if fileID == "" {
		t.Fatal("响应缺少 fileId")
	}
	if data["fileKind"] != "text" || data["fileType"] != "txt" {
		t.Errorf("类型字段异常：%v/%v", data["fileKind"], data["fileType"])
	}
	if data["expireAt"] == nil {
		t.Error("响应应包含失效时间 expireAt")
	}

	// 延迟解析：上传阶段不得产生解析缓存
	if _, ok := fileparse.Cached(fileID); ok {
		t.Error("上传阶段不应触发内容解析")
	}

	// 内容读取
	req := httptest.NewRequest(http.MethodGet, "/ai/agent/uploadFile/content?fileId="+fileID, nil)
	req.Header.Set("X-Test-User", "u1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "hello 世界") {
		t.Errorf("内容读取失败：code=%d body=%q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("应设置 nosniff，实际 %q", got)
	}

	// 删除
	delBody := strings.NewReader(`{"fileId":"` + fileID + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/ai/agent/uploadFile/delete", delBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "u1")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("删除应成功：code=%d body=%s", w.Code, w.Body.String())
	}

	// 删除后内容不可再读取
	req = httptest.NewRequest(http.MethodGet, "/ai/agent/uploadFile/content?fileId="+fileID, nil)
	req.Header.Set("X-Test-User", "u1")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Error("删除后不应再返回文件内容")
	}
}

func TestHandleUploadFile_OwnershipIsolation(t *testing.T) {
	withTempUploadStore(t)
	r := newUploadTestRouter()

	_, data, _ := doUpload(t, r, "u1", "note.txt", []byte("secret"), nil)
	fileID, _ := data["fileId"].(string)

	// 他人读取内容 → 拒绝
	req := httptest.NewRequest(http.MethodGet, "/ai/agent/uploadFile/content?fileId="+fileID, nil)
	req.Header.Set("X-Test-User", "u2")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Error("跨用户读取内容应被拒绝")
	}

	// 他人删除 → 拒绝，且文件仍在
	delBody := strings.NewReader(`{"fileId":"` + fileID + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/ai/agent/uploadFile/delete", delBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "u2")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Error("跨用户删除应被拒绝")
	}
	if !IsUploadOwner(fileID, "u1") {
		t.Error("拒绝跨用户删除后文件应仍在")
	}
}

// ─── 上传：校验失败路径 ──────────────────────────────────────

func TestHandleUploadFile_UnsupportedExt(t *testing.T) {
	withTempUploadStore(t)
	r := newUploadTestRouter()

	w, data, msg := doUpload(t, r, "u1", "malware.exe", []byte("MZ"), nil)
	if w.Code != http.StatusBadRequest || data != nil {
		t.Errorf("不支持的格式应被拒绝：code=%d", w.Code)
	}
	if !strings.Contains(msg, "仅支持") {
		t.Errorf("错误文案应说明支持范围，实际 %q", msg)
	}
}

func TestHandleUploadFile_MagicMismatch(t *testing.T) {
	withTempUploadStore(t)
	r := newUploadTestRouter()

	// 文本内容伪装成 PNG
	_, data, msg := doUpload(t, r, "u1", "fake.png", []byte("this is not a png"), nil)
	if data != nil {
		t.Error("扩展名伪装应被拒绝")
	}
	if !strings.Contains(msg, "不符") {
		t.Errorf("错误文案应说明内容与扩展名不符，实际 %q", msg)
	}
}

func TestHandleUploadFile_EmptyContent(t *testing.T) {
	withTempUploadStore(t)
	r := newUploadTestRouter()

	_, data, msg := doUpload(t, r, "u1", "empty.txt", []byte{}, nil)
	if data != nil {
		t.Errorf("空文件应被拒绝，msg=%s", msg)
	}
}

// ─── 上传：模型能力校验（前置拦截） ──────────────────────────

func TestHandleUploadFile_ImageRequiresVisionModel(t *testing.T) {
	withTempUploadStore(t)
	r := newUploadTestRouter()

	orig := resolveModelCapabilities
	defer func() { resolveModelCapabilities = orig }()

	// 文本模型：图片被拒绝
	resolveModelCapabilities = func(string) map[string]bool { return map[string]bool{} }
	_, data, msg := doUpload(t, r, "u1", "shot.png", pngFixture(t), nil)
	if data != nil {
		t.Error("文本模型下图片应被拒绝")
	}
	if !strings.Contains(msg, "不支持图片输入") {
		t.Errorf("应给出能力不匹配提示，实际 %q", msg)
	}

	// 视觉模型：图片通过，且不解析内容
	resolveModelCapabilities = func(string) map[string]bool { return map[string]bool{CapVision: true} }
	_, data, _ = doUpload(t, r, "u1", "shot.png", pngFixture(t), map[string]string{"modelId": "m-vision"})
	if data == nil {
		t.Fatal("视觉模型下图片应可上传")
	}
	if data["fileKind"] != "image" {
		t.Errorf("fileKind 应为 image，实际 %v", data["fileKind"])
	}
	if _, ok := fileparse.Cached(data["fileId"].(string)); ok {
		t.Error("图片上传阶段不应触发解析")
	}
}

// ─── 延迟解析：按需读取（read_file_data 链路） ────────────────

func TestReadFileData_OnDemandParseAndPaginate(t *testing.T) {
	withTempUploadStore(t)

	path := filepath.Join(t.TempDir(), "data.xlsx")
	writeXLSXFixture(t, path, []string{"id", "name"}, [][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}})

	// 直接登记（模拟上传完成）
	fileID := "fx-paginate"
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: fileID, UserID: "u1", FileName: "data.xlsx", DiskPath: path,
		Ext: ".xlsx", Size: 100, Kind: "table", FileType: "excel",
	})

	read := NewReadFileDataFunc("u1")

	// 首次调用触发解析
	out, err := read(context.Background(), &ReadFileDataInput{FileID: fileID, Limit: 2})
	if err != nil {
		t.Fatalf("按需解析失败：%v", err)
	}
	if out.Type != "table" || out.TotalRows != 3 || out.ReturnedRows != 2 {
		t.Fatalf("分页结果异常：%+v", out)
	}
	if out.Notice == "" {
		t.Error("存在后续数据时应给出分页提示")
	}
	if _, ok := fileparse.Cached(fileID); !ok {
		t.Error("解析结果应写入缓存")
	}

	// 第二页
	out2, err := read(context.Background(), &ReadFileDataInput{FileID: fileID, Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("分页读取失败：%v", err)
	}
	if out2.ReturnedRows != 1 || out2.Rows[0][0] != "3" {
		t.Errorf("第二页数据错误：%+v", out2)
	}

	// 跨用户读取被拒绝
	if _, err := NewReadFileDataFunc("u2")(context.Background(), &ReadFileDataInput{FileID: fileID}); err == nil {
		t.Error("跨用户读取应被拒绝")
	}
}

func TestReadFileData_ExpiredFile(t *testing.T) {
	withTempUploadStore(t)
	if _, err := NewReadFileDataFunc("u1")(context.Background(), &ReadFileDataInput{FileID: "missing"}); err == nil {
		t.Error("文件不存在/已过期应返回错误")
	}
}

// TestReadFileData_DocumentWindowing 文档类（docx）按需解析 + 字符窗口分页。
//
// 夹具为极简 docx（仅 word/document.xml）：无论环境是否具备 python-docx，
// 都应通过「优先实现失败 → 降级到内置 Go 解析」得到文本，验证降级链与工具层协作。
func TestReadFileData_DocumentWindowing(t *testing.T) {
	withTempUploadStore(t)

	path := filepath.Join(t.TempDir(), "report.docx")
	writeDocxFixture(t, path, []string{"季度经营分析报告", "华东区域增长显著"})

	fileID := "fx-doc-window"
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: fileID, UserID: "u1", FileName: "report.docx", DiskPath: path,
		Ext: ".docx", Size: 200, Kind: "doc", FileType: "docx",
	})

	read := NewReadFileDataFunc("u1")

	// 默认窗口：文档类返回全文窗口（limit 语义为「字符数」，默认 30000）
	out, err := read(context.Background(), &ReadFileDataInput{FileID: fileID})
	if err != nil {
		t.Fatalf("docx 按需解析失败：%v", err)
	}
	if out.Type != "text" {
		t.Fatalf("文档类应返回 text 结果，实际 %q", out.Type)
	}
	for _, want := range []string{"季度经营分析报告", "华东区域增长显著"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("应提取到正文 %q，实际：%q", want, out.Text)
		}
	}
	if out.CharCount == 0 {
		t.Error("应返回总字符数")
	}

	// 显式小窗口：应按字符截断并给出续读提示
	windowed, err := read(context.Background(), &ReadFileDataInput{FileID: fileID, Limit: 5})
	if err != nil {
		t.Fatalf("窗口读取失败：%v", err)
	}
	if got := len([]rune(windowed.Text)); got != 5 {
		t.Errorf("窗口大小应为 5 字符，实际 %d（%q）", got, windowed.Text)
	}
	if windowed.CharCount != out.CharCount {
		t.Errorf("字符总数应保持一致：%d vs %d", windowed.CharCount, out.CharCount)
	}
	if windowed.Notice == "" {
		t.Error("存在后续内容时应给出分页提示")
	}
}

// ─── 能力自检接口 ────────────────────────────────────────────

func TestHandleFileSupport(t *testing.T) {
	withTempUploadStore(t)
	r := newUploadTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/ai/agent/fileSupport", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("能力自检接口应可用：%d", w.Code)
	}

	var parsed struct {
		Data struct {
			Exts []struct {
				Ext       string `json:"ext"`
				Kind      string `json:"kind"`
				Parser    string `json:"parser"`
				Supported bool   `json:"supported"`
				Degraded  bool   `json:"degraded"`
				MaxSize   int64  `json:"maxSize"`
			} `json:"exts"`
			PythonAvailable bool `json:"pythonAvailable"`
			StoreTTLSeconds int  `json:"storeTTLSeconds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}

	byExt := map[string]string{}
	bySupported := map[string]bool{}
	byDegraded := map[string]bool{}
	for _, e := range parsed.Data.Exts {
		byExt[e.Ext] = e.Parser
		bySupported[e.Ext] = e.Supported
		byDegraded[e.Ext] = e.Degraded
	}

	// 接口上报的解析器必须与解析域的实际判定一致（docx/pdf 取决于是否具备 Python 依赖）
	for _, ext := range []string{".docx", ".pdf", ".csv", ".png", ".mp4", ".txt"} {
		_, wantImpl, wantOK := fileparse.Describe(ext)
		if byExt[ext] != wantImpl || bySupported[ext] != wantOK {
			t.Errorf("%s 上报不一致：接口=%q/%v，解析域=%q/%v",
				ext, byExt[ext], bySupported[ext], wantImpl, wantOK)
		}
	}
	if bySupported[".csv"] && byExt[".csv"] != "go-csv" {
		t.Errorf(".csv 应走 Go 实现，实际 %q", byExt[".csv"])
	}
	// 降级标记只对 Go 兜底解析成立（Python 主路径不标记降级）
	if want := byExt[".docx"] == "go-docx"; byDegraded[".docx"] != want {
		t.Errorf(".docx degraded 标记应与实现一致：impl=%q degraded=%v", byExt[".docx"], byDegraded[".docx"])
	}
	if parsed.Data.PythonAvailable != fileparse.PythonAvailable() {
		t.Errorf("pythonAvailable 上报不一致：接口=%v，解析域=%v", parsed.Data.PythonAvailable, fileparse.PythonAvailable())
	}
	if parsed.Data.StoreTTLSeconds != int(uploadMaxAge.Seconds()) {
		t.Errorf("暂存 TTL 应为 %v，实际 %d", uploadMaxAge, parsed.Data.StoreTTLSeconds)
	}
}

// ─── 模型能力推断（纯函数，实现位于 system 包） ──────────────

func TestCapabilitiesForModelName(t *testing.T) {
	cases := []struct {
		model         string
		vision, video bool
	}{
		{"gpt-4o", true, false},
		{"GPT-4O-mini", true, false},
		{"claude-3-5-sonnet", true, false},
		{"qwen2.5-vl-7b-instruct", true, true},
		{"gemini-1.5-pro", true, true},
		{"deepseek-chat", false, false},
		{"llama-3.1-70b", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		caps := system.CapabilitiesForModelName(c.model)
		if system.HasCapability(caps, system.CapVision) != c.vision ||
			system.HasCapability(caps, system.CapVideo) != c.video {
			t.Errorf("CapabilitiesForModelName(%q) = %v，期望 vision=%v video=%v", c.model, caps, c.vision, c.video)
		}
	}
}

func TestRequiredCapabilityAndText(t *testing.T) {
	if RequiredCapability("image") != CapVision {
		t.Error("图片应要求 vision 能力")
	}
	if RequiredCapability("video") != CapVideo {
		t.Error("视频应要求 video 能力")
	}
	if RequiredCapability("table") != "" {
		t.Error("表格不应有额外能力要求")
	}
	if !strings.Contains(CapabilityRejectText("image"), "视觉模型") {
		t.Error("图片拒绝文案应给出可操作指引")
	}
}

// ─── 暂存 TTL 与接口层一致性 ─────────────────────────────────

func TestUploadStoreTTLMatchesConst(t *testing.T) {
	if globalUploadStore.TTL() != uploadMaxAge {
		t.Errorf("全局暂存库 TTL 应为 %v，实际 %v", uploadMaxAge, globalUploadStore.TTL())
	}
	if uploadMaxAge != 30*time.Minute {
		t.Errorf("暂存有效期应与既有行为一致（30 分钟），实际 %v", uploadMaxAge)
	}
}
