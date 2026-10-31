package agent

// 流式输出审计器：解决敏感信息跨 chunk 匹配问题。
//
// 语义：
//   - 只有通过审计的前缀才能推送给前端；
//   - holdback 只保留有限字符（默认 320 rune），不缓冲整段回复；
//   - 未完成的敏感赋值（如 `password=` 后值尚未到达）必须整体留在窗口内；
//   - 窗口溢出时 fail-closed，绝不能为了继续输出而释放未审计前缀。

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrOutputSecurity 输出审计失败（窗口溢出、编码非法、重复 Close 等）。
var ErrOutputSecurity = errors.New("output security check failed")

const (
	// maxOutputTokenRunes 超长 token 的硬上限。
	// 仅对带敏感前缀的 token 整段替换，避免误伤长 URL/长标识符。
	maxOutputTokenRunes = 1024
	// maxOutputPendingRunes 待审窗口硬上限：超过仍无法闭合的敏感赋值按 fail-closed 处理
	maxOutputPendingRunes = 4096
	// minOutputHoldback 常规回退窗口：只需覆盖跨 chunk 的短敏感串（手机号/邮箱/密钥前缀）。
	// 敏感键赋值（password=...）由 secretPrefixRE/secretOpen* 扩展到匹配起点，
	// 因此不需要固定保留 320 rune，否则会显著抬高首 token 延迟。
	minOutputHoldback = 64
)

// sensitiveTokenPrefixes 需要整段丢弃的超长 token 前缀（JWT/云厂商密钥等）。
var sensitiveTokenPrefixes = []string{"eyJ", "AKIA", "ASIA", "sk-", "sk_", "ghp_", "gho_", "xoxb-", "xoxp-", "AIza"}

func hasSensitiveTokenPrefix(token string) bool {
	for _, prefix := range sensitiveTokenPrefixes {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

// shouldRedactOverlongToken 判断超长 token 是否需要整段替换。
//
// 只对「疑似凭据前缀」（JWT/AKIA/sk-/ghp_/ Slack token/Google key）整段丢弃；
// 不能把所有无空白长串（JSON 结果、长 URL、无空格标识符列表）一律抹除，
// 否则 query_data 等工具结果会被整体替成 ***，模型永远看不到数据。
func shouldRedactOverlongToken(token string) bool {
	return hasSensitiveTokenPrefix(token) || hasSensitiveTokenPrefix(strings.ToLower(token))
}

// 匹配位于窗口末尾的敏感键，以及「键 + 引号/分隔符/空白」的未完成赋值。
// 键与 = 分属不同 chunk 时也不能提前出窗；窗口溢出则 fail-closed。
var (
	secretPrefixRE     = regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|password|secret)\b\s*["']?\s*(?::|=)?\s*["']?\s*$`)
	secretOpenDoubleRE = regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|password|secret)\b\s*["']?\s*[:=]\s*"(?:\\.|[^"\\])*$`)
	secretOpenSingleRE = regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|password|secret)\b\s*["']?\s*[:=]\s*'(?:\\.|[^'\\])*$`)
)

// OutputGuard 有界状态的增量输出审计器。
type OutputGuard struct {
	masker   *DataMasker
	pending  []rune // 始终是「整个待审窗口脱敏后」的未推送后缀
	holdback int
	closed   bool
}

// NewOutputGuard 创建审计器。maxPatternRunes 小于 320 时按 320 处理。
func NewOutputGuard(masker *DataMasker, maxPatternRunes int) *OutputGuard {
	if masker == nil {
		masker = NewNoopMasker()
	}
	if maxPatternRunes < minOutputHoldback {
		maxPatternRunes = minOutputHoldback
	}
	return &OutputGuard{masker: masker, holdback: maxPatternRunes}
}

// redactOverlongTokens 把超过上限的不可拆分 token 整段替换为 ***。
//
// 只按空白和中文句读切分；@ . _ = 等仍计入 token 长度，
// 防止长邮箱/编码载荷被标点拆成多个短片段绕过上限。
func redactOverlongTokens(text string, maxRunes int) string {
	out := make([]rune, 0, len([]rune(text)))
	token := make([]rune, 0, 64)
	flush := func() {
		// 普通长 token（长 URL、长标识符、中文长句）保留；
		// 只有疑似凭据或不可拆分载荷的超长 token 才整段替换，避免把正文误删成 ***。
		if len(token) > maxRunes && shouldRedactOverlongToken(string(token)) {
			out = append(out, '*', '*', '*')
		} else {
			out = append(out, token...)
		}
		token = token[:0]
	}
	for _, r := range text {
		if unicode.IsSpace(r) || strings.ContainsRune("，。！？；：、（）【】《》", r) {
			flush()
			out = append(out, r)
			continue
		}
		token = append(token, r)
	}
	flush()
	return string(out)
}

func (g *OutputGuard) drain(final bool) (string, error) {
	// 必须先对完整 pending 匹配，再切分输出；否则跨 emit 边界的敏感串会漏检。
	masked := g.masker.MaskText(string(g.pending))
	maskedRunes := []rune(redactOverlongTokens(masked, maxOutputTokenRunes))
	if final {
		g.pending = nil
		return string(maskedRunes), nil
	}

	emitLen := 0
	if len(maskedRunes) > g.holdback {
		emitLen = len(maskedRunes) - g.holdback
	}
	// 敏感键可能后跟任意空白。值未到达前，键和分隔空白必须整体留在窗口内，
	// 否则先释放 password=，后续 hunter2 将失去键上下文而泄露。
	maskedText := string(maskedRunes) // 定位与切分必须使用同一坐标系
	for _, re := range []*regexp.Regexp{secretPrefixRE, secretOpenDoubleRE, secretOpenSingleRE} {
		if loc := re.FindStringIndex(maskedText); loc != nil {
			start := len([]rune(maskedText[:loc[0]]))
			if emitLen > start {
				emitLen = start
			}
		}
	}
	if emitLen <= 0 {
		g.pending = maskedRunes
		if len(g.pending) > maxOutputPendingRunes {
			return "", ErrOutputSecurity // 有界状态溢出，拒绝而不是释放原始前缀
		}
		return "", nil
	}

	safe := string(maskedRunes[:emitLen])
	g.pending = append(g.pending[:0], maskedRunes[emitLen:]...)
	if len(g.pending) > maxOutputPendingRunes {
		return "", ErrOutputSecurity
	}
	return safe, nil
}

// Push 追加一个 chunk，返回可以安全推送的前缀（可能为空）。
func (g *OutputGuard) Push(chunk string) (string, error) {
	if g.closed || !utf8.ValidString(chunk) {
		return "", ErrOutputSecurity
	}
	if chunk == "" {
		return "", nil
	}
	g.pending = append(g.pending, []rune(chunk)...)
	return g.drain(false)
}

// Close 结束输出并释放剩余 holdback（已审计）。
func (g *OutputGuard) Close() (string, error) {
	if g.closed {
		return "", ErrOutputSecurity
	}
	g.closed = true
	return g.drain(true)
}
