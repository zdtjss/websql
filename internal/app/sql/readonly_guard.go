package sql

import (
	"strings"

	"websql/internal/app/conn"
	"websql/internal/pkg/sqlguard"
)

// ensureConnWritable 只读连接写保护。
//
// 放在独立文件而非 exec.go：exec.go 内部存在与包名同名的局部变量 conn（*sqlx.DB），
// 在包级函数中引用 conn 包名更清晰，也不会被局部变量遮蔽。
func ensureConnWritable(connID string) (bool, string) {
	return conn.CheckWritable(connID)
}

// containsWriteStatement 判断待执行 SQL（可能包含多条语句）中是否至少有一条写语句。
func containsWriteStatement(sqlStr string) bool {
	if sqlguard.IsWriteStatement(sqlStr) {
		return true
	}
	if !strings.Contains(sqlStr, ";") {
		return false
	}
	for _, single := range splitSQLRespectQuotes(sqlStr) {
		if sqlguard.IsWriteStatement(single) {
			return true
		}
	}
	return false
}
