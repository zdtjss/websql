package admin

import (
	"fmt"
	"regexp"
	"strings"
)

// ──────────────────────────────────────────────
// SQLPermissionCheck — 统一 SQL 权限校验入口
// ──────────────────────────────────────────────
//
// 设计目标：
//   1. 表级权限：检查 SQL 涉及的所有表是否有访问权限
//   2. 列级权限（读）：SELECT 中显式引用的列必须在授权范围内
//   3. 列级权限（写）：INSERT/UPDATE 中操作的列必须在授权范围内
//   4. 结果集过滤：对查询结果按列级权限过滤（兜底保护）
//
// 调用方：ExecSQL、AI agent tools、export、import 等所有 SQL 执行入口

// SQLPermissionResult 权限校验结果
type SQLPermissionResult struct {
	Allowed        bool     // 是否允许执行
	DeniedTables   []string // 被拒绝的表
	DeniedColumns  []string // 被拒绝的列（格式：table.column）
	AllowedColumns []string // 允许的列（用于结果集过滤）
	Message        string   // 拒绝原因
}

// CheckSQLFullPermission 完整的 SQL 权限校验（表 + 列）
// 返回 SQLPermissionResult，调用方根据 Allowed 决定是否执行
func CheckSQLFullPermission(sqlStr, connId, schema, authorization string) *SQLPermissionResult {
	analysis := AnalyzeSQL(sqlStr, schema)
	return CheckAnalysisPermission(analysis, connId, authorization)
}

// CheckAnalysisPermission 基于已分析的 SQL 进行权限校验
func CheckAnalysisPermission(analysis *SQLAnalysis, connId, authorization string) *SQLPermissionResult {
	result := &SQLPermissionResult{Allowed: true}

	// 1. 检查读表权限
	for _, t := range analysis.ReadTables {
		access := GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		if access.Level == AccessNone {
			result.Allowed = false
			result.DeniedTables = append(result.DeniedTables, t.Name)
		}
	}

	// 2. 检查写表权限
	for _, t := range analysis.WriteTables {
		access := GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		if access.Level == AccessNone {
			result.Allowed = false
			result.DeniedTables = append(result.DeniedTables, t.Name)
		}
	}

	if !result.Allowed {
		result.Message = fmt.Sprintf("无权访问表: %s", strings.Join(result.DeniedTables, ", "))
		return result
	}

	// 3. 检查写列权限（INSERT/UPDATE 中指定的列）
	if len(analysis.WriteColumns) > 0 && len(analysis.WriteTables) > 0 {
		writeTable := analysis.WriteTables[0]
		access := GetTableColumnAccess(connId, writeTable.Schema, writeTable.Name, authorization)
		if access.Level == AccessColumn {
			for _, col := range analysis.WriteColumns {
				if !access.AllowedColumns[col.ColumnName] {
					result.Allowed = false
					result.DeniedColumns = append(result.DeniedColumns, writeTable.Name+"."+col.ColumnName)
				}
			}
			if !result.Allowed {
				result.Message = fmt.Sprintf("无权操作字段: %s", strings.Join(result.DeniedColumns, ", "))
				return result
			}
		}
	}

	// 4. 检查 SELECT 中显式引用的列（仅当有列级限制时）
	if analysis.OperationType == "SELECT" || analysis.OperationType == "UNKNOWN" {
		selectCols := ExtractSelectColumns(StripComments(strings.TrimSpace(analysis.OriginalSQL)))
		// 检查是否全部为 * （SELECT * 由结果集过滤兜底）
		allStar := true
		for _, sc := range selectCols {
			if !sc.IsStar {
				allStar = false
				break
			}
		}
		if len(selectCols) > 0 && !allStar {
			for _, sc := range selectCols {
				if sc.IsStar {
					continue // SELECT * / table.* 由结果集过滤兜底
				}
				colName := sc.ColumnName
				// 策略：只有当所有有列级限制的表都不允许该列时才拒绝
				// 原因：无法可靠地将别名映射到真实表名，且结果集过滤是最终兜底
				allDenied := false
				hasColumnLevelTable := false
				for _, t := range analysis.ReadTables {
					access := GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
					if access.Level == AccessColumn {
						hasColumnLevelTable = true
						if access.AllowedColumns[colName] {
							allDenied = false
							break
						}
						allDenied = true
					}
				}
				if hasColumnLevelTable && allDenied {
					result.Allowed = false
					displayName := colName
					if sc.TableAlias != "" {
						displayName = sc.TableAlias + "." + colName
					}
					result.DeniedColumns = append(result.DeniedColumns, displayName)
				}
			}
			if !result.Allowed {
				result.Message = fmt.Sprintf("无权访问字段: %s", strings.Join(result.DeniedColumns, ", "))
				return result
			}
		}
	}

	return result
}

// ──────────────────────────────────────────────
// SELECT 列提取
// ──────────────────────────────────────────────

// SelectColumn 表示 SELECT 子句中的一个列引用
type SelectColumn struct {
	TableAlias string // 表别名或表名前缀（可能为空）
	ColumnName string // 列名
	IsStar     bool   // 是否为 *
	Expression string // 原始表达式（用于调试）
}

// ExtractSelectColumns 从 SQL 中提取 SELECT 子句引用的列
// 支持：SELECT col1, t.col2, func(col3), col4 AS alias
// 不支持完美解析所有 SQL，但覆盖常见场景
func ExtractSelectColumns(sql string) []SelectColumn {
	upper := strings.ToUpper(strings.TrimSpace(sql))

	// 处理 WITH ... SELECT 的情况
	if strings.HasPrefix(upper, "WITH") {
		// 找到最后一个 SELECT（主查询）
		lastSelect := strings.LastIndex(upper, "SELECT")
		if lastSelect > 0 {
			sql = sql[lastSelect:]
			upper = strings.ToUpper(sql)
		}
	}

	if !strings.HasPrefix(upper, "SELECT") {
		return nil
	}

	// 提取 SELECT 和 FROM 之间的部分
	selectBody := extractSelectBody(sql)
	if selectBody == "" {
		return nil
	}

	// 检查是否为 SELECT *
	trimmed := strings.TrimSpace(selectBody)
	if trimmed == "*" {
		return []SelectColumn{{IsStar: true, Expression: "*"}}
	}

	// 按逗号分割（注意括号嵌套）
	parts := splitByCommaRespectParens(trimmed)
	var cols []SelectColumn

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// 处理 table.* 的情况
		if strings.HasSuffix(part, ".*") {
			cols = append(cols, SelectColumn{
				TableAlias: strings.TrimSuffix(part, ".*"),
				IsStar:     true,
				Expression: part,
			})
			continue
		}

		// 去掉 AS alias 部分
		col := removeAlias(part)

		// 跳过纯函数调用（如 COUNT(*)、NOW()）但提取函数参数中的列
		if isFunctionCall(col) {
			innerCols := extractColumnsFromExpression(col)
			cols = append(cols, innerCols...)
			continue
		}

		// 跳过常量和数字
		if isConstant(col) {
			continue
		}

		// 解析 table.column 格式
		sc := parseColumnRef(col)
		sc.Expression = part
		cols = append(cols, sc)
	}

	return cols
}

// extractSelectBody 提取 SELECT 和 FROM 之间的内容
func extractSelectBody(sql string) string {
	upper := strings.ToUpper(sql)

	// 跳过 SELECT [DISTINCT] [ALL]
	start := 6 // len("SELECT")
	rest := strings.TrimSpace(sql[start:])
	upperRest := strings.ToUpper(rest)
	if strings.HasPrefix(upperRest, "DISTINCT ") {
		start += 9 + (len(sql[start:]) - len(rest))
		rest = strings.TrimSpace(sql[start:])
	} else if strings.HasPrefix(upperRest, "ALL ") {
		start += 4 + (len(sql[start:]) - len(rest))
		rest = strings.TrimSpace(sql[start:])
	}

	// 找到 FROM（考虑子查询中的 FROM）
	fromIdx := findTopLevelKeyword(upper[start:], "FROM")
	if fromIdx == -1 {
		// 没有 FROM（如 SELECT 1+1）
		return rest
	}

	return strings.TrimSpace(sql[start : start+fromIdx])
}

// findTopLevelKeyword 在 SQL 中找到顶层（非括号内）的关键字位置
func findTopLevelKeyword(sql, keyword string) int {
	depth := 0
	upper := strings.ToUpper(sql)
	kwLen := len(keyword)

	for i := 0; i < len(upper)-kwLen; i++ {
		switch upper[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && upper[i:i+kwLen] == keyword {
				// 确保是独立的关键字（前后是空白或字符串边界）
				before := i == 0 || !isIdentChar(upper[i-1])
				after := i+kwLen >= len(upper) || !isIdentChar(upper[i+kwLen])
				if before && after {
					return i
				}
			}
		}
	}
	return -1
}

func isIdentChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
}

// splitByCommaRespectParens 按逗号分割，但忽略括号内的逗号
func splitByCommaRespectParens(s string) []string {
	var parts []string
	depth := 0
	start := 0
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
		} else if c == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
		} else if !inSingleQuote && !inDoubleQuote {
			switch c {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			case ',':
				if depth == 0 {
					parts = append(parts, s[start:i])
					start = i + 1
				}
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// removeAlias 去掉 AS alias 或隐式别名
func removeAlias(expr string) string {
	upper := strings.ToUpper(strings.TrimSpace(expr))

	// 查找 AS 关键字（顶层）
	asIdx := findTopLevelKeyword(upper, " AS ")
	if asIdx >= 0 {
		return strings.TrimSpace(expr[:asIdx])
	}

	return strings.TrimSpace(expr)
}

// isFunctionCall 判断是否为函数调用
func isFunctionCall(expr string) bool {
	trimmed := strings.TrimSpace(expr)
	// 匹配 func_name(...)
	re := regexp.MustCompile(`(?i)^\w+\s*\(`)
	return re.MatchString(trimmed)
}

// extractColumnsFromExpression 从表达式中提取列引用
// 例如：COUNT(t.col1)、IFNULL(col1, col2)
func extractColumnsFromExpression(expr string) []SelectColumn {
	var cols []SelectColumn
	// 提取括号内的内容
	re := regexp.MustCompile(`(?i)(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)`)
	matches := re.FindAllString(expr, -1)
	for _, m := range matches {
		sc := parseColumnRef(m)
		if !isSQLKeyword(sc.ColumnName) && sc.ColumnName != "*" {
			cols = append(cols, sc)
		}
	}

	// 也提取无表前缀的列名（在函数参数中）
	// 提取括号内容
	parenStart := strings.Index(expr, "(")
	parenEnd := strings.LastIndex(expr, ")")
	if parenStart >= 0 && parenEnd > parenStart {
		inner := expr[parenStart+1 : parenEnd]
		parts := splitByCommaRespectParens(inner)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "*" || p == "" || isConstant(p) {
				continue
			}
			if !strings.Contains(p, "(") && !strings.Contains(p, ".") {
				cleaned := stripBackticks(p)
				if !isSQLKeyword(cleaned) {
					cols = append(cols, SelectColumn{ColumnName: cleaned, Expression: p})
				}
			}
		}
	}

	return cols
}

// isConstant 判断是否为常量（数字、字符串等）
func isConstant(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	// 数字
	if matched, _ := regexp.MatchString(`^-?\d+(\.\d+)?$`, s); matched {
		return true
	}
	// 字符串常量
	if (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) ||
		(strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) {
		return true
	}
	// NULL
	if strings.EqualFold(s, "NULL") || strings.EqualFold(s, "TRUE") || strings.EqualFold(s, "FALSE") {
		return true
	}
	return false
}

// parseColumnRef 解析列引用（支持 table.column 和 `table`.`column` 格式）
func parseColumnRef(ref string) SelectColumn {
	ref = strings.TrimSpace(ref)

	// 处理 schema.table.column 格式
	parts := splitDottedIdentifier(ref)
	switch len(parts) {
	case 3:
		return SelectColumn{TableAlias: parts[1], ColumnName: parts[2]}
	case 2:
		return SelectColumn{TableAlias: parts[0], ColumnName: parts[1]}
	default:
		return SelectColumn{ColumnName: stripBackticks(ref)}
	}
}

// splitDottedIdentifier 按点号分割标识符（考虑反引号）
func splitDottedIdentifier(s string) []string {
	var parts []string
	current := strings.Builder{}
	inBacktick := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '`' {
			inBacktick = !inBacktick
			continue
		}
		if c == '.' && !inBacktick {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(c)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// ──────────────────────────────────────────────
// 辅助函数
// ──────────────────────────────────────────────

// resolveAliasToTable 将别名解析为实际表名
// 简单实现：如果别名与某个表名匹配，直接返回
func resolveAliasToTable(alias string, tables []TableRef) string {
	alias = stripBackticks(alias)
	for _, t := range tables {
		if strings.EqualFold(t.Name, alias) {
			return t.Name
		}
	}
	// 别名可能就是表名本身
	return alias
}

// getAccessForTable 获取指定表的列级访问权限
func getAccessForTable(connId, tableName string, tables []TableRef, authorization string) *TableColumnAccess {
	for _, t := range tables {
		if strings.EqualFold(t.Name, tableName) {
			return GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		}
	}
	return nil
}

// checkColumnInAnyTable 检查无表前缀的列是否在任何有列级限制的表中被拒绝
// 策略：如果任何一个有列级限制的表不允许该列，则拒绝
func checkColumnInAnyTable(connId, columnName string, tables []TableRef, authorization string) bool {
	for _, t := range tables {
		access := GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		if access.Level == AccessColumn {
			if !access.AllowedColumns[columnName] {
				return true // 被拒绝
			}
		}
	}
	return false
}

// FilterSchemaByColumnPermission 过滤表结构 DDL 中的未授权列
// 用于 get_table_schema 工具的结果过滤
func FilterSchemaByColumnPermission(ddl string, tableName, connId, schema, authorization string) string {
	access := GetTableColumnAccess(connId, schema, tableName, authorization)
	if access.Level == AccessFull || access.Level == AccessNone {
		if access.Level == AccessNone {
			return ""
		}
		return ddl
	}

	// 列级权限：过滤 DDL 中的列定义
	// 策略：逐行检查，保留授权列和非列定义行（如 CREATE TABLE、PRIMARY KEY 等）
	lines := strings.Split(ddl, "\n")
	var filtered []string
	columnDefRegex := regexp.MustCompile("(?i)^\\s+[`\"']?(\\w+)[`\"']?\\s+")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperTrimmed := strings.ToUpper(trimmed)

		// 保留非列定义行
		if strings.HasPrefix(upperTrimmed, "CREATE ") ||
			strings.HasPrefix(upperTrimmed, ")") ||
			strings.HasPrefix(upperTrimmed, "PRIMARY KEY") ||
			strings.HasPrefix(upperTrimmed, "KEY ") ||
			strings.HasPrefix(upperTrimmed, "INDEX ") ||
			strings.HasPrefix(upperTrimmed, "UNIQUE ") ||
			strings.HasPrefix(upperTrimmed, "CONSTRAINT ") ||
			strings.HasPrefix(upperTrimmed, "ENGINE") ||
			strings.HasPrefix(upperTrimmed, "DEFAULT CHARSET") ||
			strings.HasPrefix(upperTrimmed, "COMMENT") ||
			strings.HasPrefix(upperTrimmed, "AUTO_INCREMENT") ||
			trimmed == "" || trimmed == ";" {
			filtered = append(filtered, line)
			continue
		}

		// 尝试提取列名
		match := columnDefRegex.FindStringSubmatch(line)
		if len(match) >= 2 {
			colName := match[1]
			if access.AllowedColumns[colName] {
				filtered = append(filtered, line)
			}
			// 未授权列：跳过
		} else {
			// 无法解析的行保留
			filtered = append(filtered, line)
		}
	}

	return strings.Join(filtered, "\n")
}
