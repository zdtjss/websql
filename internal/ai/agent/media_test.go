package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websql/internal/ai/agent/uploadstore"

	"github.com/cloudwego/eino/schema"
)

// 多模态媒体注入单测：能力门禁、窗口限制、fileId 去重、预算控制与消息结构。

// newTestInjector 直接构造注入器（绕过模型配置读取，便于离线单测）
func newTestInjector(userID string, allowImage, allowVideo bool) *mediaInjector {
	return &mediaInjector{
		userID:     userID,
		allowImage: allowImage,
		allowVideo: allowVideo,
		budget:     maxMediaInjectBytes,
		injected:   make(map[string]bool),
	}
}

// saveImageFixture 落盘一个真实 PNG 并登记暂存元数据，返回 fileId 与字节内容
func saveImageFixture(t *testing.T, id, owner string, content []byte) {
	t.Helper()
	saveImageFixtureNamed(t, id, owner, "shot.png", content)
}

// saveImageFixtureNamed 同 saveImageFixture，可指定展示文件名
func saveImageFixtureNamed(t *testing.T, id, owner, name string, content []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), id+".png")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入图片夹具失败：%v", err)
	}
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: id, UserID: owner, FileName: name, DiskPath: path,
		Ext: ".png", Size: int64(len(content)), MIME: "image/png", Kind: "image", FileType: "image",
	})
}

func TestProviderSupportsVideoPart(t *testing.T) {
	if providerSupportsVideoPart("ollama") {
		t.Error("ollama 适配器不支持 video_url part")
	}
	if !providerSupportsVideoPart("openai") || !providerSupportsVideoPart(" OpenAI ") {
		t.Error("openai 兼容 provider 应支持 video part")
	}
}

func TestNewMediaInjector_CapabilityGate(t *testing.T) {
	orig := resolveModelCapabilities
	defer func() { resolveModelCapabilities = orig }()

	// 纯文本模型：不注入任何媒体（vision/video 皆无）
	resolveModelCapabilities = func(string) map[string]bool { return map[string]bool{} }
	mi := newMediaInjector("m-text", "u1")
	if mi.allowImage || mi.allowVideo {
		t.Error("纯文本模型不应注入媒体")
	}

	// 视觉模型：注入图片
	resolveModelCapabilities = func(string) map[string]bool { return map[string]bool{CapVision: true} }
	mi = newMediaInjector("m-vision", "u1")
	if !mi.allowImage || mi.allowVideo {
		t.Errorf("视觉模型应仅注入图片：allowImage=%v allowVideo=%v", mi.allowImage, mi.allowVideo)
	}
}

func TestPlanMediaInjection_WindowAndDedupe(t *testing.T) {
	mi := newTestInjector("u1", true, false)

	img := func(id string) AttachmentRef {
		return AttachmentRef{FileID: id, FileName: id + ".png", FileKind: "image"}
	}
	msgs := []SessionMessage{
		{Role: "user", Content: "第一轮", Attachments: []AttachmentRef{img("A")}},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "第二轮", Attachments: []AttachmentRef{img("B")}},
		{Role: "assistant", Content: "ok"},
		// 第三轮重复引用 A（取最新一次出现），并新增 C
		{Role: "user", Content: "第三轮", Attachments: []AttachmentRef{img("A"), img("C")}},
	}

	plan := planMediaInjection(msgs, mi)
	if len(plan) != mediaInjectWindow {
		t.Fatalf("窗口应覆盖最近 %d 条含媒体消息，实际 %d：%+v", mediaInjectWindow, len(plan), plan)
	}
	if _, ok := plan[0]; ok {
		t.Error("超出窗口的更早消息不应注入")
	}
	if refs, ok := plan[2]; !ok || len(refs) != 1 || refs[0].FileID != "B" {
		t.Errorf("第二轮应注入 B，实际 %+v", plan[2])
	}
	if refs, ok := plan[4]; !ok || len(refs) != 2 || refs[0].FileID != "A" || refs[1].FileID != "C" {
		t.Errorf("第三轮应注入 A/C（A 不因历史重复而丢失），实际 %+v", plan[4])
	}
}

func TestPlanMediaInjection_SkipsNonInjectable(t *testing.T) {
	mi := newTestInjector("u1", true, false)

	msgs := []SessionMessage{
		// 仅文本/表格类附件：不占用注入窗口
		{Role: "user", Content: "1", Attachments: []AttachmentRef{{FileID: "t1", FileKind: "table"}}},
		{Role: "user", Content: "2", Attachments: []AttachmentRef{{FileID: "v1", FileKind: "video"}}},
		{Role: "user", Content: "3", Attachments: []AttachmentRef{{FileID: "i1", FileKind: "image"}}},
	}
	plan := planMediaInjection(msgs, mi)
	if len(plan) != 1 {
		t.Fatalf("仅图片类可注入（表格/视频被能力门禁过滤），实际 %+v", plan)
	}
	if refs, ok := plan[2]; !ok || refs[0].FileID != "i1" {
		t.Errorf("第三条消息应注入 i1，实际 %+v", plan)
	}

	// 无任何能力：不产生计划
	miNone := newTestInjector("u1", false, false)
	if len(planMediaInjection(msgs, miNone)) != 0 {
		t.Error("无媒体能力时不应产生注入计划")
	}
}

func TestBuildUserMessage_InjectsBase64Image(t *testing.T) {
	withTempUploadStore(t)

	content := []byte("fake-png-bytes")
	saveImageFixture(t, "img-1", "u1", content)

	mi := newTestInjector("u1", true, false)
	refs := []AttachmentRef{{FileID: "img-1", FileName: "shot.png", FileKind: "image"}}
	msg := mi.buildUserMessage("这张图是什么？", refs)

	if msg.Role != schema.User {
		t.Errorf("角色应为 user，实际 %s", msg.Role)
	}
	// Content 必须为空：OpenAI 兼容 SDK 禁止 Content 与 MultiContent 同时非空
	if msg.Content != "" {
		t.Errorf("注入媒体时 Content 必须置空，实际 %q", msg.Content)
	}
	if len(msg.UserInputMultiContent) != 2 {
		t.Fatalf("应有 text + image 两个 part，实际 %d", len(msg.UserInputMultiContent))
	}
	if msg.UserInputMultiContent[0].Type != schema.ChatMessagePartTypeText ||
		msg.UserInputMultiContent[0].Text != "这张图是什么？" {
		t.Errorf("首个 part 应为正文文本，实际 %+v", msg.UserInputMultiContent[0])
	}
	imgPart := msg.UserInputMultiContent[1]
	if imgPart.Type != schema.ChatMessagePartTypeImageURL || imgPart.Image == nil {
		t.Fatalf("第二个 part 应为 image_url，实际 %+v", imgPart)
	}
	if imgPart.Image.Base64Data == nil || *imgPart.Image.Base64Data != base64.StdEncoding.EncodeToString(content) {
		t.Error("图片内容应为 base64 编码的原始字节")
	}
	if imgPart.Image.MIMEType != "image/png" {
		t.Errorf("MIME 应为 image/png，实际 %q", imgPart.Image.MIMEType)
	}
	if !mi.injected["img-1"] {
		t.Error("注入后应标记 fileId 去重")
	}
	if mi.budget != maxMediaInjectBytes-int64(len(content)) {
		t.Errorf("预算应扣减已注入体积，实际 %d", mi.budget)
	}

	// 同一 fileId 重复构建：第二次不再注入（纯文本消息）
	again := mi.buildUserMessage("再问一次", refs)
	if len(again.UserInputMultiContent) != 0 || again.Content != "再问一次" {
		t.Errorf("同一 fileId 不应重复注入，实际 %+v", again)
	}
}

func TestBuildUserMessage_VideoPart(t *testing.T) {
	withTempUploadStore(t)

	content := []byte("fake-mp4")
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入视频夹具失败：%v", err)
	}
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: "vid-1", UserID: "u1", FileName: "clip.mp4", DiskPath: path,
		Ext: ".mp4", Size: int64(len(content)), MIME: "video/mp4", Kind: "video", FileType: "video",
	})

	mi := newTestInjector("u1", true, true)
	msg := mi.buildUserMessage("总结这个视频", []AttachmentRef{{FileID: "vid-1", FileKind: "video"}})
	if len(msg.UserInputMultiContent) != 2 || msg.UserInputMultiContent[1].Type != schema.ChatMessagePartTypeVideoURL {
		t.Fatalf("应注入 video_url part，实际 %+v", msg.UserInputMultiContent)
	}
	if msg.UserInputMultiContent[1].Video == nil || msg.UserInputMultiContent[1].Video.Base64Data == nil {
		t.Error("视频 part 应携带 base64 数据")
	}
}

func TestBuildUserMessage_BudgetAndFailureFallback(t *testing.T) {
	withTempUploadStore(t)

	content := []byte("0123456789")
	saveImageFixture(t, "img-big", "u1", content)

	mi := newTestInjector("u1", true, false)
	mi.budget = 5 // 小于文件体积：跳过注入并给出系统提示

	msg := mi.buildUserMessage("看看", []AttachmentRef{{FileID: "img-big", FileName: "shot.png", FileKind: "image"}})
	if len(msg.UserInputMultiContent) != 0 {
		t.Fatalf("超出预算时不应注入媒体，实际 %+v", msg.UserInputMultiContent)
	}
	if msg.Content == "" || !strings.Contains(msg.Content, "未随本次消息提供内容") {
		t.Errorf("跳过注入时应向模型说明，实际 %q", msg.Content)
	}

	// 已过期/不存在：静默降级为纯文本
	mi2 := newTestInjector("u1", true, false)
	fallback := mi2.buildUserMessage("看看", []AttachmentRef{{FileID: "missing", FileKind: "image"}})
	if fallback.Content != "看看" || len(fallback.UserInputMultiContent) != 0 {
		t.Errorf("过期文件应降级为纯文本，实际 %+v", fallback)
	}

	// 他人文件：拒绝注入
	mi3 := newTestInjector("u2", true, false)
	foreign := mi3.buildUserMessage("看看", []AttachmentRef{{FileID: "img-big", FileKind: "image"}})
	if len(foreign.UserInputMultiContent) != 0 {
		t.Error("非本人文件不应注入")
	}
	if !mi3.injected["img-big"] {
		t.Error("被拒绝的文件也应标记，避免重复尝试")
	}
}

func TestBuildUserMessage_PartialInjectionKeepsNote(t *testing.T) {
	withTempUploadStore(t)

	ok := []byte("ok") // 2 字节 < 预算 5
	saveImageFixtureNamed(t, "img-ok", "u1", "ok.png", ok)
	big := []byte("0123456789")
	saveImageFixtureNamed(t, "img-huge", "u1", "huge.png", big)

	mi := newTestInjector("u1", true, false)
	mi.budget = 5
	msg := mi.buildUserMessage("看图", []AttachmentRef{
		{FileID: "img-ok", FileName: "ok.png", FileKind: "image"},
		{FileID: "img-huge", FileName: "huge.png", FileKind: "image"},
	})
	if len(msg.UserInputMultiContent) != 2 {
		t.Fatalf("小图应注入（大图超预算跳过），实际 %+v", msg.UserInputMultiContent)
	}
	text := msg.UserInputMultiContent[0].Text
	if !strings.Contains(text, "huge.png") || !strings.Contains(text, "未随本次消息提供内容") {
		t.Errorf("正文本应包含被跳过文件的说明，实际 %q", text)
	}
}
