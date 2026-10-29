package dataset

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestStoreCreateQueryAndACL(t *testing.T) {
	s := NewStore(t.TempDir(), time.Hour)
	defer s.Close()

	rows := []map[string]any{
		{"category": "A", "amount": int64(10)},
		{"category": "A", "amount": int64(20)},
		{"category": "B", "amount": int64(5)},
	}
	entry, err := s.Create(context.Background(), "u1", "s1", []string{"category", "amount"}, rows)
	if err != nil {
		t.Fatalf("Create 失败：%v", err)
	}
	if entry.RowCount != 3 {
		t.Fatalf("RowCount 期望 3，实际 %d", entry.RowCount)
	}
	if info, statErr := os.Stat(entry.Path); statErr != nil || info.Size() < 4096 {
		t.Fatalf("数据集文件异常：info=%v err=%v", info, statErr)
	}

	qr, err := s.Query(context.Background(), entry.ID, "u1", "s1",
		`SELECT category, SUM(amount) AS total FROM result GROUP BY category ORDER BY category`, 0)
	if err != nil {
		t.Fatalf("Query 失败：%v", err)
	}
	if len(qr.Data) != 2 {
		t.Fatalf("聚合行数期望 2，实际 %d：%+v", len(qr.Data), qr.Data)
	}
	if qr.Data[0]["category"] != "A" {
		t.Fatalf("首行 category 期望 A，实际 %+v", qr.Data[0])
	}

	if _, err := s.Query(context.Background(), entry.ID, "u2", "s1", `SELECT * FROM result`, 0); err == nil {
		t.Fatal("跨用户查询应被拒绝")
	}
	if _, err := s.Query(context.Background(), entry.ID, "u1", "s1", `DROP TABLE result`, 0); err == nil {
		t.Fatal("写操作应被拒绝")
	}
	if _, err := s.Query(context.Background(), entry.ID, "u1", "s1", `SELECT 1; SELECT 2`, 0); err == nil {
		t.Fatal("多语句应被拒绝")
	}
	if _, err := s.Query(context.Background(), entry.ID, "u1", "s1", `ATTACH DATABASE 'x' AS y`, 0); err == nil {
		t.Fatal("ATTACH 应被拒绝")
	}
	if _, err := s.Query(context.Background(), entry.ID, "u1", "s1", `PRAGMA table_info(result)`, 0); err == nil {
		t.Fatal("PRAGMA 应被拒绝")
	}

	// 权限修订号变化后，旧数据集失效。
	revEntry, err := s.CreateWithRevision(context.Background(), "u1", "s1", 2, []string{"v"}, []map[string]any{{"v": "x"}})
	if err != nil {
		t.Fatalf("CreateWithRevision 失败：%v", err)
	}
	if _, err := s.QueryWithRevision(context.Background(), revEntry.ID, "u1", "s1", 3, `SELECT * FROM result`, 0); err == nil {
		t.Fatal("权限修订号变化后应拒绝数据集查询")
	}
	if _, err := s.QueryWithRevision(context.Background(), revEntry.ID, "u1", "s1", 2, `SELECT * FROM result`, 0); err != nil {
		t.Fatalf("同修订号应允许：%v", err)
	}
}
