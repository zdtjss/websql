// Package mask 提供敏感信息脱敏工具，用于日志输出。
//
// 约定：数据库中已经落盘的口令、连接串、API Key 一律不得以明文进入日志、
// 不得随响应体返回前端。本包只做“输出前脱敏”，不参与加解密。
package mask

import "regexp"

var (
	// 1) 通用 key=value 形式：password=xxx / pwd:xxx / passwd="xxx"
	kvSecretRe = regexp.MustCompile(`(?i)\b(password|passwd|pwd)(\s*[=:]\s*)([^&;,\s'"]+)`)

	// 2) URL userinfo 形式：scheme://user:password@host
	urlUserInfoRe = regexp.MustCompile(`(://[^:/@\s]+:)([^@\s/]+)(@)`)

	// 3) MySQL DSN 前缀形式：user:password@tcp(host:port)/db
	//    第二组首字符排除 '/'，避免误伤 scheme:// 形式。
	mysqlDSNPrefixRe = regexp.MustCompile(`^([^:@/\\]+):([^@/][^@]*)@`)
)

// MaskDSN 对连接串中的口令脱敏，保留主机、端口、库名等排障必需信息。
// 覆盖 MySQL DSN、URL 形式（oracle://user:pass@host）与 key=value 形式（password=xxx）。
func MaskDSN(s string) string {
	if s == "" {
		return ""
	}
	out := urlUserInfoRe.ReplaceAllString(s, "${1}***${3}")
	out = mysqlDSNPrefixRe.ReplaceAllString(out, "${1}:***@")
	out = kvSecretRe.ReplaceAllString(out, "${1}${2}***")
	return out
}

// MaskSecret 兜底脱敏：用于错误信息等无法确定结构的文本。
// 仅处理可识别的 key=value 与 userinfo 形式，无法识别时原样返回。
func MaskSecret(s string) string {
	return MaskDSN(s)
}

// MaskToken 对 API Key / Token 做保留首尾的脱敏，用于必须标识“是哪一把 key”的日志场景。
func MaskToken(token string) string {
	if token == "" {
		return ""
	}
	r := []rune(token)
	if len(r) <= 8 {
		return "***"
	}
	return string(r[:4]) + "***" + string(r[len(r)-4:])
}
