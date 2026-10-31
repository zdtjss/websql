package conn

import (
	"database/sql"
	"testing"

	"websql/internal/app/admin"
	"websql/internal/config"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// conn_repo_test.go 钉住 parent_id 的可空性：
// t_conn.parent_id 为 NULL（历史脏数据或脚本直插）时，读路径不得整行报错，
// 否则连接列表、连接基础树与角色权限树会一起变成空/500。

func setupConnRepoDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stmts := []string{
		`create table t_conn (id text primary key, db_type text, parent_id text, name text,
			user text, pwd text, url text, db_schema text, db_version text)`,
		`create table t_tree (id text primary key, label text)`,
		`insert into t_tree (id, label) values ('d1', '目录一')`,
		// 一行 parent_id 为 NULL，一行挂在 d1 下
		`insert into t_conn (id, db_type, parent_id, name, user, pwd, url, db_schema, db_version)
			values ('c-null', 'sqlite', NULL, '脏数据连接', NULL, NULL, './a.db', 'main', NULL)`,
		`insert into t_conn (id, db_type, parent_id, name, user, pwd, url, db_schema, db_version)
			values ('c-dir', 'sqlite', 'd1', '正常连接', NULL, NULL, './b.db', 'main', NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("初始化测试库失败：%v", err)
		}
	}
	return db
}

func TestConnRepo_NullParentIdIsReadable(t *testing.T) {
	db := setupConnRepoDB(t)
	repo := NewConnRepo(db)

	base, err := repo.FindConnBaseList()
	if err != nil {
		t.Fatalf("FindConnBaseList 不应因 parent_id 为 NULL 失败：%v", err)
	}
	if len(base) != 2 {
		t.Fatalf("应读到 2 条连接，实际 %d", len(base))
	}
	for _, c := range base {
		if c.Id == "c-null" && c.ParentId.String() != "" {
			t.Fatalf("NULL parent_id 应归一为空串，实际 %q", c.ParentId.String())
		}
		if c.Id == "c-dir" && c.ParentId.String() != "d1" {
			t.Fatalf("parent_id 应原样读出，实际 %q", c.ParentId.String())
		}
	}

	list, total, err := repo.FindConnList2("", "", 10, 0)
	if err != nil {
		t.Fatalf("FindConnList2 不应因 parent_id 为 NULL 失败：%v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("连接列表应返回 2 条，实际 total=%d list=%d", total, len(list))
	}

	one, err := repo.FindConnById("c-null")
	if err != nil || len(one) != 1 {
		t.Fatalf("FindConnById 失败：err=%v len=%d", err, len(one))
	}
	withParent, err := repo.FindConnByIdWithParent("c-dir")
	if err != nil || len(withParent) != 1 {
		t.Fatalf("FindConnByIdWithParent 失败：err=%v len=%d", err, len(withParent))
	}
	if len(withParent) == 1 && withParent[0].ParentName == nil {
		t.Fatal("join 出的目录名应可读（parent_name 不应丢失）")
	}

	// 权限上下文缺失时必须 fail-closed（0 条）而不是 panic（历史实现会 nil 解引用）。
	original := config.Get()
	config.SetActive(&config.Config{IsRemote: true})
	t.Cleanup(func() { config.SetActive(original) })

	noPower, err := repo.FindConnList("noneParent", nil)
	if err != nil {
		t.Fatalf("FindConnList(nil userPower) 不应报错：%v", err)
	}
	if len(noPower) != 0 {
		t.Fatalf("缺少权限上下文时不得返回任何连接，实际 %d 条", len(noPower))
	}

	// 「无父级」过滤条件同时覆盖 NULL 与空串两种历史写法
	power := &admin.UserPower{Power: []string{"c-null", "c-dir"}}
	noneParent, err := repo.FindConnList("noneParent", power)
	if err != nil {
		t.Fatalf("FindConnList(noneParent) 失败：%v", err)
	}
	if len(noneParent) != 1 || noneParent[0].Id != "c-null" {
		t.Fatalf("NULL parent_id 应被归类为无父级连接，实际 %+v", idsOf(noneParent))
	}
}

func TestNullStrScan(t *testing.T) {
	var n NullStr
	if err := n.Scan(nil); err != nil || n != "" {
		t.Fatalf("NULL 应归一为空串：%v %q", err, string(n))
	}
	if err := n.Scan([]byte("d1")); err != nil || n != "d1" {
		t.Fatalf("字节串应可读：%v %q", err, string(n))
	}
	if err := n.Scan(sql.NullString{}); err != nil || n != "" {
		t.Fatalf("无效 NullString 应归一为空串：%v %q", err, string(n))
	}
	if err := n.Scan(sql.NullString{String: "d2", Valid: true}); err != nil || n != "d2" {
		t.Fatalf("有效 NullString 应可读：%v %q", err, string(n))
	}
	if err := n.Scan(123); err == nil {
		t.Fatal("不支持的类型必须报错，不能静默当作空值")
	}
}

func idsOf(list []ConnCfg) []string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.Id)
	}
	return out
}
