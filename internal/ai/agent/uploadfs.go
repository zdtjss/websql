// uploadfs.go — 把已上传文件映射为 eino filesystem 中间件可读的虚拟文件系统。
//
// 设计目标：
//   - 复用现有 read_file/grep/glob/ls，获得 IDE 式的按行读取与搜索能力；
//   - 虚拟路径 /uploads/<fileId>/content.txt、sheet-<n>.csv、meta.json；
//   - 所有读取都经过 GetUploadedFile 的归属校验，写入 /uploads 一律拒绝；
//   - 非 /uploads 路径委托给 OSFilesystemBackend（skills/exports/workspace）。
package agent

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"websql/internal/ai/agent/export"
	"websql/internal/ai/agent/fileparse"

	"github.com/cloudwego/eino/adk/filesystem"
)

const uploadVFSRoot = "/uploads"

type uploadFSBackend struct {
	os     *export.OSFilesystemBackend
	userID string
}

func newUploadFSBackend(osBackend *export.OSFilesystemBackend, userID string) *uploadFSBackend {
	return &uploadFSBackend{os: osBackend, userID: userID}
}

// denySharedExportPath 在委托给 OS 后端前拦截共享导出目录：
// 仅允许本人在 exports/users/<uid> 下的路径；exports 根、.owners 与其他用户目录一律拒绝。
func (b *uploadFSBackend) denySharedExportPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil
	}
	abs, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		return nil
	}
	shared, err := filepath.Abs(filepath.FromSlash("exports"))
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(shared, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil // 不在共享 exports 下，交给 OS 后端按用户根校验
	}
	own := filepath.Join(shared, "users", export.SandboxUserSegment(b.userID))
	ownRel, ownErr := filepath.Rel(own, abs)
	if ownErr == nil && ownRel != ".." && !strings.HasPrefix(ownRel, ".."+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("不允许通过文件工具访问共享导出目录（请使用导出工具返回的下载链接）")
}

// ──────────────────────────────────────────────
// 当前会话附件列表（仅用于 ls/glob）
// ──────────────────────────────────────────────

type uploadFileIDsKey struct{}

// WithUploadFileIDs 把当前会话可访问的文件 ID 注入 context。
func WithUploadFileIDs(ctx context.Context, ids []string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	return context.WithValue(ctx, uploadFileIDsKey{}, append([]string(nil), ids...))
}

func uploadFileIDsFrom(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	ids, _ := ctx.Value(uploadFileIDsKey{}).([]string)
	return ids
}

// collectRunAttachmentIDs 汇总本轮请求与会话历史中引用的上传文件 ID。
func collectRunAttachmentIDs(req ChatRequest, msgs []SessionMessage) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(req.Attachments)+2)
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	if req.ExcelData != nil {
		add(req.ExcelData.FileID)
	}
	for _, att := range req.Attachments {
		add(att.FileID)
	}
	for _, msg := range msgs {
		for _, att := range msg.Attachments {
			add(att.FileID)
		}
	}
	return out
}

// ──────────────────────────────────────────────
// 虚拟路径解析
// ──────────────────────────────────────────────

type virtualUploadPath struct {
	FileID string
	Name   string // content.txt / sheet-0.csv / meta.json
}

func parseVirtualUploadPath(p string) (virtualUploadPath, bool) {
	p = strings.TrimSpace(filepath.ToSlash(p))
	if p == "" || !strings.HasPrefix(p, uploadVFSRoot) {
		return virtualUploadPath{}, false
	}
	rest := strings.TrimPrefix(p, uploadVFSRoot)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return virtualUploadPath{}, true
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return virtualUploadPath{}, true
	}
	v := virtualUploadPath{FileID: parts[0]}
	if len(parts) > 1 {
		v.Name = parts[1]
	}
	return v, true
}

func virtualFilePath(fileID, name string) string {
	return path.Join(uploadVFSRoot, fileID, name)
}

func (b *uploadFSBackend) getResult(ctx context.Context, fileID string) (*fileparse.Result, *fileparse.Source, error) {
	if b == nil {
		return nil, nil, errors.New("upload backend 未初始化")
	}
	if fileID == "" {
		return nil, nil, errors.New("fileId is required")
	}
	meta, ok := LookupUploadedFile(fileID)
	if !ok {
		return nil, nil, fmt.Errorf("上传文件不存在或已过期（id=%s），请重新上传", fileID)
	}
	res, err := GetUploadedFile(ctx, fileID, b.userID)
	if err != nil {
		return nil, nil, err
	}
	return res, &fileparse.Source{FileName: meta.FileName, Ext: meta.Ext, Size: meta.Size}, nil
}

func (b *uploadFSBackend) virtualFiles(ctx context.Context, v virtualUploadPath) ([]string, error) {
	if v.FileID == "" {
		ids := uploadFileIDsFrom(ctx)
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			files, err := b.virtualFiles(ctx, virtualUploadPath{FileID: id})
			if err != nil {
				continue
			}
			out = append(out, files...)
		}
		return out, nil
	}
	res, _, err := b.getResult(ctx, v.FileID)
	if err != nil {
		return nil, err
	}
	if v.Name != "" {
		return []string{virtualFilePath(v.FileID, v.Name)}, nil
	}
	files := []string{virtualFilePath(v.FileID, "meta.json")}
	switch res.Kind {
	case fileparse.KindText, fileparse.KindDoc:
		files = append(files, virtualFilePath(v.FileID, "content.txt"))
	case fileparse.KindTable:
		if len(res.Sheets) > 0 {
			for i := range res.Sheets {
				files = append(files, virtualFilePath(v.FileID, fmt.Sprintf("sheet-%d.csv", i)))
			}
		} else {
			files = append(files, virtualFilePath(v.FileID, "sheet-0.csv"))
		}
	}
	return files, nil
}

// ──────────────────────────────────────────────
// filesystem.Backend 实现
// ──────────────────────────────────────────────

func (b *uploadFSBackend) LsInfo(ctx context.Context, req *filesystem.LsInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return nil, errors.New("path is required")
	}
	v, ok := parseVirtualUploadPath(req.Path)
	if !ok {
		if err := b.denySharedExportPath(req.Path); err != nil {
			return nil, err
		}
		return b.os.LsInfo(ctx, req)
	}
	files, err := b.virtualFiles(ctx, v)
	if err != nil {
		return nil, err
	}
	out := make([]filesystem.FileInfo, 0, len(files))
	for _, f := range files {
		out = append(out, filesystem.FileInfo{Path: f, IsDir: false})
	}
	return out, nil
}

func (b *uploadFSBackend) Read(ctx context.Context, req *filesystem.ReadRequest) (*filesystem.FileContent, error) {
	if req == nil || strings.TrimSpace(req.FilePath) == "" {
		return nil, errors.New("filePath is required")
	}
	v, ok := parseVirtualUploadPath(req.FilePath)
	if !ok {
		if err := b.denySharedExportPath(req.FilePath); err != nil {
			return nil, err
		}
		return b.os.Read(ctx, req)
	}
	if v.FileID == "" || v.Name == "" {
		return nil, errors.New("请指定虚拟文件，例如 /uploads/<fileId>/content.txt")
	}
	res, src, err := b.getResult(ctx, v.FileID)
	if err != nil {
		return nil, err
	}
	offset := req.Offset
	if offset <= 0 {
		offset = 1
	}
	limit := req.Limit
	if limit <= 0 {
		limit = fileparse.DefaultRowLimit
	}

	switch v.Name {
	case "meta.json":
		sheets := make([]map[string]any, 0, len(res.Sheets))
		for _, s := range res.Sheets {
			sheets = append(sheets, map[string]any{
				"name":      s.Name,
				"columns":   s.Columns,
				"totalRows": s.TotalRows,
				"notice":    s.Notice,
			})
		}
		content, marshalErr := json.MarshalIndent(map[string]any{
			"fileId":   v.FileID,
			"fileName": src.FileName,
			"ext":      src.Ext,
			"size":     src.Size,
			"kind":     res.Kind,
			"pages":    res.Pages,
			"chars":    res.TotalChars,
			"rows":     res.TotalRows,
			"sheets":   sheets,
		}, "", "  ")
		if marshalErr != nil {
			return nil, marshalErr
		}
		return &filesystem.FileContent{Content: string(content)}, nil
	case "content.txt":
		if res.Kind != fileparse.KindText && res.Kind != fileparse.KindDoc {
			return nil, errors.New("当前文件不是文本/文档类型，请读取 sheet-<n>.csv")
		}
		lines := strings.Split(res.Text, "\n")
		return &filesystem.FileContent{Content: joinLineWindow(lines, offset, limit)}, nil
	default:
		if strings.HasPrefix(v.Name, "sheet-") && strings.HasSuffix(v.Name, ".csv") {
			if res.Kind != fileparse.KindTable {
				return nil, errors.New("当前文件不是表格类型，请读取 content.txt")
			}
			index := 0
			if _, scanErr := fmt.Sscanf(v.Name, "sheet-%d.csv", &index); scanErr != nil {
				return nil, fmt.Errorf("无法解析工作表路径：%s", v.Name)
			}
			columns, rows := tableSheetData(res, index)
			return &filesystem.FileContent{Content: csvLineWindow(columns, rows, offset, limit)}, nil
		}
		return nil, fmt.Errorf("未知的虚拟文件：%s", v.Name)
	}
}

func (b *uploadFSBackend) GrepRaw(ctx context.Context, req *filesystem.GrepRequest) ([]filesystem.GrepMatch, error) {
	if req == nil || req.Pattern == "" {
		return nil, errors.New("pattern is required")
	}
	v, ok := parseVirtualUploadPath(req.Path)
	if strings.TrimSpace(req.Path) == "" {
		// 空 path 视为虚拟根目录，搜索当前会话全部附件。
		v, ok = virtualUploadPath{}, true
	}
	if !ok {
		if err := b.denySharedExportPath(req.Path); err != nil {
			return nil, err
		}
		return b.os.GrepRaw(ctx, req)
	}
	files, err := b.virtualFiles(ctx, v)
	if err != nil {
		return nil, err
	}
	matcher, err := grepMatcher(req.Pattern, req.CaseInsensitive)
	if err != nil {
		return nil, err
	}
	var matches []filesystem.GrepMatch
	for _, file := range files {
		if req.Glob != "" {
			if !matchGlob(req.Glob, file) && !matchGlob(req.Glob, path.Base(file)) {
				continue
			}
		}
		lines, readErr := b.readVirtualLines(ctx, file)
		if readErr != nil {
			continue
		}
		for i, line := range lines {
			if matcher.MatchString(line) {
				matches = append(matches, filesystem.GrepMatch{
					Path:    file,
					Line:    i + 1,
					Content: clampUTF8Prefix(line, 1_000),
				})
			}
		}
	}
	return matches, nil
}

func (b *uploadFSBackend) GlobInfo(ctx context.Context, req *filesystem.GlobInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil || req.Pattern == "" {
		return nil, errors.New("pattern is required")
	}
	base := strings.TrimSpace(req.Path)
	if base == "" {
		base = uploadVFSRoot
	}
	v, ok := parseVirtualUploadPath(base)
	if !ok {
		if err := b.denySharedExportPath(base); err != nil {
			return nil, err
		}
		return b.os.GlobInfo(ctx, req)
	}
	files, err := b.virtualFiles(ctx, v)
	if err != nil {
		return nil, err
	}
	pattern := req.Pattern
	if !strings.HasPrefix(pattern, "/") {
		pattern = path.Join(base, pattern)
	}
	out := make([]filesystem.FileInfo, 0, len(files))
	for _, f := range files {
		if matchGlob(pattern, f) || matchGlob(req.Pattern, path.Base(f)) {
			out = append(out, filesystem.FileInfo{Path: f, IsDir: false})
		}
	}
	sortFileInfos(out)
	return out, nil
}

func (b *uploadFSBackend) Write(ctx context.Context, req *filesystem.WriteRequest) error {
	if req != nil {
		if _, ok := parseVirtualUploadPath(req.FilePath); ok {
			return errors.New("上传文件虚拟路径为只读，禁止写入")
		}
	}
	if err := b.denySharedExportPath(req.FilePath); err != nil {
		return err
	}
	return b.os.Write(ctx, req)
}

func (b *uploadFSBackend) Edit(ctx context.Context, req *filesystem.EditRequest) error {
	if req != nil {
		if _, ok := parseVirtualUploadPath(req.FilePath); ok {
			return errors.New("上传文件虚拟路径为只读，禁止编辑")
		}
	}
	if err := b.denySharedExportPath(req.FilePath); err != nil {
		return err
	}
	return b.os.Edit(ctx, req)
}

// ──────────────────────────────────────────────
// 行窗口与 CSV 生成
// ──────────────────────────────────────────────

func joinLineWindow(lines []string, offset, limit int) string {
	if offset <= 0 {
		offset = 1
	}
	if offset > len(lines) {
		return ""
	}
	start := offset - 1
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}

func csvLineWindow(columns []string, rows [][]string, offset, limit int) string {
	totalLines := 1 + len(rows)
	if offset <= 0 {
		offset = 1
	}
	if offset > totalLines {
		return ""
	}
	start := offset - 1
	end := start + limit
	if end > totalLines {
		end = totalLines
	}
	var b strings.Builder
	for line := start; line < end; line++ {
		if line == 0 {
			b.WriteString(csvLine(columns))
			continue
		}
		b.WriteString(csvLine(rows[line-1]))
		if line < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func csvLine(fields []string) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.UseCRLF = false
	_ = w.Write(fields)
	w.Flush()
	return strings.TrimRight(buf.String(), "\r\n")
}

func tableSheetData(res *fileparse.Result, index int) ([]string, [][]string) {
	if res == nil {
		return nil, nil
	}
	if len(res.Sheets) > 0 && index >= 0 && index < len(res.Sheets) {
		return res.Sheets[index].Columns, res.Sheets[index].Rows
	}
	return res.Columns, res.Rows
}

func (b *uploadFSBackend) readVirtualLines(ctx context.Context, file string) ([]string, error) {
	v, ok := parseVirtualUploadPath(file)
	if !ok {
		return nil, errors.New("不是虚拟上传路径")
	}
	res, _, err := b.getResult(ctx, v.FileID)
	if err != nil {
		return nil, err
	}
	if v.Name == "content.txt" {
		return strings.Split(res.Text, "\n"), nil
	}
	if strings.HasPrefix(v.Name, "sheet-") && strings.HasSuffix(v.Name, ".csv") {
		index := 0
		if _, scanErr := fmt.Sscanf(v.Name, "sheet-%d.csv", &index); scanErr != nil {
			return nil, scanErr
		}
		columns, rows := tableSheetData(res, index)
		lines := make([]string, 0, 1+len(rows))
		lines = append(lines, csvLine(columns))
		for _, row := range rows {
			lines = append(lines, csvLine(row))
		}
		return lines, nil
	}
	return nil, fmt.Errorf("未知的虚拟文件：%s", v.Name)
}

func grepMatcher(pattern string, caseInsensitive bool) (*regexp.Regexp, error) {
	if caseInsensitive {
		pattern = "(?i)" + pattern
	}
	return regexp.Compile(pattern)
}

// matchGlob 支持 *、? 与 ** 的简单 glob 匹配。
// path.Match 不支持 ** 递归，这里转成正则。
func matchGlob(pattern, name string) bool {
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

func sortFileInfos(infos []filesystem.FileInfo) {
	// 小规模列表，直接插入排序，避免引入额外依赖。
	for i := 1; i < len(infos); i++ {
		for j := i; j > 0 && infos[j].Path < infos[j-1].Path; j-- {
			infos[j], infos[j-1] = infos[j-1], infos[j]
		}
	}
}
