package fileparse

import "time"

// 解析与资源限额。
//
// 全部为「防御性上限」：上传文件可能由恶意构造（zip bomb、超大单元格、
// 超长行、超大图片），解析层必须在有限资源内完成或明确失败，
// 而不是拖垮进程。
const (
	// ParseTimeout 单次解析超时（含解压、XML 遍历等 CPU 操作）
	ParseTimeout = 15 * time.Second

	// MaxTextChars 解析产出文本的字符上限（rune 计），超出后截断并给出 Notice
	MaxTextChars = 5_000_000

	// MaxTableRows 表格行数上限（不含表头），超出后截断并给出 Notice
	MaxTableRows = 500_000

	// MaxDecompressBytes zip 类文件（docx/xlsx）解压总尺寸上限，防 zip bomb
	MaxDecompressBytes int64 = 200 << 20

	// MaxZipEntries zip 条目数上限，防海量小条目耗尽句柄/内存
	MaxZipEntries = 10_000

	// MaxImagePixels 图片像素上限（宽 × 高），防解码超大图耗尽内存
	MaxImagePixels = 100_000_000

	// DefaultRowLimit 表格分页默认返回行数（与既有 read_file_data 行为一致）
	DefaultRowLimit = 100

	// MaxRowLimit 表格分页单次最大返回行数（与既有 read_file_data 行为一致）
	MaxRowLimit = 500

	// CacheTTL 解析结果缓存有效期，与上传暂存 TTL 对齐（上传过期即无需再解析）
	CacheTTL = 30 * time.Minute

	// cacheCleanSec 缓存后台清理周期
	cacheCleanSec = 5 * time.Minute

	// maxCacheEntries 解析结果缓存容量上限
	maxCacheEntries = 64
)
