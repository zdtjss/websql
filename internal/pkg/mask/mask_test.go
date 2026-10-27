package mask

import (
	"strings"
	"testing"
)

func TestMaskDSN(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "mysql_dsn",
			in:   "root:S3cret!@tcp(10.0.0.1:3306)/demo?charset=utf8mb4",
			want: "root:***@tcp(10.0.0.1:3306)/demo?charset=utf8mb4",
		},
		{
			name: "oracle_url",
			in:   "oracle://scott:tiger@10.0.0.2:1521/ORCL",
			want: "oracle://scott:***@10.0.0.2:1521/ORCL",
		},
		{
			name: "key_value",
			in:   "host=10.0.0.3 user=root password=Abc123 db=demo",
			want: "host=10.0.0.3 user=root password=*** db=demo",
		},
		{
			name: "pwd_lowercase",
			in:   "pwd=Abc123",
			want: "pwd=***",
		},
		{
			name: "sqlite_path",
			in:   "./nway.sqlite3.db",
			want: "./nway.sqlite3.db",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaskDSN(tc.in)
			if got != tc.want {
				t.Fatalf("MaskDSN(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "S3cret!") || strings.Contains(got, "tiger") || strings.Contains(got, "Abc123") {
				t.Fatalf("口令未被脱敏: %q", got)
			}
		})
	}
}

func TestMaskToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"short", "***"},
		{"sk-1234567890abcdef", "sk-1***cdef"},
	}
	for _, tc := range cases {
		if got := MaskToken(tc.in); got != tc.want {
			t.Errorf("MaskToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
