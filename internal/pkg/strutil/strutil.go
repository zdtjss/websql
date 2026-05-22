package strutil

import (
	"encoding/json"
	"websql/internal/logger"
	"strconv"
	"strings"
	"unicode"
)

func ExtractSql(s string) string {
	relSql := strings.TrimSpace(s)
	if relSql == "" || strings.HasPrefix(relSql, "--") || strings.HasPrefix(relSql, "//") || strings.HasPrefix(relSql, "/*") {
		var nsql []string
		for _, row := range strings.Split(relSql, "\n") {
			if row == "" || strings.HasPrefix(row, "--") || strings.HasPrefix(row, "//") || strings.HasPrefix(row, "/*") {
				continue
			}
			nsql = append(nsql, row)
		}
		relSql = strings.Join(nsql, "\n")
	}
	return relSql
}

func AtoUint64(s string) uint64 {
	uis, err := strconv.ParseUint(s, 10, 64)
	logger.PanicErr(err)
	return uis
}

// ConvertKeysToCamel 递归将对象中的所有下划线风格 key 转为驼峰风格
// 支持 struct、map、slice、array 等任意类?
func SnakeToCamel(v any) any {
	if v == nil {
		return v
	}

	// 先将输入转为通用?any 结构（通过 JSON?
	data, err := json.Marshal(v)
	if err != nil {
		// 如果无法序列化（如包?func、chan 等），直接返回原?
		return v
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return v
	}

	return convertRecursive(raw)
}

func convertRecursive(v any) any {
	switch val := v.(type) {
	case map[string]any:
		newMap := make(map[string]any)
		for k, v2 := range val {
			newKey := snakeToCamel(k)
			newMap[newKey] = convertRecursive(v2)
		}
		return newMap
	case []any:
		newSlice := make([]any, len(val))
		for i, item := range val {
			newSlice[i] = convertRecursive(item)
		}
		return newSlice
	default:
		return v
	}
}

// SnakeToCamel ?snake_case 转为 camelCase
func snakeToCamel(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(strings.ToLower(s), "_")
	var result strings.Builder
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 {
			result.WriteString(part)
		} else {
			runes := []rune(part)
			runes[0] = unicode.ToUpper(runes[0])
			result.WriteString(string(runes))
		}
	}
	return result.String()
}