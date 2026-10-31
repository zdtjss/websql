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

// waitFor 在超时内等待条件成立，避免测试依赖固定 sleep。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", msg)
}

// TestSkillQueueGlobalLimit 全局槽位是硬上限：并发峰值不得越过 limit。
func TestSkillQueueGlobalLimit(t *testing.T) {
	q := newSkillQueue(2, 10, 5*time.Second)

	var running, peak int32
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := q.Acquire(context.Background(), "u1", nil, nil)
			if err != nil {
				t.Errorf("Acquire 失败：%v", err)
				return
			}
			cur := atomic.AddInt32(&running, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if cur <= old || atomic.CompareAndSwapInt32(&peak, old, cur) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			atomic.AddInt32(&running, -1)
			release()
		}()
	}
	wg.Wait()

	if peak > 2 {
		t.Fatalf("并发峰值 %d 超过全局上限 2", peak)
	}
	if peak < 2 {
		t.Fatalf("并发峰值 %d 过低，阈值未生效场景无法覆盖", peak)
	}
}

// TestSkillQueuePerUserLimitAndSkip 单用户满额时，后面的其他用户不被队首阻塞。
func TestSkillQueuePerUserLimitAndSkip(t *testing.T) {
	q := newSkillQueue(2, 1, 5*time.Second)
	ctx := context.Background()

	releaseA1, err := q.Acquire(ctx, "A", nil, nil)
	if err != nil {
		t.Fatalf("A1 应直接获得槽位：%v", err)
	}

	// A2 入队：A 已满额，全局仍有 1 个空位。
	bRan := make(chan struct{})
	go func() {
		release, aerr := q.Acquire(ctx, "A", nil, nil)
		if aerr == nil {
			release()
		}
	}()
	waitFor(t, time.Second, func() bool {
		_, waiting := q.Stats()
		return waiting == 1
	}, "A2 应进入排队")

	releaseB, err := q.Acquire(ctx, "B", nil, nil)
	if err != nil {
		t.Fatalf("B 应跳过满额的 A 直接获得槽位：%v", err)
	}
	close(bRan)
	releaseB()
	releaseA1()
}

// TestSkillQueueTimeout 排队超时返回 ErrQueueTimeout，且释放后队列清空。
func TestSkillQueueTimeout(t *testing.T) {
	q := newSkillQueue(1, 2, 80*time.Millisecond)
	ctx := context.Background()

	releaseHold, err := q.Acquire(ctx, "A", nil, nil)
	if err != nil {
		t.Fatalf("占位失败：%v", err)
	}
	defer releaseHold()

	var gotPhase string
	_, err = q.Acquire(ctx, "B", func(ev QueueEvent) {
		if ev.Phase == "timeout" {
			gotPhase = ev.Phase
		}
	}, nil)
	if !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("应返回 ErrQueueTimeout，实际：%v", err)
	}
	var waitErr *QueueWaitError
	if !errors.As(err, &waitErr) || waitErr.Wait < 50*time.Millisecond {
		t.Fatalf("应携带等待时长，实际：%+v", err)
	}
	if gotPhase != "timeout" {
		t.Fatalf("应推送 timeout 事件，实际：%q", gotPhase)
	}
	waitFor(t, time.Second, func() bool {
		_, waiting := q.Stats()
		return waiting == 0
	}, "超时后队列应清空")
}

// TestSkillQueueDegrade 用户点降级后，排队中的请求立即返回 ErrQueueDegraded。
func TestSkillQueueDegrade(t *testing.T) {
	q := newSkillQueue(1, 2, 5*time.Second)
	ctx := context.Background()

	releaseHold, err := q.Acquire(ctx, "A", nil, nil)
	if err != nil {
		t.Fatalf("占位失败：%v", err)
	}
	defer releaseHold()

	degrade := make(chan struct{})
	var phase string
	go func() {
		time.Sleep(40 * time.Millisecond)
		close(degrade)
	}()

	start := time.Now()
	_, err = q.Acquire(ctx, "B", func(ev QueueEvent) { phase = ev.Phase }, degrade)
	if !errors.Is(err, ErrQueueDegraded) {
		t.Fatalf("应返回 ErrQueueDegraded，实际：%v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("降级响应过慢：%v", time.Since(start))
	}
	if phase != "degraded" {
		t.Fatalf("应推送 degraded 事件，实际：%q", phase)
	}
	waitFor(t, time.Second, func() bool {
		_, waiting := q.Stats()
		return waiting == 0
	}, "降级后队列应清空")
}

// TestSkillQueueWaitingPosition 排队时推送位次，释放后推送 acquired。
func TestSkillQueueWaitingPosition(t *testing.T) {
	q := newSkillQueue(1, 5, 5*time.Second)
	ctx := context.Background()

	releaseHold, err := q.Acquire(ctx, "A", nil, nil)
	if err != nil {
		t.Fatalf("占位失败：%v", err)
	}

	type event struct {
		phase string
		ahead int
	}
	events := make(chan event, 8)
	done := make(chan struct{})
	go func() {
		release, aerr := q.Acquire(ctx, "B", func(ev QueueEvent) {
			events <- event{phase: ev.Phase, ahead: ev.Ahead}
		}, nil)
		if aerr == nil {
			release()
		}
		close(done)
	}()

	select {
	case ev := <-events:
		if ev.phase != "waiting" {
			t.Fatalf("首个事件应为 waiting，实际：%q", ev.phase)
		}
		if ev.ahead != 0 {
			t.Fatalf("队首位次应为 0，实际：%d", ev.ahead)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未收到 waiting 事件")
	}

	releaseHold()
	select {
	case ev := <-events:
		if ev.phase != "acquired" {
			t.Fatalf("释放后应收到 acquired，实际：%q", ev.phase)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未收到 acquired 事件")
	}
	<-done
}

// TestSkillQueueContextCancel 运行被取消（客户端断开）时应让出排队位置。
func TestSkillQueueContextCancel(t *testing.T) {
	q := newSkillQueue(1, 5, 5*time.Second)

	releaseHold, err := q.Acquire(context.Background(), "A", nil, nil)
	if err != nil {
		t.Fatalf("占位失败：%v", err)
	}
	defer releaseHold()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, aerr := q.Acquire(ctx, "B", nil, nil)
		errc <- aerr
	}()
	waitFor(t, time.Second, func() bool {
		_, waiting := q.Stats()
		return waiting == 1
	}, "B 应进入排队")

	cancel()
	select {
	case aerr := <-errc:
		if !errors.Is(aerr, context.Canceled) {
			t.Fatalf("应返回 context.Canceled，实际：%v", aerr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消后未及时退出排队")
	}
	waitFor(t, time.Second, func() bool {
		_, waiting := q.Stats()
		return waiting == 0
	}, "取消后队列应清空")
}

// TestSkillQueueUnlimited 限额 <=0 时直接放行（用于关闭闸门的部署/测试）。
func TestSkillQueueUnlimited(t *testing.T) {
	q := newSkillQueue(0, 2, time.Second)
	release, err := q.Acquire(context.Background(), "A", nil, nil)
	if err != nil {
		t.Fatalf("不限制时不应报错：%v", err)
	}
	running, waiting := q.Stats()
	if running != 0 || waiting != 0 {
		t.Fatalf("不限制时不应计数：running=%d waiting=%d", running, waiting)
	}
	release()
}

// TestQueueFallbackResponse 排队失败必须产出「引导改用 Go 原生导出」的普通工具输出。
func TestQueueFallbackResponse(t *testing.T) {
	degraded := queueFallbackResponse(context.Background(), &QueueWaitError{Reason: ErrQueueDegraded, Wait: 12 * time.Second})
	if degraded.ExitCode == nil || *degraded.ExitCode != 1 {
		t.Fatalf("降级输出应有非零 exitCode：%+v", degraded.ExitCode)
	}
	for _, want := range []string{"Go 原生渲染", "12s", "export_analysis_docx", "不要再次调用 execute"} {
		if !strings.Contains(degraded.Output, want) {
			t.Fatalf("降级输出缺少 %q：%s", want, degraded.Output)
		}
	}

	timeout := queueFallbackResponse(context.Background(), &QueueWaitError{Reason: ErrQueueTimeout, Wait: 65 * time.Second})
	if !strings.Contains(timeout.Output, "1m5s") || !strings.Contains(timeout.Output, "自动降级") {
		t.Fatalf("超时输出应带等待时长与自动降级提示：%s", timeout.Output)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := queueFallbackResponse(ctx, errors.New("any"))
	if !strings.Contains(cancelled.Output, "已取消") || strings.Contains(cancelled.Output, "export_analysis_docx") {
		t.Fatalf("取消场景不应引导换用导出工具：%s", cancelled.Output)
	}
}
