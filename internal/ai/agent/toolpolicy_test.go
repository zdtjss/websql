package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// toolpolicy_test.go — 工具能力策略（文档 13.A：exec_sql 四层拦截）。

func TestLookupToolPolicy_Registry(t *testing.T) {
	policy, ok := lookupToolPolicy("exec_sql")
	if !ok {
		t.Fatal("exec_sql 必须登记在策略表（未登记即 fail-closed）")
	}
	if policy.Capability != CapabilityAIWrite {
		t.Fatalf("exec_sql 应为 AI 写能力，实际 %s", policy.Capability)
	}
	if !policy.RequiresApproval || !policy.DataTool {
		t.Fatal("exec_sql 必须要求审批且标记为数据工具")
	}
	if _, ok := lookupToolPolicy("mcp_sql_exec"); ok {
		t.Fatal("伪造工具名不得命中策略表")
	}
}

// TestDenyByCapability_ExecSQLRequiresDualGate 冻结 AI 写双门槛与既有修改能力边界。
func TestDenyByCapability_ExecSQLRequiresDualGate(t *testing.T) {
	compat := &PermissionMiddleware{Scope: &PermissionScope{
		IsRemote: true, AllowModify: true, AIPolicyEnforce: false,
	}}
	if err := compat.denyByCapability("exec_sql"); err != nil {
		t.Fatalf("兼容期应沿用 allow_modify：%v", err)
	}

	enforced := &PermissionMiddleware{Scope: &PermissionScope{
		IsRemote: true, AllowModify: true, AIAllowModify: false, AIPolicyEnforce: true,
	}}
	if err := enforced.denyByCapability("exec_sql"); err == nil {
		t.Fatal("生效期未开 AI 开关必须拒绝 exec_sql")
	}

	both := &PermissionMiddleware{Scope: &PermissionScope{
		IsRemote: true, AllowModify: true, AIAllowModify: true, AIPolicyEnforce: true,
	}}
	if err := both.denyByCapability("exec_sql"); err != nil {
		t.Fatalf("双开关允许时应通过能力检查：%v", err)
	}
	for _, name := range []string{"execute", "write_file", "edit_file"} {
		if err := both.denyByCapability(name); err != nil {
			t.Fatalf("允许修改数据时 %s 应通过能力检查：%v", name, err)
		}
	}

	noModify := &PermissionMiddleware{Scope: &PermissionScope{IsRemote: true}}
	for _, name := range []string{"execute", "write_file", "edit_file"} {
		if err := noModify.denyByCapability(name); err == nil {
			t.Fatalf("禁止修改数据时必须拒绝 %s", name)
		}
	}
	if err := noModify.denyByCapability("get_current_date_info"); err != nil {
		t.Fatalf("只读工具不应受修改能力影响：%v", err)
	}
}

// TestToolDispatch_ExecSQLCapabilityDeniedSyncAndStream 冻结同步/流式两条调用路径的能力拦截。
func TestToolDispatch_ExecSQLCapabilityDeniedSyncAndStream(t *testing.T) {
	scope := &PermissionScope{
		UserID:            "u1",
		ConnID:            "c1",
		SchemaName:        "s1",
		IsRemote:          true,
		Profile:           ProfileWebRemote,
		FullAccessSchemas: map[string]bool{},
		AllowedTables:     map[string]bool{"s1.orders": true},
		AllowModify:       true,
		AIAllowModify:     false,
		AIPolicyEnforce:   true,
	}
	mw := &PermissionMiddleware{Scope: scope}
	ctx := testRunContext(mw)

	invokable := func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
		return "ok", nil
	}
	ep, err := mw.WrapInvokableToolCall(ctx, invokable, &adk.ToolContext{Name: "exec_sql"})
	if err != nil {
		t.Fatalf("包装同步调用失败：%v", err)
	}
	if _, callErr := ep(ctx, `{"sql":"update s1.orders set amount=1"}`); !isPermissionError(callErr) {
		t.Fatalf("同步伪造调用必须被能力拦截，实际 err=%v", callErr)
	}

	streamable := func(_ context.Context, _ string, _ ...tool.Option) (*schema.StreamReader[string], error) {
		return schema.StreamReaderFromArray([]string{"ok"}), nil
	}
	streamEp, err := mw.WrapStreamableToolCall(ctx, streamable, &adk.ToolContext{Name: "exec_sql"})
	if err != nil {
		t.Fatalf("包装流式调用失败：%v", err)
	}
	if _, callErr := streamEp(ctx, `{"sql":"update s1.orders set amount=1"}`); !isPermissionError(callErr) {
		t.Fatalf("流式伪造调用必须被能力拦截，实际 err=%v", callErr)
	}
}

func isPermissionError(err error) bool {
	var permErr *PermissionError
	return errors.As(err, &permErr)
}
