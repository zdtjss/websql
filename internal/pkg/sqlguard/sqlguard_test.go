package sqlguard

import (
	"strings"
	"testing"
)

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
