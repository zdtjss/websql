package export

// JSON 载荷脱敏：export_skill 的 payload 是嵌套 JSON 字符串，
// 直接对整段文本跑脱敏正则可能破坏 JSON 结构或漏掉嵌套值，
// 因此统一解析后递归处理所有字符串叶子，再重新序列化。

import (
	"encoding/json"
	"fmt"
)

// MaskJSONStrings 解析 raw JSON 并对所有字符串叶子应用 mask。
// raw 不是合法 JSON 时返回错误（调用方应 fail-closed 拒绝出域）。
func MaskJSONStrings(raw string, mask func(string) string) (string, error) {
	if mask == nil {
		return raw, nil
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return "", fmt.Errorf("payload 不是合法 JSON，无法完成脱敏：%w", err)
	}
	value = maskJSONValue(value, mask)
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("脱敏后 JSON 序列化失败：%w", err)
	}
	return string(data), nil
}

func maskJSONValue(value any, mask func(string) string) any {
	switch v := value.(type) {
	case string:
		return mask(v)
	case []any:
		for i := range v {
			v[i] = maskJSONValue(v[i], mask)
		}
		return v
	case map[string]any:
		for key := range v {
			v[key] = maskJSONValue(v[key], mask)
		}
		return v
	default:
		return v
	}
}
