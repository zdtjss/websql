package fileparse

import (
	"sort"
	"strings"
)

// 扩展名 → 内容类别（前后端与文档共用的唯一事实源）。
//
// 新增格式时只需在此登记 + 注册解析器；上传校验、能力探测、前端提示
// 均由本矩阵派生，避免多处硬编码造成不一致。
var extKinds = map[string]Kind{
	".xlsx":     KindTable,
	".xls":      KindTable,
	".csv":      KindTable,
	".txt":      KindText,
	".md":       KindText,
	".markdown": KindText,
	".docx":     KindDoc,
	".pdf":      KindDoc,
	".jpg":      KindImage,
	".jpeg":     KindImage,
	".png":      KindImage,
	".mp4":      KindVideo,
}

const (
	// defaultMaxSize 未单独指定上限的类别默认 20MB（与既有限制一致）
	defaultMaxSize int64 = 20 << 20
	// imageMaxSize 图片上限：需经 base64 注入多模态请求，过大将显著膨胀请求体
	imageMaxSize int64 = 10 << 20
)

// NormalizeExt 归一化扩展名（小写、加前导点）
func NormalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

// KindOfExt 返回扩展名对应的内容类别
func KindOfExt(ext string) (Kind, bool) {
	k, ok := extKinds[NormalizeExt(ext)]
	return k, ok
}

// SupportedExts 返回全部受支持扩展名（稳定排序，供前端 accept 与自检接口使用）
func SupportedExts() []string {
	exts := make([]string, 0, len(extKinds))
	for ext := range extKinds {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

// AllowedSize 返回该扩展名的上传大小上限（字节）；返回 0 表示不限。
//
// 现状兼容：Markdown 不做大小限制（保持既有行为）。
func AllowedSize(ext string) int64 {
	ext = NormalizeExt(ext)
	if ext == ".md" || ext == ".markdown" {
		return 0
	}
	if k, ok := extKinds[ext]; ok && k == KindImage {
		return imageMaxSize
	}
	return defaultMaxSize
}
