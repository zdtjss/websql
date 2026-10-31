package export

// 导出产物元数据（对话式文件修复）
//
// 目的：让「修复本次会话已导出的文件」有据可依——修复不是重新生成整份文件，
// 而是回到原导出链路（原参数 / 报告源 Markdown / SQL / 图表定义 / Python 脚本），
// 做最小修改后局部重渲染。
//
// 存储位置：exports/.meta/<完整文件名>.json
//   - 仅服务端读写，不是下载路由可访问的普通导出文件；
//   - Agent 文件工具对共享 exports 根一律拒绝；技能产物发布路径也拒绝
//     .meta/.owners/.codegen 保留目录，避免通过 write_file/execute 篡改元数据；
//   - 与原导出文件同生命周期，由 CleanupOrphanArtifacts 回收孤儿记录。

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

const (
	// maxArtifactSourceChars 单次返回给模型的源内容默认上限（字符数）。
	// 改为分段回执：默认 8000，可通过 sourceLimit 提升到 16000。
	maxArtifactSourceChars = 8000
	// maxArtifactSourceLimit 单次回执的硬上限，防止模型用超大 limit 重新撑爆上下文。
	maxArtifactSourceLimit = 16000
	// maxArtifactPreviewChars 产物本体预览上限（字符数）。
	// 自包含 HTML 含内联 CSS/JS，通常超过 8k；16k 仍能覆盖典型报告的首屏结构。
	maxArtifactPreviewChars = 16000
)

var (
	artifactVersionSuffixRe   = regexp.MustCompile(`_v\d+$`)
	artifactSanitizedSuffixRe = regexp.MustCompile(`_\d{8}_\d{6}_[0-9a-f]{1,8}$`)
)

// ArtifactMeta 一次导出产物的来源与可复现参数。
//
// 仅记录当前会话产物：UserID + SessionID 是修复访问的硬约束。
type ArtifactMeta struct {
	Version  int    `json:"version"`
	URL      string `json:"url"`
	FileName string `json:"fileName"` // 含扩展名的最终文件名
	// BaseName 逻辑文件名主干（不含时间戳/随机后缀/版本号），用于跨版本稳定编号。
	BaseName string `json:"baseName,omitempty"`
	FileType string `json:"fileType"`
	Tool     string `json:"tool"`
	// ConnID 原导出使用的连接；修复必须在同一连接上下文执行，避免切换连接后套用错误权限/数据源。
	ConnID    string    `json:"connId,omitempty"`
	UserID    string    `json:"userId"`
	SessionID string    `json:"sessionId"`
	CreatedAt time.Time `json:"createdAt"`
	ParentURL string    `json:"parentUrl,omitempty"`

	// 内置报告导出参数
	Title    string       `json:"title,omitempty"`
	Subtitle string       `json:"subtitle,omitempty"`
	Theme    string       `json:"theme,omitempty"`
	Content  string       `json:"content,omitempty"`
	SQL      string       `json:"sql,omitempty"`
	Charts   []ExcelChart `json:"charts,omitempty"`
	Summary  string       `json:"summary,omitempty"`

	// export_by_code 原始脚本与产出路径
	Code       string `json:"code,omitempty"`
	OutputFile string `json:"outputFile,omitempty"`

	// 修复链信息
	Issue   string `json:"issue,omitempty"`
	Changes string `json:"changes,omitempty"`

	// ─── 产物 v2（对话式编辑）─────────────────────────────────
	//
	// ArtifactID 标识一次逻辑产物（版本链继承）；SourceKind 描述源快照类型：
	// report_markdown | workbook | code | skill。
	ArtifactID string `json:"artifactId,omitempty"`
	SourceKind string `json:"sourceKind,omitempty"`
	// Skill/SkillPayload 仅 export_skill 产物使用：payload 是重跑技能脚本的源快照。
	Skill        string `json:"skill,omitempty"`
	SkillPayload string `json:"skillPayload,omitempty"`
	// SourceHash 源快照哈希（内容/SQL/图表/代码/payload），用于无改动拒绝。
	SourceHash string `json:"sourceHash,omitempty"`
	// WorkbookEdited 表示 Excel 已含手工编辑；参数级重渲染必须拒绝，避免覆盖人工内容。
	WorkbookEdited bool `json:"workbookEdited,omitempty"`
	// Operations 本次生成/编辑实际应用的操作摘要（不含正文）。
	Operations []OperationRecord `json:"operations,omitempty"`
	// Validation 产物最后一次校验结果。
	Validation *ArtifactValidation `json:"validation,omitempty"`
	UpdatedAt  time.Time           `json:"updatedAt,omitempty"`
}

// OperationRecord 单次编辑操作摘要（审计/诊断用，不含正文）。
type OperationRecord struct {
	Op     string `json:"op"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ArtifactValidation 产物校验结果。
type ArtifactValidation struct {
	OK       bool     `json:"ok"`
	Checks   []string `json:"checks,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// artifactFilePath 返回元数据文件路径（fileName 必须是纯文件名）。
func artifactFilePath(fileName string) string {
	return filepath.Join(artifactsDir(), fileName+".json")
}

// safeArtifactFileName 校验元数据对应的文件名是纯文件名（允许 .json 等导出扩展名）。
func safeArtifactFileName(fileName string) bool {
	return safeExportBaseName(fileName)
}

// saveArtifactMeta 持久化元数据；失败只记录日志，不影响导出本身。
func saveArtifactMeta(meta *ArtifactMeta) error {
	if meta == nil || !safeArtifactFileName(meta.FileName) {
		return fmt.Errorf("导出元数据文件名非法")
	}
	if err := os.MkdirAll(artifactsDir(), 0o700); err != nil {
		return err
	}
	if meta.ArtifactID == "" {
		meta.ArtifactID = newArtifactID()
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = time.Now()
	}
	meta.SourceHash = artifactSourceHash(meta)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(artifactFilePath(meta.FileName), data, 0o600); err != nil {
		return err
	}
	invalidateArtifactMetaIndex()
	return nil
}

// newArtifactID 生成稳定的逻辑产物 ID（随机后缀，不参与定位）。
func newArtifactID() string {
	token, err := secureToken(6)
	if err != nil {
		return fmt.Sprintf("art_%d", time.Now().UnixNano())
	}
	return "art_" + token
}

// artifactSourceHash 计算源快照哈希：只覆盖可重放源，不覆盖渲染后的二进制。
func artifactSourceHash(meta *ArtifactMeta) string {
	if meta == nil {
		return ""
	}
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			_, _ = io.WriteString(h, p)
			_, _ = io.WriteString(h, "\x00")
		}
	}
	write(meta.Tool, meta.Title, meta.Subtitle, meta.Theme, meta.Content, meta.SQL, meta.Summary, meta.Code, meta.OutputFile, meta.Skill, meta.SkillPayload)
	write(meta.SourceKind, strconv.FormatBool(meta.WorkbookEdited))
	// 工作簿原地编辑不改变 Content/SQL，但会改变源快照语义：把操作日志纳入哈希，
	// 否则“无改动拒绝”会把两次不同的单元格编辑当作同一源。
	for _, op := range meta.Operations {
		write(op.Op, op.Kind, op.Target, op.Detail)
	}
	for _, c := range meta.Charts {
		write(c.ChartType, c.XAxisField, c.ChartTitle)
		for _, s := range c.Series {
			write(s.YAxisField, s.Name)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// loadArtifactMetaByFileName 按最终文件名读取元数据。
func loadArtifactMetaByFileName(fileName string) (*ArtifactMeta, error) {
	if !safeArtifactFileName(fileName) {
		return nil, fmt.Errorf("文件名非法")
	}
	data, err := os.ReadFile(artifactFilePath(fileName))
	if err != nil {
		return nil, err
	}
	var meta ArtifactMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("导出元数据损坏：%w", err)
	}
	if meta.FileName == "" {
		meta.FileName = fileName
	}
	return &meta, nil
}

// artifactMetaIndex 缓存 .meta 目录的解析结果，按目录 mtime 失效。
//
// 目的：resolveArtifact / nextArtifactVersion 不再每次读取全部元数据文件
// （长期运行后导出量增大时会成为明显开销）。
var artifactMetaIndex struct {
	mu      sync.RWMutex
	dir     string
	modTime time.Time
	loaded  bool
	items   map[string]*ArtifactMeta
}

func invalidateArtifactMetaIndex() {
	artifactMetaIndex.mu.Lock()
	artifactMetaIndex.loaded = false
	artifactMetaIndex.items = nil
	artifactMetaIndex.mu.Unlock()
}

func loadArtifactMetaIndex() map[string]*ArtifactMeta {
	dir := artifactsDir()
	info, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	artifactMetaIndex.mu.RLock()
	if artifactMetaIndex.loaded && artifactMetaIndex.dir == dir && info.ModTime().Equal(artifactMetaIndex.modTime) {
		items := artifactMetaIndex.items
		artifactMetaIndex.mu.RUnlock()
		return items
	}
	artifactMetaIndex.mu.RUnlock()

	artifactMetaIndex.mu.Lock()
	defer artifactMetaIndex.mu.Unlock()
	if artifactMetaIndex.loaded && artifactMetaIndex.dir == dir && info.ModTime().Equal(artifactMetaIndex.modTime) {
		return artifactMetaIndex.items
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		return nil
	}
	items := make(map[string]*ArtifactMeta, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		meta, loadErr := loadArtifactMetaByFileName(name)
		if loadErr != nil {
			continue
		}
		items[name] = meta
	}
	artifactMetaIndex.items = items
	artifactMetaIndex.dir = dir
	artifactMetaIndex.modTime = info.ModTime()
	artifactMetaIndex.loaded = true
	return items
}

// normalizeArtifactRef 把用户/模型给出的下载链接或文件名归一为纯文件名。
// 只做名字解析，不访问任何文件；路径成分会被丢弃，避免目录穿越。
func normalizeArtifactRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	// 兼容模型直接回传 Markdown 链接：[点击下载](/exports/x.html)
	if idx := strings.LastIndex(ref, "]("); idx >= 0 {
		rest := ref[idx+2:]
		if end := strings.Index(rest, ")"); end >= 0 {
			ref = rest[:end]
		}
	}
	ref = strings.Trim(ref, " \t\r\n`\"'")
	if u, err := url.PathUnescape(ref); err == nil {
		ref = u
	}
	if strings.Contains(ref, "://") {
		if u, err := url.Parse(ref); err == nil {
			ref = u.Path
		}
	}
	if idx := strings.Index(ref, "/exports/"); idx >= 0 {
		ref = ref[idx:]
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	ref = strings.TrimPrefix(ref, exportsURLPrefix)
	if strings.ContainsAny(ref, `/\`) {
		ref = filepath.Base(ref)
	}
	return strings.TrimSpace(ref)
}

// authorizeArtifact 校验元数据归属当前用户与当前会话，并确认原文件仍存在。
func authorizeArtifact(meta *ArtifactMeta, userID, sessionID string) (*ArtifactMeta, error) {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return nil, fmt.Errorf("无法确认当前用户/会话，已拒绝访问导出文件")
	}
	if meta.UserID != userID || meta.SessionID != sessionID {
		// 不区分“不存在”和“属于其他用户/会话”，避免文件名探测。
		return nil, fmt.Errorf("未找到当前会话中的该导出文件；请提供本次会话返回的下载链接或完整文件名")
	}
	if !safeArtifactFileName(meta.FileName) {
		return nil, fmt.Errorf("导出文件名非法")
	}
	if _, err := os.Stat(Path(meta.FileName)); err != nil {
		return nil, fmt.Errorf("原导出文件已过期或不存在，无法增量修复；请重新导出后再修复")
	}
	if allowed, known := CheckExportAccess(meta.FileName, userID); known && !allowed {
		return nil, fmt.Errorf("无权访问该导出文件")
	}
	return meta, nil
}

// resolveArtifact 在当前用户/会话范围内定位导出产物：
//  1. 完整文件名精确匹配；
//  2. 唯一包含匹配（用户只记得部分文件名时）；
//  3. 多个匹配时返回候选，要求用户确认，不猜测。
func resolveArtifact(ref, userID, sessionID string) (*ArtifactMeta, error) {
	fileName := normalizeArtifactRef(ref)
	if fileName == "" {
		return nil, fmt.Errorf("请提供要修复的导出文件名或下载链接")
	}
	if meta, err := loadArtifactMetaByFileName(fileName); err == nil {
		return authorizeArtifact(meta, userID, sessionID)
	}

	metas := loadArtifactMetaIndex()
	if metas == nil {
		return nil, fmt.Errorf("未找到该导出文件的修复源信息；请确认文件由本次会话导出，或重新导出后再修复")
	}
	var matches []*ArtifactMeta
	queryBase := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	for _, meta := range metas {
		if meta.UserID != strings.TrimSpace(userID) || meta.SessionID != strings.TrimSpace(sessionID) {
			continue
		}
		if strings.Contains(meta.FileName, fileName) || (queryBase != "" && strings.Contains(meta.FileName, queryBase)) {
			matches = append(matches, meta)
		}
	}
	switch len(matches) {
	case 0:
		// 与 authorizeArtifact 使用同一文案，避免通过不同提示探测文件是否存在。
		return nil, fmt.Errorf("未找到当前会话中的该导出文件；请提供本次会话返回的下载链接或完整文件名")
	case 1:
		return authorizeArtifact(matches[0], userID, sessionID)
	default:
		sort.Slice(matches, func(i, j int) bool { return matches[i].CreatedAt.After(matches[j].CreatedAt) })
		names := make([]string, 0, len(matches))
		for i, m := range matches {
			if i >= 5 {
				break
			}
			names = append(names, m.FileName)
		}
		return nil, fmt.Errorf("「%s」匹配到多个导出文件（%s），请指定完整文件名或下载链接", fileName, strings.Join(names, "、"))
	}
}

// artifactVersionName 为修复产物生成新版本文件名（保留原文件名主干）。
// 例：report_20261030_120000_ab12.html → report_20261030_120000_ab12_v2
func artifactVersionName(meta *ArtifactMeta) string {
	return artifactVersionNameFor(meta, nextArtifactVersion(meta))
}

// artifactVersionNameFor 按指定版本号生成文件名。
// base 预留出 _vN 与 SanitizeFileName 追加的时间戳/随机串空间，避免版本号被截断。
func artifactVersionNameFor(meta *ArtifactMeta, version int) string {
	base := meta.BaseName
	if base == "" {
		base = artifactBaseNameOf(meta.FileName)
	}
	base = artifactVersionSuffixRe.ReplaceAllString(base, "")
	runes := []rune(base)
	const maxBaseRunes = 46
	if len(runes) > maxBaseRunes {
		runes = runes[:maxBaseRunes]
	}
	if version < 2 {
		version = 2
	}
	return string(runes) + "_v" + strconv.Itoa(version)
}

// artifactBaseNameOf 从最终文件名还原逻辑主干：
// 去掉扩展名、SanitizeFileName 追加的 _时间戳_随机 以及末尾 _vN。
func artifactBaseNameOf(fileName string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	base = artifactSanitizedSuffixRe.ReplaceAllString(base, "")
	base = artifactVersionSuffixRe.ReplaceAllString(base, "")
	return strings.TrimSpace(base)
}

// nextArtifactVersion 返回下一次修复的版本号（原版本至少按 1 处理）。
// 按逻辑文件名主干扫描所有版本，避免同一父版本多次修复产生重复版本号。
func nextArtifactVersion(meta *ArtifactMeta) int {
	version := 1
	if meta != nil && meta.Version > version {
		version = meta.Version
	}
	next := version + 1
	if meta == nil {
		return next
	}
	baseKey := meta.BaseName
	if baseKey == "" {
		baseKey = artifactBaseNameOf(meta.FileName)
	}
	if baseKey == "" {
		return next
	}
	metas := loadArtifactMetaIndex()
	for _, m := range metas {
		if meta.UserID != "" && (m.UserID != meta.UserID || m.SessionID != meta.SessionID) {
			continue
		}
		mBase := m.BaseName
		if mBase == "" {
			mBase = artifactBaseNameOf(m.FileName)
		}
		if mBase == baseKey && m.Version >= next {
			next = m.Version + 1
		}
	}
	return next
}

// CleanupOrphanArtifacts 清理原导出文件已不存在的元数据记录。
func CleanupOrphanArtifacts() {
	entries, err := os.ReadDir(artifactsDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fileName := strings.TrimSuffix(entry.Name(), ".json")
		if !safeArtifactFileName(fileName) {
			_ = os.Remove(filepath.Join(artifactsDir(), entry.Name()))
			continue
		}
		if _, err := os.Stat(Path(fileName)); os.IsNotExist(err) {
			_ = os.Remove(filepath.Join(artifactsDir(), entry.Name()))
		}
	}
	invalidateArtifactMetaIndex()
}

// clipArtifactSource 截断过长的源内容，避免一次工具回执撑爆上下文。
func clipArtifactSource(s string) (string, bool) {
	page, _, truncated, _ := pageArtifactText(s, 0, maxArtifactSourceChars)
	return page, truncated
}

// pageArtifactText 按 rune 偏移分页源内容，返回当前页、总字符数、是否还有后续、下一页偏移。
func pageArtifactText(s string, offset, limit int) (page string, total int, truncated bool, nextOffset int) {
	runes := []rune(s)
	total = len(runes)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = maxArtifactSourceChars
	}
	if limit > maxArtifactSourceLimit {
		limit = maxArtifactSourceLimit
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return string(runes[offset:end]), total, end < total, end
}

// defaultArtifactSourceField 按产物类型选择默认回读的源字段。
func defaultArtifactSourceField(meta *ArtifactMeta) string {
	if meta == nil {
		return "content"
	}
	switch meta.Tool {
	case "export_excel", "export_excel_with_chart":
		return "sql"
	case "export_by_code", "repair_export_by_code":
		return "code"
	case "export_skill":
		return "skillPayload"
	}
	return "content"
}

// artifactTextPreview 读取文本类产物本体（仅当前会话已定位文件），用于诊断渲染问题。
// 非文本格式返回 ok=false（不读取二进制）。
func artifactTextPreview(fileName string) (text string, truncated bool, ok bool, err error) {
	if !isTextArtifact(fileName) {
		return "", false, false, nil
	}
	data, err := os.ReadFile(Path(fileName))
	if err != nil {
		return "", false, true, fmt.Errorf("读取产物预览失败：%w", err)
	}
	if !utf8.Valid(data) {
		return "", false, true, fmt.Errorf("产物不是合法 UTF-8 文本，无法提供预览")
	}
	text = string(data)
	runes := []rune(text)
	if len(runes) > maxArtifactPreviewChars {
		return string(runes[:maxArtifactPreviewChars]), true, true, nil
	}
	return text, false, true, nil
}

// isTextArtifact 判断扩展名是否可提供文本预览。
func isTextArtifact(fileName string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	switch ext {
	case "html", "htm", "md", "markdown", "txt", "csv", "json", "xml", "svg", "yaml", "yml", "log":
		return true
	}
	return false
}

// ─── 导出产物基本校验 ─────────────────────────────────────────

// validateExportArtifact 在返回下载链接前做基本完整性校验：
//   - HTML：可解析结构、脚本标签配对、关键渲染路径存在（有图表时需有内联 SVG）；
//   - OOXML：ZIP 可打开、必需部件存在且 XML 良构、图表部件存在（如有）；
//   - PDF/XML/ZIP/CSV/Markdown：头部/结构/编码基本校验。
//
// 该校验是「基本校验」而非完整渲染验证：用于拦截损坏、截断、编码错误与明显结构缺失。
func validateExportArtifact(ext string, data []byte, expectedCharts int) error {
	ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
	if len(data) == 0 {
		return fmt.Errorf("产物为空")
	}
	switch ext {
	case "html", "htm":
		return validateHTMLArtifact(data, expectedCharts)
	case "docx", "pptx", "xlsx":
		return validateOOXMLArtifact(ext, data, expectedCharts)
	case "pdf":
		return validatePDFArtifact(data)
	case "xml":
		if !utf8.Valid(data) {
			return fmt.Errorf("XML 不是合法 UTF-8 编码")
		}
		return validateXMLBytes(data)
	case "json":
		if !json.Valid(data) {
			return fmt.Errorf("JSON 结构不合法")
		}
		return nil
	case "zip":
		if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
			return fmt.Errorf("ZIP 结构损坏：%w", err)
		}
		return nil
	case "csv":
		if !utf8.Valid(data) {
			return fmt.Errorf("CSV 不是合法 UTF-8 编码（可能存在乱码）")
		}
		if strings.TrimSpace(string(data)) == "" {
			return fmt.Errorf("CSV 内容为空")
		}
		return nil
	case "md", "markdown", "txt":
		if !utf8.Valid(data) {
			return fmt.Errorf("文本不是合法 UTF-8 编码（可能存在乱码）")
		}
		if strings.TrimSpace(string(data)) == "" {
			return fmt.Errorf("文本内容为空")
		}
		return nil
	default:
		if !utf8.Valid(data) && !isBinaryLike(data) {
			return fmt.Errorf("产物编码异常")
		}
		return nil
	}
}

func validateHTMLArtifact(data []byte, expectedCharts int) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("HTML 不是合法 UTF-8 编码（可能存在乱码）")
	}
	z := xhtml.NewTokenizer(bytes.NewReader(data))
	scriptDepth := 0
	sawHTML := false
	sawBodyClose := false
	sawSVG := false
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			if err := z.Err(); err != nil && err != io.EOF {
				return fmt.Errorf("HTML 解析失败：%w", err)
			}
			break
		}
		switch tt {
		case xhtml.StartTagToken:
			name, _ := z.TagName()
			switch strings.ToLower(string(name)) {
			case "html":
				sawHTML = true
			case "script":
				scriptDepth++
			case "svg":
				sawSVG = true
			}
		case xhtml.SelfClosingTagToken:
			name, _ := z.TagName()
			switch strings.ToLower(string(name)) {
			case "html":
				sawHTML = true
			case "svg":
				sawSVG = true
			}
		case xhtml.EndTagToken:
			name, _ := z.TagName()
			switch strings.ToLower(string(name)) {
			case "script":
				if scriptDepth > 0 {
					scriptDepth--
				}
			case "body":
				sawBodyClose = true
			}
		}
	}
	if !sawHTML {
		return fmt.Errorf("HTML 结构不完整（缺少 <html>）")
	}
	if !sawBodyClose {
		return fmt.Errorf("HTML 结构不完整（缺少 </body>）")
	}
	if scriptDepth != 0 {
		return fmt.Errorf("HTML 脚本标签未配对，可能导致 JS 语法错误")
	}
	if expectedCharts > 0 && !sawSVG {
		return fmt.Errorf("HTML 图表渲染路径缺失（预期 %d 个图表，未发现内联 SVG）", expectedCharts)
	}
	return nil
}

func validateOOXMLArtifact(ext string, data []byte, expectedCharts int) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("%s 文件结构损坏（ZIP 无法打开）：%w", strings.ToUpper(ext), err)
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[f.Name] = f
	}
	required := map[string][]string{
		"docx": {"[Content_Types].xml", "word/document.xml"},
		"pptx": {"[Content_Types].xml", "ppt/presentation.xml"},
		"xlsx": {"[Content_Types].xml", "xl/workbook.xml"},
	}
	for _, name := range required[ext] {
		if files[name] == nil {
			return fmt.Errorf("%s 缺少必需部件 %s，文件可能已损坏", strings.ToUpper(ext), name)
		}
	}
	// 所有 XML 部件都要求良构：Excel/PPT 的损坏通常出现在 worksheet/slide 子部件，
	// 不能只校验主文档与图表部件。
	chartParts := 0
	for name, f := range files {
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".xml") {
			continue
		}
		if strings.Contains(lower, "/charts/chart") {
			chartParts++
		}
		if err := validateZipXMLPart(f); err != nil {
			return fmt.Errorf("%s 部件 %s 结构异常：%w", strings.ToUpper(ext), name, err)
		}
	}
	if ext == "pptx" {
		slides := 0
		for name := range files {
			rest, ok := strings.CutPrefix(name, "ppt/slides/")
			if ok && strings.HasPrefix(rest, "slide") && strings.HasSuffix(rest, ".xml") && !strings.Contains(rest, "/") {
				slides++
			}
		}
		if slides == 0 {
			return fmt.Errorf("PPTX 中没有幻灯片页面")
		}
	}
	if ext == "xlsx" {
		sheets := 0
		for name := range files {
			rest, ok := strings.CutPrefix(name, "xl/worksheets/")
			if ok && strings.HasPrefix(rest, "sheet") && strings.HasSuffix(rest, ".xml") {
				sheets++
			}
		}
		if sheets == 0 {
			return fmt.Errorf("XLSX 中没有工作表")
		}
	}
	if expectedCharts > 0 && chartParts == 0 {
		return fmt.Errorf("%s 缺少图表部件（预期 %d 个图表），图表结构不完整", strings.ToUpper(ext), expectedCharts)
	}
	return nil
}

func validateZipXMLPart(f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	dec.Strict = true
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func validateXMLBytes(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("XML 结构异常：%w", err)
		}
	}
}

func validatePDFArtifact(data []byte) error {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return fmt.Errorf("PDF 文件头缺失，文件可能已损坏")
	}
	tail := data
	if len(tail) > 2048 {
		tail = tail[len(tail)-2048:]
	}
	if !bytes.Contains(tail, []byte("%%EOF")) {
		return fmt.Errorf("PDF 文件尾标记缺失，文件可能被截断")
	}
	return nil
}

func isBinaryLike(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
