package admin

import (
	"errors"
	"testing"
)

func ptr(s string) *string { return &s }

func TestNormalizeAndValidateRoleSave(t *testing.T) {
	base := func() *RoleSave {
		return &RoleSave{
			Id:          "r1",
			Name:        "测试角色",
			ViewClassic: 1,
			AllowModify: 1,
			AddPowers: []*PowerDetail{
				{ConnId: "c1", Level: "conn", SchemaName: ptr("ignored"), TableName: ptr("ignored"), ColumnName: ptr("ignored")},
			},
		}
	}

	t.Run("conn 级清空下级字段", func(t *testing.T) {
		role := base()
		if err := normalizeAndValidateRoleSave(role); err != nil {
			t.Fatalf("应通过：%v", err)
		}
		p := role.AddPowers[0]
		if p.SchemaName != nil || p.TableName != nil || p.ColumnName != nil {
			t.Fatalf("conn 级应清空下级字段：%+v", p)
		}
	})

	t.Run("schema 级必须带 schema 并清空下级", func(t *testing.T) {
		role := base()
		role.AddPowers = []*PowerDetail{{ConnId: "c1", Level: "schema", SchemaName: ptr("s1"), TableName: ptr("t1")}}
		if err := normalizeAndValidateRoleSave(role); err != nil {
			t.Fatalf("应通过：%v", err)
		}
		if role.AddPowers[0].TableName != nil || role.AddPowers[0].ColumnName != nil {
			t.Fatal("schema 级应清空 table/column")
		}
		role.AddPowers = []*PowerDetail{{ConnId: "c1", Level: "schema"}}
		if err := normalizeAndValidateRoleSave(role); !errors.Is(err, ErrInvalidRoleSave) {
			t.Fatalf("缺少 schema 应拒绝，实际 %v", err)
		}
	})

	t.Run("table 级缺表名拒绝", func(t *testing.T) {
		role := base()
		role.AddPowers = []*PowerDetail{{ConnId: "c1", Level: "table", SchemaName: ptr("s1")}}
		if err := normalizeAndValidateRoleSave(role); !errors.Is(err, ErrInvalidRoleSave) {
			t.Fatalf("缺少表名应拒绝，实际 %v", err)
		}
	})

	t.Run("column 级缺列名拒绝", func(t *testing.T) {
		role := base()
		role.AddPowers = []*PowerDetail{{ConnId: "c1", Level: "column", SchemaName: ptr("s1"), TableName: ptr("t1")}}
		if err := normalizeAndValidateRoleSave(role); !errors.Is(err, ErrInvalidRoleSave) {
			t.Fatalf("缺少列名应拒绝，实际 %v", err)
		}
	})

	t.Run("未知层级拒绝", func(t *testing.T) {
		role := base()
		role.AddPowers = []*PowerDetail{{ConnId: "c1", Level: "database"}}
		if err := normalizeAndValidateRoleSave(role); !errors.Is(err, ErrInvalidRoleSave) {
			t.Fatalf("未知层级应拒绝，实际 %v", err)
		}
	})

	t.Run("add 与 del 冲突拒绝", func(t *testing.T) {
		role := base()
		role.AddPowers = []*PowerDetail{{ConnId: "c1", Level: "schema", SchemaName: ptr("s1")}}
		role.DelPowers = []*PowerDetail{{ConnId: "c1", Level: "schema", SchemaName: ptr("s1")}}
		if err := normalizeAndValidateRoleSave(role); !errors.Is(err, ErrInvalidRoleSave) {
			t.Fatalf("add/del 冲突应拒绝，实际 %v", err)
		}
	})

	t.Run("权限项角色与保存角色不一致拒绝", func(t *testing.T) {
		role := base()
		role.AddPowers = []*PowerDetail{{ConnId: "c1", Level: "conn", RoleId: "other"}}
		if err := normalizeAndValidateRoleSave(role); !errors.Is(err, ErrInvalidRoleSave) {
			t.Fatalf("角色不一致应拒绝，实际 %v", err)
		}
	})

	t.Run("角色名称为空拒绝", func(t *testing.T) {
		role := base()
		role.Name = "   "
		if err := normalizeAndValidateRoleSave(role); !errors.Is(err, ErrInvalidRoleSave) {
			t.Fatalf("空名称应拒绝，实际 %v", err)
		}
	})

	t.Run("AI 开关取值非法拒绝", func(t *testing.T) {
		role := base()
		bad := 2
		role.AIAllowModify = &bad
		if err := normalizeAndValidateRoleSave(role); !errors.Is(err, ErrInvalidRoleSave) {
			t.Fatalf("非法开关值应拒绝，实际 %v", err)
		}
	})
}

func TestComputePermissionDigest(t *testing.T) {
	powersA := []*PowerDetail{
		{ConnId: "c1", Level: "schema", SchemaName: ptr("s1")},
		{ConnId: "c1", Level: "table", SchemaName: ptr("s1"), TableName: ptr("orders")},
	}
	// 顺序不同但集合相同：摘要必须一致（与数据库返回顺序无关）
	powersB := []*PowerDetail{
		{ConnId: "c1", Level: "table", SchemaName: ptr("s1"), TableName: ptr("orders")},
		{ConnId: "c1", Level: "schema", SchemaName: ptr("s1")},
	}
	d1 := computePermissionDigest("r1", powersA, 1, 1, 1)
	d2 := computePermissionDigest("r1", powersB, 1, 1, 1)
	if d1 != d2 {
		t.Fatalf("权限集合相同应得到同一摘要：%s != %s", d1, d2)
	}

	// 能力开关变化必须改变摘要（防止切换 AI 开关后复用旧 Agent/旧摘要）
	if computePermissionDigest("r1", powersA, 1, 1, 0) == d1 {
		t.Fatal("aiAllowModify 变化应改变摘要")
	}
	if computePermissionDigest("r1", powersA, 0, 1, 1) == d1 {
		t.Fatal("viewClassic 变化应改变摘要")
	}
	if computePermissionDigest("r1", powersA, 1, 0, 1) == d1 {
		t.Fatal("allowModify 变化应改变摘要")
	}

	// 列名大小写归一：DB 返回的列名大小写差异不应产生不同摘要
	lower := []*PowerDetail{{ConnId: "c1", Level: "column", SchemaName: ptr("s1"), TableName: ptr("t"), ColumnName: ptr("id")}}
	upper := []*PowerDetail{{ConnId: "c1", Level: "column", SchemaName: ptr("s1"), TableName: ptr("t"), ColumnName: ptr("ID")}}
	if computePermissionDigest("r1", lower, 1, 1, 1) != computePermissionDigest("r1", upper, 1, 1, 1) {
		t.Fatal("列名大小写差异不应改变摘要")
	}

	// 增加权限必须改变摘要
	more := append(append([]*PowerDetail{}, powersA...), &PowerDetail{ConnId: "c1", Level: "conn"})
	if computePermissionDigest("r1", more, 1, 1, 1) == d1 {
		t.Fatal("权限项变化应改变摘要")
	}
}
