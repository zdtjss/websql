package middleware

import "testing"

func TestIsTrustedOrigin(t *testing.T) {
	cases := []struct {
		origin, host string
		want         bool
	}{
		{"", "127.0.0.1:8080", true},
		{"http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"http://localhost:5175", "localhost:9081", true}, // Vite 开发代理
		{"https://db.example.com", "db.example.com", true},
		{"https://evil.example", "127.0.0.1:8080", false},
		{"http://127.0.0.1:9999", "db.example.com", false},
		{"null", "127.0.0.1:8080", false},
	}
	for _, c := range cases {
		if got := isTrustedOrigin(c.origin, c.host); got != c.want {
			t.Errorf("isTrustedOrigin(%q, %q) = %v, want %v", c.origin, c.host, got, c.want)
		}
	}
}

func TestIsSafeLocalHost(t *testing.T) {
	safe := []string{"127.0.0.1:80", "localhost", "[::1]:8080", "192.168.1.10:80", "mypc", "mypc.local:80", ""}
	for _, h := range safe {
		if !isSafeLocalHost(h) {
			t.Errorf("expected safe host: %q", h)
		}
	}
	unsafe := []string{"evil.example.com", "rebind.attacker.net:8080"}
	for _, h := range unsafe {
		if isSafeLocalHost(h) {
			t.Errorf("expected unsafe host: %q", h)
		}
	}
}
