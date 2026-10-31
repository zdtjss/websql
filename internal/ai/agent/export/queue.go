// queue.go — 技能脚本执行的并发闸门（全局槽位 + 单用户槽位 + FIFO 排队）。
//
// 背景：execute 每次都会新起一个 Python 子进程（冷启动导入 matplotlib 约 0.6s，
// 单进程峰值内存约 100MB）。此前没有任何并发上限，突发请求会瞬间拉起大量进程，
// 在单机上先撞到内存而不是 CPU。本文件提供进程启动前的准入控制：
//
//   - 全局槽位：maxConcurrent（默认 min(2×CPU,16)），即同时运行的技能进程数上限
//   - 单用户槽位：maxConcurrentPerUser（默认 2），避免单个用户吃满全局槽位
//   - 排队：FIFO，但「该用户已满额」的队首会被跳过，避免队首阻塞（head-of-line blocking）
//   - 排队期间可被两件事打断：用户主动降级（degrade 通道）、等待超时（maxWait）
//
// 注意：排队时间不计入 execute 的 300s 执行超时——超时从拿到槽位后才开始计算。
package export

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"websql/internal/config"
)

const (
	// queueNotifyInterval 排队位次刷新间隔（SSE 推送节流）。
	queueNotifyInterval = 2 * time.Second
	// queueNotifyIdleKeep 位次未变化时的最长静默期，避免长时间无事件。
	queueNotifyIdleKeep = 10 * time.Second
	// skillQueueDefaultMaxWait 默认排队上限，超过后自动降级为 Go 原生导出。
	skillQueueDefaultMaxWait = 60 * time.Second
	// skillQueueMaxConcurrentCap 全局槽位硬上限，避免超大规格机器把内存打满。
	skillQueueMaxConcurrentCap = 16
)

var (
	// ErrQueueTimeout 排队超过上限（调用方据此提示模型改用 Go 原生导出）。
	ErrQueueTimeout = errors.New("技能执行排队超时")
	// ErrQueueDegraded 用户主动选择降级为 Go 原生导出。
	ErrQueueDegraded = errors.New("用户选择降级为 Go 原生导出")
)

// QueueEvent 排队状态事件，由 execute 通过 context 中的 notifier 推送给前端（SSE）。
type QueueEvent struct {
	Phase        string        // waiting | acquired | degraded | timeout
	Ahead        int           // 前面还在等待的任务数（仅 waiting 有效）
	Wait         time.Duration // 已等待时长
	Limit        int           // 全局槽位上限
	PerUserLimit int           // 单用户槽位上限
}

// QueueNotifier 排队事件回调；为 nil 时静默排队。
type QueueNotifier func(QueueEvent)

// QueueWaitError 排队被打断的原因（带已等待时长，便于返回可读提示）。
type QueueWaitError struct {
	Reason error
	Wait   time.Duration
}

func (e *QueueWaitError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%v（已等待 %s）", e.Reason, e.Wait.Round(time.Second))
}

func (e *QueueWaitError) Unwrap() error { return e.Reason }

// queueWaiter 一个排队中的执行请求。
//
// 除 admitted/removed 由 SkillQueue.mu 保护外，其余字段只由等待方 goroutine 读写。
type queueWaiter struct {
	userID     string
	ready      chan struct{}
	enqueuedAt time.Time
	admitted   bool
	removed    bool

	lastNotified time.Time
	lastAhead    int
}

// SkillQueue 技能脚本执行的准入队列。
type SkillQueue struct {
	limit        int
	perUserLimit int
	maxWait      time.Duration

	mu      sync.Mutex
	running int
	perUser map[string]int
	waiters *list.List
}

// NewSkillQueue 构造队列；limit<=0 表示不限制（仅用于测试/关闭闸门的部署）。
func newSkillQueue(limit, perUserLimit int, maxWait time.Duration) *SkillQueue {
	if perUserLimit <= 0 {
		perUserLimit = 1
	}
	if limit > 0 && perUserLimit > limit {
		perUserLimit = limit
	}
	return &SkillQueue{
		limit:        limit,
		perUserLimit: perUserLimit,
		maxWait:      maxWait,
		perUser:      make(map[string]int),
		waiters:      list.New(),
	}
}

var (
	skillQueueOnce sync.Once
	skillQueueInst *SkillQueue
)

// DefaultSkillQueue 返回全局队列（首次调用时按配置与环境变量初始化）。
func DefaultSkillQueue() *SkillQueue {
	skillQueueOnce.Do(func() {
		limit, perUser, maxWait := resolveSkillQueueConfig()
		skillQueueInst = newSkillQueue(limit, perUser, maxWait)
	})
	return skillQueueInst
}

// resolveSkillQueueConfig 解析闸门参数：默认值 ← config.json ← 环境变量。
func resolveSkillQueueConfig() (limit, perUser int, maxWait time.Duration) {
	limit = 2 * runtime.NumCPU()
	if limit > skillQueueMaxConcurrentCap {
		limit = skillQueueMaxConcurrentCap
	}
	if limit < 2 {
		limit = 2
	}
	perUser = 2
	maxWait = skillQueueDefaultMaxWait

	if cfg := config.Get(); cfg != nil {
		if v := cfg.Skill.MaxConcurrent; v > 0 {
			limit = v
		}
		if v := cfg.Skill.MaxConcurrentPerUser; v > 0 {
			perUser = v
		}
		if v := cfg.Skill.QueueMaxWaitSec; v > 0 {
			maxWait = time.Duration(v) * time.Second
		}
	}
	if v, ok := envPositiveInt("WEBSQL_SKILL_MAX_CONCURRENT"); ok {
		limit = v
	}
	if v, ok := envPositiveInt("WEBSQL_SKILL_MAX_CONCURRENT_PER_USER"); ok {
		perUser = v
	}
	if v, ok := envPositiveInt("WEBSQL_SKILL_QUEUE_MAX_WAIT_SEC"); ok {
		maxWait = time.Duration(v) * time.Second
	}
	if perUser > limit {
		perUser = limit
	}
	return limit, perUser, maxWait
}

func envPositiveInt(key string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// Stats 返回当前运行数与排队数（用于日志/压测观测）。
func (q *SkillQueue) Stats() (running, waiting int) {
	if q == nil {
		return 0, 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.running, q.waiters.Len()
}

// Acquire 申请一个执行槽位。
//
// 返回的 release 必须且只能调用一次（内部用 sync.Once 防重复）。
// degrade 通道关闭时立即放弃排队并返回 ErrQueueDegraded；
// 等待超过 maxWait 返回 ErrQueueTimeout；ctx 结束返回 ctx.Err()。
func (q *SkillQueue) Acquire(ctx context.Context, userID string, notifier QueueNotifier, degrade <-chan struct{}) (func(), error) {
	if q == nil || q.limit <= 0 {
		return func() {}, nil
	}
	key := queueUserKey(userID)

	q.mu.Lock()
	// 快路径：无人排队且各方都有余量，直接执行（不产生任何 SSE 事件）。
	if q.waiters.Len() == 0 && q.running < q.limit && q.perUser[key] < q.perUserLimit {
		q.running++
		q.perUser[key]++
		q.mu.Unlock()
		return q.releaseFunc(key), nil
	}
	w := &queueWaiter{
		userID:     key,
		ready:      make(chan struct{}),
		enqueuedAt: time.Now(),
	}
	elem := q.waiters.PushBack(w)
	// 入队后立即尝试准入：全局有余量但队首用户已满额时，后面的其他用户
	// 不应该被队首阻塞（否则要等到下一次释放才能执行）。
	q.dispatchLocked()
	admitted := w.admitted
	ahead := 0
	if !admitted {
		ahead = q.aheadLocked(elem)
	}
	q.mu.Unlock()

	if admitted {
		return q.releaseFunc(key), nil
	}

	w.lastNotified = time.Now()
	w.lastAhead = ahead
	notify(notifier, QueueEvent{
		Phase:        "waiting",
		Ahead:        ahead,
		Limit:        q.limit,
		PerUserLimit: q.perUserLimit,
	})

	ticker := time.NewTicker(q.notifyInterval())
	defer ticker.Stop()

	var timeoutCh <-chan time.Time
	if q.maxWait > 0 {
		timer := time.NewTimer(q.maxWait)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	for {
		select {
		case <-w.ready:
			notify(notifier, QueueEvent{
				Phase:        "acquired",
				Wait:         time.Since(w.enqueuedAt),
				Limit:        q.limit,
				PerUserLimit: q.perUserLimit,
			})
			return q.releaseFunc(key), nil

		case <-ctx.Done():
			q.abandon(elem, w)
			return nil, ctx.Err()

		case <-degrade:
			waited := time.Since(w.enqueuedAt)
			q.abandon(elem, w)
			notify(notifier, QueueEvent{
				Phase:        "degraded",
				Wait:         waited,
				Limit:        q.limit,
				PerUserLimit: q.perUserLimit,
			})
			return nil, &QueueWaitError{Reason: ErrQueueDegraded, Wait: waited}

		case <-timeoutCh:
			waited := time.Since(w.enqueuedAt)
			q.abandon(elem, w)
			notify(notifier, QueueEvent{
				Phase:        "timeout",
				Wait:         waited,
				Limit:        q.limit,
				PerUserLimit: q.perUserLimit,
			})
			return nil, &QueueWaitError{Reason: ErrQueueTimeout, Wait: waited}

		case <-ticker.C:
			q.notifyWaiting(elem, w, notifier)
		}
	}
}

// notifyInterval 位次刷新间隔；等待上限很短时（测试）相应缩短，保证至少刷新几次。
func (q *SkillQueue) notifyInterval() time.Duration {
	if q.maxWait <= 0 {
		return queueNotifyInterval
	}
	if d := q.maxWait / 3; d < queueNotifyInterval {
		if d < 50*time.Millisecond {
			return 50 * time.Millisecond
		}
		return d
	}
	return queueNotifyInterval
}

// releaseFunc 返回幂等的释放函数：归还槽位后立即尝试唤醒队首。
func (q *SkillQueue) releaseFunc(key string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			q.mu.Lock()
			q.releaseLocked(key)
			q.dispatchLocked()
			q.mu.Unlock()
		})
	}
}

func (q *SkillQueue) releaseLocked(key string) {
	if q.running > 0 {
		q.running--
	}
	if n := q.perUser[key]; n <= 1 {
		delete(q.perUser, key)
	} else {
		q.perUser[key] = n - 1
	}
}

// abandon 放弃排队。若在放弃的瞬间已被准入（与 dispatch 竞争），必须立刻归还槽位。
func (q *SkillQueue) abandon(elem *list.Element, w *queueWaiter) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if w.admitted {
		q.releaseLocked(w.userID)
	} else if !w.removed {
		w.removed = true
		q.waiters.Remove(elem)
	}
	q.dispatchLocked()
}

// dispatchLocked 从队首开始按顺序准入：跳过该用户已满额的等待者，避免队首阻塞。
func (q *SkillQueue) dispatchLocked() {
	for q.running < q.limit {
		var target *list.Element
		for e := q.waiters.Front(); e != nil; e = e.Next() {
			w := e.Value.(*queueWaiter)
			if q.perUser[w.userID] >= q.perUserLimit {
				continue
			}
			target = e
			break
		}
		if target == nil {
			return
		}
		w := target.Value.(*queueWaiter)
		w.admitted = true
		w.removed = true
		q.waiters.Remove(target)
		q.running++
		q.perUser[w.userID]++
		close(w.ready)
	}
}

// aheadLocked 返回某个等待者前面还有多少仍在排队的任务。
func (q *SkillQueue) aheadLocked(elem *list.Element) int {
	ahead := 0
	for e := q.waiters.Front(); e != nil && e != elem; e = e.Next() {
		ahead++
	}
	return ahead
}

// notifyWaiting 推送位次变化：变化即推（受间隔限制），位次长时间不变时也定期推送一次。
func (q *SkillQueue) notifyWaiting(elem *list.Element, w *queueWaiter, notifier QueueNotifier) {
	if notifier == nil {
		return
	}
	q.mu.Lock()
	if w.removed || w.admitted {
		q.mu.Unlock()
		return
	}
	ahead := q.aheadLocked(elem)
	q.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(w.lastNotified)
	if ahead == w.lastAhead {
		if elapsed < queueNotifyIdleKeep {
			return
		}
	} else if elapsed < queueNotifyInterval {
		return
	}
	w.lastNotified = now
	w.lastAhead = ahead
	notify(notifier, QueueEvent{
		Phase:        "waiting",
		Ahead:        ahead,
		Wait:         now.Sub(w.enqueuedAt),
		Limit:        q.limit,
		PerUserLimit: q.perUserLimit,
	})
}

func notify(fn QueueNotifier, ev QueueEvent) {
	if fn == nil {
		return
	}
	fn(ev)
}

// queueUserKey 统一用户键：无用户上下文（本地模式/测试）归一到同一个桶。
func queueUserKey(userID string) string {
	key := strings.TrimSpace(userID)
	if key == "" {
		return "__anonymous__"
	}
	return key
}
