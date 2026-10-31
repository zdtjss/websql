package permission

import (
	"sort"
	"testing"
)

// TestExtractTablesFromSQL_UnicodeIdentifiers 中文/非 ASCII 表名必须被提取出来：
// 提取不到会让表级权限校验与“来源不明列”脱敏判定拿到空集合，既可能误伤也可能被绕过。
func TestExtractTablesFromSQL_UnicodeIdentifiers(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"中文表名", "SELECT 状态, COUNT(*) AS cnt FROM 订单 GROUP BY 状态", []string{"订单"}},
		{"中文表名带 Schema", "SELECT * FROM main.订单 WHERE 状态 = '已完成'", []string{"main.订单"}},
		{"中文多表 JOIN", "SELECT a.姓名, b.金额 FROM 客户 a JOIN 订单 b ON a.cid = b.cid", []string{"客户", "订单"}},
		{"中文逗号多表", "SELECT * FROM 客户, 订单 WHERE 客户.cid = 订单.cid", []string{"客户", "订单"}},
		{"ASCII 回归", "SELECT * FROM users JOIN orders ON users.id = orders.uid", []string{"users", "orders"}},
		{"反引号中文", "SELECT * FROM `订单`", []string{"订单"}},
		{"DESCRIBE 中文", "DESCRIBE 客户", []string{"客户"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTablesFromSQL(tc.sql)
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("提取结果不符：sql=%q got=%v want=%v", tc.sql, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("提取结果不符：sql=%q got=%v want=%v", tc.sql, got, want)
				}
			}
		})
	}
}
