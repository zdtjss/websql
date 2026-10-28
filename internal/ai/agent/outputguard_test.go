package agent

import (
	"regexp"
	"strings"
	"testing"
)

// collectGuard 把 chunk 序列喂给 guard，返回最终可见文本与错误。
func collectGuard(t *testing.T, guard *OutputGuard, chunks []string) (string, error) {
	t.Helper()
	var out strings.Builder
	for _, chunk := range chunks {
		safe, err := guard.Push(chunk)
		if err != nil {
			return out.String(), err
		}
		out.WriteString(safe)
	}
	tail, err := guard.Close()
	if err != nil {
		return out.String(), err
	}
	out.WriteString(tail)
	return out.String(), nil
}

func TestOutputGuard_MasksAcrossChunkBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		secret string
	}{
		{"phone split", []string{"联系我 138", "0013", "8000 谢谢"}, "13800138000"},
		{"email split", []string{"邮箱 zhang", "san@example", ".com 完毕"}, "zhangsan@example.com"},
		{"idcard split", []string{"身份证 110101", "19900101", "1234"}, "110101199001011234"},
		{"secret assignment", []string{"password=", "hunter2"}, "hunter2"},
		{"secret key split", []string{"api_key", " = ", "\"sk-1234567890\""}, "sk-1234567890"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guard := NewOutputGuard(NewNoopMasker(), 0)
			got, err := collectGuard(t, guard, tc.chunks)
			if err != nil {
				t.Fatalf("guard 返回错误：%v", err)
			}
			if strings.Contains(got, tc.secret) {
				t.Fatalf("敏感内容泄露：%q", got)
			}
		})
	}
}

func TestOutputGuard_SingleCharChunks(t *testing.T) {
	guard := NewOutputGuard(NewNoopMasker(), 0)
	text := "password=hunter2 结束"
	chunks := make([]string, 0, len(text))
	for _, r := range text {
		chunks = append(chunks, string(r))
	}
	got, err := collectGuard(t, guard, chunks)
	if err != nil {
		t.Fatalf("guard 返回错误：%v", err)
	}
	if strings.Contains(got, "hunter2") {
		t.Fatalf("逐字符输入仍泄露敏感值：%q", got)
	}
}

func TestOutputGuard_RedactsOverlongToken(t *testing.T) {
	guard := NewOutputGuard(NewNoopMasker(), 0)
	long := strings.Repeat("a", maxOutputTokenRunes+50)
	got, err := collectGuard(t, guard, []string{"前缀 ", long, " 后缀"})
	if err != nil {
		t.Fatalf("guard 返回错误：%v", err)
	}
	if strings.Contains(got, long) {
		t.Fatal("超长 token 应整段替换")
	}
	if !strings.Contains(got, "***") {
		t.Fatalf("应包含替换标记，实际：%q", got)
	}
}

func TestOutputGuard_OverflowFailClosed(t *testing.T) {
	guard := NewOutputGuard(NewNoopMasker(), 0)
	// 敏感键后跟超长空白：键上下文未结束，无法安全释放前缀，窗口溢出必须 fail-closed
	payload := "password" + strings.Repeat(" ", maxOutputPendingRunes+100)
	if _, err := guard.Push(payload); err == nil {
		t.Fatal("窗口溢出应返回 ErrOutputSecurity")
	}
}

func TestOutputGuard_RejectsInvalidUTF8AndDoubleClose(t *testing.T) {
	guard := NewOutputGuard(NewNoopMasker(), 0)
	if _, err := guard.Push(string([]byte{0xff, 0xfe})); err == nil {
		t.Fatal("非法 UTF-8 应被拒绝")
	}
	if _, err := guard.Close(); err != nil {
		t.Fatalf("首次 Close 不应报错：%v", err)
	}
	if _, err := guard.Close(); err == nil {
		t.Fatal("重复 Close 应返回 ErrOutputSecurity")
	}
}

func TestOutputGuard_KeepsNormalText(t *testing.T) {
	guard := NewOutputGuard(NewNoopMasker(), 0)
	input := "这是一段正常的中文回复，包含 SELECT * FROM orders 这样的说明。"
	got, err := collectGuard(t, guard, []string{input})
	if err != nil {
		t.Fatalf("guard 返回错误：%v", err)
	}
	if got != input {
		t.Fatalf("正常文本不应被改写\n got=%q\nwant=%q", got, input)
	}
}

func FuzzOutputGuard(f *testing.F) {
	f.Add("password=hunter2")
	f.Add("正常文本 13800138000")
	f.Add("{\"api_key\": \"sk-123\"}")
	// 与内置 secretRE 语义一致：敏感键后必须存在赋值分隔符，
	// 否则 pAsswordhunter2 这类拼接词不是可识别的敏感信息。
	sensitivePair := regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|password|secret)\b\s*["']?\s*[:=]\s*["']?hunter2`)
	f.Fuzz(func(t *testing.T, input string) {
		guard := NewOutputGuard(NewNoopMasker(), 0)
		got, err := collectGuard(t, guard, []string{input})
		if err != nil {
			return // fail-closed 是合法结果
		}
		if sensitivePair.MatchString(input) && strings.Contains(got, "hunter2") {
			t.Fatalf("敏感键值泄露：%q", got)
		}
	})
}
