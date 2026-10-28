package admin

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// setupRevisionDB 构建带有 t_permission_revision 单行表的独立管理库。
func setupRevisionDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`create table t_permission_revision (
		id integer primary key,
		revision integer not null default 0,
		updated_at datetime
	)`); err != nil {
		t.Fatalf("建表失败：%v", err)
	}
	if _, err := db.Exec(`insert into t_permission_revision (id, revision) values (1, 0)`); err != nil {
		t.Fatalf("初始化修订号失败：%v", err)
	}
	Init(db)
	t.Cleanup(func() { Init(nil) })
	return db
}

// TestPermissionRevision_SyncBumpAndLease 冻结「对账 / 事务内递增 / 租约过期」语义。
func TestPermissionRevision_SyncBumpAndLease(t *testing.T) {
	db := setupRevisionDB(t)

	MarkPermissionRevisionStale()
	if PermissionRevisionFresh() {
		t.Fatal("强制过期后不得宣称租约有效")
	}
	if _, fresh := SnapshotPermissionRevision(); fresh {
		t.Fatal("强制过期后快照应不可信")
	}

	if err := SyncPermissionRevision(); err != nil {
		t.Fatalf("首次对账失败：%v", err)
	}
	if !PermissionRevisionFresh() {
		t.Fatal("成功对账后租约应有效")
	}
	if revision := CurrentPermissionRevision(); revision != 0 {
		t.Fatalf("初始修订号应为 0，实际 %d", revision)
	}

	tx, err := db.Beginx()
	if err != nil {
		t.Fatalf("开启事务失败：%v", err)
	}
	if err := BumpPermissionRevisionTx(tx); err != nil {
		t.Fatalf("事务内递增失败：%v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	if CurrentPermissionRevision() != 0 {
		t.Fatal("提交后未对账前本地值不应变化（由调用方显式同步）")
	}
	if err := SyncPermissionRevision(); err != nil {
		t.Fatalf("对账失败：%v", err)
	}
	if revision := CurrentPermissionRevision(); revision != 1 {
		t.Fatalf("递增后修订号应为 1，实际 %d", revision)
	}

	// 空事务必须拒绝：保证「权限数据 + 修订号」原子提交约束不可被绕过。
	if err := BumpPermissionRevisionTx(nil); err == nil {
		t.Fatal("空事务必须报错")
	}
}

// TestBumpPermissionRevisionTx_EnsuresRow 存量库缺行时自动补建并递增。
func TestBumpPermissionRevisionTx_EnsuresRow(t *testing.T) {
	db := setupRevisionDB(t)
	if _, err := db.Exec(`delete from t_permission_revision`); err != nil {
		t.Fatalf("清理单行失败：%v", err)
	}

	tx, err := db.Beginx()
	if err != nil {
		t.Fatalf("开启事务失败：%v", err)
	}
	if err := BumpPermissionRevisionTx(tx); err != nil {
		t.Fatalf("缺行时递增失败：%v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交失败：%v", err)
	}

	var revision uint64
	if err := db.Get(&revision, `select revision from t_permission_revision where id = 1`); err != nil {
		t.Fatalf("读取修订号失败：%v", err)
	}
	if revision != 1 {
		t.Fatalf("补建后修订号应为 1，实际 %d", revision)
	}
}
