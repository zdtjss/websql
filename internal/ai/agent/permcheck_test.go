package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// permcheck_test.go 覆盖「用户未批准执行」工具结果的构造：
// 理由来自客户端，必须能安全编码为 JSON，且截断不得切断多字节字符（否则前端/模型侧出现 U+FFFD）。

func TestCancelResultPayload(t *testing.T) {
	t.Run("空理由归一为默认文案", func(t *testing.T) {
		for _, in := range []string{"", "   ", "\n\t"} {
			got, err := cancelResultPayload(in)
			if err != nil {
				t.Fatalf("不应报错：%v", err)
			}
			var obj struct {
				Message      string `json:"message"`
				AffectedRows int    `json:"affectedRows"`
			}
			if err := json.Unmarshal([]byte(got), &obj); err != nil {
				t.Fatalf("结果必须是合法 JSON，实际=%s err=%v", got, err)
			}
			if obj.Message != "用户取消执行" || obj.AffectedRows != 0 {
				t.Fatalf("默认文案不正确：%+v", obj)
			}
		}
	})

	t.Run("引号反斜杠换行不会破坏 JSON 结构", func(t *testing.T) {
		hostile := `"abc" }, {"affectedRows": 99, "x": "y` + "\n" + `tail\end`
		got, err := cancelResultPayload(hostile)
		if err != nil {
			t.Fatalf("不应报错：%v", err)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(got), &obj); err != nil {
			t.Fatalf("结果必须可解析，实际=%s err=%v", got, err)
		}
		if len(obj) != 2 {
			t.Fatalf("客户端不得注入额外字段，实际键=%v（%s）", payloadKeys(obj), got)
		}
		if obj["message"] != hostile {
			t.Fatalf("message 应原样保留为字符串，实际=%v", obj["message"])
		}
		if rows, ok := obj["affectedRows"].(float64); !ok || rows != 0 {
			t.Fatalf("affectedRows 必须为 0，实际=%v", obj["affectedRows"])
		}
	})

	t.Run("超长理由按字符截断且不出现替换字符", func(t *testing.T) {
		long := strings.Repeat("取消原因示例", 200) // 1000 个汉字
		got, err := cancelResultPayload(long)
		if err != nil {
			t.Fatalf("不应报错：%v", err)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(got), &obj); err != nil {
			t.Fatalf("结果必须可解析：%v", err)
		}
		msg, _ := obj["message"].(string)
		if runes := []rune(msg); len(runes) != 200 {
			t.Fatalf("应截断到 200 个字符，实际 %d", len(runes))
		}
		if strings.ContainsRune(msg, 0xFFFD) {
			t.Fatalf("截断不得切断多字节字符产生 U+FFFD：%q", msg)
		}
	})

	t.Run("emoji 理由不被切断", func(t *testing.T) {
		got, err := cancelResultPayload("⚠️ 需要人工复核 🚩")
		if err != nil {
			t.Fatalf("不应报错：%v", err)
		}
		if strings.Contains(got, "\\ufffd") || strings.Contains(got, string(rune(0xFFFD))) {
			t.Fatalf("不得出现替换字符：%s", got)
		}
	})
}

func payloadKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
