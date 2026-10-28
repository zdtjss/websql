package agent

// 输入预处理：在进入会话与模型之前完成大小、编码、注入风险与能力限制。
//
// 约束：
//   - 必须先于 AppendMessage 与模型调用执行；
//   - 注入检测只是风险信号，不能替代后续工具权限、审批与输出审计；
//   - 检测器异常按高风险处理（不允许把异常当作低风险继续执行写操作）。

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// maxQuestionBytes 单次提问的字节上限（32 KiB）。
const maxQuestionBytes = 32 * 1024

// InputRisk 输入风险等级。
type InputRisk int

const (
	// InputLow 正常处理
	InputLow InputRisk = iota
	// InputMedium 允许普通对话，但本轮禁用 AI 写操作与代码/文件写入
	InputMedium
	// InputHigh 拒绝本次请求
	InputHigh
)

// CleanInput 预处理结果。
type CleanInput struct {
	Text string
	// RestrictWrites 中风险时为 true：本轮收缩写能力
	RestrictWrites bool
	Risk           InputRisk
}

// ErrInputRejected 输入为空、超长、编码无效或规范化后为空。
var ErrInputRejected = errors.New("输入为空、过长或编码无效")

// PreprocessQuestion 规范化并评估提问。
//
// score 为注入风险检测器；传 nil 时使用内置规则 scoreInputRisk。
func PreprocessQuestion(raw string, score func(string) InputRisk) (CleanInput, error) {
	if raw == "" || len(raw) > maxQuestionBytes || !utf8.ValidString(raw) {
		return CleanInput{}, ErrInputRejected
	}

	normalized := raw
	if needsCompatibilityNormalization(raw) {
		normalized = norm.NFKC.String(raw)
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return r
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			return -1 // 控制字符、零宽字符只从检测/存储副本移除
		default:
			return r
		}
	}, normalized)
	if strings.TrimSpace(cleaned) == "" {
		return CleanInput{}, ErrInputRejected
	}

	if score == nil {
		score = scoreInputRisk
	}
	switch safeScoreInputRisk(score, cleaned) {
	case InputHigh:
		recordSecurityEvent(StageInput, "injection", "deny")
		return CleanInput{}, &PermissionError{Message: "输入触发安全策略，请调整提问"}
	case InputMedium:
		recordSecurityEvent(StageInput, "injection", "restricted")
		return CleanInput{Text: cleaned, RestrictWrites: true, Risk: InputMedium}, nil
	default:
		recordSecurityEvent(StageInput, "injection", "allow")
		return CleanInput{Text: cleaned, Risk: InputLow}, nil
	}
}

// safeScoreInputRisk 捕获检测器异常并按高风险处理（fail-closed）。
func safeScoreInputRisk(score func(string) InputRisk, text string) (risk InputRisk) {
	risk = InputHigh
	defer func() {
		if recover() != nil {
			risk = InputHigh
		}
	}()
	return score(text)
}

var (
	// 高风险：索取系统提示词/密钥、要求绕过权限或审批、诱导调用未登记工具
	highRiskPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(输出|显示|打印|告诉我|给我|复述|泄露|reveal|show|print|repeat).{0,12}(系统提示词|系统指令|初始指令|system\s*prompt|developer\s*message|内部指令)`),
		regexp.MustCompile(`(?i)(连接密码|数据库密码|db\s*password|连接串|connection\s*string|access[_-]?token|api[_-]?key|secret[_-]?key|私钥|密钥).{0,12}(是什么|告诉我|给我|输出|显示|发我|reveal|show|print)`),
		regexp.MustCompile(`(?i)(告诉我|给我|输出|显示|发我|reveal|show|print).{0,12}(连接密码|数据库密码|api[_-]?key|access[_-]?token|secret[_-]?key|私钥)`),
		regexp.MustCompile(`(?i)(绕过|跳过|解除|关闭).{0,6}(权限|鉴权|审批|审核|校验|限制|安全检查)`),
		regexp.MustCompile(`(?i)(bypass|skip|disable|turn\s*off).{0,12}(permission|auth|approval|security\s*check|policy)`),
		regexp.MustCompile(`(?i)(免审批|无需审批|不用确认|直接执行不要确认|不要请求确认)`),
		regexp.MustCompile(`(?i)(调用|使用|开启).{0,8}(未登记|未注册|隐藏|内部).{0,4}工具`),
		regexp.MustCompile(`(?i)(export_by_code|exec_sql|write_file|edit_file).{0,12}(无需|跳过|绕过).{0,6}(权限|审批|限制)`),
	}
	// 中风险：指令覆盖、角色扮演、伪造授权、要求隐藏行为
	mediumRiskPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(忽略|无视|忘记|抛弃).{0,6}(之前|以上|上面|前面|所有|全部).{0,6}(指令|要求|规则|限制|设定)`),
		regexp.MustCompile(`(?i)(ignore|disregard|forget).{0,12}(previous|above|prior|all).{0,12}(instruction|rule|prompt|constraint)`),
		regexp.MustCompile(`(?i)(你现在是|从现在开始你是|假装你是|扮演|roleplay\s+as|act\s+as).{0,20}(管理员|admin|root|超级用户|开发者)`),
		regexp.MustCompile(`(?i)(用户|管理员|领导|老板|上级).{0,4}(已|已经|早就).{0,4}(授权|批准|同意)`),
		regexp.MustCompile(`(?i)(不要|别|禁止).{0,6}(记录|写|留下).{0,6}(审计|日志|痕迹)`),
		regexp.MustCompile(`(?i)(不要告诉|别告诉|瞒着).{0,10}(用户|管理员|审计)`),
	}
)

// riskAnchors 注入检测的关键词锚点。
//
// 性能：先做一次廉价的子串扫描，未命中任一锚点时不执行完整正则集；
// 锚点集覆盖全部内置规则的必要关键字，因此不会放宽检测范围。
var riskAnchors = []string{
	"密码", "密钥", "私钥", "提示词", "指令", "权限", "鉴权", "审批", "审核", "校验",
	"工具", "忽略", "忘记", "无视", "抛弃", "系统", "授权", "批准", "同意", "审计", "日志",
	"痕迹", "告诉", "管理员", "扮演", "免审批",
	"password", "passwd", "secret", "token", "prompt", "permission", "approval", "policy",
	"ignore", "forget", "instruction", "system", "admin", "auth", "bypass", "roleplay",
}

func hasRiskAnchor(text string) bool {
	lower := strings.ToLower(text)
	for _, anchor := range riskAnchors {
		if strings.Contains(lower, anchor) {
			return true
		}
	}
	return false
}

// scoreInputRisk 基于结构化规则评估注入风险。
//
// 只匹配明确的攻击意图短语，避免把「正常 SQL 教学」「忽略上一条查询条件」等
// 业务表达误判为注入（误报会直接阻断用户提问）。
func scoreInputRisk(text string) InputRisk {
	probe := normalizeMaskText(text) // NFKC + 去零宽，防止变形绕过
	if !hasRiskAnchor(probe) {
		return InputLow
	}
	for _, re := range highRiskPatterns {
		if re.MatchString(probe) {
			return InputHigh
		}
	}
	for _, re := range mediumRiskPatterns {
		if re.MatchString(probe) {
			return InputMedium
		}
	}
	return InputLow
}
