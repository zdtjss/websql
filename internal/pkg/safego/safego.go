// Package safego 提供安全的 goroutine 启动工具，确保任何 goroutine 内的 panic
// 都不会导致整个进程退出。这是 Go 服务稳定性的核心兜底机制。
//
// Go 语言中，一个未被 recover 的 goroutine panic 会导致整个进程崩溃退出。
// 使用 safego.Go() 替代裸 go 语句启动 goroutine，可以自动捕获 panic、
// 记录堆栈信息，并防止进程退出。
//
// 用法：
//
//	// 替代 go func() { ... }()
//	safego.Go(func() {
//	    // 你的逻辑...
//	})
//
//	// 替代 go someFunc(args...)
//	safego.Go(func() { someFunc(args...) })
package safego

import (
	"log"
	"runtime/debug"
	"sync/atomic"
)

// recoveredCount 记录全局已恢复的 panic 次数，可用于监控告警。
var recoveredCount atomic.Int64

// RecoveredCount 返回进程生命周期内已被安全恢复的 goroutine panic 总数。
func RecoveredCount() int64 {
	return recoveredCount.Load()
}

// Go 安全地启动一个 goroutine。
// 如果 fn 内部发生 panic，会被自动 recover，记录日志和堆栈，进程不会退出。
func Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				recoveredCount.Add(1)
				stack := debug.Stack()
				log.Printf("[SafeGo] goroutine panic 已恢复 - panic=%v\n%s", r, string(stack))
			}
		}()
		fn()
	}()
}

// GoWithName 安全地启动一个带名称标识的 goroutine。
// 名称会出现在恢复日志中，便于定位问题来源。
func GoWithName(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				recoveredCount.Add(1)
				stack := debug.Stack()
				log.Printf("[SafeGo:%s] goroutine panic 已恢复 - panic=%v\n%s", name, r, string(stack))
			}
		}()
		fn()
	}()
}

// Recover 是一个可直接用在 defer 中的恢复函数。
// 适用于已有 goroutine 但需要添加 recover 保护的场景。
//
// 用法：
//
//	go func() {
//	    defer safego.Recover("my-task")
//	    // 你的逻辑...
//	}()
func Recover(name string) {
	if r := recover(); r != nil {
		recoveredCount.Add(1)
		stack := debug.Stack()
		log.Printf("[SafeGo:%s] panic 已恢复 - panic=%v\n%s", name, r, string(stack))
	}
}
