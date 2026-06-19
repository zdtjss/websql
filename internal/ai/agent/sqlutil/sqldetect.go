package sqlutil

import "strings"

// SQLType 表示 SQL 语句类型。
type SQLType string

const (
	SQLTypeSelect   SQLType = "SELECT"
	SQLTypeInsert   SQLType = "INSERT"
	SQLTypeUpdate   SQLType = "UPDATE"
	SQLTypeDelete   SQLType = "DELETE"
	SQLTypeDrop     SQLType = "DROP"
	SQLTypeTruncate SQLType = "TRUNCATE"
	SQLTypeAlter    SQLType = "ALTER"
	SQLTypeCreate   SQLType = "CREATE"
	SQLTypeReplace  SQLType = "REPLACE"
	SQLTypeMerge    SQLType = "MERGE"
	SQLTypeUnknown  SQLType = "UNKNOWN"
)

// RiskLevel 表示 SQL 风险等级。
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// DetectSQLType 检测 SQL 类型。合并自 handler.go detectSQLType。
//
// 保持与原 detectSQLType 完全一致的逻辑：仅做 TrimSpace + ToUpper，
// 不剥离 SQL 注释；前缀匹配不带尾随空格。
func DetectSQLType(sql string) SQLType {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	for _, prefix := range []string{"DROP", "TRUNCATE", "DELETE", "ALTER", "CREATE", "INSERT", "UPDATE", "REPLACE", "MERGE"} {
		if strings.HasPrefix(upper, prefix) {
			return SQLType(prefix)
		}
	}
	return SQLTypeUnknown
}

// DetectRiskLevel 检测风险等级。合并自 handler.go detectRiskLevel。
//
// 保持与原 detectRiskLevel 完全一致的逻辑：仅做 TrimSpace + ToUpper，
// 不剥离 SQL 注释；DROP/TRUNCATE 为 high，DELETE/ALTER 无 WHERE 为 high。
func DetectRiskLevel(sql string) RiskLevel {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(upper, "DROP") || strings.HasPrefix(upper, "TRUNCATE") {
		return RiskHigh
	}
	if strings.HasPrefix(upper, "DELETE") || strings.HasPrefix(upper, "ALTER") {
		if !strings.Contains(upper, "WHERE") {
			return RiskHigh
		}
		return RiskMedium
	}
	return RiskMedium
}

// IsDangerousSQL 判断是否为危险 SQL（写操作）。合并自 middleware.go isDangerousSQL。
//
// 保持与原 isDangerousSQL 完全一致的逻辑：先 StripSQLComments 再 TrimSpace +
// ToUpper；前缀匹配带尾随空格（"DROP " 等），避免误判 "DELETEFROM" 之类的拼写。
func IsDangerousSQL(sql string) bool {
	stripped := StripSQLComments(strings.TrimSpace(sql))
	upper := strings.ToUpper(stripped)
	for _, p := range []string{
		"DROP ", "TRUNCATE ", "DELETE ",
		"ALTER ", "CREATE ", "REPLACE ",
		"INSERT ", "UPDATE ", "MERGE ",
	} {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}
