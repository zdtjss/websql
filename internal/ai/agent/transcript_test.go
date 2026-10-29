package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestWriteTranscriptReadableByReadOffload(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REDUCTION_OFFLOAD_DIR", root)
	ctx := WithRunIdentity(context.Background(), RunIdentity{UserID: "u1", SessionID: "s1"})
	messages := []*schema.Message{
		schema.UserMessage("用户问题"),
		schema.AssistantMessage("助手回答", nil),
	}
	path, err := writeTranscript(ctx, messages)
	if err != nil {
		t.Fatalf("写入 transcript 失败：%v", err)
	}
	if path == "" {
		t.Fatal("应生成 transcript 路径")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("transcript 文件不存在：%v", err)
	}

	out, err := NewReadOffloadFunc()(ctx, &ReadOffloadInput{Path: path, Offset: 1, Limit: 10})
	if err != nil {
		t.Fatalf("read_offload 读取 transcript 失败：%v", err)
	}
	if out.ReturnedLines < 2 || !strings.Contains(out.Lines[0].Text, "用户问题") {
		t.Fatalf("transcript 内容不正确：%+v", out)
	}
}
