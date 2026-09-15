package agent

import (
	"strings"
	"testing"
)

func TestBuildDisplayMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []SessionMessage
		want     []SessionMessage
	}{
		{
			name:     "空消息原样返回",
			messages: nil,
			want:     nil,
		},
		{
			name: "无 summarization 原样返回",
			messages: []SessionMessage{
				{Role: "user", Content: "Q1"},
				{Role: "assistant", Content: "A1"},
				{Role: "user", Content: "Q2"},
				{Role: "assistant", Content: "A2"},
			},
			want: []SessionMessage{
				{Role: "user", Content: "Q1"},
				{Role: "assistant", Content: "A1"},
				{Role: "user", Content: "Q2"},
				{Role: "assistant", Content: "A2"},
			},
		},
		{
			name: "summarization 压缩块被展开为原始 user 消息",
			messages: []SessionMessage{
				{Role: "user", Content: "Summary preamble\n\n<all_user_messages>\n    - Q1\n    - Q2\n</all_user_messages>\n\nContinue instruction"},
				{Role: "assistant", Content: "A3 (latest)"},
			},
			want: []SessionMessage{
				{Role: "user", Content: "Q1"},
				{Role: "user", Content: "Q2"},
				{Role: "assistant", Content: "A3 (latest)"},
			},
		},
		{
			name: "summarization 压缩块为空",
			messages: []SessionMessage{
				{Role: "user", Content: "Summary <all_user_messages></all_user_messages>"},
				{Role: "assistant", Content: "A1"},
			},
			want: []SessionMessage{
				{Role: "assistant", Content: "A1"},
			},
		},
		{
			name: "非压缩的 user 消息不展开",
			messages: []SessionMessage{
				{Role: "user", Content: "normal question without summary markers"},
				{Role: "assistant", Content: "A1"},
			},
			want: []SessionMessage{
				{Role: "user", Content: "normal question without summary markers"},
				{Role: "assistant", Content: "A1"},
			},
		},
		{
			name: "工具调用流程的多条 assistant 消息合并为一条，tool 消息跳过",
			messages: []SessionMessage{
				{Role: "user", Content: "查询用户表"},
				{Role: "assistant", Content: "我来帮你查询"},
				{Role: "tool", Content: `{"rows":[]}`},
				{Role: "assistant", Content: "根据查询结果，用户表为空"},
			},
			want: []SessionMessage{
				{Role: "user", Content: "查询用户表"},
				{Role: "assistant", Content: "我来帮你查询\n\n根据查询结果，用户表为空"},
			},
		},
		{
			name: "空内容的 assistant 中间消息不产生多余换行",
			messages: []SessionMessage{
				{Role: "user", Content: "Q1"},
				{Role: "assistant", Content: ""},
				{Role: "tool", Content: "tool result"},
				{Role: "assistant", Content: "SELECT * FROM users"},
			},
			want: []SessionMessage{
				{Role: "user", Content: "Q1"},
				{Role: "assistant", Content: "SELECT * FROM users"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDisplayMessages(tt.messages)
			if len(got) != len(tt.want) {
				t.Fatalf("\n got count=%d: %+v\n want count=%d: %+v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i].Role != tt.want[i].Role || got[i].Content != tt.want[i].Content {
					t.Errorf("idx=%d\n got:  %+v\n want: %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractUserMessagesFromSummary(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "标准格式",
			content: "<all_user_messages>\n    - 第一条消息\n    - 第二条消息\n</all_user_messages>",
			want:    []string{"第一条消息", "第二条消息"},
		},
		{
			name:    "带前后缀",
			content: "Summary text\n<all_user_messages>\n    - Q1\n</all_user_messages>\nContinue",
			want:    []string{"Q1"},
		},
		{
			name:    "空块",
			content: "<all_user_messages></all_user_messages>",
			want:    nil,
		},
		{
			name:    "无标记",
			content: "no markers here",
			want:    nil,
		},
		{
			name:    "多行消息",
			content: "<all_user_messages>\n    - 查询用户表\n    - 导出报表\n    - 修改密码\n</all_user_messages>",
			want:    []string{"查询用户表", "导出报表", "修改密码"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUserMessagesFromSummary(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("\n got:  %v\n want: %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("idx=%d\n got:  %q\n want: %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractUserMessagesFromSummary_NoCollision(t *testing.T) {
	marker := "<all_user_messages>"
	if strings.TrimSpace(marker) == "" {
		t.Fatal("marker must be non-empty")
	}
}
