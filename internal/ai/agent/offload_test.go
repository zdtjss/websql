package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/schema"
)

func TestReadOffloadLineWindowAndACL(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REDUCTION_OFFLOAD_DIR", root)

	ctx := WithRunIdentity(context.Background(), RunIdentity{UserID: "u1", SessionID: "s1"})
	dir := filepath.Join(offloadRootForContext(ctx), "trunc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建 offload 目录失败：%v", err)
	}
	file := filepath.Join(dir, "call-1")
	content := "{\"columns\":[\"id\"]}\n[1,\"a\"]\n[2,\"b\"]\n[3,\"c\"]\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("写入 offload 文件失败：%v", err)
	}

	read := NewReadOffloadFunc()
	out, err := read(ctx, &ReadOffloadInput{Path: file, Offset: 2, Limit: 2})
	if err != nil {
		t.Fatalf("read_offload 失败：%v", err)
	}
	if out.ReturnedLines != 2 || len(out.Lines) != 2 {
		t.Fatalf("期望返回 2 行，实际 %+v", out)
	}
	if out.Lines[0].Line != 2 || !strings.Contains(out.Lines[0].Text, `[1,"a"]`) {
		t.Fatalf("首行内容不正确：%+v", out.Lines[0])
	}

	// 其他用户不能读取同一路径
	otherCtx := WithRunIdentity(context.Background(), RunIdentity{UserID: "u2", SessionID: "s1"})
	if _, err := read(otherCtx, &ReadOffloadInput{Path: file}); err == nil {
		t.Fatal("跨用户读取 offload 应被拒绝")
	}

	// 路径穿越必须被拒绝
	escape := filepath.Join(dir, "..", "..", "..", safeSegment("u2"), "s1", "trunc", "x")
	if _, err := read(ctx, &ReadOffloadInput{Path: escape}); err == nil {
		t.Fatal("路径穿越应被拒绝")
	}

	// 缺少运行身份必须拒绝
	if _, err := read(context.Background(), &ReadOffloadInput{Path: file}); err == nil {
		t.Fatal("缺少运行身份应被拒绝")
	}

	// 符号链接必须拒绝（Windows 创建链接通常需要权限，跳过）
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link")
		if err := os.Symlink(file, link); err == nil {
			if _, err := read(ctx, &ReadOffloadInput{Path: link}); err == nil {
				t.Fatal("符号链接应被拒绝")
			}
		}
	}
}

func TestQueryTruncHandlerWritesJSONL(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REDUCTION_OFFLOAD_DIR", root)
	ctx := WithRunIdentity(context.Background(), RunIdentity{UserID: "u1", SessionID: "s1"})

	rows := make([]map[string]any, 0, 80)
	for i := 0; i < 80; i++ {
		rows = append(rows, map[string]any{
			"id":   i,
			"text": strings.Repeat("x", 1024),
		})
	}
	raw, err := json.Marshal(&QueryOutput{Columns: []string{"id", "text"}, Data: rows, Count: len(rows)})
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	detail := &reduction.ToolDetail{
		ToolContext: &adk.ToolContext{CallID: "call-2"},
		ToolResult: &schema.ToolResult{Parts: []schema.ToolOutputPart{
			{Type: schema.ToolPartTypeText, Text: string(raw)},
		}},
	}

	result, err := queryTruncHandler(ctx, detail)
	if err != nil {
		t.Fatalf("queryTruncHandler 失败：%v", err)
	}
	if !result.NeedTrunc || !result.NeedOffload {
		t.Fatalf("大结果应触发截断/落盘：%+v", result)
	}
	if result.ToolResult == nil || !strings.Contains(result.ToolResult.Parts[0].Text, "read_offload") {
		t.Fatalf("提示应包含 read_offload：%+v", result.ToolResult)
	}
	if !strings.Contains(filepath.ToSlash(result.OffloadFilePath), "/users/") {
		t.Fatalf("offload 路径应按用户隔离：%s", result.OffloadFilePath)
	}
	lines := strings.Split(strings.TrimRight(result.OffloadContent, "\n"), "\n")
	if len(lines) != len(rows)+1 {
		t.Fatalf("JSONL 行数期望 %d，实际 %d", len(rows)+1, len(lines))
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &meta); err != nil {
		t.Fatalf("首行元数据不是合法 JSON：%v", err)
	}
	if meta["format"] != "jsonl" {
		t.Fatalf("元数据 format 期望 jsonl，实际 %+v", meta)
	}
}

func TestOsfsReductionBackendWritesUserScopedPath(t *testing.T) {
	root := t.TempDir()
	backend := &osfsReductionBackend{rootDir: root}
	path := filepath.Join(root, safeSegment("u1"), safeSegment("s1"), "trunc", "call-3")
	if err := backend.Write(context.Background(), &filesystem.WriteRequest{FilePath: path, Content: "ok"}); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "ok" {
		t.Fatalf("写入内容不正确：data=%q err=%v", data, err)
	}
	outside := filepath.Join(root, "..", "escape.txt")
	if err := backend.Write(context.Background(), &filesystem.WriteRequest{FilePath: outside, Content: "x"}); err == nil {
		t.Fatal("越界路径应被拒绝")
	}
}

func TestReadOffloadRejectsStalePermissionRevision(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REDUCTION_OFFLOAD_DIR", root)
	base := WithRunIdentity(context.Background(), RunIdentity{UserID: "u1", SessionID: "s1"})
	ctxV1 := WithRunSecurity(base, &RunSecurityContext{UserID: "u1", Revision: 1, Profile: ProfileDesktopLocal})
	ctxV2 := WithRunSecurity(base, &RunSecurityContext{UserID: "u1", Revision: 2, Profile: ProfileDesktopLocal})

	path, err := reductionOffloadPath(ctxV1, "trunc", "call-old")
	if err != nil {
		t.Fatalf("生成路径失败：%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old-data\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	read := NewReadOffloadFunc()
	if _, err := read(ctxV1, &ReadOffloadInput{Path: path}); err != nil {
		t.Fatalf("同修订号应可读：%v", err)
	}
	if _, err := read(ctxV2, &ReadOffloadInput{Path: path}); err == nil {
		t.Fatal("权限修订号变化后，旧 offload 应不可读")
	}
}

func TestSafeSegment(t *testing.T) {
	if got := safeSegment("u1"); got != "u1" {
		t.Fatalf("safeSegment(u1) = %q", got)
	}
	if got := safeSegment("../x"); strings.Contains(got, "..") || strings.Contains(got, "/") {
		t.Fatalf("safeSegment 未清理路径分隔符：%q", got)
	}
}
