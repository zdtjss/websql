package agent

import "testing"

func TestApplyRowLimit(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		driver   string
		maxRows  int
		want     string
		noChange bool // expect exact same string
	}{
		{
			name:    "MySQL 无 LIMIT → 追加",
			sql:     "SELECT * FROM users",
			driver:  "mysql",
			maxRows: 500,
			want:    "SELECT * FROM users LIMIT 500",
		},
		{
			name:     "MySQL 已有 LIMIT → 不追加",
			sql:      "SELECT * FROM users LIMIT 100",
			driver:   "mysql",
			maxRows:  500,
			noChange: true,
		},
		{
			name:     "MySQL LIMIT offset,count → 不追加",
			sql:      "SELECT * FROM users LIMIT 10, 20",
			driver:   "mysql",
			maxRows:  500,
			noChange: true,
		},
		{
			name:     "MySQL LIMIT count OFFSET offset → 不追加",
			sql:      "SELECT * FROM users LIMIT 20 OFFSET 10",
			driver:   "mysql",
			maxRows:  500,
			noChange: true,
		},
		{
			name:     "SQLite 已有 LIMIT OFFSET → 不追加",
			sql:      "SELECT * FROM users LIMIT 50 OFFSET 5",
			driver:   "sqlite",
			maxRows:  500,
			noChange: true,
		},
		{
			name:     "Oracle 12c+ FETCH → 不追加",
			sql:      "SELECT * FROM users ORDER BY id FETCH FIRST 50 ROWS ONLY",
			driver:   "oracle",
			maxRows:  500,
			noChange: true,
		},
		{
			name:     "Oracle 12c+ NEXT ROWS ONLY → 不追加",
			sql:      "SELECT * FROM users ORDER BY id FETCH NEXT 50 ROWS ONLY",
			driver:   "oracle",
			maxRows:  500,
			noChange: true,
		},
		{
			name:     "Oracle ROWNUM 谓词 → 不追加",
			sql:      "SELECT * FROM users WHERE ROWNUM <= 100",
			driver:   "oracle",
			maxRows:  500,
			noChange: true,
		},
		{
			name:    "Oracle 无任何 LIMIT 形式 → 追加 OFFSET/FETCH",
			sql:     "SELECT * FROM users",
			driver:  "oracle",
			maxRows: 500,
			want:    "SELECT * FROM users OFFSET 0 ROWS FETCH NEXT 500 ROWS ONLY",
		},
		{
			name:    "尾部带分号 → 去掉后再判断",
			sql:     "SELECT * FROM users;",
			driver:  "mysql",
			maxRows: 500,
			want:    "SELECT * FROM users LIMIT 500",
		},
		{
			name:    "注释里的 LIMIT 不算（应被剥注释后忽略）",
			sql:     "SELECT * FROM users -- LIMIT 999\nWHERE id > 0",
			driver:  "mysql",
			maxRows: 500,
			// 注释里的 LIMIT 999 会被 StripSQLComments 过滤掉，所以函数照常追加 LIMIT 500
			want: "SELECT * FROM users \nWHERE id > 0 LIMIT 500",
		},
		{
			name:     "MySQL 注释里 LIMIT + 真实 LIMIT → 不追加",
			sql:      "SELECT * FROM users -- LIMIT 999\nWHERE id > 0 LIMIT 10",
			driver:   "mysql",
			maxRows:  500,
			noChange: true,
		},
		{
			name:     "大写 LIMIT",
			sql:      "SELECT * FROM users LIMIT 5",
			driver:   "mysql",
			maxRows:  500,
			noChange: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyRowLimit(tt.sql, tt.driver, tt.maxRows)
			if tt.noChange {
				if got != tt.sql {
					t.Errorf("noChange expected, but got modified:\n in:  %q\n got: %q", tt.sql, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("\n in:   %q\n want: %q\n got:  %q", tt.sql, tt.want, got)
			}
		})
	}
}

func TestExtractExplicitSchemaFromSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "无 schema 前缀",
			sql:  "SELECT * FROM users",
			want: "",
		},
		{
			name: "显式 schema 前缀 (无引号)",
			sql:  "SELECT * FROM mySchema.users",
			want: "MYSCHEMA",
		},
		{
			name: "显式 schema 前缀 (反引号)",
			sql:  "SELECT * FROM `mySchema`.`users`",
			want: "MYSCHEMA",
		},
		{
			name: "显式 schema 前缀 (双引号)",
			sql:  `SELECT * FROM "mySchema"."users"`,
			want: "MYSCHEMA",
		},
		{
			name: "JOIN 也算",
			sql:  "SELECT a.* FROM orders o JOIN mySchema.products p ON o.pid = p.id",
			want: "MYSCHEMA",
		},
		{
			name: "多个表，都带 schema",
			sql:  "SELECT * FROM s1.t1 JOIN s2.t2 ON 1=1",
			want: "S1",
		},
		{
			name: "INSERT INTO 也算",
			sql:  "INSERT INTO mySchema.t1 VALUES (1)",
			want: "MYSCHEMA",
		},
		{
			name: "DELETE FROM 也算",
			sql:  "DELETE FROM mySchema.t1 WHERE id=1",
			want: "MYSCHEMA",
		},
		{
			name: "UPDATE 也算",
			sql:  "UPDATE mySchema.t1 SET c=1",
			want: "MYSCHEMA",
		},
		{
			name: "注释里的 schema 不算",
			sql:  "SELECT * FROM /* mySchema.t1 */ users",
			want: "",
		},
		{
			name: "字符串内的 . 不算",
			sql:  "SELECT * FROM users WHERE name = 'a.b.c'",
			want: "",
		},
		{
			name: "大小写",
			sql:  "select * from MySchema.MyTable",
			want: "MYSCHEMA",
		},
		{
			name: "空字符串",
			sql:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractExplicitSchemaFromSQL(tt.sql)
			if got != tt.want {
				t.Errorf("\n in:   %q\n want: %q\n got:  %q", tt.sql, tt.want, got)
			}
		})
	}
}

func TestExtractExplicitSchemaFromSQL_FalsePositives(t *testing.T) {
	// 这些复杂 SQL 不应被误识别为跨库操作
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "CTE 不应被识别为 schema",
			sql: `WITH monthly_stats AS (
				SELECT DATE_FORMAT(created_at, '%Y-%m') AS month, COUNT(*) AS cnt
				FROM orders GROUP BY month
			)
			SELECT * FROM monthly_stats WHERE cnt > 100`,
			want: "",
		},
		{
			name: "多层 CTE 不应被识别为 schema",
			sql: `WITH cte1 AS (SELECT id FROM users), cte2 AS (SELECT id FROM orders)
			SELECT * FROM cte1 JOIN cte2 ON cte1.id = cte2.id`,
			want: "",
		},
		{
			name: "子查询不应误判",
			sql:  "SELECT * FROM (SELECT id, name FROM users WHERE status = 1) AS subq JOIN orders ON subq.id = orders.user_id",
			want: "",
		},
		{
			name: "函数调用中的点号不应误判",
			sql:  "SELECT COUNT(*) FROM users WHERE DATE_FORMAT(created_at, '%Y') = '2025'",
			want: "",
		},
		{
			name: "表别名引用不应误判（alias.column 不是 schema.table）",
			sql:  "SELECT t1.name, t2.amount FROM users t1 JOIN orders t2 ON t1.id = t2.user_id",
			want: "",
		},
		{
			name: "INFORMATION_SCHEMA 应被排除",
			sql:  "SELECT * FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = 'mydb'",
			want: "",
		},
		{
			name: "系统 schema SYS 应被排除",
			sql:  "SELECT * FROM SYS.v$session",
			want: "",
		},
		{
			name: "真实的 schema 前缀仍然识别",
			sql:  "SELECT * FROM business_db.orders WHERE id > 100",
			want: "BUSINESS_DB",
		},
		{
			name: "复杂 CASE WHEN 不应误判",
			sql: `SELECT 
				CASE WHEN status = 1 THEN 'active' ELSE 'inactive' END AS status_label,
				COUNT(*) AS cnt
			FROM users GROUP BY status`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractExplicitSchemaFromSQL(tt.sql)
			if got != tt.want {
				t.Errorf("\n in:   %q\n want: %q\n got:  %q", tt.sql, tt.want, got)
			}
		})
	}
}

func TestExtractAllExplicitSchemasFromSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "无 schema",
			sql:  "SELECT * FROM users JOIN orders ON users.id = orders.user_id",
			want: nil,
		},
		{
			name: "单 schema",
			sql:  "SELECT * FROM mydb.users JOIN mydb.orders ON mydb.users.id = mydb.orders.user_id",
			want: []string{"MYDB"},
		},
		{
			name: "多个不同 schema",
			sql:  "SELECT * FROM schema_a.users JOIN schema_b.orders ON schema_a.users.id = schema_b.orders.user_id",
			want: []string{"SCHEMA_A", "SCHEMA_B"},
		},
		{
			name: "系统 schema 被排除",
			sql:  "SELECT * FROM INFORMATION_SCHEMA.TABLES",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAllExplicitSchemasFromSQL(tt.sql)
			if len(got) != len(tt.want) {
				t.Errorf("\n in:   %q\n want: %v\n got:  %v", tt.sql, tt.want, got)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("\n in:   %q\n want: %v\n got:  %v", tt.sql, tt.want, got)
					break
				}
			}
		})
	}
}
