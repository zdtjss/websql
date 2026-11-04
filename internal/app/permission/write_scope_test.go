package permission

import (
	"testing"

	admin "websql/internal/app/admin"
)

// TestCheckWriteTableAccessWithRoles 冻结经典写路径的写范围语义：
// 只有 allow_modify>0 的角色自身的授权可以授予写权限，只读角色的连接级
// 授权不得通过并集放大为可写。
func TestCheckWriteTableAccessWithRoles(t *testing.T) {
	connID := "c1"
	schema := "s1"
	orders := "orders"

	readOnly := &admin.PowerDetail{RoleId: "r_read", ConnId: connID, Level: "conn"}
	writeTable := &admin.PowerDetail{
		RoleId: "r_write", ConnId: connID, Level: "table",
		SchemaName: &schema, TableName: &orders,
	}
	byRole := admin.GroupPowerDetailsByRole([]*admin.PowerDetail{readOnly, writeTable}, connID)
	writeRoles := writeCapableRoleIDs([]*admin.Role{
		{Id: "r_read", AllowModify: 0},
		{Id: "r_write", AllowModify: 1},
	})

	if !hasWriteCapableRoleOnConn(byRole, writeRoles) {
		t.Fatal("连接上存在可写角色，应判定为存在写能力")
	}
	if !checkWriteTableAccessWithRoles(byRole, writeRoles, schema, orders) {
		t.Fatal("可写角色自身的表应允许写入")
	}
	if checkWriteTableAccessWithRoles(byRole, writeRoles, schema, "secret_salary") {
		t.Fatal("只读角色的连接级授权不得授予写权限")
	}
	if checkWriteTableAccessWithRoles(byRole, writeRoles, "s2", orders) {
		t.Fatal("可写角色未授权的 schema 必须拒绝")
	}

	// 没有任何可写角色：一律拒绝，即使存在只读的连接级授权。
	onlyRead := writeCapableRoleIDs([]*admin.Role{{Id: "r_read", AllowModify: 0}})
	if hasWriteCapableRoleOnConn(byRole, onlyRead) {
		t.Fatal("只读角色不应被判定为可写角色")
	}
	if checkWriteTableAccessWithRoles(byRole, onlyRead, schema, orders) {
		t.Fatal("无可写角色时必须拒绝写入")
	}
}

// TestHasWriteCapableRoleOnConn_NoPowerOnConn 可写角色在本连接无授权时不得生效。
func TestHasWriteCapableRoleOnConn_NoPowerOnConn(t *testing.T) {
	connID := "c1"
	schema := "s1"
	orders := "orders"
	otherConn := &admin.PowerDetail{
		RoleId: "r_write", ConnId: "c2", Level: "table",
		SchemaName: &schema, TableName: &orders,
	}
	byRole := admin.GroupPowerDetailsByRole([]*admin.PowerDetail{otherConn}, connID)
	writeRoles := writeCapableRoleIDs([]*admin.Role{{Id: "r_write", AllowModify: 1}})

	if hasWriteCapableRoleOnConn(byRole, writeRoles) {
		t.Fatal("可写角色在 c1 没有授权，不应授予 c1 写能力")
	}
	if checkWriteTableAccessWithRoles(byRole, writeRoles, schema, orders) {
		t.Fatal("跨连接的写授权不得生效")
	}
}

// TestAnalyzeSQL_ReplaceMergeTargets REPLACE/MERGE 必须能解析出写目标表，
// 否则写范围校验会因“无法确定目标表”而误拒合法写入。
func TestAnalyzeSQL_ReplaceMergeTargets(t *testing.T) {
	cases := []struct {
		sql  string
		want string
	}{
		{"REPLACE INTO s1.orders (id) VALUES (1)", "orders"},
		{"MERGE INTO s1.orders t USING s1.src s ON t.id = s.id", "orders"},
	}
	for _, tc := range cases {
		a := AnalyzeSQL(tc.sql, "s1")
		if len(a.WriteTables) == 0 || a.WriteTables[0].Name != tc.want {
			t.Errorf("%q 写目标解析失败：%+v", tc.sql, a.WriteTables)
		}
	}
}
