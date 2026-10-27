package dialect

import "testing"

// TestWrapPage 分页包装：各方言都要能表达 limit + offset。
func TestWrapPage(t *testing.T) {
	cases := []struct {
		name   string
		dbType string
		sql    string
		offset int
		want   string
		params []any
	}{
		{
			name:   "mysql_offset_0",
			dbType: "mysql",
			sql:    "select * from t",
			offset: 0,
			want:   "select * from t LIMIT ? OFFSET ?",
			params: []any{200, 0},
		},
		{
			name:   "mysql_strip_semicolon",
			dbType: "mysql",
			sql:    "select * from t;",
			offset: 400,
			want:   "select * from t LIMIT ? OFFSET ?",
			params: []any{200, 400},
		},
		{
			name:   "sqlite",
			dbType: "sqlite",
			sql:    "select 1",
			offset: 0,
			want:   "select 1 LIMIT ? OFFSET ?",
			params: []any{200, 0},
		},
		{
			name:   "oracle_rownum",
			dbType: "oracle",
			sql:    "select * from t",
			offset: 400,
			want:   "SELECT * FROM (SELECT a.*, ROWNUM rn FROM (select * from t) a WHERE ROWNUM <= ?) WHERE rn > ?",
			params: []any{600, 400},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, params := WrapPage(tc.dbType, tc.sql, 200, tc.offset)
			if got != tc.want {
				t.Fatalf("SQL 不符\n got: %s\nwant: %s", got, tc.want)
			}
			if len(params) != len(tc.params) {
				t.Fatalf("参数数量不符，got=%v want=%v", params, tc.params)
			}
			for i := range params {
				if params[i] != tc.params[i] {
					t.Errorf("参数[%d] 期望 %v，实际 %v", i, tc.params[i], params[i])
				}
			}
		})
	}
}

func TestSupportsPaging(t *testing.T) {
	for _, db := range []string{"mysql", "mariadb", "sqlite", "oracle"} {
		if !SupportsPaging(db) {
			t.Errorf("%s 应支持分页", db)
		}
	}
	if SupportsPaging("postgres") {
		t.Error("未支持的方言应返回 false，避免拼出错误 SQL")
	}
}
