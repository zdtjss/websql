package uploadstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("准备测试文件失败：%v", err)
	}
	return p
}

func TestStore_SaveGetOwnerDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "uploads"), time.Minute, time.Hour, 0)
	defer s.Close()

	path := writeFile(t, dir, "a.txt", "hello")
	s.Save(&FileMeta{ID: "f1", UserID: "u1", FileName: "a.txt", DiskPath: path, Ext: ".txt", Kind: "text", Size: 5})

	meta, ok := s.Get("f1")
	if !ok {
		t.Fatal("应能读取已登记文件")
	}
	if meta.ExpireAt.IsZero() || !meta.ExpireAt.After(time.Now()) {
		t.Errorf("ExpireAt 应被自动设置为未来时间，实际 %v", meta.ExpireAt)
	}

	if !s.IsOwner("f1", "u1") {
		t.Error("本人应可访问")
	}
	if s.IsOwner("f1", "u2") {
		t.Error("他人不应可访问")
	}
	if s.IsOwner("missing", "u1") {
		t.Error("文件不存在应返回 false")
	}

	s.Delete("f1")
	if _, ok := s.Get("f1"); ok {
		t.Error("删除后不应再读取到元数据")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("删除后磁盘文件应被清理，实际 err=%v", err)
	}
}

func TestStore_LocalModeEmptyOwner(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "uploads"), time.Minute, time.Hour, 0)
	defer s.Close()

	path := writeFile(t, dir, "a.txt", "x")
	s.Save(&FileMeta{ID: "f1", UserID: "", DiskPath: path, Ext: ".txt"})

	// 本地/桌面免登录模式无归属信息，任意调用方均可访问（与既有行为一致）
	if !s.IsOwner("f1", "anyone") {
		t.Error("未记录归属时应视为可访问")
	}
}

func TestStore_ExpiryRemovesDiskFile(t *testing.T) {
	dir := t.TempDir()
	// TTL 20ms、清理周期 10ms：等待清理循环触发淘汰
	s := NewStore(filepath.Join(dir, "uploads"), 20*time.Millisecond, 10*time.Millisecond, 0)
	defer s.Close()

	path := writeFile(t, dir, "a.txt", "hello")
	s.Save(&FileMeta{ID: "f1", UserID: "u1", DiskPath: path, Ext: ".txt"})

	if _, ok := s.Get("f1"); !ok {
		t.Fatal("过期前应可读取")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := s.Get("f1"); ok {
		t.Error("过期后不应再读取到元数据")
	}

	// 等待后台清理（含调度抖动）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("过期文件应被后台清理删除")
}

func TestStore_MaxEntriesEvictsOldest(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "uploads"), time.Minute, time.Hour, 2)
	defer s.Close()

	p1 := writeFile(t, dir, "1.txt", "1")
	p2 := writeFile(t, dir, "2.txt", "2")
	p3 := writeFile(t, dir, "3.txt", "3")

	s.Save(&FileMeta{ID: "f1", DiskPath: p1})
	time.Sleep(2 * time.Millisecond)
	s.Save(&FileMeta{ID: "f2", DiskPath: p2})
	time.Sleep(2 * time.Millisecond)
	s.Save(&FileMeta{ID: "f3", DiskPath: p3})

	if _, ok := s.Get("f1"); ok {
		t.Error("超过容量上限应淘汰最旧条目")
	}
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Error("淘汰条目的磁盘文件应被清理")
	}
	if _, ok := s.Get("f3"); !ok {
		t.Error("最新条目应保留")
	}
	if s.Count() != 2 {
		t.Errorf("容量应为 2，实际 %d", s.Count())
	}
}

func TestStore_TTLAccessor(t *testing.T) {
	s := NewStore(t.TempDir(), 15*time.Minute, time.Hour, 0)
	defer s.Close()
	if s.TTL() != 15*time.Minute {
		t.Errorf("TTL 应可读取，实际 %v", s.TTL())
	}
}
