package permission

import "testing"

// ──────────────────────────────────────────────────────────────
// 经典路径 SQL 分类与 fail-closed 门禁回归测试
//
// 修复前的绕过：
//  1. `WITH x AS (...) DELETE FROM t` 被归为 SELECT（allow_modify 校验被跳过，
//     只读角色可借 SQL 编辑器/导出接口写库）；
//  2. `WITH/*c*/x AS (...)`、`DELETE/*c*/FROM` 因注释插在关键字中间被归为 UNKNOWN，
//     表级与写权限校验整体被跳过。
// ──────────────────────────────────────────────────────────────

func TestAnalyzeSQL_CTEWriteClassification(t *testing.T) {
	cases := []struct {
		name      string
		sql       string
		wantOp    string
		wantWrite bool // true 表示必须要求「数据修改权限」
	}{
		{"CTE DELETE 归为写", "WITH x AS (SELECT 1) DELETE FROM t", "DELETE", true},
		{"CTE DELETE 注释变体", "WITH/*c*/x AS (SELECT 1) DELETE FROM t", "DELETE", true},
		{"DELETE 注释变体", "DELETE/*c*/FROM t", "DELETE", true},
		{"DELETE 制表符变体", "DELETE\tFROM t", "DELETE", true},
		{"UPDATE 制表符变体", "UPDATE\tusers SET a=1", "UPDATE", true},
		{"CTE UPDATE 归为写", "WITH x AS (SELECT 1) UPDATE t SET a=1", "UPDATE", true},
		{"CTE INSERT 归为写", "WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x", "INSERT", true},
		{"SELECT 只读", "SELECT * FROM t", "SELECT", false},
		{"CTE SELECT 只读", "WITH x AS (SELECT 1) SELECT * FROM x", "SELECT", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := AnalyzeSQL(tc.sql, "s1")
			if a.OperationType != tc.wantOp {
				t.Errorf("OperationType = %q, want %q（sql=%q）", a.OperationType, tc.wantOp, tc.sql)
			}
			if got := requiresModifyPermission(a); got != tc.wantWrite {
				t.Errorf("requiresModifyPermission = %v, want %v（sql=%q）", got, tc.wantWrite, tc.sql)
			}
		})
	}
}

// TestRequiresModifyPermission_FailClosed 非只读白名单语句一律要求修改权限
func TestRequiresModifyPermission_FailClosed(t *testing.T) {
	for _, sql := range []string{
		"SET GLOBAL general_log=1",
		"CALL some_proc()",
		"GRANT SELECT ON t TO u",
		"BEGIN",
		"PRAGMA journal_mode=WAL",
		"/* 只有注释 */",
		"",
	} {
		if !requiresModifyPermission(AnalyzeSQL(sql, "s1")) {
			t.Errorf("%q 应 fail-closed 要求数据修改权限", sql)
		}
	}

	for _, sql := range []string{"SELECT 1", "SHOW TABLES", "EXPLAIN SELECT 1", "DESC t"} {
		if requiresModifyPermission(AnalyzeSQL(sql, "s1")) {
			t.Errorf("%q 为明确只读语句，不应要求数据修改权限", sql)
		}
	}

	if !requiresModifyPermission(nil) {
		t.Error("nil 分析应 fail-closed 要求修改权限")
	}
}

// TestExtractTablesFromSQL_CommentVariant 注释变体仍须能提取写入目标表（表级校验不得被跳过）
func TestExtractTablesFromSQL_CommentVariant(t *testing.T) {
	for _, sql := range []string{
		"WITH/*c*/x AS (SELECT 1) DELETE FROM t_secret",
		"DELETE/*c*/FROM t_secret",
	} {
		found := false
		for _, tb := range ExtractTablesFromSQL(sql) {
			if tb == "t_secret" {
				found = true
			}
		}
		if !found {
			t.Errorf("应提取到写入目标表 t_secret（sql=%q），实际 %v", sql, ExtractTablesFromSQL(sql))
		}
	}
}