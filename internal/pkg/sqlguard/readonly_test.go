package sqlguard

import "testing"

func TestValidateReadOnlyQuery_Allows(t *testing.T) {
	cases := []string{
		"SELECT id, name FROM users WHERE status = 'DELETE' LIMIT 10",
		"select count(*) from t where note like '%update%'",
		"WITH x AS (SELECT 1 AS a) SELECT a FROM x",
		"SHOW CREATE TABLE `orders`",
		"SHOW TABLES",
		"DESCRIBE orders",
		"EXPLAIN SELECT * FROM orders WHERE id = 1",
		"SELECT REPLACE(name, 'a', 'b'), INSERT(name, 1, 2, 'x'), TRUNCATE(price, 2) FROM p",
		"SELECT `update`, `lock` FROM t",
		"SELECT \"delete\" FROM t",
		"SELECT 1 INTO @v",
		"SELECT * FROM t;",
		"SELECT '用户;删除' AS 备注 FROM 用户表",
		"SELECT q'[it's fine]' FROM dual",
		"SELECT /*+ INDEX(t idx) */ id FROM t",
	}
	for _, sql := range cases {
		if err := ValidateReadOnlyQuery(sql); err != nil {
			t.Errorf("expected allowed: %q, got %v", sql, err)
		}
	}
}

func TestValidateReadOnlyQuery_Rejects(t *testing.T) {
	cases := []string{
		"/*!50000 DELETE FROM users */",
		"SELECT 1 /*!50000 , (SELECT 1) */",
		"SELECT * FROM t INTO OUTFILE '/tmp/x'",
		"SELECT * FROM t /*!50000 INTO OUTFILE '/tmp/x' */",
		"SELECT LOAD_FILE('/etc/passwd')",
		"SELECT * FROM t FOR UPDATE",
		"SELECT * FROM t LOCK IN SHARE MODE",
		"SELECT * FROM t FOR SHARE",
		"EXPLAIN ANALYZE DELETE t FROM t JOIN u ON t.id = u.id",
		"WITH x AS (SELECT 1) DELETE FROM t",
		"SELECT 1; DROP TABLE t",
		"SELECT 1; (SELECT 2)",
		"UPDATE t SET a = 1",
		"CALL cleanup()",
		"SET @a = 1",
		"RENAME TABLE a TO b",
		"REPLACE INTO t VALUES (1)",
		"SELECT * INTO new_table FROM t",
		"SELECT q'[x']' || UTL_HTTP.REQUEST('http://e/') FROM dual",
		"SELECT 'a\\'' , 1 FROM t WHERE '' = '' ; DELETE FROM t; SELECT '",
		"SELECT load_extension('evil')",
		"",
	}
	for _, sql := range cases {
		if err := ValidateReadOnlyQuery(sql); err == nil {
			t.Errorf("expected rejected: %q", sql)
		}
	}
}

func TestRequiresApproval(t *testing.T) {
	need := []string{
		"/*!50000 DELETE FROM users */",
		"DELETE FROM users WHERE id = 1",
		"CALL p()",
		"SET GLOBAL x = 1",
		"RENAME TABLE a TO b",
		"LOCK TABLES t WRITE",
	}
	for _, sql := range need {
		if !RequiresApproval(sql) {
			t.Errorf("expected approval required: %q", sql)
		}
	}
	if RequiresApproval("SELECT 1") {
		t.Error("plain SELECT should not require approval")
	}
}

func TestHasExecutableComment(t *testing.T) {
	if !HasExecutableComment("SELECT 1 /*!40101 SET x=1 */") {
		t.Error("expected executable comment detected")
	}
	if HasExecutableComment("SELECT '/*! not a comment */'") {
		t.Error("string literal must not count as executable comment")
	}
}
