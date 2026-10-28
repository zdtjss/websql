package admin

import (
	"strconv"
	"strings"
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
	raw := getSystemConfigValue(RoleCapabilityVersionKey)
	if raw == "" {
		return 1
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 1 {
		return 1
	}
	return v
}

// AIPolicyEnforced 判断 AI 写双门槛是否已生效。
//
// 读取失败按 false 处理（兼容期语义，不会放宽既有 allow_modify 行为）。
func AIPolicyEnforced() bool {
	return strings.EqualFold(strings.TrimSpace(getSystemConfigValue(AIPolicyEnforceKey)), "true")
}

// getSystemConfigValue 直接读取系统配置。
//
// admin 包不能依赖 app/system（后者依赖 admin，会形成循环），故在此直接查库。
func getSystemConfigValue(key string) string {
	db := getDBOrNil()
	if db == nil {
		return ""
	}
	var value string
	if err := db.Get(&value, "select config_value from t_system_config where config_key = ?", key); err != nil {
		return ""
	}
	return value
}
