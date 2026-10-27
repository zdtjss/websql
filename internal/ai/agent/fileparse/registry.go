package fileparse

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"sync"

	"websql/internal/pkg/safego"
)

// 解析器注册表。
//
// 同一扩展名可注册多个实现，按优先级从高到低选择第一个「可用」的实现：
//   - 可用性由 Available 回调动态判定（如 Python 解析器依赖解释器与三方库）；
//   - 高优先级实现不可用时自动降级到低优先级实现（如 docx 的 Go 兜底），
//     全部不可用才返回「不支持」错误。
//
// 实现名会写入 Result.Impl，并被能力自检接口（fileSupport）用于向用户
// 说明当前环境实际生效的解析方式。

// Parser 单个格式的解析实现
type Parser interface {
	Parse(ctx context.Context, src Source) (*Result, error)
}

// ParserFunc 函数式解析实现
type ParserFunc func(ctx context.Context, src Source) (*Result, error)

// Parse 实现 Parser 接口
func (f ParserFunc) Parse(ctx context.Context, src Source) (*Result, error) { return f(ctx, src) }

// impl 注册表中的一条实现记录
type impl struct {
	name      string
	priority  int
	available func() bool // nil 表示始终可用
	parser    Parser
}

var (
	regMu    sync.RWMutex
	registry = map[string][]impl{}
)

// Register 注册扩展名的解析实现。
//
// name 为实现名（写入 Result.Impl）；priority 越大越优先；
// available 为 nil 时视为始终可用；exts 会被归一化（小写、加前导点）。
func Register(name string, priority int, available func() bool, exts []string, p Parser) {
	if p == nil {
		return
	}
	regMu.Lock()
	defer regMu.Unlock()
	for _, ext := range exts {
		ext = NormalizeExt(ext)
		if ext == "" {
			continue
		}
		registry[ext] = append(registry[ext], impl{name: name, priority: priority, available: available, parser: p})
		sort.SliceStable(registry[ext], func(i, j int) bool {
			return registry[ext][i].priority > registry[ext][j].priority
		})
	}
}

// pickImpl 选择该扩展名当前可用的最高优先级实现；ok=false 表示无可用实现。
func pickImpl(ext string) (impl, bool) {
	regMu.RLock()
	list := registry[NormalizeExt(ext)]
	copied := make([]impl, len(list))
	copy(copied, list)
	regMu.RUnlock()

	for _, it := range copied {
		if it.available == nil || it.available() {
			return it, true
		}
	}
	return impl{}, false
}

// IsSupported 判断扩展名当前是否有可用解析实现
func IsSupported(ext string) bool {
	_, ok := pickImpl(ext)
	return ok
}

// Describe 返回扩展名的类别与当前生效的实现名（无可用实现时 impl="none"）。
// 供 /ai/agent/fileSupport 能力自检接口与上传校验使用。
func Describe(ext string) (Kind, string, bool) {
	ext = NormalizeExt(ext)
	kind, known := KindOfExt(ext)
	if !known {
		return "", "none", false
	}
	it, ok := pickImpl(ext)
	if !ok {
		return kind, "none", false
	}
	return kind, it.name, true
}

// Parse 按扩展名分发解析，统一叠加超时与 panic 恢复。
//
// 降级链：按优先级依次尝试**所有可用实现**——不仅在高优先级实现不可用（依赖缺失）
// 时降级，也在其解析失败时降级（例如 python-docx 对某些非标准 OOXML 报错，
// 而内置 Go 解析仍能提取正文）。全部失败时返回最后一个错误。
//
// panic 恢复：解析三方库对畸形输入可能 panic，此处兜住并转为错误，
// 避免单次解析拖垮整个 Agent 进程。
func Parse(ctx context.Context, src Source) (result *Result, err error) {
	src.Ext = NormalizeExt(src.Ext)
	if src.Path == "" {
		return nil, fmt.Errorf("文件路径为空")
	}

	candidates := availableImpls(src.Ext)
	if len(candidates) == 0 {
		if kind, known := KindOfExt(src.Ext); known {
			return nil, fmt.Errorf("当前环境暂不支持 %s 类型文件（%s）的内容解析", kind, src.Ext)
		}
		return nil, fmt.Errorf("不支持的文件格式：%s", src.Ext)
	}

	parseCtx, cancel := context.WithTimeout(ctx, ParseTimeout)
	defer cancel()

	var lastErr error
	for idx, it := range candidates {
		res, attemptErr := parseWithImpl(parseCtx, it, src)
		if attemptErr == nil {
			if idx > 0 {
				log.Printf("[FileParse] 已降级到较低优先级实现 - ext=%s, impl=%s, 前序失败原因=%v\n",
					src.Ext, it.name, lastErr)
			}
			return res, nil
		}
		lastErr = attemptErr
		// 上下文已结束（超时/取消）：不再尝试后续实现，避免无意义的失败与日志噪音
		if parseCtx.Err() != nil {
			return nil, attemptErr
		}
		if idx < len(candidates)-1 {
			log.Printf("[FileParse] 解析实现失败，尝试降级 - ext=%s, impl=%s, err=%v\n", src.Ext, it.name, attemptErr)
		}
	}
	return nil, lastErr
}

// parseWithImpl 使用单个实现解析，含 panic 恢复与结果规范化。
func parseWithImpl(ctx context.Context, it impl, src Source) (result *Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[FileParse] 解析 panic 已恢复 - impl=%s, ext=%s, file=%s, panic=%v\n%s",
				it.name, src.Ext, src.FileName, r, string(debug.Stack()))
			result = nil
			err = fmt.Errorf("解析文件失败（解析器异常），请检查文件是否损坏")
		}
	}()

	res, err := it.parser.Parse(ctx, src)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("解析文件失败（无结果）")
	}
	if res.Impl == "" {
		res.Impl = it.name
	}
	if res.Kind == "" {
		res.Kind, _ = KindOfExt(src.Ext)
	}
	return res, nil
}

// availableImpls 返回该扩展名当前可用的实现（按优先级降序）。
func availableImpls(ext string) []impl {
	regMu.RLock()
	list := registry[NormalizeExt(ext)]
	copied := make([]impl, len(list))
	copy(copied, list)
	regMu.RUnlock()

	out := make([]impl, 0, len(copied))
	for _, it := range copied {
		if it.available == nil || it.available() {
			out = append(out, it)
		}
	}
	return out
}

// ParseCached 带结果缓存的解析：命中缓存直接返回，否则解析并写入缓存。
//
// cacheKey 建议使用上传文件 ID（同文件多次读取只解析一次）。
// 缓存写入失败（容量淘汰）不影响结果返回。
func ParseCached(ctx context.Context, cacheKey string, src Source) (*Result, bool, error) {
	if cacheKey != "" {
		if r, ok := Cached(cacheKey); ok {
			return r, true, nil
		}
	}
	r, err := Parse(ctx, src)
	if err != nil {
		return nil, false, err
	}
	if cacheKey != "" {
		Put(cacheKey, r)
	}
	return r, false, nil
}

// logRegistry 输出注册表概览（启动自检用）。
func logRegistry() {
	exts := SupportedExts()
	for _, ext := range exts {
		kind, implName, ok := Describe(ext)
		status := implName
		if !ok {
			status = "none"
		}
		log.Printf("[FileParse] 格式支持 - ext=%s, kind=%s, impl=%s\n", ext, kind, status)
	}
}

// InitLog 在服务启动阶段输出一次格式支持概览，并预热 Python 依赖探测，
// 便于排查「某格式不可用」类问题（幂等，重复调用只执行一次）。
func InitLog() {
	initLogOnce.Do(func() {
		safego.GoWithName("fileparse-log", func() {
			logRegistry()
			WarmPythonProbes()
		})
	})
}

var initLogOnce sync.Once
