package sanitize

import (
	"regexp"
	"strings"
)

var stackLinePattern = regexp.MustCompile(`(?i)^\s*(goroutine\s+\d+|runtime\/|stack\s+trace|panic\s+recovered|fatal\s+error|panic\(|\.go:\d+\s*$)`)
var logPrefixPattern = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}\s+`)
var stackFramePattern = regexp.MustCompile(`^\s+`)
var hexAddrPattern = regexp.MustCompile(`0x[0-9a-fA-F]+`)

var filePathPattern = regexp.MustCompile(`(?i)(/tmp/|/var/|/home/|[A-Z]:\\|\\Users\\)[^\s]*`)

func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return extractErrorMsg(err.Error())
}

func SanitizeErrMsg(msg string) string {
	return extractErrorMsg(msg)
}

func extractErrorMsg(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "系统错误"
	}

	lines := strings.Split(msg, "\n")
	var meaningfulLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if stackLinePattern.MatchString(trimmed) {
			continue
		}

		cleaned := logPrefixPattern.ReplaceAllString(trimmed, "")
		cleaned = strings.TrimSpace(cleaned)

		if cleaned == "" {
			continue
		}

		if stackLinePattern.MatchString(cleaned) {
			continue
		}

		if strings.HasPrefix(cleaned, "PANIC:") {
			cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "PANIC:"))
			if cleaned == "" || stackLinePattern.MatchString(cleaned) {
				continue
			}
		}

		cleaned = hexAddrPattern.ReplaceAllString(cleaned, "")
		cleaned = strings.TrimSpace(cleaned)

		if cleaned != "" {
			meaningfulLines = append(meaningfulLines, cleaned)
		}
	}

	if len(meaningfulLines) == 0 {
		return "系统内部错误"
	}

	result := strings.Join(meaningfulLines, "; ")
	result = redactFilePaths(result)

	if len(result) > 500 {
		result = result[:500] + "..."
	}

	return result
}

func redactFilePaths(msg string) string {
	return filePathPattern.ReplaceAllString(msg, "***")
}

func UserErrMsg(prefix string, err error) string {
	return prefix + SanitizeError(err)
}
