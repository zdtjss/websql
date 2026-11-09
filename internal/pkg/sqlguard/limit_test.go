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

// TestHasTopLevelRowNumLimit 覆盖 Oracle 行数上限的「是否已有限制」判断：
// 只认顶层且能限制行数的 ROWNUM 上界谓词，避免子查询/ROWNUM > 0 跳过安全上限。
func TestHasTopLevelRowNumLimit(t *testing.T) {
	limited := []string{
		"SELECT * FROM t WHERE ROWNUM <= 5",
		"SELECT * FROM t WHERE ROWNUM < 5",
		"SELECT * FROM t WHERE ROWNUM = 1",
		"SELECT * FROM t WHERE ROWNUM between 1 and 5",
		"SELECT * FROM (SELECT * FROM t) x WHERE ROWNUM <= 10",
	}
	for _, s := range limited {
		if !HasTopLevelRowNumLimit(s) {
			t.Errorf("应判定为已有 ROWNUM 上界: %q", s)
		}
	}

	unlimited := []string{
		"",
		"SELECT * FROM t",
		"SELECT ROWNUM FROM t",
		"SELECT ROWNUM AS rn FROM t",
		"SELECT * FROM t WHERE ROWNUM > 0",
		"SELECT * FROM t WHERE ROWNUM >= 1",
		"SELECT * FROM (SELECT * FROM t WHERE ROWNUM <= 5) x",
		"SELECT * FROM t WHERE id IN (SELECT id FROM x WHERE ROWNUM <= 5)",
		"SELECT * FROM t WHERE name = 'ROWNUM <= 5'",
		"SELECT * FROM t -- ROWNUM <= 5",
		"SELECT * FROM t /* ROWNUM <= 5 */",
	}
	for _, s := range unlimited {
		if HasTopLevelRowNumLimit(s) {
			t.Errorf("应判定为没有 ROWNUM 上界: %q", s)
		}
	}
}
