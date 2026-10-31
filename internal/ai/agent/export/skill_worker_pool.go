// skill_worker_pool.go — 技能脚本的常驻 Python worker 池。
//
// 目标：把「每次 execute 新起解释器」换成「进程内复用已导入的模块」，省掉每次
// 0.6~1.5s 的 import 开销，并让同时运行的进程数恒定（池大小 = 并发闸门槽位数）。
//
// 与闸门的关系：ExecuteStreaming 先通过 SkillQueue 拿到槽位（全局 ≤ limit），
// 池的上限同样是 limit，因此 checkout 只会在「池已满且无人归还」时短暂等待。
//
// 降级策略：spawn/握手失败视为 worker 不可用，回退到原有 one-shot 进程路径；
// 请求已发出后的失败（超时/崩溃/协议错乱）不重放脚本，避免产物重复或半成品。
package export

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"websql/internal/config"
)

const (
	executorAuto    = "auto"
	executorWorker  = "worker"
	executorProcess = "process"

	workerHandshakeTimeout = 20 * time.Second
	workerReapInterval     = time.Minute
	workerDefaultIdleTTL   = 5 * time.Minute
	workerDefaultMaxJobs   = 50
	workerDefaultMinWarm   = 1
)

var (
	// errWorkerUnavailable worker 起不来（脚本缺失/解释器异常）：可安全回退 one-shot。
	errWorkerUnavailable = errors.New("skill worker unavailable")
	// errWorkerExited 请求已发出后 worker 退出/崩溃：不重放脚本。
	errWorkerExited = errors.New("skill worker exited unexpectedly")
	// errWorkerProtocol 协议错乱（id 不匹配/无法解析）：不重放脚本。
	errWorkerProtocol = errors.New("skill worker protocol error")
)

// skillExecSpec 一次技能脚本执行的全部输入。
type skillExecSpec struct {
	Script       string
	Args         []string
	Python       string
	PythonEnvDir string
	Cwd          string
	Env          map[string]string
	StdinFile    string
	Timeout      time.Duration
}

// skillExecResult 与 filesystem.ExecuteResponse 对应。
type skillExecResult struct {
	Output   string
	ExitCode int
}

type workerRequest struct {
	ID        int64             `json:"id"`
	Script    string            `json:"script"`
	Argv      []string          `json:"argv,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	StdinPath string            `json:"stdinPath,omitempty"`
	TimeoutMs int64             `json:"timeoutMs,omitempty"`
}

type workerResponse struct {
	ID         int64  `json:"id"`
	ExitCode   *int   `json:"exitCode"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
	Ready      bool   `json:"ready,omitempty"`
}

// skillWorker 一个常驻解释器进程。
type skillWorker struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	respCh chan workerResponse
	done   chan struct{}

	jobs     int
	lastUsed time.Time
}

func (w *skillWorker) alive() bool {
	select {
	case <-w.done:
		return false
	default:
		return true
	}
}

func (w *skillWorker) kill() {
	if w == nil {
		return
	}
	_ = w.stdin.Close()
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
}

// skillWorkerPool 常驻 worker 池。
type skillWorkerPool struct {
	python       string
	pythonEnvDir string
	workerScript string
	max          int
	minWarm      int
	idleTTL      time.Duration
	maxJobs      int

	mu    sync.Mutex
	idle  []*skillWorker
	total int
	wake  chan struct{}
	seq   atomic.Int64

	// spawnFailures/disabled 熔断：连续多次起不来就叫停，避免每个请求都卡在启动超时上。
	spawnFailures int
	disabled      bool

	reaperOnce sync.Once
}

var (
	skillPoolOnce sync.Once
	skillPoolInst *skillWorkerPool
)

// defaultWorkerPool 按配置返回池；返回 nil 表示应使用 one-shot 进程路径。
func defaultWorkerPool(python, pythonEnvDir string) *skillWorkerPool {
	skillPoolOnce.Do(func() {
		mode := skillExecutorMode()
		if mode == executorProcess {
			log.Printf("[SkillWorker] executor=process，使用 one-shot 进程路径")
			return
		}
		limit, _, _ := resolveSkillQueueConfig()
		script := filepath.Join(skillsRootDir(), "lib", "skill_worker.py")
		if fi, err := os.Stat(script); err != nil || !fi.Mode().IsRegular() {
			log.Printf("[SkillWorker] 未找到 %s，回退 one-shot 进程路径", script)
			return
		}
		pool := &skillWorkerPool{
			python:       python,
			pythonEnvDir: pythonEnvDir,
			workerScript: script,
			max:          limit,
			minWarm:      workerDefaultMinWarm,
			idleTTL:      workerDefaultIdleTTL,
			maxJobs:      workerDefaultMaxJobs,
			wake:         make(chan struct{}),
		}
		if cfg := config.Get(); cfg != nil {
			if v := cfg.Skill.WorkerMinWarm; v > 0 {
				pool.minWarm = v
			}
			if v := cfg.Skill.WorkerIdleTTLSec; v > 0 {
				pool.idleTTL = time.Duration(v) * time.Second
			}
			if v := cfg.Skill.WorkerMaxJobs; v > 0 {
				pool.maxJobs = v
			}
		}
		if pool.minWarm > pool.max {
			pool.minWarm = pool.max
		}
		pool.reaperOnce.Do(func() { go pool.reapLoop() })
		log.Printf("[SkillWorker] 已启用常驻池 - max=%d, minWarm=%d, idleTTL=%v, maxJobs=%d, python=%s",
			pool.max, pool.minWarm, pool.idleTTL, pool.maxJobs, python)
		pool.prewarm()
		skillPoolInst = pool
	})
	return skillPoolInst
}

// skillExecutorMode 解析 executor 配置：环境变量 > config.json > auto。
func skillExecutorMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("WEBSQL_SKILL_EXECUTOR")))
	if mode == "" {
		if cfg := config.Get(); cfg != nil {
			mode = strings.ToLower(strings.TrimSpace(cfg.Skill.Executor))
		}
	}
	switch mode {
	case executorWorker, executorProcess:
		return mode
	default:
		return executorAuto
	}
}

// Run 在常驻 worker 上执行脚本；失败时由调用方决定是否回退 one-shot。
func (p *skillWorkerPool) Run(ctx context.Context, spec skillExecSpec) (*skillExecResult, error) {
	w, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}

	id := p.seq.Add(1)
	req := workerRequest{
		ID:        id,
		Script:    spec.Script,
		Argv:      spec.Args,
		Cwd:       spec.Cwd,
		Env:       spec.Env,
		StdinPath: spec.StdinFile,
		TimeoutMs: spec.Timeout.Milliseconds(),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		p.discard(w)
		return nil, fmt.Errorf("构造 worker 请求失败：%w", err)
	}
	payload = append(payload, '\n')
	if _, err := w.stdin.Write(payload); err != nil {
		p.discard(w)
		return nil, fmt.Errorf("%w: 写入请求失败: %v", errWorkerExited, err)
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = executeDefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp := <-w.respCh:
		if resp.ID != id {
			p.discard(w)
			return nil, fmt.Errorf("%w: 响应 id=%d，期望 %d", errWorkerProtocol, resp.ID, id)
		}
		p.release(w)
		return responseToResult(resp), nil

	case <-timer.C:
		p.discard(w)
		return &skillExecResult{
			Output:   fmt.Sprintf("(执行超过 %v 已被终止)", timeout),
			ExitCode: -1,
		}, nil

	case <-ctx.Done():
		p.discard(w)
		return nil, ctx.Err()

	case <-w.done:
		p.discard(w)
		return nil, fmt.Errorf("%w: 进程已退出", errWorkerExited)
	}
}

func responseToResult(resp workerResponse) *skillExecResult {
	code := 0
	if resp.ExitCode != nil {
		code = *resp.ExitCode
	} else if resp.Error != "" {
		code = -1
	}
	output := strings.TrimSpace(resp.Output)
	if resp.Truncated && !strings.Contains(output, "已截断") {
		output += "\n...(输出过长，已截断)"
	}
	if resp.Error != "" {
		output = strings.TrimSpace(output + "\n" + resp.Error)
	}
	return &skillExecResult{Output: output, ExitCode: code}
}

// Close 关闭池：停用并杀掉所有空闲 worker（正在执行的由其请求路径回收）。
func (p *skillWorkerPool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.disabled = true
	idle := p.idle
	p.idle = nil
	p.total -= len(idle)
	if p.total < 0 {
		p.total = 0
	}
	p.broadcastLocked()
	p.mu.Unlock()
	for _, w := range idle {
		w.kill()
	}
}

// acquire 取一个空闲 worker；池未满时新建，满员时等待归还。
func (p *skillWorkerPool) acquire(ctx context.Context) (*skillWorker, error) {
	for {
		p.mu.Lock()
		if p.disabled {
			p.mu.Unlock()
			return nil, fmt.Errorf("%w: 已熔断（连续启动失败）", errWorkerUnavailable)
		}
		for len(p.idle) > 0 {
			w := p.idle[len(p.idle)-1]
			p.idle = p.idle[:len(p.idle)-1]
			if w.alive() {
				p.mu.Unlock()
				return w, nil
			}
			p.total--
		}
		if p.total < p.max {
			p.total++
			p.mu.Unlock()
			w, err := p.spawn()
			if err != nil {
				p.mu.Lock()
				p.total--
				if p.total < 0 {
					p.total = 0
				}
				p.spawnFailures++
				if p.spawnFailures >= 3 && !p.disabled {
					p.disabled = true
					log.Printf("[SkillWorker] 连续启动失败 3 次，后续请求回退 one-shot - err=%v", err)
				}
				p.broadcastLocked()
				p.mu.Unlock()
				return nil, fmt.Errorf("%w: %v", errWorkerUnavailable, err)
			}
			p.mu.Lock()
			p.spawnFailures = 0
			p.mu.Unlock()
			return w, nil
		}
		wake := p.wake
		p.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// release 归还健康 worker；达到回收条件时淘汰。
//
// jobs/lastUsed 与 reapLoop 的读取共用 p.mu 保护，避免 data race。
func (p *skillWorkerPool) release(w *skillWorker) {
	p.mu.Lock()
	w.jobs++
	w.lastUsed = time.Now()
	jobs := w.jobs
	alive := w.alive()
	if alive && jobs < p.maxJobs {
		p.idle = append(p.idle, w)
		p.broadcastLocked()
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.discard(w)
}

// discard 淘汰 worker（杀掉进程并从计数中移除）。
func (p *skillWorkerPool) discard(w *skillWorker) {
	w.kill()
	p.mu.Lock()
	p.total--
	if p.total < 0 {
		p.total = 0
	}
	p.broadcastLocked()
	p.mu.Unlock()
}

func (p *skillWorkerPool) broadcastLocked() {
	close(p.wake)
	p.wake = make(chan struct{})
}

func (p *skillWorkerPool) spawn() (*skillWorker, error) {
	cmd := exec.Command(p.python, "-u", "-B", p.workerScript)
	hideWindow(cmd)
	cmd.Dir = os.TempDir()
	env := sandboxProcessEnv(map[string]string{
		"PYTHONPATH":       os.Getenv("PYTHONPATH"),
		"PYTHONIOENCODING": "utf-8",
		"PYTHONUTF8":       "1",
	})
	if p.pythonEnvDir != "" {
		for i, kv := range env {
			if strings.HasPrefix(kv, "PATH=") {
				env[i] = "PATH=" + p.pythonEnvDir + string(os.PathListSeparator) + strings.TrimPrefix(kv, "PATH=")
				break
			}
		}
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr cappedBuffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	w := &skillWorker{
		cmd:      cmd,
		stdin:    stdin,
		respCh:   make(chan workerResponse, 1),
		done:     make(chan struct{}),
		lastUsed: time.Now(),
	}
	go w.readLoop(stdout)
	go func() {
		_ = cmd.Wait()
		close(w.done)
	}()

	// 握手：确认解释器可用且协议版本匹配，失败则视为 worker 不可用（可安全回退）。
	select {
	case resp := <-w.respCh:
		if !resp.Ready {
			w.kill()
			return nil, fmt.Errorf("握手失败：%s %s", resp.Error, stderr.String())
		}
	case <-time.After(workerHandshakeTimeout):
		w.kill()
		return nil, fmt.Errorf("握手超时（%v）：%s", workerHandshakeTimeout, stderr.String())
	}
	return w, nil
}

// readLoop 持续解析 worker 响应；进程退出或管道关闭时结束。
func (w *skillWorker) readLoop(stdout io.ReadCloser) {
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			line = strings.TrimSpace(line)
			if line != "" {
				var resp workerResponse
				if jsonErr := json.Unmarshal([]byte(line), &resp); jsonErr == nil {
					select {
					case w.respCh <- resp:
					default:
						// 不应出现：单请求单响应。丢弃多余响应避免阻塞读循环。
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// prewarm 预拉 minWarm 个空闲 worker。
//
// 只预热解释器进程（worker 本身仅导入标准库），不预先导入 matplotlib/pptx——
// 那些依赖由首个脚本执行时导入，避免启动就把 N×75MB 拉进内存。
func (p *skillWorkerPool) prewarm() {
	if p.minWarm <= 0 {
		return
	}
	go func() {
		for i := 0; i < p.minWarm; i++ {
			p.mu.Lock()
			if p.total >= p.max || p.disabled {
				p.mu.Unlock()
				return
			}
			p.total++
			p.mu.Unlock()
			w, err := p.spawn()
			if err != nil {
				p.mu.Lock()
				p.total--
				if p.total < 0 {
					p.total = 0
				}
				p.broadcastLocked()
				p.mu.Unlock()
				log.Printf("[SkillWorker] 预热失败：%v", err)
				return
			}
			p.mu.Lock()
			p.idle = append(p.idle, w)
			p.broadcastLocked()
			p.mu.Unlock()
		}
		log.Printf("[SkillWorker] 预热完成 - warm=%d", p.minWarm)
	}()
}

// reapLoop 空闲回收：保留 minWarm 个热进程，其余超过 idleTTL 的退出。
func (p *skillWorkerPool) reapLoop() {
	ticker := time.NewTicker(workerReapInterval)
	defer ticker.Stop()
	for range ticker.C {
		p.mu.Lock()
		if len(p.idle) <= p.minWarm {
			p.mu.Unlock()
			continue
		}
		keep := make([]*skillWorker, 0, len(p.idle))
		var victims []*skillWorker
		for _, w := range p.idle {
			if len(p.idle)-len(victims) > p.minWarm && time.Since(w.lastUsed) > p.idleTTL {
				victims = append(victims, w)
				continue
			}
			keep = append(keep, w)
		}
		p.idle = keep
		p.total -= len(victims)
		if p.total < 0 {
			p.total = 0
		}
		p.broadcastLocked()
		p.mu.Unlock()
		for _, w := range victims {
			w.kill()
		}
		if len(victims) > 0 {
			log.Printf("[SkillWorker] 空闲回收 %d 个 worker，当前保留 %d 个", len(victims), len(keep))
		}
	}
}
