package audit

import (
	"sync"
	"testing"
)

// TestAsyncWriter_ConcurrentEnqueueAndShutdown 回归 F-20：
// shutdown 与 enqueue 并发时不得出现 send on closed channel panic。
func TestAsyncWriter_ConcurrentEnqueueAndShutdown(t *testing.T) {
	for round := 0; round < 20; round++ {
		w := newAsyncWriter()
		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = i
				w.enqueue(&AuditLog{ID: "rec", ToolName: "query_data", UserID: "u1", Source: "test"})
			}(i)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.shutdown()
		}()
		wg.Wait()
		// 重复 shutdown 必须安全
		w.shutdown()
	}
}

// TestAsyncWriter_EnqueueAfterShutdown 关闭后写入应被拒绝而不是 panic。
func TestAsyncWriter_EnqueueAfterShutdown(t *testing.T) {
	w := newAsyncWriter()
	w.shutdown()
	if w.enqueue(&AuditLog{ID: "rec", ToolName: "exec_sql", UserID: "u1", Source: "test"}) {
		t.Fatal("已关闭的 writer 不应接受新记录")
	}
}
