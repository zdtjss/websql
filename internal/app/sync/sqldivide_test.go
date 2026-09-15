package sync

import (
	"reflect"
	"testing"
)

func TestSplitSQLStatements(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "基本切分",
			input: "INSERT INTO t VALUES (1);\nUPDATE t SET a=1 WHERE id=2;\n",
			want:  []string{"INSERT INTO t VALUES (1)", "UPDATE t SET a=1 WHERE id=2"},
		},
		{
			name:  "字符串内含分号不被切分",
			input: "INSERT INTO `t` (`a`) VALUES ('hello;world');\nDELETE FROM `t` WHERE `id` = 1;",
			want: []string{
				"INSERT INTO `t` (`a`) VALUES ('hello;world')",
				"DELETE FROM `t` WHERE `id` = 1",
			},
		},
		{
			name:  "转义单引号与分号共存",
			input: "INSERT INTO t (a) VALUES ('it''s; ok');",
			want:  []string{"INSERT INTO t (a) VALUES ('it''s; ok')"},
		},
		{
			name:  "行注释中的分号",
			input: "-- note; not a separator\nINSERT INTO t VALUES (1);",
			want:  []string{"-- note; not a separator\nINSERT INTO t VALUES (1)"},
		},
		{
			name:  "块注释中的分号",
			input: "/* a; b */ INSERT INTO t VALUES (1); INSERT INTO t VALUES (2);",
			want:  []string{"/* a; b */ INSERT INTO t VALUES (1)", "INSERT INTO t VALUES (2)"},
		},
		{
			name:  "空输入与多余分号",
			input: ";;  ;",
			want:  []string{},
		},
		{
			name:  "末尾无分号",
			input: "DELETE FROM t WHERE id = 3",
			want:  []string{"DELETE FROM t WHERE id = 3"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSQLStatements(tc.input)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitSQLStatements() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
