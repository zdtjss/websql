// offload.go — 大结果落盘文件的读取与访问控制。
//
// 设计目标：
//   - 截断/clear 落盘的文件不再是“只给模型一个路径”，而是可通过 read_offload
//     按行分页读取的可恢复句柄；
//   - 不同用户/会话的 offload 文件物理隔离，通用 read_file/ls/grep 不能访问
//     reduction 目录，避免跨用户读取原始查询结果或上传文件内容；
//   - 行式格式（JSONL/CSV/文本）下，offset/limit 以“行”为单位，和 IDE
//     read_file 的分页模型一致。
package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"websql/internal/pkg/idgen"
)

const (
	// defaultReadOffloadLimit read_offload 默认返回行数
	defaultReadOffloadLimit = 200
	// maxReadOffloadLimit read_offload 单次最大返回行数（服务端安全上限）
	maxReadOffloadLimit = 2_000
	// maxReadOffloadLineBytes 单行最大字节数，超出部分截断并提示
	maxReadOffloadLineBytes = 64 * 1024
	// maxReadOffloadBytes 单次返回内容的最大字节数。
	// 低于 reduction 截断阈值，避免 read_offload 自身再次撑爆上下文。
	maxReadOffloadBytes = 24 * 1024
	// maxReadOffloadFileBytes 单文件扫描上限；超过时退化为“只扫描请求窗口”
	maxReadOffloadFileBytes = 64 << 20
)

// ReadOffloadInput read_offload 工具入参。
type ReadOffloadInput struct {
	Path          string `json:"path" jsonschema:"required" jsonschema_description:"截断/摘要提示中给出的 offload 文件路径"`
	Offset        int    `json:"offset,omitempty" jsonschema_description:"起始行号（1-based），默认 1"`
	Limit         int    `json:"limit,omitempty" jsonschema_description:"返回行数，默认 200，最大 2000"`
	Pattern       string `json:"pattern,omitempty" jsonschema_description:"可选：只返回匹配行"`
	Regex         bool   `json:"regex,omitempty" jsonschema_description:"pattern 是否按正则解释"`
	CaseSensitive bool   `json:"caseSensitive,omitempty" jsonschema_description:"是否区分大小写"`
}

// ReadOffloadLine 单行结果。
type ReadOffloadLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// ReadOffloadOutput read_offload 工具出参。
type ReadOffloadOutput struct {
	Path          string            `json:"path"`
	Offset        int               `json:"offset"`
	ReturnedLines int               `json:"returnedLines"`
	TotalLines    int               `json:"totalLines"` // -1 表示文件过大，未统计总行数
	Truncated     bool              `json:"truncated,omitempty"`
	Lines         []ReadOffloadLine `json:"lines,omitempty"`
	Notice        string            `json:"notice,omitempty"`
}

// reductionOffloadRoot 返回 reduction offload 根目录（绝对路径）。
// 与 middleware.go 共用，确保写盘路径与读取校验一致。
func reductionOffloadRoot() string {
	dir := os.Getenv("REDUCTION_OFFLOAD_DIR")
	if dir == "" {
		dir = filepath.Join("data", "reduction")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir
}

// safeSegment 把用户/会话 ID 转为安全的路径段。
// 允许字母、数字、下划线、短横线、点；其他字符替换为下划线。
// 若发生替换或长度过长，追加原文哈希前缀，避免不同 ID 归一化后碰撞。
func safeSegment(s string) string {
	if s == "" {
		return "_"
	}
	var b strings.Builder
	changed := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
			changed = true
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		out = "_"
		changed = true
	}
	if changed || len(out) > 64 {
		sum := sha256.Sum256([]byte(s))
		suffix := hex.EncodeToString(sum[:4])
		if len(out) > 48 {
			out = out[:48]
		}
		out = out + "-" + suffix
	}
	return out
}

// offloadRootForIdentity 返回当前用户/会话的 offload 根目录（不含权限修订号）。
// 统一放在 users/ 子目录下，避免与旧版全局 trunc/clear 目录或特殊 userID 冲突。
func offloadRootForIdentity(id RunIdentity) string {
	return filepath.Join(reductionOffloadRoot(), "users", safeSegment(id.UserID), safeSegment(id.SessionID))
}

// offloadRootForContext 返回当前用户/会话/权限修订号对应的 offload 根目录。
// 权限修订号变化后，旧 offload 文件不再可读，防止撤销权限后继续读取旧结果。
func offloadRootForContext(ctx context.Context) string {
	id, ok := runIdentityFrom(ctx)
	if !ok || id.UserID == "" || id.SessionID == "" {
		return ""
	}
	var revision uint64
	if sec, err := RunSecurityFrom(ctx); err == nil {
		if sec.UserID != "" && id.UserID != "" && sec.UserID != id.UserID {
			return ""
		}
		revision = sec.Revision
	}
	return filepath.Join(offloadRootForIdentity(id), fmt.Sprintf("rev-%d", revision))
}

// reductionOffloadPath 生成当前用户/会话下的 offload 文件路径。
// kind 为 trunc 或 clear；callID 为空时生成随机文件名。
func reductionOffloadPath(ctx context.Context, kind, callID string) (string, error) {
	id, ok := runIdentityFrom(ctx)
	if !ok || id.UserID == "" || id.SessionID == "" {
		return "", errors.New("缺少运行身份，拒绝 offload")
	}
	if kind != "trunc" && kind != "clear" {
		return "", fmt.Errorf("未知的 offload 类型：%s", kind)
	}
	name := safeSegment(callID)
	if strings.TrimSpace(callID) == "" {
		name = idgen.RandomStr()
	}
	root := offloadRootForContext(ctx)
	if root == "" {
		return "", errors.New("缺少运行身份，拒绝 offload")
	}
	return filepath.Join(root, kind, name), nil
}

// resolveOffloadPath 校验路径必须位于当前用户/会话的 offload 目录内。
// 拒绝路径穿越、符号链接和非普通文件。
func resolveOffloadPath(ctx context.Context, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("path is required")
	}
	id, ok := runIdentityFrom(ctx)
	if !ok || id.UserID == "" || id.SessionID == "" {
		return "", errors.New("缺少运行身份，无法访问 offload 文件")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("路径无效：%w", err)
	}
	abs = filepath.Clean(abs)
	root := offloadRootForContext(ctx)
	if root == "" || !pathWithinRoot(abs, root) {
		return "", fmt.Errorf("offload 路径 %q 不在当前用户/会话的允许范围内", p)
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("offload 文件不存在或已过期：%w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("拒绝读取符号链接形式的 offload 文件")
	}
	if !fi.Mode().IsRegular() {
		return "", errors.New("offload 路径不是普通文件")
	}
	if hasSymlinkComponent(abs, root) {
		return "", errors.New("offload 路径包含符号链接，拒绝读取")
	}
	return abs, nil
}

// pathWithinRoot 判断 absPath 是否位于 root 内（Windows 下大小写不敏感）。
func pathWithinRoot(absPath, root string) bool {
	absPath = normalizePathForCompare(absPath)
	root = normalizePathForCompare(root)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	// Windows 下 Rel 已处理大小写；此处再防御一次绝对路径回退。
	return !filepath.IsAbs(rel)
}

// hasSymlinkComponent 检查 path（含 root 本身）的路径组件是否存在符号链接。
// 不依赖 EvalSymlinks，避免 Windows 上对普通文件误报路径不存在。
func hasSymlinkComponent(path, root string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return true
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	cur := absRoot
	if fi, statErr := os.Lstat(cur); statErr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return true
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, statErr := os.Lstat(cur)
		if statErr != nil {
			// 尚未创建的子路径（如写入目标）不算符号链接。
			return false
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// normalizePathForCompare 去掉 Windows EvalSymlinks 可能返回的 \\?\ 长路径前缀。
func normalizePathForCompare(p string) string {
	p = filepath.Clean(p)
	if strings.HasPrefix(p, `\\?\UNC\`) {
		return `\\` + p[len(`\\?\UNC\`):]
	}
	if strings.HasPrefix(p, `\\?\`) {
		return p[len(`\\?\`):]
	}
	return p
}

// CleanupLegacyOffload 清理旧版全局 offload 目录（trunc/clear）。
// 新版路径为 <root>/<user>/<session>/{trunc,clear}，旧文件不再可读，启动时直接删除。
func CleanupLegacyOffload() {
	for _, kind := range []string{"trunc", "clear"} {
		_ = os.RemoveAll(filepath.Join(reductionOffloadRoot(), kind))
	}
}

// RemoveOffloadSession 删除某用户/会话的 offload 文件。
func RemoveOffloadSession(userID, sessionID string) {
	if userID == "" || sessionID == "" {
		return
	}
	root := filepath.Join(reductionOffloadRoot(), "users", safeSegment(userID), safeSegment(sessionID))
	_ = os.RemoveAll(root)
}

// NewReadOffloadFunc 构造 read_offload 工具。
func NewReadOffloadFunc() func(ctx context.Context, input *ReadOffloadInput) (*ReadOffloadOutput, error) {
	return func(ctx context.Context, input *ReadOffloadInput) (*ReadOffloadOutput, error) {
		if input == nil {
			return nil, errors.New("input is required")
		}
		if sec, secErr := RunSecurityFrom(ctx); secErr == nil {
			if sec.DataEgressBlocked {
				recordAgentMetric("ai_offload_read_total", "egress_blocked")
				return nil, errors.New("数据出域安全依赖不可用，已拒绝 offload 读取")
			}
			if !sec.RevisionStillValid() {
				recordAgentMetric("ai_offload_read_total", "stale_revision")
				return nil, errors.New("权限已发生变化，offload 读取已失效，请重新发起")
			}
		}
		filePath, err := resolveOffloadPath(ctx, input.Path)
		if err != nil {
			recordAgentMetric("ai_offload_read_total", "denied")
			return nil, err
		}

		offset := input.Offset
		if offset <= 0 {
			offset = 1
		}
		limit := input.Limit
		if limit <= 0 {
			limit = defaultReadOffloadLimit
		}
		if limit > maxReadOffloadLimit {
			limit = maxReadOffloadLimit
		}

		var matcher *regexp.Regexp
		pattern := strings.TrimSpace(input.Pattern)
		if pattern != "" {
			if input.Regex {
				expr := pattern
				if !input.CaseSensitive {
					expr = "(?i)" + expr
				}
				matcher, err = regexp.Compile(expr)
				if err != nil {
					return nil, fmt.Errorf("pattern 不是合法正则：%w", err)
				}
			}
		}

		f, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("打开 offload 文件失败：%w", err)
		}
		defer f.Close()

		fi, err := f.Stat()
		if err != nil {
			return nil, fmt.Errorf("读取 offload 文件信息失败：%w", err)
		}
		largeFile := fi.Size() > maxReadOffloadFileBytes

		reader := bufio.NewReaderSize(f, 64*1024)
		lineNo := 0
		returnedBytes := 0
		lines := make([]ReadOffloadLine, 0, limit)
		totalLines := 0
		truncated := false
		noticeParts := make([]string, 0, 2)

		for {
			line, lineTruncated, readErr := readLineCapped(reader, maxReadOffloadLineBytes)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("读取 offload 文件失败：%w", readErr)
			}
			if line == "" && errors.Is(readErr, io.EOF) {
				break
			}
			lineNo++
			totalLines++
			if lineTruncated {
				truncated = true
				noticeParts = append(noticeParts, fmt.Sprintf("第 %d 行超过 %d 字节，已截断", lineNo, maxReadOffloadLineBytes))
			}

			// 大文件的窗口模式只扫描到请求窗口结束；pattern 模式需要继续扫描，
			// 否则会在前 limit 行内找不到匹配就误报“没有命中”。
			if largeFile && matcher == nil && pattern == "" && lineNo >= offset+limit {
				totalLines = -1
				truncated = true
				break
			}

			match := false
			if matcher != nil {
				match = matcher.MatchString(line)
			} else if pattern != "" {
				if input.CaseSensitive {
					match = strings.Contains(line, pattern)
				} else {
					match = strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
				}
			} else {
				match = lineNo >= offset
			}

			if match {
				if len(lines) >= limit {
					truncated = true
					break
				}
				text := line
				remaining := maxReadOffloadBytes - returnedBytes
				if remaining <= 0 {
					truncated = true
					break
				}
				if len(text) > remaining {
					text = clampUTF8Prefix(text, remaining)
					truncated = true
				}
				returnedBytes += len(text)
				lines = append(lines, ReadOffloadLine{Line: lineNo, Text: text})
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}

		if largeFile {
			noticeParts = append(noticeParts, "文件较大，未统计完整总行数；可用 pattern 或更小窗口继续读取")
		}
		if len(lines) == 0 {
			noticeParts = append(noticeParts, "没有匹配的行")
		}

		recordAgentMetric("ai_offload_read_total", "success")
		return &ReadOffloadOutput{
			Path:          filePath,
			Offset:        offset,
			ReturnedLines: len(lines),
			TotalLines:    totalLines,
			Truncated:     truncated,
			Lines:         lines,
			Notice:        strings.Join(noticeParts, "；"),
		}, nil
	}
}

// readLineCapped 读取一行，超过 max 字节的部分丢弃但继续读到换行。
func readLineCapped(r *bufio.Reader, max int) (string, bool, error) {
	var b strings.Builder
	truncated := false
	for {
		chunk, err := r.ReadSlice('\n')
		if len(chunk) > 0 {
			remaining := max - b.Len()
			if remaining <= 0 {
				truncated = true
			} else if len(chunk) > remaining {
				b.Write(chunk[:remaining])
				truncated = true
			} else {
				b.Write(chunk)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			return strings.TrimRight(b.String(), "\r\n"), truncated, io.EOF
		}
		if err != nil {
			return strings.TrimRight(b.String(), "\r\n"), truncated, err
		}
		return strings.TrimRight(b.String(), "\r\n"), truncated, nil
	}
}

// clampUTF8Prefix 按 UTF-8 边界截断字符串。
func clampUTF8Prefix(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
