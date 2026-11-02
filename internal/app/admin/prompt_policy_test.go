package admin

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// setupPromptPolicyDB 构建最小管理库（角色 + 用户角色），用于验证管理员判定。
func setupPromptPolicyDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`create table t_role (
		id text primary key, name text, allow_modify integer default 0, ai_allow_modify integer default 0)`); err != nil {
		t.Fatalf("建表 t_role 失败：%v", err)
	}
	if _, err := db.Exec(`create table t_user_role (
		id text primary key, user_id text, role_id text)`); err != nil {
		t.Fatalf("建表 t_user_role 失败：%v", err)
	}
	if _, err := db.Exec(`insert into t_role (id, name) values ('r-admin', 'admin'), ('r-rdm', 'rdm')`); err != nil {
		t.Fatalf("插入角色失败：%v", err)
	}
	if _, err := db.Exec(`insert into t_user_role (id, user_id, role_id) values
		('ur-1', 'u-admin', 'r-admin'), ('ur-2', 'u-rdm', 'r-rdm')`); err != nil {
		t.Fatalf("插入用户角色失败：%v", err)
	}
	Init(db)
	invalidatePowerCacheAll()
	t.Cleanup(func() {
		invalidatePowerCacheAll()
		Init(nil)
	})
	return db
}

// TestPromptAccessPolicy 覆盖提示词读/写/删的归属与角色判定。
func TestPromptAccessPolicy(t *testing.T) {
	setupPromptPolicyDB(t)

	strPtr := func(s string) *string { return &s }

	adminID := "u-admin"
	creatorID := "u-rdm"
	otherID := "u-other"
	roleID := "r-rdm"

	personalOwn := &Prompt{Id: "p-own", CreatedBy: strPtr(creatorID)}
	personalOther := &Prompt{Id: "p-other", CreatedBy: strPtr(otherID)}
	rolePrompt := &Prompt{Id: "p-role", CreatedBy: strPtr(adminID), RoleId: strPtr(roleID)}

	cases := []struct {
		name      string
		prompt    *Prompt
		userID    string
		roleIDs   []string
		shared    bool
		wantRead  bool
		wantWrite bool
		wantDel   bool
	}{
		{"创建者读取个人提示词", personalOwn, creatorID, nil, false, true, true, true},
		{"他人读取未分享的个人提示词", personalOwn, otherID, nil, false, false, false, false},
		{"被分享用户可读不可写", personalOwn, otherID, nil, true, true, false, false},
		{"管理员可读写任意个人提示词", personalOther, adminID, nil, false, true, true, true},
		{"同角色用户可读角色提示词", rolePrompt, creatorID, []string{roleID}, false, true, false, false},
		{"非本角色用户不可读角色提示词", rolePrompt, otherID, []string{"r-other"}, false, false, false, false},
		{"管理员可读写角色提示词", rolePrompt, adminID, []string{"r-admin"}, false, true, true, true},
		{"无归属信息一律拒绝", &Prompt{Id: "p-empty"}, creatorID, nil, false, false, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canReadPrompt(tc.prompt, tc.userID, tc.roleIDs, tc.shared); got != tc.wantRead {
				t.Fatalf("canReadPrompt=%v, want %v", got, tc.wantRead)
			}
			if got := canWritePrompt(tc.prompt, tc.userID, tc.roleIDs); got != tc.wantWrite {
				t.Fatalf("canWritePrompt=%v, want %v", got, tc.wantWrite)
			}
			if got := canDeletePrompt(tc.prompt, tc.userID); got != tc.wantDel {
				t.Fatalf("canDeletePrompt=%v, want %v", got, tc.wantDel)
			}
		})
	}
}

// TestPromptSaveLimits 校验标题/内容长度、空白与分享用户归一化。
func TestPromptSaveLimits(t *testing.T) {
	if _, err := normalizePromptTitle("   "); err == nil {
		t.Fatal("空白标题必须拒绝")
	}
	if got, err := normalizePromptTitle("  月度分析  "); err != nil || got != "月度分析" {
		t.Fatalf("标题应去空白：got=%q err=%v", got, err)
	}
	if _, err := normalizePromptTitle(strings.Repeat("题", promptMaxTitleRunes+1)); err == nil {
		t.Fatal("超长标题必须拒绝")
	}
	if got, err := normalizePromptTitle(strings.Repeat("题", promptMaxTitleRunes)); err != nil || len([]rune(got)) != promptMaxTitleRunes {
		t.Fatalf("标题长度上限应可保存：err=%v", err)
	}

	if _, err := normalizePromptContent("\r\n\t "); err == nil {
		t.Fatal("空白内容必须拒绝")
	}
	if _, err := normalizePromptContent(strings.Repeat("文", promptMaxContentRunes+1)); err == nil {
		t.Fatal("超长内容必须拒绝")
	}
	if got, err := normalizePromptContent(strings.Repeat("文", promptMaxContentRunes)); err != nil || len([]rune(got)) != promptMaxContentRunes {
		t.Fatalf("内容长度上限应可保存：err=%v", err)
	}

	ids, err := normalizePromptSharedUserIDs([]string{" u1 ", "", "u1", "u2"})
	if err != nil {
		t.Fatalf("分享用户归一化失败：%v", err)
	}
	if len(ids) != 2 || ids[0] != "u1" || ids[1] != "u2" {
		t.Fatalf("分享用户应去空白去重：%v", ids)
	}

	tooMany := make([]string, promptMaxSharedUsers+1)
	for i := range tooMany {
		tooMany[i] = "u" + strings.Repeat("x", i)
	}
	if _, err := normalizePromptSharedUserIDs(tooMany); err == nil {
		t.Fatal("分享用户超过上限必须拒绝")
	}
}

// TestEffectiveSharedUserIDs 角色提示词不支持单独分享，个人提示词原样保留。
func TestEffectiveSharedUserIDs(t *testing.T) {
	ids := []string{"u1", "u2"}
	if got := effectiveSharedUserIDs(true, ids); len(got) != 0 {
		t.Fatalf("角色提示词应忽略分享列表，实际 %v", got)
	}
	if got := effectiveSharedUserIDs(false, ids); len(got) != 2 || got[0] != "u1" || got[1] != "u2" {
		t.Fatalf("个人提示词分享列表应原样返回，实际 %v", got)
	}
	if got := effectiveSharedUserIDs(true, nil); got != nil {
		t.Fatalf("角色提示词空列表应返回 nil，实际 %v", got)
	}
}

// TestExcludeSharedSelf 分享列表包含当前用户时移除，其它用户保留。
func TestExcludeSharedSelf(t *testing.T) {
	got := excludeUserID([]string{"u1", "me", "u2"}, "me")
	if len(got) != 2 || got[0] != "u1" || got[1] != "u2" {
		t.Fatalf("应移除当前用户并保留其它用户，实际 %v", got)
	}
	if got := excludeUserID([]string{"u1"}, "other"); len(got) != 1 || got[0] != "u1" {
		t.Fatalf("不含当前用户时列表不应变化，实际 %v", got)
	}
	if got := excludeUserID(nil, "me"); got != nil {
		t.Fatalf("空列表应返回 nil，实际 %v", got)
	}
}

// TestDeleteUserCleansPromptShares 删除用户时清理其作为接收者/发起者的分享记录。
func TestDeleteUserCleansPromptShares(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	stmts := []string{
		`create table t_user (id text primary key, name text, login_name text)`,
		`create table t_user_role (id text primary key, user_id text, role_id text)`,
		`create table t_prompt_share (id text primary key, prompt_id text not null, shared_by text not null, shared_to text not null)`,
		`create table t_permission_revision (id integer primary key, revision integer not null default 0, updated_at datetime)`,
		`insert into t_permission_revision (id, revision) values (1, 0)`,
		`insert into t_user (id, name, login_name) values ('u-del', 'Del', 'del'), ('u-keep', 'Keep', 'keep'), ('u-other', 'Other', 'other')`,
		`insert into t_prompt_share (id, prompt_id, shared_by, shared_to) values
			('s-to', 'p1', 'u-keep', 'u-del'),
			('s-by', 'p2', 'u-del', 'u-keep'),
			('s-keep', 'p3', 'u-keep', 'u-other')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("初始化测试库失败：%v\n语句：%s", err, stmt)
		}
	}

	Init(db)
	invalidatePowerCacheAll()
	t.Cleanup(func() {
		invalidatePowerCacheAll()
		Init(nil)
	})

	if err := NewUserRepo(db).Delete("u-del"); err != nil {
		t.Fatalf("删除用户失败：%v", err)
	}

	var remaining []string
	if err := db.Select(&remaining, "select id from t_prompt_share order by id"); err != nil {
		t.Fatalf("查询剩余分享记录失败：%v", err)
	}
	if len(remaining) != 1 || remaining[0] != "s-keep" {
		t.Fatalf("应仅保留与删除用户无关的分享记录，实际 %v", remaining)
	}
}

// TestPromptUsersExistMultipleIDs 验证 IN (?) 切片展开：多个分享用户必须正确校验。
func TestPromptUsersExistMultipleIDs(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`create table t_user (id text primary key, name text, login_name text)`); err != nil {
		t.Fatalf("建表失败：%v", err)
	}
	if _, err := db.Exec(`insert into t_user (id, name, login_name) values ('u1','A','a'), ('u2','B','b')`); err != nil {
		t.Fatalf("插入用户失败：%v", err)
	}
	Init(db)
	t.Cleanup(func() { Init(nil) })

	if !promptUsersExist([]string{"u1", "u2"}) {
		t.Fatal("全部存在的分享用户应通过校验")
	}
	if promptUsersExist([]string{"u1", "missing"}) {
		t.Fatal("含不存在用户的分享列表必须拒绝")
	}
	if !promptUsersExist(nil) {
		t.Fatal("空分享列表应直接通过")
	}
}
