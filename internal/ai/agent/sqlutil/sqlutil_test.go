package sqlutil

import "testing"

func TestStripSQLComments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "无注释原样保留",
			in:   "SELECT id, name FROM users WHERE status = 1",
			want: "SELECT id, name FROM users WHERE status = 1",
		},
		{
			name: "行注释 -- 整行去掉",
			in:   "SELECT id FROM users -- this is comment\nWHERE age > 18",
			want: "SELECT id FROM users \nWHERE age > 18",
		},
		{
			name: "行注释 # 整行去掉",
			in:   "SELECT id FROM users # mysql comment\nWHERE age > 18",
			want: "SELECT id FROM users \nWHERE age > 18",
		},
		{
			name: "块注释 /* */ 跨行",
			in:   "SELECT id /* col1 */ FROM /* table */ users",
			want: "SELECT id   FROM   users",
		},
		{
			name: "块注释跨越多行",
			in:   "SELECT id\n/*\n  this is a\n  multi-line comment\n*/\nFROM users",
			want: "SELECT id\n \nFROM users",
		},
		{
			name: "块注释未闭合视为全部是注释",
			in:   "SELECT id /* unterminated",
			want: "SELECT id",
		},
		{
			name: "单引号字符串内 -- 不视为注释",
			in:   "SELECT * FROM users WHERE name = 'a--b'",
			want: "SELECT * FROM users WHERE name = 'a--b'",
		},
		{
			name: "单引号字符串内 # 不视为注释",
			in:   "SELECT * FROM users WHERE name = 'a#b'",
			want: "SELECT * FROM users WHERE name = 'a#b'",
		},
		{
			name: "单引号字符串内 /* 不视为注释",
			in:   "SELECT * FROM users WHERE name = 'a/*b'",
			want: "SELECT * FROM users WHERE name = 'a/*b'",
		},
		{
			name: "单引号转义 '' 正确处理",
			in:   "SELECT 'it''s ok' FROM dual",
			want: "SELECT 'it''s ok' FROM dual",
		},
		{
			name: "双引号标识符内 # 不视为注释",
			in:   `SELECT "col#name" FROM "users"`,
			want: `SELECT "col#name" FROM "users"`,
		},
		{
			name: "MySQL 优化器提示 /*+ */ 按块注释处理（项目策略：剥离所有块注释）",
			in:   "SELECT /*+ INDEX(users idx_name) */ * FROM users",
			want: "SELECT   * FROM users",
		},
		{
			name: "关键字 from 出现在注释里不应当被识别为表名（合作方需 StripSQLComments 后再提取表名）",
			in:   "SELECT id FROM /* from_logs */ users",
			want: "SELECT id FROM   users",
		},
		{
			name: "空字符串",
			in:   "",
			want: "",
		},
		{
			name: "全注释",
			in:   "-- this is a comment\n# another comment\n/* block */",
			want: "",
		},
		{
			name: "前后空白被 Trim",
			in:   "   \n\t  SELECT 1   \t\n",
			want: "SELECT 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSQLComments(tt.in)
			if got != tt.want {
				t.Errorf("\n in:   %q\n want: %q\n got:  %q", tt.in, tt.want, got)
			}
		})
	}
}

func TestContainsMultipleStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		// ─── 安全：应检测为多语句（返回 true）───
		{
			name: "经典注入: SELECT + DROP",
			sql:  "SELECT * FROM users; DROP TABLE users",
			want: true,
		},
		{
			name: "经典注入: SELECT + DELETE",
			sql:  "SELECT 1; DELETE FROM users",
			want: true,
		},
		{
			name: "换行分隔的多语句",
			sql:  "SELECT 1;\nDELETE FROM users",
			want: true,
		},
		{
			name: "多条 SELECT 也不允许",
			sql:  "SELECT 1; SELECT 2",
			want: true,
		},
		{
			name: "分号后有空格再跟语句",
			sql:  "SELECT * FROM t1;   INSERT INTO t2 VALUES(1)",
			want: true,
		},
		{
			name: "三条语句",
			sql:  "SELECT 1; SELECT 2; DROP TABLE t",
			want: true,
		},

		// ─── 正常：不是多语句（返回 false）───
		{
			name: "普通 SELECT 无分号",
			sql:  "SELECT * FROM users WHERE id = 1",
			want: false,
		},
		{
			name: "尾部分号",
			sql:  "SELECT * FROM users;",
			want: false,
		},
		{
			name: "尾部分号+空白",
			sql:  "SELECT * FROM users;   ",
			want: false,
		},
		{
			name: "尾部多个分号",
			sql:  "SELECT * FROM users;;;",
			want: false,
		},
		{
			name: "字符串内含分号",
			sql:  "SELECT * FROM users WHERE name = 'a;b;c'",
			want: false,
		},
		{
			name: "双引号标识符内含分号",
			sql:  `SELECT "col;name" FROM users`,
			want: false,
		},
		{
			name: "注释中含分号（行注释）",
			sql:  "SELECT * FROM users -- ; DROP TABLE users",
			want: false,
		},
		{
			name: "注释中含分号（块注释）",
			sql:  "SELECT * FROM users /* ; DROP TABLE users */",
			want: false,
		},
		{
			name: "分号后只有注释",
			sql:  "SELECT * FROM users; -- just a comment",
			want: false,
		},
		{
			name: "分号后只有块注释",
			sql:  "SELECT * FROM users; /* nothing here */",
			want: false,
		},
		{
			name: "空字符串",
			sql:  "",
			want: false,
		},
		{
			name: "复杂 WHERE 字符串含分号",
			sql:  "SELECT * FROM logs WHERE message LIKE '%;error;%'",
			want: false,
		},
		{
			name: "Oracle 存储过程语法不在 query_data 范畴但函数单语句",
			sql:  "SELECT my_func('param;1') FROM dual",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsMultipleStatements(tt.sql)
			if got != tt.want {
				t.Errorf("ContainsMultipleStatements(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}
