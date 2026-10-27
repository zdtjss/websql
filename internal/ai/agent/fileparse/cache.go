package fileparse

import (
	"websql/internal/pkg/ttlcache"
)

// 解析结果缓存。
//
// 目的：同一次上传的文件在一次会话中可能被分页读取多次
// （read_file_data 的 offset/limit 分页），缓存避免重复解析与重复读盘。
// 生命周期与上传暂存一致（CacheTTL = 30 分钟）；文件删除时由调用方调用 Purge。
var parseCache = func() *ttlcache.TTLCache[*Result] {
	c := ttlcache.New[*Result](CacheTTL, cacheCleanSec)
	c.SetMaxEntries(maxCacheEntries)
	return c
}()

// Cached 读取缓存的解析结果
func Cached(key string) (*Result, bool) {
	if key == "" {
		return nil, false
	}
	return parseCache.Get(key)
}

// Put 写入解析结果缓存
func Put(key string, r *Result) {
	if key == "" || r == nil {
		return
	}
	parseCache.Set(key, r)
}

// Purge 清除指定文件的解析缓存（上传文件删除/过期时调用）
func Purge(key string) {
	if key == "" {
		return
	}
	parseCache.Delete(key)
}

// PurgeAll 清空解析缓存（测试与配置变更场景使用）
func PurgeAll() {
	parseCache.InvalidateAll()
}
