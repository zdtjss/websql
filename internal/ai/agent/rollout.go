package agent

// 安全策略灰度：按用户稳定分桶，一次运行内模式固定。
//
// 不存在可回到「已知越权实现」的 legacy 模式：
//   - baseline：永久安全修复全部生效，仅候选策略（更严格的注入/脱敏规则）不启用；
//   - shadow  ：在 baseline 上计算候选策略的 wouldDeny/wouldMask/wouldQuarantine，不改变执行；
//   - enforce ：baseline 与候选策略全部执行。
//
// `AIPolicyEnforce` 与权限修订号属于永久安全基线，不随候选模式回滚。
//
// 候选策略集合见 shadow.go（require_schema_qualified / mask_high_risk_column_names /
// quarantine_assistant_without_resources）：shadow 只累加 would_* 指标，enforce 才执行，
// 因此后续新增候选策略无需改动分桶与优先级逻辑。

import (
	"encoding/json"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	system "websql/internal/app/system"
)

// RolloutMode 灰度模式。
type RolloutMode string

const (
	// RolloutBaseline 永久安全基线
	RolloutBaseline RolloutMode = "baseline"
	// RolloutShadow 影子预检：只记录候选策略差异
	RolloutShadow RolloutMode = "shadow"
	// RolloutEnforce 全量执行候选策略
	RolloutEnforce RolloutMode = "enforce"
)

// PolicyRolloutConfigKey 灰度配置在 t_system_config 中的键名。
const PolicyRolloutConfigKey = "ai.securityRollout"

// PolicyRollout 单一策略配置对象（支持按用户分桶，不使用无法分桶的全局枚举）。
type PolicyRollout struct {
	DefaultMode     string   `json:"defaultMode"`
	EnforcePercent  uint8    `json:"enforcePercent"`
	ShadowUserIDs   []string `json:"shadowUserIds"`
	EnforceUserIDs  []string `json:"enforceUserIds"`
	AIPolicyEnforce bool     `json:"aiPolicyEnforce"`
	Revision        uint64   `json:"revision"`
}

// defaultPolicyRollout 默认基线：永久安全修复生效，候选策略与 AI 双门槛均关闭。
func defaultPolicyRollout() PolicyRollout {
	return PolicyRollout{DefaultMode: string(RolloutBaseline)}
}

const rolloutConfigCacheTTL = 5 * time.Second

var (
	rolloutMu        sync.Mutex
	rolloutCache     PolicyRollout
	rolloutCachedAt  time.Time
	rolloutConfigKey string
)

// LoadPolicyRollout 读取灰度配置（5s 本地缓存，避免每次请求都查管理库）。
func LoadPolicyRollout() PolicyRollout {
	rolloutMu.Lock()
	defer rolloutMu.Unlock()
	if rolloutConfigKey != "" && time.Since(rolloutCachedAt) < rolloutConfigCacheTTL {
		return rolloutCache
	}
	raw := system.GetSystemConfigValue(PolicyRolloutConfigKey)
	cfg := defaultPolicyRollout()
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			// 配置损坏：按最保守的 baseline 处理，并让下次调用重试读取
			cfg = defaultPolicyRollout()
			rolloutCachedAt = time.Now()
			rolloutCache = cfg
			return cfg
		}
	}
	if cfg.DefaultMode == "" {
		cfg.DefaultMode = string(RolloutBaseline)
	}
	rolloutCache = cfg
	rolloutCachedAt = time.Now()
	rolloutConfigKey = PolicyRolloutConfigKey
	return cfg
}

// InvalidateRolloutCache 在配置保存后清除本地缓存。
func InvalidateRolloutCache() {
	rolloutMu.Lock()
	rolloutCachedAt = time.Time{}
	rolloutConfigKey = ""
	rolloutMu.Unlock()
}

// ResolveRolloutMode 按用户解析灰度模式（一次运行内固定）。
func ResolveRolloutMode(userID string) RolloutMode {
	return ResolveRolloutModeWith(LoadPolicyRollout(), userID)
}

// ResolveRolloutModeWith 使用给定配置解析模式，便于单元测试。
//
// 优先级：显式 enforce 用户 > 显式 shadow 用户 > 稳定桶比例 > DefaultMode。
func ResolveRolloutModeWith(cfg PolicyRollout, userID string) RolloutMode {
	mode := normalizeRolloutMode(cfg.DefaultMode)
	if userID == "" {
		return RolloutBaseline
	}
	if containsFold(cfg.EnforceUserIDs, userID) {
		return RolloutEnforce
	}
	if containsFold(cfg.ShadowUserIDs, userID) {
		return RolloutShadow
	}
	if cfg.EnforcePercent > 0 && stableBucket(userID) < int(cfg.EnforcePercent) {
		return RolloutEnforce
	}
	return mode
}

func normalizeRolloutMode(raw string) RolloutMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(RolloutEnforce):
		return RolloutEnforce
	case string(RolloutShadow):
		return RolloutShadow
	default:
		return RolloutBaseline
	}
}

// CandidatePoliciesEnabled 判断候选策略是否需要真正执行（enforce）。
func CandidatePoliciesEnabled(mode RolloutMode) bool {
	return mode == RolloutEnforce
}

// CandidatePoliciesEvaluated 判断是否需要计算候选策略差异（shadow 与 enforce）。
func CandidatePoliciesEvaluated(mode RolloutMode) bool {
	return mode == RolloutEnforce || mode == RolloutShadow
}

// stableBucket 返回 [0,100) 的稳定桶号。
//
// 使用 userId + deploymentSalt 的 FNV-1a：同一用户在任何实例、任何重启后落同一桶，
// 不会出现「同一用户的工具调用随机切换模式」。
func stableBucket(userID string) int {
	return StableBucket(userID, rolloutSalt())
}

// StableBucket 使用显式盐计算稳定桶（便于单元测试）。
func StableBucket(userID, salt string) int {
	if strings.TrimSpace(salt) == "" {
		salt = "websql"
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(salt))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(userID))
	return int(h.Sum64() % 100)
}

func rolloutSalt() string {
	salt := system.GetSystemConfigValue("ai.deploymentSalt")
	if strings.TrimSpace(salt) == "" {
		return "websql"
	}
	return strings.TrimSpace(salt)
}

func containsFold(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}
