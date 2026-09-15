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

// writeKeywords 写操作关键字集合（词级匹配，大小写不敏感）。
var writeKeywords = map[string]bool{
	"DROP": true, "TRUNCATE": true, "DELETE": true, "ALTER": true,
	"CREATE": true, "REPLACE": true, "INSERT": true, "UPDATE": true,
	"MERGE": true,
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentRune(r rune) bool {
	return r == '_' || r == '$' ||
		(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// FirstKeyword 返回 SQL 的首个关键字（剥离注释、跳过前导空白/标点，统一大写）。
//
// 安全性说明：历史实现使用 strings.HasPrefix(upper, "DROP ") 之类的「关键字+空格」
// 前缀匹配，`DROP\tTABLE`（制表符）与 `DELETE\nFROM users`（换行）都不命中，
// 导致危险 SQL 审批被静默绕过。这里改为「词法切分取首词」，对任意空白
// （空格/制表符/换行/回车）与任意前导括号、分号均保持一致判定。
func FirstKeyword(sql string) string {
	stripped := StripSQLComments(strings.TrimSpace(sql))
	i := 0
	for i < len(stripped) && !isIdentByte(stripped[i]) {
		i++
	}
	j := i
	for j < len(stripped) && isIdentByte(stripped[j]) {
		j++
	}
	return strings.ToUpper(stripped[i:j])
}

// DetectSQLType 检测 SQL 类型。基于首个关键字做词级匹配。
func DetectSQLType(sql string) SQLType {
	kw := FirstKeyword(sql)
	switch kw {
	case "DROP":
		return SQLTypeDrop
	case "TRUNCATE":
		return SQLTypeTruncate
	case "DELETE":
		return SQLTypeDelete
	case "ALTER":
		return SQLTypeAlter
	case "CREATE":
		return SQLTypeCreate
	case "INSERT":
		return SQLTypeInsert
	case "UPDATE":
		return SQLTypeUpdate
	case "REPLACE":
		return SQLTypeReplace
	case "MERGE":
		return SQLTypeMerge
	}
	return SQLTypeUnknown
}

// DetectRiskLevel 检测风险等级。
// DROP/TRUNCATE 为 high；DELETE/ALTER 无 WHERE 为 high，有 WHERE 为 medium。
func DetectRiskLevel(sql string) RiskLevel {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	switch FirstKeyword(sql) {
	case "DROP", "TRUNCATE":
		return RiskHigh
	case "DELETE", "ALTER":
		if !strings.Contains(upper, "WHERE") {
			return RiskHigh
		}
		return RiskMedium
	}
	return RiskMedium
}

// IsDangerousSQL 判断是否为危险 SQL（写操作）。
//
// 判定口径：剥离注释后取首个「词」判断是否属于写关键字，
// 因此 `DROP\tTABLE`、`DELETE\nFROM`、`UPDATE\r\nusers` 均能正确命中。
func IsDangerousSQL(sql string) bool {
	return writeKeywords[FirstKeyword(sql)]
}

// HasWriteKeyword 判断 SQL 中是否出现写操作关键字（词级扫描整条语句）。
//
// 用途：WITH（CTE）包裹的写语句首关键字是 WITH，
// 例如 `WITH x AS (SELECT 1) DELETE FROM t` 或
// `WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x`，
// 仅靠 FirstKeyword 无法识别，需要整句扫描。
//
// 说明：采用词级匹配而非子串匹配，列名 `update_time` 不会被误判；
// 未加引号的列名恰为 `update` 时可能误报，方向是「多一次确认」而非漏放行。
func HasWriteKeyword(sql string) bool {
	stripped := StripSQLComments(sql)
	for _, tok := range strings.FieldsFunc(stripped, func(r rune) bool { return !isIdentRune(r) }) {
		if writeKeywords[strings.ToUpper(tok)] {
			return true
		}
	}
	return false
}