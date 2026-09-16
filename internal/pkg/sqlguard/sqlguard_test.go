package sqlguard

import (
	"strings"
	"testing"
)

// ──────────────────────────────────────────────────────────────
// SQL 语句分类回归测试
//
// 覆盖历史上导致「权限校验被跳过」的两类绕过：
//  1. 「关键字+空格」前缀匹配：`DELETE\tFROM` / `DELETE\nFROM` 不命中
//  2. 关键字中插入注释：`DELETE/*c*/FROM`、`WITH/*c*/x AS (...) DELETE` 无法识别首关键字
// ──────────────────────────────────────────────────────────────

func TestIsWriteStatement_CommentAndWhitespaceBypass(t *testing.T) {
	writeCases := []string{
		"WITH x AS (SELECT 1) DELETE FROM t",
		"WITH/*c*/x AS (SELECT 1) DELETE FROM t",
		"WITH x AS (SELECT 1) UPDATE t SET a=1",
		"WITH RECURSIVE r AS (SELECT 1) INSERT INTO t SELECT * FROM r",
		"DELETE/*c*/FROM t",
		"DELETE\tFROM t",
		"DELETE\nFROM t",
		"update  users set a=1",
		"DROP/*c*/TABLE t",
		"CREATE/*c*/TABLE t (a int)",
		"TRUNCATE\tTABLE t",
	}
	for _, sql := range writeCases {
		if !IsWriteStatement(sql) {
			t.Errorf("IsWriteStatement(%q) = false，写操作识别被绕过", sql)
		}
		if IsReadOnlyStatement(sql) {
			t.Errorf("IsReadOnlyStatement(%q) = true，写操作不得判定为只读", sql)
		}
	}

	readCases := []string{
		"SELECT * FROM t",
		"select/**/1",
		"SHOW TABLES",
		"DESCRIBE t",
		"DESC t",
		"EXPLAIN SELECT 1",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"-- c\nSELECT 1",
	}
	for _, sql := range readCases {
		if IsWriteStatement(sql) {
			t.Errorf("IsWriteStatement(%q) = true，只读语句被误判为写操作", sql)
		}
		if !IsReadOnlyStatement(sql) {
			t.Errorf("IsReadOnlyStatement(%q) = false，明确只读语句应被判定为只读", sql)
		}
	}
}

// TestIsReadOnlyStatement_FailClosed 白名单之外的语句（含无法识别者）不得判定为只读
func TestIsReadOnlyStatement_FailClosed(t *testing.T) {
	for _, sql := range []string{
		"SET GLOBAL general_log=1",
		"USE other_db",
		"PRAGMA journal_mode=WAL",
		"CALL some_proc()",
		"GRANT SELECT ON t TO u",
		"BEGIN",
		"/* 只有注释 */",
		"",
	} {
		if IsReadOnlyStatement(sql) {
			t.Errorf("IsReadOnlyStatement(%q) = true，应 fail-closed 判定为非只读", sql)
		}
	}
}

func TestWriteKind_CTE(t *testing.T) {
	cases := map[string]string{
		"WITH x AS (SELECT 1) DELETE FROM t":     "DELETE",
		"WITH/*c*/x AS (SELECT 1) UPDATE t SET a=1": "UPDATE",
		"WITH x AS (SELECT 1) SELECT * FROM x":   "WITH",
		"DELETE\tFROM t":                         "DELETE",
		"SELECT 1":                               "SELECT",
	}
	for sql, want := range cases {
		if got := WriteKind(sql); got != want {
			t.Errorf("WriteKind(%q) = %q, want %q", sql, got, want)
		}
	}
}

func TestFirstKeyword_CommentBypass(t *testing.T) {
	cases := map[string]string{
		"WITH/*c*/x AS (SELECT 1) DELETE FROM t": "WITH",
		"DELETE/*c*/FROM t":                      "DELETE",
		"-- c\nUPDATE t SET a=1":                 "UPDATE",
		"/* c */\tDROP\tTABLE t":                 "DROP",
		"   ":                                    "",
	}
	for sql, want := range cases {
		if got := FirstKeyword(sql); got != want {
			t.Errorf("FirstKeyword(%q) = %q, want %q", sql, got, want)
		}
	}
}

func TestStripComments_PreservesLiterals(t *testing.T) {
	// 字符串字面量内的 -- / # 不得被当作注释剥离（历史正则实现的缺陷）
	got := StripComments("SELECT 'a--b', 'c#d' FROM t -- 真实注释")
	want := "SELECT 'a--b', 'c#d' FROM t"
	if got != want {
		t.Errorf("StripComments() = %q, want %q", got, want)
	}
}

func TestValidateSchemaSQL(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		wantErr string // 空表示期望通过；非空表示期望错误包含该子串
	}{
		{
			name: "ALTER TABLE 允许",
			sql:  "ALTER TABLE t ADD COLUMN c INT",
		},
		{
			name: "CREATE TABLE 允许",
			sql:  "CREATE TABLE `base_employee` (`id` bigint NOT NULL AUTO_INCREMENT, PRIMARY KEY (`id`)) ENGINE=InnoDB",
		},
		{
			name: "CREATE TABLE 注释含 DROP TABLE 字样不误拦",
			sql:  "CREATE TABLE t (`a` varchar(10) COMMENT '执行 DROP TABLE 前的备份' )",
		},
		{
			name:    "CREATE TABLE 含 INTO OUTFILE 仍拦截",
			sql:     "CREATE TABLE t (a varchar(10)) INTO OUTFILE '/tmp/x'",
			wantErr: "危险操作",
		},
		{
			name:    "DROP TABLE 不允许",
			sql:     "DROP TABLE t",
			wantErr: "不允许执行的SQL类型",
		},
		{
			name:    "ALTER TABLE 含 TRUNCATE 拦截",
			sql:     "ALTER TABLE t COMMENT 'x'; TRUNCATE TABLE t",
			wantErr: "危险操作",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSchemaSQL(tc.sql)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateSchemaSQL() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("ValidateSchemaSQL() expected error containing %q, got nil", tc.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ValidateSchemaSQL() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
