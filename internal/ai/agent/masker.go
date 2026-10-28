package agent

// 数据脱敏：对「已授权」的值与文本执行确定性脱敏。
//
// 设计约束：
//   - 脱敏不能替代权限过滤：未授权列只能删除或拒绝，不允许用打码代替授权；
//   - 规则必须幂等、可测试，且只使用 Go RE2 固定规则（禁止把管理员输入编译为正则，避免 ReDoS）；
//   - tokenize 使用带密钥的 HMAC，不能直接 SHA-256 低熵个人信息。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	admin "websql/internal/app/admin"

	"golang.org/x/text/unicode/norm"
)

// MaskAction 脱敏动作。
type MaskAction string

const (
	// MaskPartial 保留首尾字符的部分脱敏
	MaskPartial MaskAction = "partial"
	// MaskTokenize 以带密钥的 HMAC 令牌替换原值（同值同令牌，可关联不可逆）
	MaskTokenize MaskAction = "tokenize"
	// MaskRedact 完全替换为 ***
	MaskRedact MaskAction = "redact"
)

// MaskRule 一条列级脱敏规则。
type MaskRule struct {
	ConnID string
	Schema string
	Table  string
	Column string
	Action MaskAction
}

// DataMasker 不可变的脱敏器：规则表与密钥在构建后不再修改，可安全并发使用。
type DataMasker struct {
	rules   map[string]MaskAction // key: conn|schema.table|column
	key     []byte
	version string
}

// maskRuleKey 规则匹配键。列名统一小写：数据库列名大小写差异不应绕过脱敏。
func maskRuleKey(connID, schema, table, column string) string {
	return connID + "|" + scopeKey(schema, table) + "|" + strings.ToLower(trimIdent(column))
}

// NewDataMasker 构建脱敏器。
//
// hmacKey 仅在存在 tokenize 规则时必需；缺失或长度不足时整份规则拒绝加载
// （fail-closed，不降级为无密钥哈希）。
func NewDataMasker(rules []MaskRule, hmacKey []byte, version string) (*DataMasker, error) {
	m := &DataMasker{
		rules:   make(map[string]MaskAction, len(rules)),
		version: version,
	}
	needKey := false
	for _, r := range rules {
		if strings.TrimSpace(r.ConnID) == "" || strings.TrimSpace(r.Table) == "" || strings.TrimSpace(r.Column) == "" {
			return nil, errors.New("脱敏规则缺少连接、表或列")
		}
		switch r.Action {
		case MaskPartial, MaskTokenize, MaskRedact:
		case "":
			continue // 空动作视为未配置
		default:
			return nil, fmt.Errorf("未知脱敏动作 %q", r.Action)
		}
		if r.Action == MaskTokenize {
			needKey = true
		}
		m.rules[maskRuleKey(r.ConnID, r.Schema, r.Table, r.Column)] = r.Action
	}
	if needKey {
		if len(hmacKey) < 32 {
			return nil, errors.New("脱敏 HMAC 密钥长度不足")
		}
		m.key = append([]byte(nil), hmacKey...)
	}
	return m, nil
}

// NewNoopMasker 返回只执行内置文本保护的脱敏器（无列级规则）。
func NewNoopMasker() *DataMasker {
	return &DataMasker{rules: map[string]MaskAction{}}
}

// Version 返回规则版本（用于消息安全元数据）。
func (m *DataMasker) Version() string {
	if m == nil {
		return ""
	}
	return m.version
}

// HasRules 判断是否存在列级规则。
func (m *DataMasker) HasRules() bool {
	return m != nil && len(m.rules) > 0
}

// ActionFor 返回指定列配置的动作；未配置返回空串。
func (m *DataMasker) ActionFor(connID, schema, table, column string) MaskAction {
	if m == nil {
		return ""
	}
	return m.rules[maskRuleKey(connID, schema, table, column)]
}

var (
	secretDoubleRE = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|password|secret)\b(\s*["']?\s*[:=]\s*)("(?:\\.|[^"\\])*")`)
	secretSingleRE = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|password|secret)\b(\s*["']?\s*[:=]\s*)('(?:\\.|[^'\\])*')`)
	secretRE       = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|password|secret)\b(\s*["']?\s*[:=]\s*["']?)([^\s,;"']+)`)
	idCardRE       = regexp.MustCompile(`\b[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[0-9Xx]\b`)
	phoneRE        = regexp.MustCompile(`\b1[3-9]\d{9}\b`)
	emailRE        = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
)

// normalizeMaskText 统一全角/兼容字符并移除零宽/格式字符，避免绕过检测。
//
// 性能：NFKC 对纯 ASCII/中日韩统一表意文字是空操作，先做廉价范围扫描再规范化，
// 未命中时直接返回原串（不分配）。
func normalizeMaskText(text string) string {
	if needsCompatibilityNormalization(text) {
		text = norm.NFKC.String(text)
	}
	if !containsFormatChar(text) {
		return text
	}
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
}

func containsFormatChar(text string) bool {
	for _, r := range text {
		if r >= 0xE0000 { // 标签字符等 Cf 主要分布在高位平面
			if unicode.Is(unicode.Cf, r) {
				return true
			}
			continue
		}
		if r >= 0x200B && r <= 0x206F && unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

// needsCompatibilityNormalization 判断文本是否含有 NFKC 可能改写的字符。
//
// 覆盖：Latin-1 补充、间距修饰符、CJK 兼容/标点/全角形式与连字。
// CJK 统一表意文字（4E00-9FFF）不在范围内，因此普通中文可走快速路径。
func needsCompatibilityNormalization(text string) bool {
	for _, r := range text {
		switch {
		case r < 0x80:
			continue
		case r >= 0x00A0 && r <= 0x2FFF:
			return true
		case r >= 0x3000 && r <= 0x303F:
			return true
		case r >= 0xFB00 && r <= 0xFB4F:
			return true
		case r >= 0xFF00 && r <= 0xFFEF:
			return true
		}
	}
	return false
}

// sensitiveAnchors 内置规则的关键词锚点：不命中任一锚点即不可能命中规则。
var sensitiveAnchors = []string{
	"password", "passwd", "pwd", "secret", "token", "api_key", "apikey", "access_token",
}

// sensitiveFlags 文本中与内置规则相关的候选特征。
type sensitiveFlags struct {
	digit   bool
	at      bool
	assign  bool
	encoded bool
	anchor  bool
}

func scanSensitive(text string) sensitiveFlags {
	var f sensitiveFlags
	for _, r := range text {
		switch {
		case r >= '0' && r <= '9':
			f.digit = true
		case r == '@':
			f.at = true
		case r == '=' || r == ':':
			f.assign = true
		case r == '+' || r == '/' || r == '%':
			f.encoded = true
		}
	}
	if f.assign {
		lower := strings.ToLower(text)
		for _, anchor := range sensitiveAnchors {
			if strings.Contains(lower, anchor) {
				f.anchor = true
				break
			}
		}
	}
	return f
}

// mayContainSensitive 快速判定文本是否可能命中内置敏感规则。
//
// 注意：这里只做「是否存在候选特征」的剪枝，不会放宽任何规则；
// 命中后仍由完整正则集处理。
func mayContainSensitive(text string) bool {
	f := scanSensitive(text)
	if f.at || f.digit {
		return true
	}
	if f.encoded && len(text) >= 16 {
		return true
	}
	return f.assign && f.anchor
}

// containsPlainSensitive 判断文本是否包含内置敏感信息规则。
func containsPlainSensitive(text string) bool {
	return secretDoubleRE.MatchString(text) || secretSingleRE.MatchString(text) ||
		secretRE.MatchString(text) || idCardRE.MatchString(text) ||
		phoneRE.MatchString(text) || emailRE.MatchString(text)
}

// maskEncodedToken 对单个 token 做「单次、有界」的 URL/Base64 解码检测。
//
// 只有解码结果命中敏感规则时才替换整个原始 token：非敏感链接与普通文本保持原样，
// 且不输出编码原文或解码后的敏感值。
func maskEncodedToken(token string) string {
	candidate := strings.Trim(token, "`\"'()[]{}<>，。！？；：、")
	if len(candidate) < 8 || len(candidate) > 2048 {
		return token
	}
	decoded := ""
	if strings.Contains(candidate, "%") {
		if value, err := url.PathUnescape(candidate); err == nil && len(value) <= 1024 {
			decoded = value
		}
	} else {
		encodings := []*base64.Encoding{
			base64.StdEncoding, base64.RawStdEncoding,
			base64.URLEncoding, base64.RawURLEncoding,
		}
		for _, encoding := range encodings {
			value, err := encoding.DecodeString(candidate)
			if err == nil && len(value) <= 1024 && utf8.Valid(value) {
				decoded = string(value)
				break
			}
		}
	}
	if decoded != "" && containsPlainSensitive(normalizeMaskText(decoded)) {
		return strings.Replace(token, candidate, "***", 1)
	}
	return token
}

// maskEncodedTokens 按空白切分 token 并逐个做有界解码检测。
func maskEncodedTokens(text string) string {
	out := make([]rune, 0, len([]rune(text)))
	token := make([]rune, 0, 64)
	flush := func() {
		if len(token) > 0 {
			out = append(out, []rune(maskEncodedToken(string(token)))...)
			token = token[:0]
		}
	}
	for _, r := range text {
		if unicode.IsSpace(r) {
			flush()
			out = append(out, r)
			continue
		}
		token = append(token, r)
	}
	flush()
	return string(out)
}

// MaskText 对普通文本执行内置敏感信息脱敏。
//
// 用于 assistant/thinking/工具文本/导出正文；列级规则由 MaskQueryResult 处理。
func (m *DataMasker) MaskText(text string) string {
	if text == "" {
		return text
	}
	normalized := normalizeMaskText(text)
	masked := m.applyTextRules(normalized)
	// 仅规范化带来差异（全角标点、零宽字符）且未命中任何敏感规则时，保留原文：
	// 不能因为检测需要而改变用户可见的正常中文标点。
	if masked == normalized && normalized != text {
		masked = m.applyTextRules(text)
		if masked == text {
			return text
		}
	}
	return masked
}

// applyTextRules 在给定文本上执行内置敏感信息规则。
func (m *DataMasker) applyTextRules(text string) string {
	if text == "" {
		return text
	}
	// 按候选特征逐类执行：免去无关规则的正则开销（不缩小检测范围）。
	f := scanSensitive(text)
	if !f.digit && !f.at && !(f.assign && f.anchor) && !(f.encoded && len(text) >= 16) {
		return text
	}
	if (f.encoded || f.assign) && len(text) >= 16 {
		text = maskEncodedTokens(text)
	}
	if f.assign && f.anchor {
		// 引号值支持空白与转义引号，必须整体替换，不能产生 "*** world"。
		text = secretDoubleRE.ReplaceAllString(text, `${1}${2}"***"`)
		text = secretSingleRE.ReplaceAllString(text, `${1}${2}'***'`)
		text = secretRE.ReplaceAllString(text, `${1}${2}***`)
	}
	if f.digit {
		text = idCardRE.ReplaceAllStringFunc(text, func(v string) string {
			return v[:6] + "********" + v[14:]
		})
		text = phoneRE.ReplaceAllStringFunc(text, func(v string) string {
			return v[:3] + "****" + v[7:]
		})
	}
	if f.at {
		text = emailRE.ReplaceAllStringFunc(text, func(v string) string {
			at := strings.LastIndexByte(v, '@')
			if at <= 0 {
				return "***"
			}
			return v[:1] + "***" + v[at:]
		})
	}
	return text
}

// maskWithAction 按动作处理单个值。nil 保持 nil（避免把 NULL 变成字符串）。
func (m *DataMasker) maskWithAction(action MaskAction, value any) any {
	if value == nil {
		return nil
	}
	switch action {
	case MaskRedact:
		return "***"
	case MaskTokenize:
		raw := fmt.Sprint(value)
		mac := hmac.New(sha256.New, m.key)
		_, _ = mac.Write([]byte(raw))
		return "tok_" + hex.EncodeToString(mac.Sum(nil))[:16]
	case MaskPartial:
		raw := fmt.Sprint(value)
		if masked := m.MaskText(raw); masked != raw {
			return masked
		}
		runes := []rune(raw)
		if len(runes) <= 2 {
			return "***"
		}
		return string(runes[:1]) + "***" + string(runes[len(runes)-1:])
	default:
		return value
	}
}

// MaskQueryResult 先做权限过滤，再按「输出列 → 原始 ResourceRef」脱敏。
//
// origins 必须由方言解析结果生成（见 resolveOutputColumnOrigins），
// 否则 `SELECT password AS x` 会因别名而绕过列规则。
// 存在生效规则但输出列来源不明时按 redact 处理（最严格）。
func (m *DataMasker) MaskQueryResult(
	payload string,
	tables []string,
	origins map[string]ResourceRef,
	scope *PermissionScope,
) (string, error) {
	var out QueryOutput
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return "", &PermissionError{Message: "脱敏前结果格式校验失败"}
	}
	m.MaskQueryOutput(&out, tables, origins, scope)
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// MaskQueryOutput 就地完成「权限过滤 + 列级脱敏」，供调用方复用单次 JSON 解析。
func (m *DataMasker) MaskQueryOutput(
	out *QueryOutput,
	tables []string,
	origins map[string]ResourceRef,
	scope *PermissionScope,
) {
	if out == nil {
		return
	}
	if scope != nil {
		out.Columns, out.Data = scope.FilterResultColumns(out.Columns, out.Data, tables)
		out.Count = len(out.Data)
	}

	for _, col := range out.Columns {
		action := MaskAction("")
		origin, knownOrigin := origins[col]
		if knownOrigin {
			// origins 的 Column 才是规则匹配键，不能使用 SELECT 别名 col。
			action = m.ActionFor(origin.ConnID, origin.Schema, origin.Table, origin.Column)
		} else if m.HasRules() {
			action = MaskRedact // 有生效规则但来源不明，默认最严格处理
		}

		for _, row := range out.Data {
			value, ok := row[col]
			if !ok {
				continue
			}
			if action != "" {
				row[col] = m.maskWithAction(action, value)
			} else if text, ok := value.(string); ok {
				row[col] = m.MaskText(text) // 内置文本保护，不改变数值类型
			}
		}
	}
}

// MaskTable 对二维字符串表格按列脱敏（导出链路使用）。
//
// 与 MaskQueryResult 使用同一规则与「来源不明按 redact」策略：
// 导出结果会直接落盘，没有结果过滤兜底，必须与查询结果同等对待。
func (m *DataMasker) MaskTable(columns []string, rows [][]string, origins map[string]ResourceRef) [][]string {
	if m == nil || len(columns) == 0 || len(rows) == 0 {
		return rows
	}
	actions := make([]MaskAction, len(columns))
	for i, col := range columns {
		if origin, ok := origins[col]; ok {
			actions[i] = m.ActionFor(origin.ConnID, origin.Schema, origin.Table, origin.Column)
		} else if m.HasRules() && !m.columnNameMatchesAnyRule(col) {
			actions[i] = MaskRedact
		}
	}
	out := make([][]string, len(rows))
	for r, row := range rows {
		cp := make([]string, len(row))
		copy(cp, row)
		for c := range cp {
			if c >= len(actions) || actions[c] == "" {
				cp[c] = m.MaskText(cp[c])
				continue
			}
			cp[c] = fmt.Sprint(m.maskWithAction(actions[c], cp[c]))
		}
		out[r] = cp
	}
	return out
}

// columnNameMatchesAnyRule 判断列名是否命中任一条规则的列名部分。
//
// 仅在来源无法解析时用于避免误将「已配置规则的列」当作普通文本（仍按规则脱敏）。
func (m *DataMasker) columnNameMatchesAnyRule(column string) bool {
	col := strings.ToLower(trimIdent(column))
	for key := range m.rules {
		if strings.HasSuffix(key, "|"+col) {
			return true
		}
	}
	return false
}

// ──────────────────────────────────────────────
// 脱敏规则提供者：最后一份已验证规则 + 周期刷新
// ──────────────────────────────────────────────

const maskerRefreshInterval = 5 * time.Second

// MaskerProvider 维护「最后一次成功加载」的脱敏规则快照。
//
// 规则加载失败时保留旧规则（不部分加载）；从未成功加载时 Available()==false，
// 远程摸式必须禁止数据结果出域（由调用方判定）。
type MaskerProvider struct {
	mu        sync.Mutex
	current   atomic.Pointer[DataMasker]
	loadedAt  atomic.Int64
	lastError atomic.Value // string
}

var globalMaskerProvider = &MaskerProvider{}

func init() {
	// 规则变更后立即刷新本地快照：脱敏规则失效窗口直接影响数据出域方式，
	// 不能等待 Get() 的周期刷新（默认 5s）。
	admin.OnMaskRulesChanged(func() {
		if err := GetMaskerProvider().Refresh(); err != nil {
			log.Printf("[Masker] 规则变更后刷新失败（保留上一已验证快照） - err=%v", err)
		}
	})
}

// GetMaskerProvider 返回全局脱敏规则提供者。
func GetMaskerProvider() *MaskerProvider {
	return globalMaskerProvider
}

// Available 判断是否存在已验证规则快照。无列级规则也是有效快照（只做内置文本保护）。
func (p *MaskerProvider) Available() bool {
	return p.current.Load() != nil
}

// Current 返回当前脱敏器；未成功加载时返回 nil。
func (p *MaskerProvider) Current() *DataMasker {
	return p.current.Load()
}

// LastError 返回最近一次加载失败原因（不含规则内容）。
func (p *MaskerProvider) LastError() string {
	if v, ok := p.lastError.Load().(string); ok {
		return v
	}
	return ""
}

// Refresh 从管理库重新加载规则。失败时保留旧快照并返回错误。
func (p *MaskerProvider) Refresh() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	records, err := admin.ListMaskRules()
	if err != nil {
		p.lastError.Store(err.Error())
		return err
	}
	rules := make([]MaskRule, 0, len(records))
	needKey := false
	for _, r := range records {
		action := MaskAction(strings.ToLower(strings.TrimSpace(r.Action)))
		if !admin.ValidMaskAction(string(action)) {
			err := fmt.Errorf("脱敏规则动作非法: %s", r.Action)
			p.lastError.Store(err.Error())
			return err
		}
		if action == MaskTokenize {
			needKey = true
		}
		rules = append(rules, MaskRule{
			ConnID: r.ConnID,
			Schema: r.SchemaName,
			Table:  r.TableName,
			Column: r.ColumnName,
			Action: action,
		})
	}

	var key []byte
	if needKey {
		key, _, err = admin.EnsureMaskKey()
		if err != nil {
			p.lastError.Store(err.Error())
			return err
		}
	}
	version, err := admin.MaskRulesDigest()
	if err != nil {
		p.lastError.Store(err.Error())
		return err
	}
	masker, err := NewDataMasker(rules, key, version)
	if err != nil {
		// 整份规则拒绝，保留上一已验证版本
		p.lastError.Store(err.Error())
		return err
	}
	p.current.Store(masker)
	p.loadedAt.Store(time.Now().UnixNano())
	p.lastError.Store("")
	return nil
}

// Get 返回可用脱敏器：快照过期时尝试刷新，刷新失败则沿用旧快照。
func (p *MaskerProvider) Get() *DataMasker {
	if m := p.current.Load(); m != nil && time.Since(time.Unix(0, p.loadedAt.Load())) < maskerRefreshInterval {
		return m
	}
	if err := p.Refresh(); err != nil {
		if m := p.current.Load(); m != nil {
			return m
		}
		return nil
	}
	return p.current.Load()
}
