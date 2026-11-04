package agent

import (
	"testing"

	admin "websql/internal/app/admin"
)

// TestBuildPermissionScope_WriteScopeExcludesReadOnlyRole 冻结 G1 的写范围语义：
//
//	角色 A（只读，conn 级）+ 角色 B（可写，仅 orders 表）合并后：
//	  - 读范围仍为并集（A 的 conn 级生效）；
//	  - 写范围只能来自 B 自身，不得包含 A 连接的其它表。
func TestBuildPermissionScope_WriteScopeExcludesReadOnlyRole(t *testing.T) {
	db := setupGoldenDB(t)
	if _, err := db.Exec(`insert or replace into t_system_config (id, config_key, config_value, config_type)
		values ('cfg_ws1', 'ai.aiPolicyEnforce', 'true', 'ai')`); err != nil {
		t.Fatalf("写入策略开关失败：%v", err)
	}
	admin.InvalidateAIPolicyCache()

	seedRole(t, db, "ws_read", 0, 1)
	seedPower(t, db, "ws_read", "c1", "", "", "", "conn")
	seedRole(t, db, "ws_write", 1, 1)
	seedPower(t, db, "ws_write", "c1", "s1", "orders", "", "table")
	seedUserRole(t, db, "ws_user", "ws_read")
	seedUserRole(t, db, "ws_user", "ws_write")

	scope := BuildPermissionScope("ws_user", "c1", []string{"s1"})
	if !scope.IsTableAllowedStrict("s1", "secret_salary") {
		t.Fatal("读范围应为 conn 级全量（并集授权）")
	}
	if scope.WriteScope == nil {
		t.Fatal("存在可写角色时 WriteScope 不应为空")
	}
	if got := scope.GetWriteTableAccessLevel("orders"); got != "full" {
		t.Fatalf("orders 应可写，实际 %s", got)
	}
	if got := scope.GetWriteTableAccessLevel("secret_salary"); got != "none" {
		t.Fatalf("只读角色的连接级数据不得进入写范围，secret_salary 实际 %s", got)
	}
	if scope.CanWriteTableRef("s1.secret_salary") {
		t.Fatal("写范围不得因只读角色的 conn 级授权而放行 secret_salary")
	}
	if !scope.CanWriteTableRef("s1.orders") {
		t.Fatal("可写角色自身的表应放行")
	}
	if scope.IsWriteColumnAllowed("secret_salary", "amount") {
		t.Fatal("写范围外的列必须拒绝")
	}
}

// TestBuildPermissionScope_NoWriteRoleMeansNoWriteScope 无写能力角色时写范围为空。
func TestBuildPermissionScope_NoWriteRoleMeansNoWriteScope(t *testing.T) {
	db := setupGoldenDB(t)
	if _, err := db.Exec(`insert or replace into t_system_config (id, config_key, config_value, config_type)
		values ('cfg_ws2', 'ai.aiPolicyEnforce', 'true', 'ai')`); err != nil {
		t.Fatalf("写入策略开关失败：%v", err)
	}
	admin.InvalidateAIPolicyCache()

	seedRole(t, db, "ws_reader_only", 0, 1)
	seedPower(t, db, "ws_reader_only", "c1", "", "", "", "conn")
	seedUserRole(t, db, "ws_user2", "ws_reader_only")

	scope := BuildPermissionScope("ws_user2", "c1", []string{"s1"})
	if scope.WriteScope != nil {
		t.Fatal("没有任何可写角色时 WriteScope 必须为 nil")
	}
	if scope.CanWriteTableRef("s1.orders") {
		t.Fatal("无可写角色时不得放行任何写表")
	}
}

// TestBuildPermissionScope_WriteScopePerConnection 写能力不得泄漏到角色无授权的连接。
func TestBuildPermissionScope_WriteScopePerConnection(t *testing.T) {
	db := setupGoldenDB(t)
	if _, err := db.Exec(`insert or replace into t_system_config (id, config_key, config_value, config_type)
		values ('cfg_ws3', 'ai.aiPolicyEnforce', 'true', 'ai')`); err != nil {
		t.Fatalf("写入策略开关失败：%v", err)
	}
	admin.InvalidateAIPolicyCache()

	seedRole(t, db, "ws_read_c2", 0, 1)
	seedPower(t, db, "ws_read_c2", "c2", "", "", "", "conn")
	seedRole(t, db, "ws_write_c1", 1, 1)
	seedPower(t, db, "ws_write_c1", "c1", "s1", "orders", "", "table")
	seedUserRole(t, db, "ws_user3", "ws_read_c2")
	seedUserRole(t, db, "ws_user3", "ws_write_c1")

	scope := BuildPermissionScope("ws_user3", "c2", []string{"s2"})
	if scope.WriteScope == nil {
		t.Fatal("存在全局可写角色时 WriteScope 不应为空（但应无该连接的授权）")
	}
	if scope.CanWriteTableRef("s2.any_table") {
		t.Fatal("可写角色在 c2 无任何授权，不得因读角色的 conn 级权限放行写操作")
	}
	if got := scope.GetWriteTableAccessLevel("any_table"); got != "none" {
		t.Fatalf("c2 写范围应为 none，实际 %s", got)
	}
}

// TestBuildPermissionScope_AdminKeepsWriteOnFullData 内置管理员的数据权限由
// adminFullPowerDetails 动态生成（RoleId 为空），写范围不能被角色 ID 过滤掉。
func TestBuildPermissionScope_AdminKeepsWriteOnFullData(t *testing.T) {
	db := setupGoldenDB(t)
	if _, err := db.Exec(`insert or replace into t_system_config (id, config_key, config_value, config_type)
		values ('cfg_ws5', 'ai.aiPolicyEnforce', 'true', 'ai')`); err != nil {
		t.Fatalf("写入策略开关失败：%v", err)
	}
	admin.InvalidateAIPolicyCache()

	// 角色名 "admin" 触发内置管理员识别（存量库无 is_admin 列时的名称兜底）。
	seedRole(t, db, "admin", 1, 1)
	if _, err := db.Exec(`insert or replace into t_conn (id, name, db_type) values ('c_admin', 'c_admin', 'sqlite')`); err != nil {
		t.Fatalf("注册连接失败：%v", err)
	}
	seedUserRole(t, db, "ws_admin_user", "admin")

	// 不写 t_power：管理员权限由 adminFullPowerDetails 动态生成。
	scope := BuildPermissionScope("ws_admin_user", "c_admin", []string{"s_admin"})
	if !scope.SkipChecks() {
		t.Fatal("管理员应获得动态全量数据权限")
	}
	if scope.WriteScope == nil {
		t.Fatal("管理员存在可写角色时 WriteScope 不应为空")
	}
	if !scope.CanWriteTableRef("s_admin.any_table") {
		t.Fatal("管理员写范围应与其全量数据范围一致")
	}
	if !scope.IsWriteColumnAllowed("any_table", "any_column") {
		t.Fatal("管理员写范围应覆盖任意列")
	}
}

// TestBuildPermissionScope_WriteScopeRequiresAIDoubleGate 生效期只有同时允许
// allow_modify 与 ai_allow_modify 的角色才能构成 AI 写范围。
func TestBuildPermissionScope_WriteScopeRequiresAIDoubleGate(t *testing.T) {
	db := setupGoldenDB(t)
	if _, err := db.Exec(`insert or replace into t_system_config (id, config_key, config_value, config_type)
		values ('cfg_ws4', 'ai.aiPolicyEnforce', 'true', 'ai')`); err != nil {
		t.Fatalf("写入策略开关失败：%v", err)
	}
	admin.InvalidateAIPolicyCache()

	seedRole(t, db, "ws_read4", 0, 1)
	seedPower(t, db, "ws_read4", "c1", "", "", "", "conn")
	seedRole(t, db, "ws_write_noai", 1, 0)
	seedPower(t, db, "ws_write_noai", "c1", "s1", "orders", "", "table")
	seedUserRole(t, db, "ws_user4", "ws_read4")
	seedUserRole(t, db, "ws_user4", "ws_write_noai")

	scope := BuildPermissionScope("ws_user4", "c1", []string{"s1"})
	if scope.CanAIModifyData() {
		t.Fatal("ai_allow_modify=0 的角色在生效期不得授予 AI 写能力")
	}
	if scope.WriteScope != nil {
		t.Fatal("AI 写范围只能由同时允许两项能力的角色构成")
	}
}
