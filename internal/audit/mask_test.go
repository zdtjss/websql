package audit

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMaskSQL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "无字面量时保持原样",
			in:   "SELECT id, name FROM users WHERE id = 1",
			want: "SELECT id, name FROM users WHERE id = 1",
		},
		{
			name: "字符串字面量内容脱敏",
			in:   "SELECT * FROM users WHERE phone = '13800138000'",
			want: "SELECT * FROM users WHERE phone = '***'",
		},
		{
			name: "单引号转义不破坏闭合",
			in:   "SELECT * FROM users WHERE name = 'O''Brien' AND id = 1",
			want: "SELECT * FROM users WHERE name = '***' AND id = 1",
		},
		{
			name: "多个字面量",
			in:   "INSERT INTO t (name, idcard) VALUES ('张三', '110101199001011234')",
			want: "INSERT INTO t (name, idcard) VALUES ('***', '***')",
		},
		{
			name: "-- 行注释内的引号不会吞掉后续字面量",
			in:   "SELECT 1 -- 备注：'13800138000'\nFROM t WHERE phone = '13800138000'",
			want: "SELECT 1 -- 备注：'13800138000'\nFROM t WHERE phone = '***'",
		},
		{
			name: "# 行注释内的引号不会吞掉后续字面量",
			in:   "SELECT 1 # '13800138000'\nFROM t WHERE a = 'x'",
			want: "SELECT 1 # '13800138000'\nFROM t WHERE a = '***'",
		},
		{
			name: "/* */ 块注释内的引号不会吞掉后续字面量",
			in:   "SELECT /* 过滤 '13800138000' */ * FROM t WHERE a = 'secret'",
			want: "SELECT /* 过滤 '13800138000' */ * FROM t WHERE a = '***'",
		},
		{
			name: "注释后仍继续脱敏",
			in:   "-- 统计\nSELECT count(*) FROM t WHERE id = '1001'",
			want: "-- 统计\nSELECT count(*) FROM t WHERE id = '***'",
		},
		{
			name: "空串",
			in:   "",
			want: "",
		},
		{
			name: "未闭合引号按失配处理（fail-closed）",
			in:   "SELECT 'abc",
			want: "SELECT '***'",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MaskSQL(c.in); got != c.want {
				t.Fatalf("MaskSQL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestMaskSQLTruncate(t *testing.T) {
	long := strings.Repeat("SELECT 1 FROM t; ", 300) // 5700 字符，无字面量
	got := MaskSQL(long)

	if !strings.HasSuffix(got, maskSQLTruncatedSuffix) {
		t.Fatalf("超长 SQL 应追加 %q，实际结尾: %q", maskSQLTruncatedSuffix, tail(got, 32))
	}
	wantLen := maskSQLMaxLen + len([]rune(maskSQLTruncatedSuffix))
	if n := len([]rune(got)); n != wantLen {
		t.Fatalf("截断后字符数 = %d, want %d", n, wantLen)
	}
	if want := long[:maskSQLMaxLen] + maskSQLTruncatedSuffix; got != want {
		t.Fatalf("截断内容不符合预期: %q", tail(got, 32))
	}
}

// TestMaskSQLTruncateMultibyte 校验按 rune 截断，不会切出非法 UTF-8。
func TestMaskSQLTruncateMultibyte(t *testing.T) {
	got := MaskSQL(strings.Repeat("数", 5000))
	if !utf8.ValidString(got) {
		t.Fatal("截断结果不是合法 UTF-8")
	}
	wantLen := maskSQLMaxLen + len([]rune(maskSQLTruncatedSuffix))
	if n := len([]rune(got)); n != wantLen {
		t.Fatalf("截断后字符数 = %d, want %d", n, wantLen)
	}
}

// TestMaskSQLIdempotent 校验重复脱敏（Record 与落库两处边界都会调用）结果稳定。
func TestMaskSQLIdempotent(t *testing.T) {
	in := "SELECT * FROM t WHERE a = '13800138000' AND b = 'O''Brien'"
	once := MaskSQL(in)
	if twice := MaskSQL(once); twice != once {
		t.Fatalf("幂等性被破坏: once=%q twice=%q", once, twice)
	}
}

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}