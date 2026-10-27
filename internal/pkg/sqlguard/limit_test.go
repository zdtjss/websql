package sqlguard

import "testing"

// TestHasTopLevelLimit 覆盖执行时「是否还需要追加行数上限」的判断。
// 回归缺陷 D6：多行 LIMIT 被漏判后追加第二个 LIMIT，导致语法错误。
func TestHasTopLevelLimit(t *testing.T) {
	limited := []string{
		"select * from t limit 10",
		"select * from t LIMIT 10",
		"select * from t\nLIMIT 10",         // 原实现漏判
		"select * from t\n  limit 10;",      // 原实现漏判（附带分号）
		"SELECT * FROM t LIMIT 10 OFFSET 5", // 带偏移
		"select 1 fetch first 10 rows only",
		"select top 10 * from t",
		"select * from t limit ?",
	}
	for _, s := range limited {
		if !HasTopLevelLimit(s) {
			t.Errorf("应判定为已有限制: %q", s)
		}
	}

	unlimited := []string{
		"select * from t",
		"select * from t where name = 'limit 10'",   // 字符串内的关键字不算
		"select * from t -- limit 10",               // 注释内的关键字不算
		"select * from (select * from t limit 5) x", // 子查询的 LIMIT 不限制外层
		"select limit_no from t",                    // 列名前缀不该误判
		"",
	}
	for _, s := range unlimited {
		if HasTopLevelLimit(s) {
			t.Errorf("应判定为未限制: %q", s)
		}
	}
}
