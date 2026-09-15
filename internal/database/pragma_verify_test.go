package database

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// 验证 WithSQLitePragmas 通过 DSN 为池中每个连接生效 busy_timeout，
// 并发写入不再出现 SQLITE_BUSY（回归防护）。
func TestWithSQLitePragmasConcurrentWrite(t *testing.T) {
	dsn := WithSQLitePragmas(filepath.Join(t.TempDir(), "busy_test.db"), true)
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)

	// 校验新建连接的 busy_timeout 已生效
	var bt int
	if err := db.Get(&bt, "PRAGMA busy_timeout"); err != nil || bt != 30000 {
		t.Fatalf("busy_timeout 未生效: bt=%d, err=%v", bt, err)
	}

	if _, err := db.Exec("create table t_probe (id text primary key, v text)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 200)
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if _, err := db.Exec("insert into t_probe (id, v) values (?, ?)",
					fmt.Sprintf("g%d-%d", g, i), "x"); err != nil {
					errCh <- err
				}
			}
		}(g)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("并发写入超时")
	}
	close(errCh)
	for err := range errCh {
		t.Errorf("并发写入失败: %v", err)
	}
}
