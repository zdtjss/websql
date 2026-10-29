package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websql/internal/ai/agent/export"
	"websql/internal/ai/agent/uploadstore"

	"github.com/cloudwego/eino/adk/filesystem"
)

func TestUploadFSReadGrepAndWriteDenied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	content := "# 标题一\n第一段内容\n\n## 标题二\n需要查找的关键词在这里\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}
	globalUploadStore.Save(&uploadstore.FileMeta{
		ID: "f-vfs", UserID: "u1", DiskPath: path, FileName: "note.md",
		Ext: ".md", Size: int64(len(content)), Kind: "text", FileType: "markdown",
	})
	defer RemoveUploadedFile("f-vfs")

	backend := newUploadFSBackend(export.NewOSFilesystemBackend(), "u1")
	ctx := WithRunIdentity(context.Background(), RunIdentity{UserID: "u1", SessionID: "s1"})
	ctx = WithUploadFileIDs(ctx, []string{"f-vfs"})

	read, err := backend.Read(ctx, &filesystem.ReadRequest{
		FilePath: "/uploads/f-vfs/content.txt",
		Offset:   1,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("读取虚拟文件失败：%v", err)
	}
	if !strings.Contains(read.Content, "标题一") || !strings.Contains(read.Content, "关键词") {
		t.Fatalf("虚拟文件内容不正确：%q", read.Content)
	}

	matches, err := backend.GrepRaw(ctx, &filesystem.GrepRequest{
		Pattern: "关键词",
		Path:    "/uploads/f-vfs",
	})
	if err != nil {
		t.Fatalf("grep 失败：%v", err)
	}
	if len(matches) == 0 {
		t.Fatal("应至少命中一行")
	}
	if !strings.Contains(matches[0].Path, "content.txt") {
		t.Fatalf("命中路径不正确：%+v", matches[0])
	}

	if _, err := backend.Read(ctx, &filesystem.ReadRequest{FilePath: "/uploads/f-other/content.txt"}); err == nil {
		t.Fatal("非本人文件应被拒绝")
	}
	if err := backend.Write(ctx, &filesystem.WriteRequest{FilePath: "/uploads/f-vfs/content.txt", Content: "x"}); err == nil {
		t.Fatal("虚拟上传路径写入应被拒绝")
	}

	infos, err := backend.LsInfo(ctx, &filesystem.LsInfoRequest{Path: "/uploads/f-vfs"})
	if err != nil {
		t.Fatalf("ls 失败：%v", err)
	}
	if len(infos) < 2 {
		t.Fatalf("应列出 meta.json + content.txt，实际 %+v", infos)
	}
}
