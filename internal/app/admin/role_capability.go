package admin

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"websql/internal/pkg/dberr"
)

// 角色能力版本与 AI 策略开关，均存放于 t_system_config。
//
// 兼容期（AIPolicyEnforce=false）：
//   - ai_allow_modify 不开放编辑；
//   - 有效 AI 写能力直接取 allow_modify，保证升级前后行为一致。
//
// 生效期（AIPolicyEnforce=true）：ai_allow_modify 进入永久安全基线，
// 只有同一角色同时允许 allow_modify 与 ai_allow_modify 才可能执行 AI 写操作。
const (
	// RoleCapabilityVersionKey 角色能力版本：1=旧协议，2=支持独立 AI 开关
	RoleCapabilityVersionKey = "ai.roleCapabilityVersion"
	// AIPolicyEnforceKey AI 写双门槛是否已生效
	AIPolicyEnforceKey = "ai.aiPolicyEnforce"
)

// AllowsAIModify 判断单个角色是否同时允许「修改数据」与「AI 修改数据」。
//
// 这是「角色内部双与」的唯一实现：ai_allow_modify 只能收紧 allow_modify，
// 不能单独提权。兼容期（AIPolicyEnforce=false）由调用方回退到 AllowModify，
// 不使用本方法。
func (r *Role) AllowsAIModify() bool {
	if r == nil {
		return false
	}
	return r.AllowModify > 0 && r.AIAllowModify > 0
}

// RolesAllowModify 跨角色取并集：任一角色允许修改数据即可。
func RolesAllowModify(roles []*Role) bool {
	for _, role := range roles {
		if role != nil && role.AllowModify > 0 {
			return true
		}
	}
	return false
}

// RolesAllowAIModify 跨角色取并集：任一角色同时满足双条件即成立。
//
// 关键语义：并集作用在「角色级双与的结果」上，而不是先分别并集
// allow_modify 与 ai_allow_modify 再取与，否则 A(1,0)+B(0,1) 会被错误放行。
func RolesAllowAIModify(roles []*Role) bool {
	for _, role := range roles {
		if role.AllowsAIModify() {
			return true
		}
	}
	return false
}

// RoleCapabilityVersion 返回当前角色能力版本，未配置时按 1 处理。
func RoleCapabilityVersion() int {
	raw, err := systemConfigReader(RoleCapabilityVersionKey)
	if err != nil {
		return 1
	}
	if raw == "" {
		return 1
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 1 {
		return 1
	}
	return v
}

// errSystemConfigUnavailable 表示管理库未就绪，无法读取系统配置。
var errSystemConfigUnavailable = errors.New("system config store unavailable")

// aiPolicyCacheTTL AI 策略开关的进程内缓存时间。
//
// 配置保存后由 app 容器调用 InvalidateAIPolicyCache 立即失效，
// TTL 只是兜底，避免每个请求都查库。
const aiPolicyCacheTTL = 30 * time.Second

type aiPolicySnapshot struct {
	value    bool
	loadedAt time.Time
	loaded   bool
}

var (
	aiPolicyMu    sync.RWMutex
	aiPolicyCache aiPolicySnapshot
	// systemConfigReader 仅测试可替换；生产始终走 readSystemConfigFromDB。
	systemConfigReader = readSystemConfigFromDB
)

// AIPolicyEnforcedState 判断 AI 写双门槛是否已生效，并返回读取错误。
//
// 安全语义（fail-closed）：
//   - 读取成功：按配置值，并更新进程内快照；
//   - 读取失败且存在上次成功值：沿用上次值，同时回传错误供上层告警；
//   - 读取失败且从未成功读取：按“已启用”处理，绝不能退化为兼容期语义。
func AIPolicyEnforcedState() (bool, error) {
	now := time.Now()
	aiPolicyMu.RLock()
	snap := aiPolicyCache
	aiPolicyMu.RUnlock()
	if snap.loaded && now.Sub(snap.loadedAt) < aiPolicyCacheTTL {
		return snap.value, nil
	}

	raw, err := systemConfigReader(AIPolicyEnforceKey)
	if err != nil {
		if snap.loaded {
			return snap.value, err
		}
		// 未配置读不到时按已启用处理：双门槛只会更严，不会放宽。
		return true, err
	}

	value := strings.EqualFold(strings.TrimSpace(raw), "true")
	aiPolicyMu.Lock()
	aiPolicyCache = aiPolicySnapshot{value: value, loadedAt: now, loaded: true}
	aiPolicyMu.Unlock()
	return value, nil
}

// AIPolicyEnforced 兼容包装：读取失败时按 AIPolicyEnforcedState 的 fail-closed 结果。
func AIPolicyEnforced() bool {
	value, _ := AIPolicyEnforcedState()
	return value
}

// InvalidateAIPolicyCache 清空 AI 策略开关缓存（系统配置保存后调用）。
func InvalidateAIPolicyCache() {
	aiPolicyMu.Lock()
	aiPolicyCache = aiPolicySnapshot{}
	aiPolicyMu.Unlock()
}

// readSystemConfigFromDB 直接读取系统配置。
//
// admin 包不能依赖 app/system（后者依赖 admin，会形成循环），故在此直接查库。
// 未找到记录返回 ("", nil)（表示未配置）；其他错误原样返回，由调用方决定 fail-open/fail-closed。
func readSystemConfigFromDB(key string) (string, error) {
	db := getDBOrNil()
	if db == nil {
		return "", errSystemConfigUnavailable
	}
	var value string
	if err := db.Get(&value, "select config_value from t_system_config where config_key = ?", key); err != nil {
		if dberr.IsNoRows(err) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

// getSystemConfigValue 保留旧接口：读取失败按空值处理。
// 新增安全相关配置请使用 AIPolicyEnforcedState / systemConfigReader。
func getSystemConfigValue(key string) string {
	value, _ := systemConfigReader(key)
	return value
}
