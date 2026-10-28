// Package mask 提供敏感信息脱敏工具，用于日志与审计输出。
//
// 约定：数据库中已经落盘的口令、连接串、API Key 一律不得以明文进入日志，
// 不得随响应体返回前端。本包只做“输出前脱敏”，不参与加密解密。
package mask

import (
	"regexp"
	"strings"
)

var (
	// 1) 通用 key=value 形式：password=xxx / pwd:xxx / passwd="xxx"
	kvSecretRe = regexp.MustCompile(`(?i)\b(password|passwd|pwd)(\s*[=:]\s*)([^&;,\s'"]+)`)

	// 2) URL userinfo 形式：scheme://user:password@host
	urlUserInfoRe = regexp.MustCompile(`(://[^:/@\s]+:)([^@\s/]+)(@)`)

	// 3) MySQL DSN 前缀形式：user:password@tcp(host:port)/db
	//    第二组首字符排除 '/'，避免误伤 scheme:// 形式。
	mysqlDSNPrefixRe = regexp.MustCompile(`^([^:@/\\]+):([^@/][^@]*)@`)

	// 普通文本中的常见敏感信息（审计文本、日志摘要使用）
	textSecretRe = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|password|secret)\b(\s*["']?\s*[:=]\s*["']?)([^\s,;"']+)`)
	textIDCardRe = regexp.MustCompile(`\b[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[0-9Xx]\b`)
	textPhoneRe  = regexp.MustCompile(`\b1[3-9]\d{9}\b`)
	textEmailRe  = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
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

// MaskText 对普通文本执行内置敏感信息脱敏（密钥赋值、身份证、手机号、邮箱）。
//
// 与 agent 包的数据脱敏器共享同一套规则语义，但本包不依赖 agent（避免循环依赖），
// 只用于审计落库与日志摘要等“文本级”场景。
func MaskText(text string) string {
	if text == "" {
		return text
	}
	out := textSecretRe.ReplaceAllString(text, `${1}${2}***`)
	out = textIDCardRe.ReplaceAllStringFunc(out, func(v string) string {
		return v[:6] + "********" + v[14:]
	})
	out = textPhoneRe.ReplaceAllStringFunc(out, func(v string) string {
		return v[:3] + "****" + v[7:]
	})
	out = textEmailRe.ReplaceAllStringFunc(out, func(v string) string {
		at := strings.LastIndexByte(v, '@')
		if at <= 0 {
			return "***"
		}
		return v[:1] + "***" + v[at:]
	})
	return out
}

// MaskAuditText 审计文本的统一脱敏入口：先处理 SQL 字面量，再处理普通文本敏感信息。
//
// 调用方传入的往往是“SQL + 注释”混合文本；两步都必须执行，且均幂等。
func MaskAuditText(text string) string {
	return MaskText(MaskSecret(text))
}
