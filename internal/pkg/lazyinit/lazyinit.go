package lazyinit

import "sync"

// Holder 提供线程安全的延迟初始化封装，替代各包散落的
// var (defaultXxx T; defaultXxxOnce sync.Once; func ensureDefaultXxx()) 模式。
type Holder[T any] struct {
	once sync.Once
	val  T
}

// Get 返回已初始化的实例；首次调用时通过 init 构造。
func (h *Holder[T]) Get(init func() T) T {
	h.once.Do(func() { h.val = init() })
	return h.val
}
