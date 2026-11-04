package admin

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// A4 回归：空条件时管理员分页必须返回全部用户，而不是恒返回空列表。
func TestFindUserListPage_EmptyConditionReturnsAll(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`create table t_user (id text primary key, name text, login_name text, pwd text, bio text)`,
		`insert into t_user (id, name, login_name, pwd, bio) values
			('u1', '张三', 'zhangsan', 'x', ''),
			('u2', '李四', 'lisi', 'x', ''),
			('u3', '王五', 'wangwu', 'x', '')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("初始化测试库失败：%v", err)
		}
	}

	repo := NewUserRepo(db)
	list, total, err := repo.FindUserListPage("", "", "", "", 1, 2)
	if err != nil {
		t.Fatalf("分页查询失败：%v", err)
	}
	if total != 3 {
		t.Fatalf("total 应为 3，实际 %d", total)
	}
	if len(list) != 2 {
		t.Fatalf("第一页应返回 2 条，实际 %d", len(list))
	}

	page2, _, err := repo.FindUserListPage("", "", "", "", 2, 2)
	if err != nil {
		t.Fatalf("第二页查询失败：%v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("第二页应返回 1 条，实际 %d", len(page2))
	}

	// 条件搜索仍然生效
	filtered, filteredTotal, err := repo.FindUserListPage("", "张", "", "", 1, 2)
	if err != nil {
		t.Fatalf("条件查询失败：%v", err)
	}
	if filteredTotal != 1 || len(filtered) != 1 || filtered[0].Id != "u1" {
		t.Fatalf("按姓名过滤结果不符：total=%d list=%+v", filteredTotal, filtered)
	}
}
