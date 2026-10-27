package monitor

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// newTestDB 返回一个内存 SQLite 连接，仅用于构造任意列集的结果集。
func newTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("打开内存 SQLite 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestParseProcessRows_ExtraColumns 复现 D8：服务端在 SHOW FULL PROCESSLIST
// 结果尾部追加扩展列时，解析必须成功而不是整批失败。
func TestParseProcessRows_ExtraColumns(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{
			// 腾讯云 CDB MySQL 开启动态线程池后的列集
			name: "tencent_cdb_thread_pool",
			query: `SELECT 1 AS "Id", 'app' AS "User", '10.0.0.1:1234' AS "Host", 'demo' AS "db",
				'Query' AS "Command", 7 AS "Time", 'Sending data' AS "State", 'SELECT 1' AS "Info",
				3 AS "Moved_to_per_thread", 1 AS "Moved_to_thread_pool"`,
		},
		{
			// MariaDB 的 SHOW FULL PROCESSLIST 带 PROGRESS 列
			name: "mariadb_progress",
			query: `SELECT 1 AS "Id", 'app' AS "User", '10.0.0.2:5678' AS "Host", 'demo' AS "db",
				'Sleep' AS "Command", 12 AS "Time", '' AS "State", NULL AS "Info",
				0 AS "Progress"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			rows, err := db.Queryx(tc.query)
			if err != nil {
				t.Fatalf("执行构造查询失败: %v", err)
			}
			defer rows.Close()

			processes, columns, err := parseProcessRows(rows)
			if err != nil {
				t.Fatalf("存在额外列时解析不应失败，实际错误: %v (columns=%v)", err, columns)
			}
			if len(processes) != 1 {
				t.Fatalf("期望 1 条会话，实际 %d 条", len(processes))
			}
			got := processes[0]
			if got.Id != 1 {
				t.Errorf("Id 期望 1，实际 %d", got.Id)
			}
			if got.User != "app" {
				t.Errorf("User 期望 app，实际 %q", got.User)
			}
			if got.Command != "Query" && got.Command != "Sleep" {
				t.Errorf("Command 解析异常: %q", got.Command)
			}
		})
	}
}

// TestParseProcessRows_MissingOptionalColumns 缺列时降级为空值，不能整体失败。
func TestParseProcessRows_MissingOptionalColumns(t *testing.T) {
	db := newTestDB(t)
	rows, err := db.Queryx(`SELECT 42 AS "Id", 'only_id_user' AS "User"`)
	if err != nil {
		t.Fatalf("执行构造查询失败: %v", err)
	}
	defer rows.Close()

	processes, _, err := parseProcessRows(rows)
	if err != nil {
		t.Fatalf("缺少可选列时不应失败，实际错误: %v", err)
	}
	if len(processes) != 1 {
		t.Fatalf("期望 1 条会话，实际 %d 条", len(processes))
	}
	got := processes[0]
	if got.Id != 42 || got.User != "only_id_user" {
		t.Errorf("必需列解析异常: %+v", got)
	}
	if got.Host != "" || got.Db != "" || got.State != "" || got.Info != "" || got.Command != "" || got.Time != 0 {
		t.Errorf("缺失列应降级为空值，实际: %+v", got)
	}
}

// TestParseProcessRows_MissingID 缺少 Id 列时必须返回错误，并回传列名供排障。
func TestParseProcessRows_MissingID(t *testing.T) {
	db := newTestDB(t)
	rows, err := db.Queryx(`SELECT 'app' AS "User", 'Sleep' AS "Command"`)
	if err != nil {
		t.Fatalf("执行构造查询失败: %v", err)
	}
	defer rows.Close()

	_, columns, err := parseProcessRows(rows)
	if err == nil {
		t.Fatal("缺少 Id 列时应返回错误")
	}
	if len(columns) != 2 {
		t.Errorf("期望回传 2 个列名用于排障，实际 %v", columns)
	}
}

// TestParseNameValueRows 服务器变量：两列正常解析，追加列时忽略而不报错。
func TestParseNameValueRows(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"plain", `SELECT 'max_connections' AS "Variable_name", '151' AS "Value"`},
		{"extra_column", `SELECT 'max_connections' AS "Variable_name", '151' AS "Value", 'x' AS "Extra"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			rows, err := db.Queryx(tc.query)
			if err != nil {
				t.Fatalf("执行构造查询失败: %v", err)
			}
			defer rows.Close()

			items, _, err := parseNameValueRows(rows, []string{"variable_name", "name"}, []string{"value"})
			if err != nil {
				t.Fatalf("解析服务器变量失败: %v", err)
			}
			if len(items) != 1 || items[0].Name != "max_connections" || items[0].Value != "151" {
				t.Fatalf("解析结果异常: %+v", items)
			}
		})
	}
}

// TestProcessListErrMessage 用户可见文案不得回显驱动原文。
func TestProcessListErrMessage(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errNil(), "获取进程列表失败"},
	}

	for _, tc := range cases {
		if got := processListErrMessage(tc.err); got != tc.want {
			t.Errorf("期望 %q，实际 %q", tc.want, got)
		}
	}

	denied := &simpleErr{"Error 1227: Access denied; you need (at least one of) the PROCESS privilege(s) for this operation"}
	if got := processListErrMessage(denied); got == denied.Error() {
		t.Errorf("不应把驱动原文回显给用户: %q", got)
	}
}

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }

func errNil() error { return nil }
