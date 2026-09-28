package ai

import (
	"errors"
	"strings"
	"testing"
)

// 连接测试的错误提示必须「可自助排查但不回显原文」：
// 错误原文可能包含内网地址、请求路径、密钥前缀（SSRF 缓解措施之一）。
func TestClassifyTestConfigError_NoRawEcho(t *testing.T) {
	cases := []struct {
		err  error
		want string // 期望包含的关键字
	}{
		{errors.New(`Post "http://10.0.0.8:11434/v1/chat/completions": dial tcp 10.0.0.8:11434: connect: connection refused`), "无法连接"},
		{errors.New("error, status code: 401, message: invalid_api_key sk-abcdefghijklmn"), "认证失败"},
		{errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)"), "超时"},
		{errors.New("error, status code: 429, message: rate limit exceeded"), "限流"},
		{errors.New("some unknown failure"), "服务端日志"},
	}
	for _, c := range cases {
		got := classifyTestConfigError(c.err)
		if !strings.Contains(got, c.want) {
			t.Errorf("classifyTestConfigError(%v) = %q，应包含 %q", c.err, got, c.want)
		}
		// 关键：不得回显原始错误中的内网地址与密钥
		for _, leak := range []string{"10.0.0.8", "sk-abcdefghijklmn", "http://"} {
			if strings.Contains(got, leak) {
				t.Errorf("提示文案泄漏了原始错误内容 %q：%s", leak, got)
			}
		}
	}
}
