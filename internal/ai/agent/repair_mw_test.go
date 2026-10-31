package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

// TestCheckExportAccess_RepairFromArtifact repair_export 允许不带 content/sql：
// 工具会从当前会话元数据回填源内容，实际取数仍由 runner/DataSource 校验。
func TestCheckExportAccess_RepairFromArtifact(t *testing.T) {
	scope := &PermissionScope{UserID: "u1", ConnID: "c1", IsRemote: false, AllowModify: true, AIAllowModify: true}
	sec := &RunSecurityContext{
		UserID: "u1", DefaultConnID: "c1",
		Scopes: map[string]*PermissionScope{"c1": scope},
		Masker: NewNoopMasker(),
	}
	ctx := WithRunSecurity(context.Background(), sec)
	mw := &PermissionMiddleware{Scope: scope, ConnID: "c1"}
	endpoint := func(_ context.Context, _ string, _ ...tool.Option) (string, error) { return "ok", nil }

	// 只改 summary（Excel 场景）：不应再被“无法解析 SQL 参数”拒绝。
	out, err := mw.checkExportAccess(ctx, `{"file":"/exports/x.xlsx","summary":"修正结论"}`, endpoint, "repair_export")
	if err != nil || out != "ok" {
		t.Fatalf("repair_export 无 content/sql 应放行到工具层：out=%q err=%v", out, err)
	}

	// 其他导出工具仍必须提供 content 或 sql。
	if _, err := mw.checkExportAccess(ctx, `{"title":"空报告"}`, endpoint, "export_html"); err == nil {
		t.Fatal("非修复导出工具在无 content/sql 时应继续拒绝")
	}
}
