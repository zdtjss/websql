package sql

import (
	"testing"
	"time"
)

// TestCellTextAndCsvEscape 文本导出的基础转义：NULL 空串、含逗号/引号/换行需加引号。
func TestCellTextAndCsvEscape(t *testing.T) {
	if got := cellText(nil); got != "" {
		t.Errorf("NULL 应输出空串，实际 %q", got)
	}
	if got := cellText([]byte("abc")); got != "abc" {
		t.Errorf("[]byte 应转字符串，实际 %q", got)
	}
	if got := cellText(42); got != "42" {
		t.Errorf("数字应转文本，实际 %q", got)
	}
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	if got := cellText(ts); got != "2026-01-02 03:04:05" {
		t.Errorf("时间格式不符: %q", got)
	}

	cases := map[string]string{
		"plain":        "plain",
		"a,b":          `"a,b"`,
		`say "hi"`:     `"say ""hi"""`,
		"line1\nline2": "\"line1\nline2\"",
		"":             "",
	}
	for in, want := range cases {
		if got := csvEscape(in); got != want {
			t.Errorf("csvEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCsvLine CSV 行拼接：列之间用逗号，NULL 输出空字段。
func TestCsvLine(t *testing.T) {
	line := csvLine([]any{"a", nil, 1, "x,y"})
	if line != `a,,1,"x,y"` {
		t.Fatalf("CSV 行不符: %q", line)
	}
}

// TestExportLiteral SQL 字面量：NULL/数字/布尔原样，字符串按方言转义。
func TestExportLiteral(t *testing.T) {
	cases := []struct {
		name   string
		val    any
		dbType string
		want   string
	}{
		{"NULL", nil, "mysql", "NULL"},
		{"整数", 42, "mysql", "42"},
		{"浮点", 1.5, "mysql", "1.5"},
		{"布尔真", true, "mysql", "1"},
		{"布尔假", false, "mysql", "0"},
		{"单引号转义", "it's", "mysql", "'it''s'"},
		{"MySQL 反斜杠转义", `a\b`, "mysql", `'a\\b'`},
		{"Oracle 不转义反斜杠", `a\b`, "oracle", `'a\b'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exportLiteral(tc.val, tc.dbType); got != tc.want {
				t.Fatalf("期望 %q，实际 %q", tc.want, got)
			}
		})
	}
}

// TestSQLInsertLine 生成 INSERT 语句：标识符按方言引用。
func TestSQLInsertLine(t *testing.T) {
	line := sqlInsertLine("t_user", []string{"id", "name"}, []any{1, "O'Brien"}, "mysql")
	want := "INSERT INTO `t_user` (`id`, `name`) VALUES (1, 'O''Brien');"
	if line != want {
		t.Fatalf("INSERT 语句不符\n got: %s\nwant: %s", line, want)
	}

	oraLine := sqlInsertLine("t_user", []string{"id"}, []any{1}, "oracle")
	if oraLine != `INSERT INTO "t_user" ("id") VALUES (1);` {
		t.Fatalf("Oracle INSERT 不符: %s", oraLine)
	}
}

// TestExportContentType 各格式的响应类型与扩展名。
func TestExportContentType(t *testing.T) {
	cases := map[string][2]string{
		"csv":  {"text/csv; charset=utf-8", "csv"},
		"json": {"application/json; charset=utf-8", "json"},
		"sql":  {"text/plain; charset=utf-8", "sql"},
	}
	for format, want := range cases {
		ct, ext := exportContentType(format)
		if ct != want[0] || ext != want[1] {
			t.Errorf("%s: 期望 %v，实际 %s/%s", format, want, ct, ext)
		}
	}
}
