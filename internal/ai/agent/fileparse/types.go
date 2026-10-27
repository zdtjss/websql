// Package fileparse 提供上传文件的内容解析能力。
//
// 设计要点：
//   - 与存储域解耦：仅接收 Source（磁盘路径 + 元信息），不依赖上传暂存实现，
//     便于单测直接使用 testdata 文件；
//   - 解析器以「扩展名 → 实现」注册，支持多实现按优先级降级
//     （docx：Python python-docx 优先，无 Python 环境时回退内置 Go 解析）；
//   - 上传阶段不触发本包任何解析（延迟解析）：仅在用户明确需要文件内容时调用，
//     配合 cache.go 的解析结果缓存避免同一文件重复解析。
package fileparse

// Kind 文件内容类别（存储与能力校验共用的类型词汇）
type Kind string

const (
	// KindTable 表格类（xlsx/xls/csv）：解析为列名 + 行数据
	KindTable Kind = "table"
	// KindText 纯文本类（txt/md/markdown）：解析为文本内容
	KindText Kind = "text"
	// KindDoc 文档类（docx/pdf）：解析为文本内容
	KindDoc Kind = "doc"
	// KindImage 图片类（jpg/jpeg/png）：解析为尺寸等元信息，内容经多模态注入
	KindImage Kind = "image"
	// KindVideo 视频类（mp4）：解析为时长/分辨率等元信息
	KindVideo Kind = "video"
)

// Source 待解析文件描述
type Source struct {
	Path     string // 磁盘路径（服务端控制，非用户输入）
	FileName string // 展示名（仅用于错误提示与结果标注）
	Ext      string // 扩展名，小写且含点，如 ".docx"
	Size     int64  // 字节数（0 表示未知）
}

// Result 解析结果。
//
// 表格类填充 Columns/Rows/TotalRows；文本与文档类填充 Text/TotalChars/Pages；
// 图片/视频类填充 Meta。分页（返回行窗口/字符窗口）由调用方基于本结果完成，
// 解析层始终返回完整内容（受 limits.go 的硬上限约束）。
type Result struct {
	Kind       Kind
	Columns    []string
	Rows       [][]string // 不含表头行
	TotalRows  int
	Text       string
	TotalChars int
	Pages      int
	Meta       map[string]any
	// Impl 实际生效的解析实现名（go / python:<lib>），用于可观测性与能力自检
	Impl string
	// Degraded 是否走了降级实现（如 docx 的 Go 兜底解析），提示模型解析质量可能有限
	Degraded bool
	// Notice 面向模型的说明（降级原因、截断、扫描件等）
	Notice string
}

// IsTabular 是否为表格类结果
func (r *Result) IsTabular() bool { return r != nil && r.Kind == KindTable }
