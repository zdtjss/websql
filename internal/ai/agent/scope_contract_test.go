package agent

import (
	"testing"

	"websql/internal/config"
)

// scope_contract_test.go — 权限契约冻结（文档 13.A / 计划批次 2）。
//
// 这些用例只断言现有语义：任何断言变化都必须先评审权限模型变更。

// newContractScope 构造一个只有 s1.orders 表、orders.id 列权限的远程 Scope。
func newContractScope() *PermissionScope {
	return newScopeForContract("c1",
		map[string]bool{"s1.orders": true},
		map[string]map[string]bool{"s1.orders": {"id": true}},
	)
}

// TestPermissionContract_TableRefForms 冻结引号 / 反引号 / 方括号 / 三段式名称解析。
func TestPermissionContract_TableRefForms(t *testing.T) {
	scope := newContractScope()
	allowed := []string{
		"orders",
		"`orders`",
		`"orders"`,
		"[orders]",
		"s1.orders",
		"`s1`.`orders`",
		`"s1"."orders"`,
		"[s1].[orders]",
		"db1.s1.orders", // 三段式取后两段
	}
	for _, ref := range allowed {
		if !scope.IsTableRefAllowed(ref) {
			t.Fatalf("表引用 %q 应被允许", ref)
		}
	}
	denied := []string{
		"",
		"other_orders",
		"s2.orders", // schema 不匹配
		"db1.s2.orders",
		"S1.orders", // 大小写严格
		"s1.Orders",
	}
	for _, ref := range denied {
		if scope.IsTableRefAllowed(ref) {
			t.Fatalf("表引用 %q 必须拒绝", ref)
		}
	}
}

// TestPermissionContract_AccessLevels 冻结 conn / schema / table / column 单级与组合授权。
func TestPermissionContract_AccessLevels(t *testing.T) {
	full := &PermissionScope{
		UserID: "u1", ConnID: "c1", IsRemote: true, Profile: ProfileWebRemote,
		AllSchemasFull:    true,
		FullAccessSchemas: map[string]bool{"s1": true},
	}
	if !full.IsTableAllowedStrict("s1", "any_table") {
		t.Fatal("schema 级完整授权应覆盖其下任意表")
	}
	if full.IsTableAllowedStrict("s2", "any_table") {
		t.Fatal("未授权 Schema 必须拒绝")
	}

	schemaOnly := &PermissionScope{
		UserID: "u1", ConnID: "c1", IsRemote: true, Profile: ProfileWebRemote,
		FullAccessSchemas: map[string]bool{"s1": true},
	}
	if !schemaOnly.IsTableAllowedStrict("s1", "orders") {
		t.Fatal("schema 级授权应覆盖表")
	}
	if schemaOnly.IsTableAllowedStrict("s2", "orders") {
		t.Fatal("其他 Schema 必须拒绝")
	}

	tableOnly := newScopeForContract("c1", map[string]bool{"s1.orders": true}, nil)
	if !tableOnly.IsTableAllowedStrict("s1", "orders") {
		t.Fatal("表级授权应放行")
	}
	if !tableOnly.IsColumnAllowed("orders", "amount") {
		t.Fatal("表级（全列）授权应放行任意列")
	}

	// 列级配置：权限解析器不会为同表再写入表级键，表中列取交集。
	columnOnly := newScopeForContract("c1", nil, map[string]map[string]bool{"s1.orders": {"id": true}})
	if columnOnly.GetTableAccessLevel("orders") != "column" {
		t.Fatalf("应判定为列级授权，实际 %s", columnOnly.GetTableAccessLevel("orders"))
	}
	if !columnOnly.IsColumnAllowed("orders", "id") {
		t.Fatal("已授权列应放行")
	}
	if columnOnly.IsColumnAllowed("orders", "amount") {
		t.Fatal("未授权列必须拒绝")
	}
}

// TestPermissionContract_IdentifierCase 冻结标识符大小写口径：表名严格、列名折叠。
func TestPermissionContract_IdentifierCase(t *testing.T) {
	scope := newContractScope()
	if scope.IsTableAllowedStrict("s1", "ORDERS") {
		t.Fatal("表名大小写必须严格匹配")
	}
	columnOnly := newScopeForContract("c1", nil, map[string]map[string]bool{"s1.orders": {"id": true}})
	if !columnOnly.IsColumnAllowed("orders", "ID") {
		t.Fatal("列名比较按大小写不敏感（与方言折叠一致）")
	}
	if columnOnly.IsColumnAllowed("orders", "Amount") {
		t.Fatal("未授权列（任意大小写）必须拒绝")
	}
}

// TestPermissionContract_EvaluationSchemasIncludeUnselected 冻结「conn 授权只选 A 而 B 有限制」。
//
// 评估集合必须并入权限明细中出现的全部 Schema，否则只选无限制的 A 时
// 会把 B 的表/列限制误判成连接级完整授权。
func TestPermissionContract_EvaluationSchemasIncludeUnselected(t *testing.T) {
	db := setupGoldenDB(t)
	config.SetActive(&config.Config{IsRemote: true}) // 与其他用例的本地模式隔离

	seedRole(t, db, "r_eval", 1, 1)
	seedPower(t, db, "r_eval", "c1", "", "", "", "conn")
	seedPower(t, db, "r_eval", "c1", "s2", "secret_t", "", "table")
	seedUserRole(t, db, "u_eval", "r_eval")

	schemas := permissionEvaluationSchemas("u_eval", "c1", []string{"s1"})
	found := false
	for _, s := range schemas {
		if s == "s2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("评估集合必须包含权限明细中的 s2，实际 %v", schemas)
	}

	scope := BuildPermissionScope("u_eval", "c1", schemas)
	if scope.SkipChecks() {
		t.Fatal("存在未选中 Schema 的表级限制时不得启用连接级快速路径")
	}
	if scope.IsTableAllowedStrict("s2", "secret_t") != true {
		t.Fatal("s2 的表级授权本身应放行")
	}
	if scope.IsTableAllowedStrict("s2", "other_t") {
		t.Fatal("s2 在存在表级配置后必须收缩到配置的表（表级配置是白名单）")
	}
	// conn 级授权对「无更具体配置」的 Schema 仍然有效；s2 因存在表级配置而收缩。
	if !scope.IsTableAllowedStrict("s1", "anything") {
		t.Fatal("s1 没有更具体的限制，连接级授权应继续覆盖")
	}
}
