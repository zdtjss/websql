package agent

import (
	"context"
	"errors"
	"testing"
)

// TestExportQueryRunner_RejectsBeforeQuery 验证原生导出在取数前完成权限校验。
//
// 使用 nil 连接：若校验未拦截，取数会返回「数据库连接不可用」而不是权限错误，
// 因此可以区分「权限拒绝」与「通过校验后执行失败」。
func TestExportQueryRunner_RejectsBeforeQuery(t *testing.T) {
	sec := &RunSecurityContext{
		Profile:       ProfileDesktopLocal,
		UserID:        "u1",
		DefaultConnID: "c1",
		Masker:        NewNoopMasker(),
	}
	ctx := WithRunSecurity(context.Background(), sec)

	tableScope := newScopeForContract("c1", map[string]bool{"s1.orders": true}, nil)
	runner := newExportQueryRunner(nil, tableScope, "u1")

	assertPermissionDenied := func(name string, err error) {
		t.Helper()
		var pe *PermissionError
		if err == nil || !errors.As(err, &pe) {
			t.Fatalf("%s：应因权限拒绝，实际 err=%v", name, err)
		}
	}

	_, err := runner.QueryForExport(ctx, "DELETE FROM s1.orders", 100)
	assertPermissionDenied("写语句", err)

	_, err = runner.QueryForExport(ctx, "SELECT id FROM s1.secret", 100)
	assertPermissionDenied("无权表", err)

	columnScope := newScopeForContract("c1", nil, map[string]map[string]bool{"s1.orders": {"id": true}})
	columnRunner := newExportQueryRunner(nil, columnScope, "u1")
	_, err = columnRunner.QueryForExport(ctx, "SELECT * FROM s1.orders", 100)
	assertPermissionDenied("列级表 SELECT *", err)

	_, err = columnRunner.QueryForExport(ctx, "SELECT amount FROM s1.orders", 100)
	assertPermissionDenied("未授权列", err)

	// 通过权限校验后才因缺少连接失败（非权限错误）
	_, err = runner.QueryForExport(ctx, "SELECT id FROM s1.orders", 100)
	if err == nil {
		t.Fatal("nil 连接应返回执行错误")
	}
	var pe *PermissionError
	if errors.As(err, &pe) {
		t.Fatalf("授权查询不应被权限拒绝：%v", err)
	}
}

func TestExportQueryRunner_RequiresSecurityContext(t *testing.T) {
	scope := newScopeForContract("c1", map[string]bool{"s1.orders": true}, nil)
	runner := newExportQueryRunner(nil, scope, "u1")
	if _, err := runner.QueryForExport(context.Background(), "SELECT id FROM s1.orders", 100); err == nil {
		t.Fatal("缺少运行安全上下文时必须拒绝导出")
	}
}
