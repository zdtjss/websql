package database

import (
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestGetResultRowsLimited(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开 sqlite 失败：%v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER, name TEXT)`); err != nil {
		t.Fatalf("建表失败：%v", err)
	}
	for i := 1; i <= 10; i++ {
		if _, err := db.Exec(`INSERT INTO t (id, name) VALUES (?, ?)`, i, "n"); err != nil {
			t.Fatalf("插入失败：%v", err)
		}
	}
	rows, err := db.Queryx(`SELECT id, name FROM t ORDER BY id`)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	defer rows.Close()

	data, truncated, err := GetResultRowsLimited("sqlite", rows, 3)
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if len(data) != 3 || !truncated {
		t.Fatalf("期望 3 行且截断，实际 rows=%d truncated=%v", len(data), truncated)
	}
}
