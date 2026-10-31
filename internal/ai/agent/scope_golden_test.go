package agent

import (
	"sync"
	"testing"

	admin "websql/internal/app/admin"
	"websql/internal/config"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

var (
	goldenDBOnce sync.Once
	goldenDBInst *sqlx.DB
	goldenDBErr  error
)

// setupGoldenDB 构建一个进程级内存管理库，用于验证 BuildPermissionScope 的真实权限语义。
//
// 复用同一个库的原因：admin 包的用户/角色仓储是惰性单例，首次 getDB() 后固定持有该连接，
// 多个测试各自新建库会导致后续用例读到旧库。
func setupGoldenDB(t *testing.T) *sqlx.DB {
	t.Helper()
	goldenDBOnce.Do(func() {
		db, err := sqlx.Open("sqlite", ":memory:")
		if err != nil {
			goldenDBErr = err
			return
		}
		db.SetMaxOpenConns(1)
		stmts := []string{
			`create table t_role (id text primary key, name text, view_classic integer default 0,
			allow_modify integer default 1, ai_allow_modify integer not null default 1)`,
			`create table t_power (id text primary key, role_id text, conn_id text, schema_name text,
			table_name text, column_name text, power_level text)`,
			`create table t_user_role (id text primary key, user_id text, role_id text)`,
			`create table t_conn (id text primary key, name text, db_type text)`,
			`create table t_system_config (id text primary key, config_key text unique, config_value text,
			config_type text, remark text, create_time datetime, update_time datetime)`,
		}
		for _, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				goldenDBErr = err
				return
			}
		}
		admin.Init(db)
		// 测试固定为远程模式（零值 Profile 也按远程处理）
		config.SetActive(&config.Config{IsRemote: true})
		goldenDBInst = db
	})
	if goldenDBErr != nil {
		t.Fatalf("初始化管理库失败：%v", goldenDBErr)
	}
	return goldenDBInst
}

func seedRole(t *testing.T, db *sqlx.DB, id string, allowModify, aiAllowModify int) {
	t.Helper()
	// insert or replace：CI 以 -count=100 重复运行，种子必须幂等。
	if _, err := db.Exec(`insert or replace into t_role (id, name, allow_modify, ai_allow_modify) values (?, ?, ?, ?)`,
		id, id, allowModify, aiAllowModify); err != nil {
		t.Fatalf("插入角色失败：%v", err)
	}
}

func seedPower(t *testing.T, db *sqlx.DB, roleID, connID, schemaName, tableName, columnName, level string) {
	t.Helper()
	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	if _, err := db.Exec(`insert or replace into t_power (id, role_id, conn_id, schema_name, table_name, column_name, power_level)
		values (?, ?, ?, ?, ?, ?, ?)`,
		roleID+"-"+level+"-"+schemaName+"-"+tableName+"-"+columnName,
		roleID, connID, nullable(schemaName), nullable(tableName), nullable(columnName), level); err != nil {
		t.Fatalf("插入权限失败：%v", err)
	}
}

func seedUserRole(t *testing.T, db *sqlx.DB, userID, roleID string) {
	t.Helper()
	if _, err := db.Exec(`insert or replace into t_user_role (id, user_id, role_id) values (?, ?, ?)`,
		userID+"-"+roleID, userID, roleID); err != nil {
		t.Fatalf("插入用户角色失败：%v", err)
	}
}

// TestBuildPermissionScope_GoldenContract 冻结权限语义：任何改动都必须先改这里并评审。
func TestBuildPermissionScope_GoldenContract(t *testing.T) {
	db := setupGoldenDB(t)
	// -count=100 重复运行时清理上一轮写入的开关，保证「切换前」断言独立。
	if _, err := db.Exec(`delete from t_system_config where config_key = 'ai.aiPolicyEnforce'`); err != nil {
		t.Fatalf("清理策略开关失败：%v", err)
	}
	// 测试直接写库，不会触发应用的配置变更钩子：手动失效进程内缓存。
	admin.InvalidateAIPolicyCache()

	// 角色 r_conn：conn 级授权 + s1.restricted 表级限制
	seedRole(t, db, "r_conn", 1, 0)
	seedPower(t, db, "r_conn", "c1", "", "", "", "conn")
	seedPower(t, db, "r_conn", "c1", "s1", "restricted", "", "table")
	seedUserRole(t, db, "u_conn", "r_conn")

	scope := BuildPermissionScope("u_conn", "c1", []string{"s1"})
	if !scope.IsRemote {
		t.Fatal("应处于远程模式")
	}
	if scope.SkipChecks() {
		t.Fatal("存在表级限制时不得启用连接级快速路径")
	}
	if !scope.IsTableAllowedStrict("s1", "restricted") {
		t.Fatal("conn 授权 + 表级配置应允许该表")
	}
	// 契约：某 Schema 存在更具体的 table/column 配置时，该 Schema 按具体配置收缩
	if scope.IsTableAllowedStrict("s1", "orders") {
		t.Fatal("存在表级配置的 Schema 应收缩到具体配置，不得恢复 conn 全量授权")
	}

	// 角色 r_col：仅 s1.restricted 的 id 列
	seedRole(t, db, "r_col", 1, 1)
	seedPower(t, db, "r_col", "c1", "s1", "restricted", "id", "column")
	seedUserRole(t, db, "u_col", "r_col")
	seedUserRole(t, db, "u_both", "r_conn")
	seedUserRole(t, db, "u_both", "r_col")

	// 单角色：column 配置优先，不能因同表另有 table 配置恢复全列
	seedPower(t, db, "r_col", "c1", "s1", "restricted", "", "table")
	colOnly := BuildPermissionScope("u_col", "c1", []string{"s1"})
	if colOnly.GetTableAccessLevel("restricted") != "column" {
		t.Fatalf("单角色同表 table+column 应列级优先，实际 %s", colOnly.GetTableAccessLevel("restricted"))
	}
	if !colOnly.IsColumnAllowed("restricted", "id") {
		t.Fatal("已授权列应放行")
	}
	if colOnly.IsColumnAllowed("restricted", "amount") {
		t.Fatal("未授权列必须拒绝")
	}

	// 多角色合并：一个角色的 table(full) 授权可放行另一角色的列级限制（现有合并规则）
	both := BuildPermissionScope("u_both", "c1", []string{"s1"})
	if both.GetTableAccessLevel("restricted") != "full" {
		t.Fatalf("多角色应取并集（table full 放行），实际 %s", both.GetTableAccessLevel("restricted"))
	}
	if !both.IsColumnAllowed("restricted", "amount") {
		t.Fatal("多角色合并后应放行全部列")
	}

	// AI 写能力：同角色双允许才成立（r_col: allow=1 ai=1；r_conn: ai=0）
	seedUserRole(t, db, "u_ainomod", "r_conn")
	noAI := BuildPermissionScope("u_ainomod", "c1", []string{"s1"})
	if noAI.AIPolicyEnforce {
		t.Fatal("未开启 ai.aiPolicyEnforce 时不应宣称独立开关生效")
	}
	if !noAI.CanAIModifyData() {
		t.Fatal("兼容期应沿用 allow_modify")
	}

	// 打开 AIPolicyEnforce 后：只有同一角色双允许才可写
	if _, err := db.Exec(`insert or replace into t_system_config (id, config_key, config_value, config_type)
		values ('cfg1', 'ai.aiPolicyEnforce', 'true', 'ai')`); err != nil {
		t.Fatalf("写入策略开关失败：%v", err)
	}
	admin.InvalidateAIPolicyCache()
	enforced := BuildPermissionScope("u_ainomod", "c1", []string{"s1"})
	if !enforced.AIPolicyEnforce {
		t.Fatal("应读取到 AIPolicyEnforce=true")
	}
	if enforced.CanAIModifyData() {
		t.Fatal("角色 allow=1/ai=0 在生效期不得写数据")
	}
	dual := BuildPermissionScope("u_both", "c1", []string{"s1"})
	if !dual.CanAIModifyData() {
		t.Fatal("r_col 同时允许两项能力，应可写")
	}

	// 无任何权限明细：远程拒绝
	empty := BuildPermissionScope("u_none", "c1", []string{"s1"})
	if empty.HasAnyAccess() {
		t.Fatal("无权限明细必须拒绝")
	}
}

// TestBuildPermissionScope_LocalProfile 桌面/本地 Profile：完整数据权限，但不跳过后续安全守卫。
func TestBuildPermissionScope_LocalProfile(t *testing.T) {
	config.SetActive(&config.Config{IsRemote: false})
	defer config.SetActive(&config.Config{IsRemote: true})

	scope := BuildPermissionScope("local", "c1", []string{"s1"})
	if scope.Profile != ProfileDesktopLocal {
		t.Fatalf("本地模式应使用桌面 Profile，实际 %d", scope.Profile)
	}
	if scope.IsRemote {
		t.Fatal("本地模式 IsRemote 应为 false")
	}
	if !scope.SkipChecks() {
		t.Fatal("本地模式应跳过资源权限检查（仍是同一策略引擎的本地 Profile）")
	}
	if !scope.AllowModify || !scope.AIAllowModify || !scope.CanAIModifyData() {
		t.Fatal("本地模式应保持完整数据修改能力")
	}
	if !scope.IsTableAllowedStrict("any_schema", "any_table") {
		t.Fatal("本地模式应允许任意表")
	}
}

// TestBuildPermissionScope_MultiSchemaSameTable 冻结跨 Schema 同名表的交集语义。
func TestBuildPermissionScope_MultiSchemaSameTable(t *testing.T) {
	db := setupGoldenDB(t)

	seedRole(t, db, "r_multi", 0, 0)
	seedPower(t, db, "r_multi", "c1", "a", "orders", "", "table")
	seedPower(t, db, "r_multi", "c1", "b", "orders", "id", "column")
	seedUserRole(t, db, "u_multi", "r_multi")

	scope := BuildPermissionScope("u_multi", "c1", []string{"a", "b"})
	if scope.GetTableAccessLevel("orders") != "column" {
		t.Fatalf("同名表应取交集（column 优先），实际 %s", scope.GetTableAccessLevel("orders"))
	}
	if !scope.IsColumnAllowed("orders", "id") {
		t.Fatal("两个 Schema 都允许 id 时应放行")
	}
	if scope.IsColumnAllowed("orders", "amount") {
		t.Fatal("a 的表级授权不得解除 b 的列限制")
	}
	// 带 schema 限定：a.orders 为 full，b.orders 为 column
	if scope.GetTableAccessLevel("a.orders") != "full" {
		t.Fatalf("a.orders 应为 full，实际 %s", scope.GetTableAccessLevel("a.orders"))
	}
	if scope.GetTableAccessLevel("b.orders") != "column" {
		t.Fatalf("b.orders 应为 column，实际 %s", scope.GetTableAccessLevel("b.orders"))
	}
}
