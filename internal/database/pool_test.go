package database

import (
	"os"
	"strings"
	"testing"

	"websql/internal/config"

	"github.com/jmoiron/sqlx"
)

// TestMain 注入空配置：GetConn 会读 config.Get().DB 的连接池参数，
// 而单测环境不会加载 config.json（未注入时 Get() 返回 nil）。
func TestMain(m *testing.M) {
	config.SetActive(&config.Config{})
	os.Exit(m.Run())
}

// TestCreateKey_TempConnIsolated 复现 D1：未保存的连接（Id 为空）不能共用同一个池 key，
// 否则「测完 A 库再测 B 库」会复用 A 的连接。
func TestCreateKey_TempConnIsolated(t *testing.T) {
	a := &DBParam{DbType: "mysql", User: "root", Pwd: "p1", Url: "tcp(10.0.0.1:3306)/db_a"}
	b := &DBParam{DbType: "mysql", User: "root", Pwd: "p2", Url: "tcp(10.0.0.2:3306)/db_b"}

	keyA, keyB := createKey(a), createKey(b)
	if keyA == keyB {
		t.Fatalf("不同目标的临时连接不应共用池 key: %q", keyA)
	}
	if keyA == "" || keyB == "" {
		t.Fatalf("临时连接必须生成非空 key，实际: %q / %q", keyA, keyB)
	}
	if !strings.HasPrefix(keyA, "tmp:") {
		t.Errorf("临时连接 key 应以 tmp: 前缀标识，实际 %q", keyA)
	}

	// 完全相同的目标应命中同一个池（避免重复建池）
	sameAsA := &DBParam{DbType: "mysql", User: "root", Pwd: "p1", Url: "tcp(10.0.0.1:3306)/db_a"}
	if createKey(sameAsA) != keyA {
		t.Errorf("相同目标应复用同一个池 key")
	}

	// 口令不同也必须视为不同目标
	diffPwd := *a
	diffPwd.Pwd = "p1-changed"
	if createKey(&diffPwd) == keyA {
		t.Errorf("口令不同应生成不同的池 key")
	}
}

// TestCreateKey_SavedConnUsesId 已保存连接仍以 Id 为 key，保持既有行为不变。
func TestCreateKey_SavedConnUsesId(t *testing.T) {
	p := &DBParam{Id: "1234567890", DbType: "mysql", Url: "tcp(127.0.0.1:3306)/db"}
	if got := createKey(p); got != "1234567890" {
		t.Fatalf("已保存连接应以 Id 为 key，实际 %q", got)
	}
}

// TestTempConnKey_NoPlaintextPwd key 会被写入日志，禁止包含明文口令。
func TestTempConnKey_NoPlaintextPwd(t *testing.T) {
	secret := "Sup3rSecretPwd!"
	key := createKey(&DBParam{DbType: "mysql", User: "root", Pwd: secret, Url: "tcp(127.0.0.1:3306)/db"})
	if strings.Contains(key, secret) {
		t.Fatalf("临时连接 key 不得包含明文口令: %q", key)
	}
}

// TestWithTempConn_ReleasesPool 一次性连接执行完必须从池中移除，不留 goroutine/连接。
func TestWithTempConn_ReleasesPool(t *testing.T) {
	param := &DBParam{DbType: "sqlite", Url: ":memory:"}

	called := false
	err := WithTempConn(param, func(db *sqlx.DB) error {
		called = true
		if db == nil {
			t.Fatal("回调收到 nil 连接")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("一次性连接执行失败: %v", err)
	}
	if !called {
		t.Fatal("回调未被调用")
	}

	dbMapMu.RLock()
	remaining := len(DBMap)
	dbMapMu.RUnlock()
	if remaining != 0 {
		t.Fatalf("一次性连接应从池中释放，实际仍有 %d 个池", remaining)
	}
}

// TestWithTempConn_ConvertsPanicToError 连接失败必须返回 error 而不是 panic 到 HTTP 层。
func TestWithTempConn_ConvertsPanicToError(t *testing.T) {
	param := &DBParam{DbType: "mysql", Url: "tcp(127.0.0.1:1)/not_exists"}
	err := WithTempConn(param, func(db *sqlx.DB) error { return nil })
	if err == nil {
		t.Fatal("不可达的数据库应返回 error")
	}
}
