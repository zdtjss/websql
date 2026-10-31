package agent

import (
	"fmt"
	"testing"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/system"

	"github.com/jmoiron/sqlx"
)

// 本文件覆盖 F-22（每请求重复查询与 Agent 缓存失效）的行为契约：
// 角色与权限明细是用户级数据，一次运行内只允许加载一次；
// 命中进程内安全上下文缓存时，整轮构造都不应再触发权限加载。

// setupRunCtxDB 把 system 包指向与 admin 同一份进程级内存管理库，
// 构造链路中的 rollout 开关读取才能命中它。
func setupRunCtxDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db := setupGoldenDB(t)
	system.Init(db)
	t.Cleanup(func() { system.Init(nil) })
	return db
}

// countingRoleLoaders 注入 FindUserRoles / FindUserPowerDetails 的计数器，
// 返回值仍来自生产实现，保证权限语义不变。
func countingRoleLoaders(t *testing.T) (*int, *int) {
	t.Helper()
	rolesCalls := 0
	powerCalls := 0
	origRoles, origPower := findUserRolesFunc, findUserPowerDetailsFunc
	t.Cleanup(func() {
		findUserRolesFunc, findUserPowerDetailsFunc = origRoles, origPower
	})
	findUserRolesFunc = func(userID string) []*admin.Role {
		rolesCalls++
		return origRoles(userID)
	}
	findUserPowerDetailsFunc = func(userID string) []*admin.PowerDetail {
		powerCalls++
		return origPower(userID)
	}
	return &rolesCalls, &powerCalls
}

// TestBuildRunSecurityContextLoadsRoleDataOnce 多连接会话只加载一次用户级权限数据。
func TestBuildRunSecurityContextLoadsRoleDataOnce(t *testing.T) {
	db := setupRunCtxDB(t)
	seedRole(t, db, "r_once", 1, 1)
	// seedPower 的幂等主键由 role/level/schema/table/column 组成，conn 级行不区分 schema，
	// 两个连接会主键相撞，这里显式用固定主键插入。
	for i, connID := range []string{"c1", "c2"} {
		if _, err := db.Exec(`insert or replace into t_power (id, role_id, conn_id, schema_name, table_name, column_name, power_level)
			values (?, ?, ?, NULL, NULL, NULL, 'conn')`, fmt.Sprintf("p_once_%d", i), "r_once", connID); err != nil {
			t.Fatalf("插入连接级权限失败：%v", err)
		}
	}
	seedUserRole(t, db, "u_once", "r_once")

	rolesCalls, powerCalls := countingRoleLoaders(t)

	refs := []SchemaRef{{ConnID: "c1", Schema: "s1"}, {ConnID: "c2", Schema: "s1"}}
	sec, err := BuildRunSecurityContext(ProfileWebRemote, "u_once", "c1", refs, 0, NewNoopMasker())
	if err != nil {
		t.Fatalf("多连接安全上下文构造失败：%v", err)
	}
	if len(sec.Scopes) != 2 {
		t.Fatalf("应为两个连接各建一个 Scope，实际 %d", len(sec.Scopes))
	}
	if *rolesCalls != 1 {
		t.Fatalf("两连接会话应只加载一次用户角色，实际 %d 次", *rolesCalls)
	}
	if *powerCalls != 1 {
		t.Fatalf("两连接会话应只加载一次权限明细，实际 %d 次", *powerCalls)
	}
}

// TestBuildRunSecurityContext_DropsRevokedConn 验证权限收缩后的优雅降级：
// 会话中途回收某个连接后，下一句提问里的失权连接被剔除、其余连接仍可用；
// 默认连接被回收时降级到剩余连接；全部失效时仍 fail-closed（403）。
func TestBuildRunSecurityContext_DropsRevokedConn(t *testing.T) {
	db := setupRunCtxDB(t)
	seedRole(t, db, "r_revoke", 1, 1)
	// 只给 conn-a 授权，conn-b 未授权（等同于已在会话中途被回收）
	if _, err := db.Exec(`insert or replace into t_power (id, role_id, conn_id, schema_name, table_name, column_name, power_level)
		values ('p_revoke_a', 'r_revoke', 'conn-a', NULL, NULL, NULL, 'conn')`); err != nil {
		t.Fatalf("插入授权失败：%v", err)
	}
	seedUserRole(t, db, "u_revoke", "r_revoke")

	refs := []SchemaRef{{ConnID: "conn-a", Schema: "main"}, {ConnID: "conn-b", Schema: "main"}}
	// 默认连接指向已失权的 conn-b：应降级到 conn-a 而不是整轮失败
	sec, err := BuildRunSecurityContext(ProfileWebRemote, "u_revoke", "conn-b", refs, 1, NewNoopMasker())
	if err != nil {
		t.Fatalf("部分连接失权时不应整轮失败：%v", err)
	}
	if len(sec.Scopes) != 1 {
		t.Fatalf("应只保留可用连接，实际 %d 个 Scope", len(sec.Scopes))
	}
	if _, ok := sec.Scopes["conn-a"]; !ok {
		t.Fatalf("可用连接 conn-a 应保留，实际 %v", sec.Scopes)
	}
	if sec.DefaultConnID != "conn-a" {
		t.Fatalf("默认连接失权后应降级为 conn-a，实际 %q", sec.DefaultConnID)
	}

	// 全部失效：保持 fail-closed
	if _, err := BuildRunSecurityContext(ProfileWebRemote, "u_revoke", "conn-b",
		[]SchemaRef{{ConnID: "conn-b", Schema: "main"}}, 1, NewNoopMasker()); err == nil {
		t.Fatal("所有连接都失权时必须拒绝本轮")
	}
}

// TestRunSecurityCacheHitSkipsRoleLoading 命中缓存时不再构造安全上下文，
// 因而不再产生任何权限加载。用随机 revision 作为缓存键的一部分，
// 使本用例在 -count=N 重复运行时不互相污染进程内缓存。
func TestRunSecurityCacheHitSkipsRoleLoading(t *testing.T) {
	db := setupRunCtxDB(t)
	seedRole(t, db, "r_cached", 1, 1)
	seedPower(t, db, "r_cached", "c1", "", "", "", "conn")
	seedUserRole(t, db, "u_cached", "r_cached")

	rolesCalls, powerCalls := countingRoleLoaders(t)

	nonce := uint64(time.Now().UnixNano())
	refs := []SchemaRef{{ConnID: "c1", Schema: "s1"}}
	key := runSecurityCacheKey(ProfileWebRemote, "u_cached", "c1", refs, nonce)

	builds := 0
	build := func() (*RunSecurityContext, error) {
		builds++
		return BuildRunSecurityContext(ProfileWebRemote, "u_cached", "c1", refs, nonce, NewNoopMasker())
	}

	first, err := resolveCachedRunSecurity(key, build)
	if err != nil {
		t.Fatalf("首次构造失败：%v", err)
	}
	if builds != 1 || *rolesCalls != 1 || *powerCalls != 1 {
		t.Fatalf("首次应真正构造一次（builds=%d, roles=%d, power=%d）", builds, *rolesCalls, *powerCalls)
	}

	second, err := resolveCachedRunSecurity(key, build)
	if err != nil {
		t.Fatalf("第二次调用失败：%v", err)
	}
	if builds != 1 {
		t.Fatalf("缓存命中不得重新构造（builds=%d）", builds)
	}
	if *rolesCalls != 1 || *powerCalls != 1 {
		t.Fatalf("缓存命中后不得再次加载权限（roles=%d, power=%d）", *rolesCalls, *powerCalls)
	}
	if second != first {
		t.Fatal("缓存命中应返回同一份基础上下文")
	}

	// 权限修订号变化必须换键并重新构造，避免沿用旧权限投影。
	otherKey := runSecurityCacheKey(ProfileWebRemote, "u_cached", "c1", refs, nonce+1)
	if _, err := resolveCachedRunSecurity(otherKey, build); err != nil {
		t.Fatalf("修订号变化后构造失败：%v", err)
	}
	if builds != 2 {
		t.Fatalf("修订号变化应触发重新构造（builds=%d）", builds)
	}
}
