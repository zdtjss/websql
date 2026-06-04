// Package dberr 提供数据库错误判断的统一 helper 函数，
// 替代散布在各处的 strings.Contains(err.Error(), ...) 模式。
//
// 优势：
//   - 集中管理错误模式，SQL 驱动升级后只需改一处
//   - 语义化函数名，代码可读性更高
//   - 避免 err == nil 时 .Error() panic
package dberr

import (
	"database/sql"
	"strings"
)

// IsTableNotExist 判断错误是否为"表不存在"
func IsTableNotExist(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "doesn't exist")
}

// IsColumnNotExist 判断错误是否为"列不存在"
func IsColumnNotExist(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such column")
}

// IsNoRows 判断错误是否为"无数据行"（sql.ErrNoRows 或包含 "no rows"/"not found"）
func IsNoRows(err error) bool {
	if err == nil {
		return false
	}
	if err == sql.ErrNoRows {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "no rows") || strings.Contains(msg, "not found")
}

// IsUniqueConstraint 判断错误是否为唯一约束冲突
func IsUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "Duplicate entry") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "ORA-00001")
}

// IsTableNotExistOrNoRows 判断错误是否为"表不存在"或"无数据行"的联合条件
func IsTableNotExistOrNoRows(err error) bool {
	return IsTableNotExist(err) || IsNoRows(err)
}
