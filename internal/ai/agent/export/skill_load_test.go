package export

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSkillQueuePoolLoad 闸门 + worker 池的组合压测（模拟多用户并发导出）。
//
// 验收点：
//   - 脚本并发峰值不超过全局槽位；
//   - 单用户并发峰值不超过单用户槽位；
//   - 所有任务成功，无超时/无协议错误。
func TestSkillQueuePoolLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("压测在 -short 模式下跳过")
	}
	dir := t.TempDir()
	script := writeScript(t, dir, "slow.py", `
import json
import time

time.sleep(0.15)
print(json.dumps({"ok": True}))
`)
	pool := newTestPool(t, 4)
	defer pool.Close()

	const (
		total      = 20
		globalCap  = 4
		perUserCap = 2
	)
	users := []string{"u1", "u2", "u3", "u4", "u5"}
	queue := newSkillQueue(globalCap, perUserCap, 10*time.Second)

	var (
		mu          sync.Mutex
		running     int
		peak        int
		perUser     = map[string]int{}
		perUserPeak = map[string]int{}
		failures    int32
		ctx         = context.Background()
		spec        = skillExecSpec{Script: script, Cwd: dir, Timeout: 30 * time.Second}
	)

	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < total; i++ {
		user := users[i%len(users)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := queue.Acquire(ctx, user, nil, nil)
			if err != nil {
				atomic.AddInt32(&failures, 1)
				return
			}
			defer release()

			mu.Lock()
			running++
			if running > peak {
				peak = running
			}
			perUser[user]++
			if perUser[user] > perUserPeak[user] {
				perUserPeak[user] = perUser[user]
			}
			mu.Unlock()
			defer func() {
				mu.Lock()
				running--
				perUser[user]--
				mu.Unlock()
			}()

			res, runErr := pool.Run(ctx, spec)
			if runErr != nil || res.ExitCode != 0 || !strings.Contains(res.Output, `"ok": true`) {
				atomic.AddInt32(&failures, 1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if failures != 0 {
		t.Fatalf("%d 个任务失败", failures)
	}
	if peak > globalCap {
		t.Fatalf("全局并发峰值 %d 超过上限 %d", peak, globalCap)
	}
	if peak < 2 {
		t.Fatalf("并发峰值 %d 过低，压测未形成真实并发", peak)
	}
	for user, p := range perUserPeak {
		if p > perUserCap {
			t.Fatalf("用户 %s 并发峰值 %d 超过单用户上限 %d", user, p, perUserCap)
		}
	}
	t.Logf("压测完成：%d 任务 / 5 用户 / 全局上限 %d / 单用户上限 %d，峰值 %d，总耗时 %v",
		total, globalCap, perUserCap, peak, elapsed.Round(time.Millisecond))
}

// TestCodegenQueueFallbackMessage 高级导出的降级提示必须指向 Go 原生工具。
func TestCodegenQueueFallbackMessage(t *testing.T) {
	msg := codegenQueueFallbackMessage(context.Background(), &QueueWaitError{Reason: ErrQueueDegraded, Wait: 8 * time.Second})
	for _, want := range []string{"Go 原生渲染", "8s", "export_analysis_docx", "不要再次调用 export_by_code"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("降级提示缺少 %q：%s", want, msg)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := codegenQueueFallbackMessage(ctx, errors.New("any"))
	if !strings.Contains(cancelled, "已取消") || strings.Contains(cancelled, "export_by_code") {
		t.Fatalf("取消场景不应引导改用其他工具：%s", cancelled)
	}
}
