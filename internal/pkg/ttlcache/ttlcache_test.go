package ttlcache

import (
	"testing"
	"time"
)

func TestTTLCache_MaxEntriesEvictsOldest(t *testing.T) {
	c := New[string](time.Minute, time.Minute)
	defer c.Close()
	c.SetMaxEntries(2)

	c.Set("a", "1")
	time.Sleep(2 * time.Millisecond)
	c.Set("b", "2")
	time.Sleep(2 * time.Millisecond)
	c.Set("c", "3")

	if _, ok := c.Get("a"); ok {
		t.Error("超过容量上限应淘汰最旧条目 a")
	}
	if v, ok := c.Get("b"); !ok || v != "2" {
		t.Errorf("b 应保留：%v/%v", v, ok)
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("c 应写入成功")
	}
}

func TestTTLCache_OnEvictOutsideLock(t *testing.T) {
	done := make(chan struct{})
	var c *TTLCache[string]
	c = New[string](time.Minute, time.Minute, func(key string, value string) {
		// 回调内访问缓存：若回调在锁内执行会死锁
		_, _ = c.Get(key)
		close(done)
	})
	defer c.Close()
	c.SetMaxEntries(1)

	c.Set("a", "1")
	c.Set("b", "2") // 触发 a 淘汰 → onEvict

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onEvict 回调在锁内执行导致阻塞/死锁")
	}
}

func TestTTLCache_ExpiredEntryMisses(t *testing.T) {
	c := New[string](10*time.Millisecond, time.Hour)
	defer c.Close()

	c.Set("k", "v")
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Fatalf("未过期条目应命中：%v/%v", v, ok)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Error("已过期条目不应命中")
	}
}

func TestTTLCache_DeleteTriggersEvict(t *testing.T) {
	evicted := make(chan string, 1)
	c := New[string](time.Minute, time.Minute, func(key string, _ string) {
		evicted <- key
	})
	defer c.Close()

	c.Set("k", "v")
	c.Delete("k")

	select {
	case key := <-evicted:
		if key != "k" {
			t.Errorf("应淘汰 k，实际 %s", key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Delete 应触发 onEvict")
	}
}
