package sanitize

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// identifierPattern 匹配合法的 SQL 标识符（表名、schema 名、列名等）
// 允许：字母、数字、下划线、$，首字符必须为字母或下划线
// 长度限制 1-64 字符（覆盖 MySQL/Oracle/SQLite 的标识符长度限制）
var identifierPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_$]{0,63}$`)

// IsValidIdentifier 校验是否为合法的 SQL 标识符
// 用于防止通过表名/schema 名/列名进行的 SQL 注入
func IsValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	return identifierPattern.MatchString(name)
}

// ValidateIdentifier 校验标识符，非法时返回错误
func ValidateIdentifier(name, label string) error {
	if !IsValidIdentifier(name) {
		return fmt.Errorf("非法的%s: %q", label, name)
	}
	return nil
}

// QuoteIdentifier 对标识符进行安全的引号包裹
// MySQL/MariaDB/SQLite 使用反引号，Oracle 使用双引号
func QuoteIdentifier(name, dbType string) string {
	if !IsValidIdentifier(name) {
		return ""
	}
	switch dbType {
	case "oracle":
		return "\"" + name + "\""
	default:
		return "`" + name + "`"
	}
}

// QuoteSchemaTable 返回带 schema 前缀的安全表引用
func QuoteSchemaTable(schema, table, dbType string) (string, error) {
	if err := ValidateIdentifier(schema, "schema名"); err != nil {
		return "", err
	}
	if err := ValidateIdentifier(table, "表名"); err != nil {
		return "", err
	}
	switch dbType {
	case "oracle":
		return fmt.Sprintf("\"%s\".\"%s\"", schema, table), nil
	default:
		return fmt.Sprintf("`%s`.`%s`", schema, table), nil
	}
}

var safeFileNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_\-.]+$`)

// SafeFileName 校验文件名是否安全（仅允许字母、数字、下划线、中划线、点）。
// 防止路径穿越攻击（如 ../、绝对路径等）。返回 false 表示文件名不安全。
func SafeFileName(name string) bool {
	if name == "" || len(name) > 255 {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	return safeFileNamePattern.MatchString(name)
}

// SanitizeFileName 清洗文件名，移除路径分隔符和 .. 等危险字符。
// 如果清洗后为空，返回 fallback。
func SanitizeFileName(name, fallback string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "..", "")
	if SafeFileName(name) {
		return name
	}
	return fallback
}
