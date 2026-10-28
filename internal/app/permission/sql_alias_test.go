package permission

import "testing"

func TestExtractTableAliases(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want map[string]string
	}{
		{
			name: "显式 AS 别名",
			sql:  "SELECT u.id FROM users AS u WHERE u.active = 1",
			want: map[string]string{"U": "users", "USERS": "users"},
		},
		{
			name: "隐式别名",
			sql:  "SELECT o.id FROM orders o JOIN users u ON o.user_id = u.id",
			want: map[string]string{"O": "orders", "USERS": "users", "U": "users", "ORDERS": "orders"},
		},
		{
			name: "schema.table",
			sql:  "SELECT a.id FROM sales.orders a",
			want: map[string]string{"A": "orders", "ORDERS": "orders", "SALES.ORDERS": "orders"},
		},
		{
			name: "反引号与双引号",
			sql:  "SELECT `o`.`id` FROM `orders` `o` JOIN \"users\" u ON 1=1",
			want: map[string]string{"O": "orders", "U": "users", "ORDERS": "orders", "USERS": "users"},
		},
		{
			name: "逗号连接多表",
			sql:  "SELECT a.x FROM t1 a, t2 b WHERE a.id = b.id",
			want: map[string]string{"A": "t1", "B": "t2", "T1": "t1", "T2": "t2"},
		},
		{
			name: "字符串与注释中的假表名",
			sql:  "SELECT id FROM orders -- FROM fake\n WHERE note = 'FROM ghost'",
			want: map[string]string{"ORDERS": "orders"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTableAliases(tc.sql)
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("alias %s = %q, want %q (all=%v)", k, got[k], v, got)
				}
			}
			for _, ghost := range []string{"FAKE", "GHOST"} {
				if _, ok := got[ghost]; ok {
					t.Fatalf("不应解析字符串/注释中的假表名：%v", got)
				}
			}
		})
	}
}

func TestResolveSelectColumns(t *testing.T) {
	t.Run("JOIN 同名列按别名解析来源", func(t *testing.T) {
		cols, complete := ResolveSelectColumns("SELECT b.id FROM a b JOIN c ON b.id = c.a_id")
		if !complete {
			t.Fatal("显式别名场景应可确定全部来源")
		}
		if len(cols) != 1 || cols[0].TableName != "a" || cols[0].ColumnName != "id" {
			t.Fatalf("解析结果错误：%+v", cols)
		}
	})

	t.Run("多表裸列来源不明", func(t *testing.T) {
		_, complete := ResolveSelectColumns("SELECT id FROM a, b")
		if complete {
			t.Fatal("多表裸列应标记来源不明")
		}
	})

	t.Run("单表裸列可确定来源", func(t *testing.T) {
		cols, complete := ResolveSelectColumns("SELECT id FROM orders")
		if !complete || cols[0].TableName != "orders" {
			t.Fatalf("单表裸列应解析到 orders，实际：%+v complete=%v", cols, complete)
		}
	})

	t.Run("跨 schema 同名列", func(t *testing.T) {
		cols, complete := ResolveSelectColumns(
			"SELECT a.id, b.id FROM sales.a a JOIN archive.b b ON a.id = b.id")
		if !complete || len(cols) != 2 {
			t.Fatalf("解析失败：%+v", cols)
		}
		if cols[0].TableName != "a" || cols[1].TableName != "b" {
			t.Fatalf("两侧来源应分别解析：%+v", cols)
		}
	})

	t.Run("SELECT * 带别名", func(t *testing.T) {
		cols, _ := ResolveSelectColumns("SELECT o.* FROM orders o")
		if len(cols) != 1 || !cols[0].IsStar || cols[0].TableName != "orders" {
			t.Fatalf("星号应解析到 orders：%+v", cols)
		}
	})

	t.Run("派生表来源不明", func(t *testing.T) {
		_, complete := ResolveSelectColumns("SELECT x.id FROM (SELECT id FROM orders) x")
		if complete {
			t.Fatal("派生表列来源应标记为不明")
		}
	})
}

func TestIsVerifiedNoResourceQuery(t *testing.T) {
	allowed := []string{
		"SELECT 1",
		"SELECT 1 FROM DUAL",
		"SELECT NOW()",
		"SELECT 'hello' AS greeting",
		"SELECT 1 + 2",
		"select current_date",
	}
	for _, sql := range allowed {
		if !IsVerifiedNoResourceQuery(sql) {
			t.Fatalf("应识别为无资源查询：%q", sql)
		}
	}

	denied := []string{
		"SELECT id FROM orders",
		"SELECT * FROM t",
		"SELECT (SELECT 1)",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT id",
		"SELECT abs(amount) FROM orders",
		"UPDATE t SET a=1",
	}
	for _, sql := range denied {
		if IsVerifiedNoResourceQuery(sql) {
			t.Fatalf("不应识别为无资源查询：%q", sql)
		}
	}
}
