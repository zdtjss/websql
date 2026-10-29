package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type stubToolCallingModel struct{}

func (stubToolCallingModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}

func (s stubToolCallingModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}

func (s stubToolCallingModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

func TestBuildAnalyzeDataTool(t *testing.T) {
	scope := &PermissionScope{UserID: "u1"}
	tool, err := buildAnalyzeDataTool(context.Background(), stubToolCallingModel{}, "", "sqlite", "", nil, nil, scope, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildAnalyzeDataTool 失败：%v", err)
	}
	if tool == nil {
		t.Fatal("应返回 analyze_data 工具")
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info 失败：%v", err)
	}
	if info.Name != "analyze_data" {
		t.Fatalf("工具名期望 analyze_data，实际 %q", info.Name)
	}
	if info.ParamsOneOf == nil {
		t.Fatal("应提供 request 入参 schema")
	}
}
