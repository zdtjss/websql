package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websql/internal/ai/agent/uploadstore"
)

// 会话附件（新上传链路）单测：归属/过期过滤、服务端元数据回填、提示词构建与持久化还原。

// saveAttachmentFixture 登记一个暂存文件元数据（不落盘，仅元数据）。
func saveAttachmentFixture(t *testing.T, id, owner, name, kind, ext string, size int64) {
	t.Helper()
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: id, UserID: owner, FileName: name, DiskPath: "/tmp/" + id + ext,
		Ext: ext, Size: size, Kind: kind, FileType: strings.TrimPrefix(ext, "."),
	})
}

func TestSanitizeChatAttachments(t *testing.T) {
	withTempUploadStore(t)
	saveAttachmentFixture(t, "f-own", "u1", "销售数据.xlsx", "table", ".xlsx", 1024)
	saveAttachmentFixture(t, "f-other", "u2", "机密.csv", "table", ".csv", 10)
	saveAttachmentFixture(t, "f-anon", "", "本地.txt", "text", ".txt", 5) // 免登录模式：无归属

	t.Run("空列表返回 nil", func(t *testing.T) {
		if got := SanitizeChatAttachments(nil, "u1"); got != nil {
			t.Errorf("nil 输入应返回 nil，实际 %v", got)
		}
	})

	t.Run("过滤不存在与空标识", func(t *testing.T) {
		refs := []AttachmentRef{
			{FileID: ""},
			{FileID: "  "},
			{FileID: "not-exist"},
			{FileID: "f-own"},
		}
		got := SanitizeChatAttachments(refs, "u1")
		if len(got) != 1 || got[0].FileID != "f-own" {
			t.Fatalf("应仅保留本人有效文件，实际 %+v", got)
		}
	})

	t.Run("丢弃他人文件", func(t *testing.T) {
		got := SanitizeChatAttachments([]AttachmentRef{{FileID: "f-other"}}, "u1")
		if got != nil {
			t.Errorf("他人文件应被丢弃，实际 %+v", got)
		}
	})

	t.Run("无归属文件放行（本地模式）", func(t *testing.T) {
		got := SanitizeChatAttachments([]AttachmentRef{{FileID: "f-anon"}}, "u1")
		if len(got) != 1 {
			t.Fatalf("无归属文件应放行，实际 %+v", got)
		}
	})

	t.Run("去重与服务端元数据回填", func(t *testing.T) {
		refs := []AttachmentRef{
			// 客户端伪造展示名/类别/大小：一律被服务端元数据覆盖
			{FileID: "f-own", FileName: "伪造名.exe", FileKind: "video", FileSize: 999999999},
			{FileID: "f-own"},
		}
		got := SanitizeChatAttachments(refs, "u1")
		if len(got) != 1 {
			t.Fatalf("重复引用应去重，实际 %+v", got)
		}
		if got[0].FileName != "销售数据.xlsx" || got[0].FileKind != "table" || got[0].FileSize != 1024 {
			t.Errorf("展示字段应按服务端元数据回填，实际 %+v", got[0])
		}
		if got[0].FileExt != ".xlsx" || got[0].ExpireAt == 0 {
			t.Errorf("应回填扩展名与过期时间，实际 %+v", got[0])
		}
	})

	t.Run("超出上限截断", func(t *testing.T) {
		refs := make([]AttachmentRef, 0, maxChatAttachments+5)
		for i := 0; i < maxChatAttachments+5; i++ {
			id := fmt.Sprintf("f-cap-%d", i)
			saveAttachmentFixture(t, id, "u1", "a.txt", "text", ".txt", 1)
			refs = append(refs, AttachmentRef{FileID: id})
		}
		got := SanitizeChatAttachments(refs, "u1")
		if len(got) != maxChatAttachments {
			t.Errorf("应截断到 %d 个，实际 %d", maxChatAttachments, len(got))
		}
	})
}

func TestBuildAttachmentsPrompt(t *testing.T) {
	if got := buildAttachmentsPrompt(nil); got != "" {
		t.Errorf("无附件时应返回空串，实际 %q", got)
	}

	refs := []AttachmentRef{
		{FileID: "f1", FileName: "销售.xlsx", FileKind: "table", FileSize: 2048},
		{FileID: "f2", FileName: "报告.pdf", FileKind: "doc", FileSize: 1024},
		{FileID: "f3", FileName: "截图.png", FileKind: "image", FileSize: 512},
		{FileID: "f4", FileName: "演示.mp4", FileKind: "video", FileSize: 4096},
		{FileID: "f5", FileName: "销售2.xlsx", FileKind: "table", FileSize: 1024},
	}
	got := buildAttachmentsPrompt(refs)

	// 元信息：文件名 / fileId / 大小
	for _, want := range []string{"销售.xlsx", "f1", "2KB", "报告.pdf", "截图.png", "演示.mp4"} {
		if !strings.Contains(got, want) {
			t.Errorf("提示词应包含 %q，实际：%s", want, got)
		}
	}
	// 按需读取 + 不可信数据声明
	if !strings.Contains(got, "read_file_data") {
		t.Error("提示词应引导模型按需调用 read_file_data")
	}
	if !strings.Contains(got, "不可信") {
		t.Error("提示词必须包含不可信数据安全声明")
	}
	if !strings.Contains(got, "不得") {
		t.Error("安全声明应明确禁止执行文件内容中的指令")
	}
	// 类别指引
	for _, want := range []string{"表格类文件", "文本/文档类文件", "图片类文件", "视频类文件"} {
		if !strings.Contains(got, want) {
			t.Errorf("提示词应包含类别指引 %q", want)
		}
	}
	// 同类去重：表格指引只出现一次
	if n := strings.Count(got, "表格类文件"); n != 1 {
		t.Errorf("同类指引应去重，表格类出现 %d 次", n)
	}
}

func TestFormatMessageAttachments(t *testing.T) {
	if got := formatMessageAttachments(nil); got != "" {
		t.Errorf("无附件时应返回空串，实际 %q", got)
	}
	got := formatMessageAttachments([]AttachmentRef{
		{FileID: "f1", FileName: "销售.xlsx", FileKind: "table"},
	})
	if !strings.Contains(got, "f1") || !strings.Contains(got, "销售.xlsx") || !strings.Contains(got, "表格") {
		t.Errorf("历史消息附件引用应含 fileId/文件名/类别，实际 %q", got)
	}
}

// TestBuildDisplayMessagesKeepsAttachments 历史回看：用户消息的附件引用需透传到展示层。
func TestBuildDisplayMessagesKeepsAttachments(t *testing.T) {
	refs := []AttachmentRef{{FileID: "f1", FileName: "销售.xlsx", FileKind: "table"}}
	msgs := []SessionMessage{
		{Role: "user", Content: "分析这份数据", Attachments: refs},
		{Role: "assistant", Content: "好的"},
		{Role: "tool", Content: "{}", ToolCallID: "t1", ToolName: "query_data"},
		{Role: "assistant", Content: "结果如下"},
		{Role: "user", Content: "再汇总下"},
	}
	display := buildDisplayMessages(msgs)
	if len(display) != 3 {
		t.Fatalf("应合并为 3 条展示消息，实际 %d：%+v", len(display), display)
	}
	if len(display[0].Attachments) != 1 || display[0].Attachments[0].FileID != "f1" {
		t.Errorf("用户消息附件应透传，实际 %+v", display[0])
	}
	if len(display[2].Attachments) != 0 {
		t.Errorf("无附件消息不应携带附件，实际 %+v", display[2])
	}
}

// TestCollectAttachmentIDs 删除会话：从消息中汇总待清理的文件 ID（去重、忽略空值）。
func TestCollectAttachmentIDs(t *testing.T) {
	if got := collectAttachmentIDs(nil); len(got) != 0 {
		t.Errorf("空消息应返回空列表，实际 %+v", got)
	}

	msgs := []SessionMessage{
		{Role: "user", Content: "a", Attachments: []AttachmentRef{{FileID: "f1"}, {FileID: "f2"}}},
		{Role: "assistant", Content: "x"},
		{Role: "user", Content: "b", Attachments: []AttachmentRef{{FileID: "f2"}, {FileID: "  "}, {FileID: " f3 "}}},
	}
	got := collectAttachmentIDs(msgs)
	want := []string{"f1", "f2", "f3"}
	if len(got) != len(want) {
		t.Fatalf("应汇总出 %d 个文件 ID，实际 %+v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个应为 %q，实际 %q", i, want[i], got[i])
		}
	}
}

// TestRemoveSessionAttachments 删除会话后的文件清理：仅删除本人文件，磁盘文件一并移除，缺失文件幂等跳过。
func TestRemoveSessionAttachments(t *testing.T) {
	withTempUploadStore(t)
	dir := t.TempDir()
	ownPath := filepath.Join(dir, "own.txt")
	otherPath := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(ownPath, []byte("own"), 0o600); err != nil {
		t.Fatalf("准备本人文件失败：%v", err)
	}
	if err := os.WriteFile(otherPath, []byte("other"), 0o600); err != nil {
		t.Fatalf("准备他人文件失败：%v", err)
	}
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: "f-own", UserID: "u1", DiskPath: ownPath, FileName: "own.txt", Ext: ".txt", Size: 3, Kind: "text",
	})
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: "f-other", UserID: "u2", DiskPath: otherPath, FileName: "other.txt", Ext: ".txt", Size: 5, Kind: "text",
	})
	t.Cleanup(func() { RemoveUploadedFile("f-other") })

	RemoveSessionAttachments([]string{"f-own", "f-other", "f-missing"}, "u1")

	if _, ok := LookupUploadedFile("f-own"); ok {
		t.Error("本人文件元数据应随会话删除被清理")
	}
	if _, err := os.Stat(ownPath); !os.IsNotExist(err) {
		t.Errorf("本人文件磁盘内容应被删除，err=%v", err)
	}
	if _, ok := LookupUploadedFile("f-other"); !ok {
		t.Error("他人文件不得被删除")
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Errorf("他人文件磁盘内容不应被删除：%v", err)
	}
}
