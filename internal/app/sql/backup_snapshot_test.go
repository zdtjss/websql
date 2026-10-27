package sql

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// newBackupTestDB 建一张带一行数据的表，用于验证写操作快照。
func newBackupTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开内存 SQLite 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec("create table t_user (id integer primary key, name text)"); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := db.Exec("insert into t_user (id, name) values (1, 'before')"); err != nil {
		t.Fatalf("插入初始数据失败: %v", err)
	}
	return db
}

// TestReadBackupSnapshot_ReadsPreWriteState 复现 D3：
// 快照必须在写语句执行【之前】读取，否则回滚数据会变成“修改后”的值。
func TestReadBackupSnapshot_ReadsPreWriteState(t *testing.T) {
	db := newBackupTestDB(t)

	operationType, backupSQL := buildBackupSQL("update t_user set name='after' where id=1")
	if operationType != "update" {
		t.Fatalf("操作类型识别异常: %q", operationType)
	}
	if backupSQL == "" {
		t.Fatal("应能从 UPDATE 语句解析出备份查询 SQL")
	}

	// 1) 写操作之前取快照
	rows, truncated, err := readBackupSnapshot(db, nil, backupSQL, 5000)
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if truncated {
		t.Fatal("单行快照不应被截断")
	}
	if len(rows) != 1 || rows[0]["name"] != "before" {
		t.Fatalf("快照应记录修改前的值，实际: %+v", rows)
	}

	// 2) 执行写操作
	if _, err := db.Exec("update t_user set name='after' where id=1"); err != nil {
		t.Fatalf("执行更新失败: %v", err)
	}

	// 3) 写后再取快照应看到新值（用于证明确实存在时序差异，原实现可能取到这一份）
	after, _, err := readBackupSnapshot(db, nil, backupSQL, 5000)
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if len(after) != 1 || after[0]["name"] != "after" {
		t.Fatalf("写后快照应为新值，实际: %+v", after)
	}
}

// TestReadBackupSnapshot_TruncatesLargeResult 大表快照必须按 limit 截断，避免打爆内存。
func TestReadBackupSnapshot_TruncatesLargeResult(t *testing.T) {
	db := newBackupTestDB(t)
	for i := 2; i <= 5; i++ {
		if _, err := db.Exec("insert into t_user (id, name) values (?, 'x')", i); err != nil {
			t.Fatalf("插入数据失败: %v", err)
		}
	}

	rows, truncated, err := readBackupSnapshot(db, nil, "select * from t_user order by id", 2)
	if err != nil {
		t.Fatalf("读取快照失败: %v", err)
	}
	if !truncated {
		t.Fatal("超过 limit 时应标记为截断")
	}
	if len(rows) != 2 {
		t.Fatalf("截断后应返回 limit 行，实际 %d 行", len(rows))
	}
}

// TestBuildBackupSQL 无法可靠解析时不生成备份 SQL，但历史记录仍需落库。
func TestBuildBackupSQL(t *testing.T) {
	cases := []struct {
		sql      string
		wantType string
		wantSQL  string
	}{
		{
			sql:      "update t_user set name='a' where id=1",
			wantType: "update",
			wantSQL:  "select * from t_user where id=1",
		},
		{
			sql:      "delete from t_user where id=1",
			wantType: "delete",
			wantSQL:  "select * from t_user where id=1",
		},
		{
			// 没有 where 条件：不做快照（避免全表读）
			sql:      "update t_user set name='a'",
			wantType: "update",
			wantSQL:  "",
		},
	}
	for _, tc := range cases {
		gotType, gotSQL := buildBackupSQL(tc.sql)
		if gotType != tc.wantType {
			t.Errorf("buildBackupSQL(%q) type = %q, want %q", tc.sql, gotType, tc.wantType)
		}
		if gotSQL != tc.wantSQL {
			t.Errorf("buildBackupSQL(%q) sql = %q, want %q", tc.sql, gotSQL, tc.wantSQL)
		}
	}
}
