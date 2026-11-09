package agent

import (
	"context"
	"strings"
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
	tool, err := buildAnalyzeDataTool(context.Background(), stubToolCallingModel{}, "", "sqlite", "", "", nil, nil, scope, nil, nil, nil)
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

func TestBuildAnalystDialectPrompt(t *testing.T) {
	const v11 = "Oracle Database 11g Enterprise Edition Release 11.2.0.4.0"
	const v12 = "Oracle Database 12c Enterprise Edition Release 12.1.0.2.0"

	p11 := buildAnalystDialectPrompt("oracle", v11)
	if !strings.Contains(p11, "禁止 LIMIT") || !strings.Contains(p11, "ROWNUM") {
		t.Fatalf("11g 子 agent 提示词应禁止 LIMIT 并给出 ROWNUM：%s", p11)
	}
	if strings.Contains(p11, "行数限制：OFFSET n ROWS FETCH NEXT") {
		t.Fatalf("11g 子 agent 提示词不应允许 FETCH：%s", p11)
	}

	p12 := buildAnalystDialectPrompt("oracle", v12)
	if !strings.Contains(p12, "FETCH NEXT") {
		t.Fatalf("12c 子 agent 提示词应允许 FETCH：%s", p12)
	}

	pUnknown := buildAnalystDialectPrompt("oracle", "")
	if !strings.Contains(pUnknown, "版本未知") || !strings.Contains(pUnknown, "ROWNUM") {
		t.Fatalf("版本未知时应按保守写法提示：%s", pUnknown)
	}

	if !strings.Contains(buildAnalystDialectPrompt("mysql", ""), "LIMIT") {
		t.Fatal("MySQL 子 agent 提示词应包含 LIMIT 写法")
	}
}
