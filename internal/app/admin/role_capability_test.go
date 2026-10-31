package admin

import (
	"errors"
	"testing"
	"time"
)

// TestRoleAllowsAIModify_Matrix 冻结「角色内部双与」全矩阵。
//
// ai_allow_modify 是 allow_modify 的附加约束：任何单项为 0 都不得放行 AI 写，
// 避免旧客户端只提交其中一个开关时被误判为已授权。
func TestRoleAllowsAIModify_Matrix(t *testing.T) {
	cases := []struct {
		name        string
		allowModify int
		aiAllow     int
		want        bool
	}{
		{"双拒", 0, 0, false},
		{"仅 AI 开关不能提权", 0, 1, false},
		{"仅 allow_modify 不自动放开 AI", 1, 0, false},
		{"双允许才成立", 1, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			role := &Role{AllowModify: tc.allowModify, AIAllowModify: tc.aiAllow}
			if got := role.AllowsAIModify(); got != tc.want {
				t.Fatalf("AllowsAIModify(allow=%d, ai=%d)=%v, want %v",
					tc.allowModify, tc.aiAllow, got, tc.want)
			}
			if got := RolesAllowAIModify([]*Role{role}); got != tc.want {
				t.Fatalf("单角色并集应等于角色级双与，got %v want %v", got, tc.want)
			}
		})
	}
	if (*Role)(nil).AllowsAIModify() {
		t.Fatal("nil 角色必须按拒绝处理")
	}
}

// TestRolesAllowAIModify_CrossRoleUnion 冻结跨角色并集语义。
//
// 并集必须作用在「角色级双与的结果」上：A(allow=1, ai=0) + B(allow=0, ai=1)
// 拼不出任何单一角色都未授予的 AI 写能力。
func TestRolesAllowAIModify_CrossRoleUnion(t *testing.T) {
	cases := []struct {
		name  string
		roles []*Role
		want  bool
	}{
		{"空角色集", nil, false},
		{"单角色双允许", []*Role{{AllowModify: 1, AIAllowModify: 1}}, true},
		{"拆分开关不得拼接", []*Role{
			{AllowModify: 1, AIAllowModify: 0},
			{AllowModify: 0, AIAllowModify: 1},
		}, false},
		{"任一角色双允许即放行", []*Role{
			{AllowModify: 1, AIAllowModify: 0},
			{AllowModify: 1, AIAllowModify: 1},
		}, true},
		{"nil 角色被忽略", []*Role{nil, {AllowModify: 1, AIAllowModify: 1}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RolesAllowAIModify(tc.roles); got != tc.want {
				t.Fatalf("RolesAllowAIModify=%v, want %v", got, tc.want)
			}
		})
	}
}

// TestRolesAllowModify_Union 冻结 allow_modify 的跨角色并集（经典 SQL 能力）。
func TestRolesAllowModify_Union(t *testing.T) {
	if RolesAllowModify(nil) {
		t.Fatal("空角色集不得允许修改数据")
	}
	if RolesAllowModify([]*Role{{AllowModify: 0}, {AllowModify: 0}}) {
		t.Fatal("全部角色拒绝时不得允许修改数据")
	}
	if !RolesAllowModify([]*Role{{AllowModify: 0}, {AllowModify: 1}}) {
		t.Fatal("任一角色允许即应放行")
	}
	if RolesAllowModify([]*Role{nil}) {
		t.Fatal("nil 角色不得被计为允许")
	}
}

// resetAIPolicyTestState 恢复全局读取器与缓存，避免用例间相互污染。
func resetAIPolicyTestState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		systemConfigReader = readSystemConfigFromDB
		InvalidateAIPolicyCache()
	})
	InvalidateAIPolicyCache()
}

// expireAIPolicyCache 让缓存超过 TTL，模拟“需要重新读取配置”的时机。
func expireAIPolicyCache() {
	aiPolicyMu.Lock()
	aiPolicyCache.loadedAt = time.Now().Add(-2 * aiPolicyCacheTTL)
	aiPolicyMu.Unlock()
}

func TestAIPolicyEnforcedState_ConfiguredValue(t *testing.T) {
	resetAIPolicyTestState(t)
	systemConfigReader = func(key string) (string, error) {
		if key != AIPolicyEnforceKey {
			t.Fatalf("unexpected key: %s", key)
		}
		return " TRUE ", nil
	}

	enforced, err := AIPolicyEnforcedState()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enforced {
		t.Fatal("配置为 true 时应返回已启用")
	}
}

func TestAIPolicyEnforcedState_NotConfigured(t *testing.T) {
	resetAIPolicyTestState(t)
	systemConfigReader = func(string) (string, error) { return "", nil }

	enforced, err := AIPolicyEnforcedState()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enforced {
		t.Fatal("未配置时应按兼容期语义返回 false")
	}
}

func TestAIPolicyEnforcedState_ReadFailureWithoutCache(t *testing.T) {
	resetAIPolicyTestState(t)
	systemConfigReader = func(string) (string, error) {
		return "", errors.New("db unavailable")
	}

	enforced, err := AIPolicyEnforcedState()
	if err == nil {
		t.Fatal("读库失败应回传错误供上层告警")
	}
	if !enforced {
		t.Fatal("从未成功读取时必须 fail-closed，按已启用处理")
	}
}

func TestAIPolicyEnforcedState_ReadFailureKeepsLastGood(t *testing.T) {
	resetAIPolicyTestState(t)
	systemConfigReader = func(string) (string, error) { return "false", nil }
	if enforced, err := AIPolicyEnforcedState(); err != nil || enforced {
		t.Fatalf("首次读取异常：enforced=%v err=%v", enforced, err)
	}

	expireAIPolicyCache()
	systemConfigReader = func(string) (string, error) { return "", errors.New("db unavailable") }

	enforced, err := AIPolicyEnforcedState()
	if err == nil {
		t.Fatal("降级时应回传错误")
	}
	if enforced {
		t.Fatal("应沿用上次成功值 false，而不是按已启用处理")
	}
}

func TestAIPolicyEnforcedState_CacheHitSkipsReader(t *testing.T) {
	resetAIPolicyTestState(t)
	calls := 0
	systemConfigReader = func(string) (string, error) {
		calls++
		return "true", nil
	}

	if _, err := AIPolicyEnforcedState(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := AIPolicyEnforcedState(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("TTL 内应只读取一次配置，实际 %d 次", calls)
	}
}

func TestAIPolicyEnforcedState_InvalidateForcesReload(t *testing.T) {
	resetAIPolicyTestState(t)
	value := "true"
	systemConfigReader = func(string) (string, error) { return value, nil }

	if enforced, _ := AIPolicyEnforcedState(); !enforced {
		t.Fatal("首次应读取 true")
	}
	value = "false"
	InvalidateAIPolicyCache()
	if enforced, _ := AIPolicyEnforcedState(); enforced {
		t.Fatal("失效后应重新读取 false")
	}
}

func TestRoleCapabilityVersion_ReadFailureDefaultsToOne(t *testing.T) {
	resetAIPolicyTestState(t)
	systemConfigReader = func(string) (string, error) {
		return "", errors.New("db unavailable")
	}
	if got := RoleCapabilityVersion(); got != 1 {
		t.Fatalf("读取失败应按版本 1 处理，实际 %d", got)
	}
}
