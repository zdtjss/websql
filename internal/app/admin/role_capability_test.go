package admin

import "testing"

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
