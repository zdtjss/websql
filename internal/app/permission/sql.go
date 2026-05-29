package permission

import (
	"fmt"
	"regexp"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/config"
)

type SQLAnalysis struct {
	OperationType string
	ReadTables    []TableRef
	WriteTables   []TableRef
	WriteColumns  []ColumnRef
	OriginalSQL   string
}

type TableRef struct {
	Schema string
	Name   string
}

type ColumnRef struct {
	TableName  string
	ColumnName string
}

type ColumnAccessLevel string

const (
	AccessFull   ColumnAccessLevel = "full"
	AccessColumn ColumnAccessLevel = "column"
	AccessNone   ColumnAccessLevel = "none"
)

type TableColumnAccess struct {
	Level          ColumnAccessLevel
	AllowedColumns map[string]bool
}

func AnalyzeSQL(sqlStr string, defaultSchema string) *SQLAnalysis {
	trimmed := strings.TrimSpace(sqlStr)
	analysis := &SQLAnalysis{
		OperationType: extractOperationType(trimmed),
		OriginalSQL:   trimmed,
	}

	allTables := ExtractTablesFromSQL(trimmed)

	switch analysis.OperationType {
	case "SELECT":
		for _, t := range allTables {
			schema, name := resolveTableName(t, defaultSchema)
			analysis.ReadTables = append(analysis.ReadTables, TableRef{Schema: schema, Name: name})
		}
	case "INSERT":
		target, cols := extractInsertTarget(trimmed, defaultSchema)
		if target != nil {
			analysis.WriteTables = append(analysis.WriteTables, *target)
			analysis.WriteColumns = cols
		}
		for _, t := range allTables {
			schema, name := resolveTableName(t, defaultSchema)
			if target == nil || schema != target.Schema || name != target.Name {
				analysis.ReadTables = append(analysis.ReadTables, TableRef{Schema: schema, Name: name})
			}
		}
	case "UPDATE":
		target, cols := extractUpdateTarget(trimmed, defaultSchema)
		if target != nil {
			analysis.WriteTables = append(analysis.WriteTables, *target)
			analysis.WriteColumns = cols
		}
		for _, t := range allTables {
			schema, name := resolveTableName(t, defaultSchema)
			if target == nil || schema != target.Schema || name != target.Name {
				analysis.ReadTables = append(analysis.ReadTables, TableRef{Schema: schema, Name: name})
			}
		}
	case "DELETE":
		target := extractDeleteTarget(trimmed, defaultSchema)
		if target != nil {
			analysis.WriteTables = append(analysis.WriteTables, *target)
		}
	case "DDL":
		target := extractDDLTarget(trimmed, defaultSchema)
		if target != nil {
			analysis.WriteTables = append(analysis.WriteTables, *target)
		}
	}

	return analysis
}

func extractOperationType(sql string) string {
	upper := strings.ToUpper(sql)
	for strings.HasPrefix(upper, "/*") {
		idx := strings.Index(upper, "*/")
		if idx == -1 {
			break
		}
		upper = strings.TrimSpace(upper[idx+2:])
	}
	for strings.HasPrefix(upper, "--") || strings.HasPrefix(upper, "#") {
		idx := strings.Index(upper, "\n")
		if idx == -1 {
			break
		}
		upper = strings.TrimSpace(upper[idx+1:])
	}

	type opPattern struct {
		prefix string
		op     string
	}
	patterns := []opPattern{
		{"SELECT", "SELECT"},
		{"INSERT", "INSERT"},
		{"UPDATE", "UPDATE"},
		{"DELETE", "DELETE"},
		{"WITH", "SELECT"},
		{"ALTER", "DDL"},
		{"DROP", "DDL"},
		{"CREATE", "DDL"},
		{"TRUNCATE", "DDL"},
		{"RENAME", "DDL"},
		{"REPLACE", "INSERT"},
	}
	for _, p := range patterns {
		if strings.HasPrefix(upper, p.prefix+" ") || strings.HasPrefix(upper, p.prefix+"\n") || strings.HasPrefix(upper, p.prefix+"\t") {
			return p.op
		}
	}
	return "UNKNOWN"
}

func resolveTableName(name, defaultSchema string) (string, string) {
	name = strings.Trim(name, "`")
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		return strings.Trim(parts[0], "`"), strings.Trim(parts[1], "`")
	}
	return defaultSchema, name
}

func extractInsertTarget(sql string, defaultSchema string) (*TableRef, []ColumnRef) {
	re := regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?`)
	match := re.FindString(sql)
	if match == "" {
		return nil, nil
	}
	tablePart := regexp.MustCompile(`(?i)\bINTO\s+(.+)`).FindStringSubmatch(match)
	if len(tablePart) < 2 {
		return nil, nil
	}
	tableName := strings.TrimSpace(tablePart[1])
	schema, name := resolveTableName(tableName, defaultSchema)
	target := &TableRef{Schema: schema, Name: name}

	colRe := regexp.MustCompile(`(?i)\bINTO\s+(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?\s*\(([^)]+)\)`)
	colMatch := colRe.FindStringSubmatch(sql)
	if len(colMatch) >= 2 {
		colStr := colMatch[1]
		colNames := strings.Split(colStr, ",")
		var cols []ColumnRef
		for _, c := range colNames {
			c = strings.TrimSpace(c)
			c = strings.Trim(c, "`")
			if c != "" {
				cols = append(cols, ColumnRef{TableName: name, ColumnName: c})
			}
		}
		return target, cols
	}

	return target, nil
}

func extractUpdateTarget(sql string, defaultSchema string) (*TableRef, []ColumnRef) {
	re := regexp.MustCompile(`(?i)\bUPDATE\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`)
	match := re.FindStringSubmatch(sql)
	if len(match) < 2 {
		return nil, nil
	}
	tableName := strings.TrimSpace(match[1])
	schema, name := resolveTableName(tableName, defaultSchema)
	target := &TableRef{Schema: schema, Name: name}

	setRe := regexp.MustCompile(`(?i)\bSET\s+(.+?)(?:\bWHERE\b|\bORDER\b|\bLIMIT\b|\bRETURNING\b|$)`)
	setMatch := setRe.FindStringSubmatch(sql)
	if len(setMatch) >= 2 {
		setStr := setMatch[1]
		var cols []ColumnRef
		assignments := strings.Split(setStr, ",")
		for _, a := range assignments {
			a = strings.TrimSpace(a)
			eqIdx := strings.Index(a, "=")
			if eqIdx > 0 {
				colName := strings.TrimSpace(a[:eqIdx])
				colName = strings.Trim(colName, "`")
				dotIdx := strings.LastIndex(colName, ".")
				if dotIdx >= 0 {
					colName = colName[dotIdx+1:]
				}
				if colName != "" {
					cols = append(cols, ColumnRef{TableName: name, ColumnName: colName})
				}
			}
		}
		return target, cols
	}

	return target, nil
}

func extractDeleteTarget(sql string, defaultSchema string) *TableRef {
	re := regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`)
	match := re.FindStringSubmatch(sql)
	if len(match) >= 2 {
		tableName := strings.TrimSpace(match[1])
		schema, name := resolveTableName(tableName, defaultSchema)
		return &TableRef{Schema: schema, Name: name}
	}

	re2 := regexp.MustCompile(`(?i)\bDELETE\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?\s+FROM)`)
	match2 := re2.FindStringSubmatch(sql)
	if len(match2) >= 2 {
		tablePart := match2[1]
		tablePart = strings.TrimSuffix(strings.TrimSpace(tablePart), " FROM")
		tablePart = strings.TrimSuffix(strings.TrimSpace(tablePart), " from")
		tableName := strings.TrimSpace(tablePart)
		schema, name := resolveTableName(tableName, defaultSchema)
		return &TableRef{Schema: schema, Name: name}
	}

	return nil
}

func extractDDLTarget(sql string, defaultSchema string) *TableRef {
	patterns := []string{
		`(?i)\bALTER\s+TABLE\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
		`(?i)\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
		`(?i)\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
		`(?i)\bTRUNCATE\s+(?:TABLE\s+)?((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
		`(?i)\bRENAME\s+TABLE\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		match := re.FindStringSubmatch(sql)
		if len(match) >= 2 {
			tableName := strings.TrimSpace(match[1])
			schema, name := resolveTableName(tableName, defaultSchema)
			return &TableRef{Schema: schema, Name: name}
		}
	}
	return nil
}

func CheckSQLPermission(analysis *SQLAnalysis, connId, authorization string) {
	if !config.Cfg.IsRemote {
		return
	}

	for _, t := range analysis.ReadTables {
		admin.CheckSchemaAccess(connId, t.Schema, authorization)
		admin.CheckTableAccess(connId, t.Schema, t.Name, authorization)
	}

	for _, t := range analysis.WriteTables {
		admin.CheckSchemaAccess(connId, t.Schema, authorization)
		admin.CheckTableAccess(connId, t.Schema, t.Name, authorization)
	}

	if len(analysis.WriteColumns) > 0 {
		for _, c := range analysis.WriteColumns {
			admin.CheckColumnAccess(connId, analysis.WriteTables[0].Schema, c.TableName, c.ColumnName, authorization)
		}
	}
}

func parseColumnNameForPerm(raw string) string {
	if idx := strings.Index(raw, "  "); idx > 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}

func GetTableColumnAccess(connId, schemaName, tableName, authorization string) *TableColumnAccess {
	if !config.Cfg.IsRemote {
		return &TableColumnAccess{Level: AccessFull}
	}

	userPower := admin.GetUserPower(authorization)
	if userPower == nil {
		return &TableColumnAccess{Level: AccessNone}
	}

	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return &TableColumnAccess{Level: AccessNone}
	}

	hasConnLevel := false
	hasSchemaLevel := false
	hasTableLevel := false
	hasTableOrColumnForSchema := false
	allowedCols := make(map[string]bool)

	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			hasConnLevel = true
		case "schema":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				hasSchemaLevel = true
			}
		case "table":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				hasTableOrColumnForSchema = true
				if p.TableName != nil && *p.TableName == tableName {
					hasTableLevel = true
				}
			}
		case "column":
			if p.SchemaName != nil && *p.SchemaName == schemaName {
				hasTableOrColumnForSchema = true
				if p.TableName != nil && *p.TableName == tableName && p.ColumnName != nil {
					colName := parseColumnNameForPerm(*p.ColumnName)
					allowedCols[colName] = true
				}
			}
		}
	}

	if hasConnLevel && !hasTableOrColumnForSchema {
		return &TableColumnAccess{Level: AccessFull}
	}
	if hasSchemaLevel && !hasTableOrColumnForSchema {
		return &TableColumnAccess{Level: AccessFull}
	}
	if hasTableLevel {
		return &TableColumnAccess{Level: AccessFull}
	}
	if len(allowedCols) > 0 {
		return &TableColumnAccess{Level: AccessColumn, AllowedColumns: allowedCols}
	}
	return &TableColumnAccess{Level: AccessNone}
}

func GetTableAccessDowngraded(connId, schemaName, tableName, authorization string) *TableColumnAccess {
	access := GetTableColumnAccess(connId, schemaName, tableName, authorization)
	if access.Level == AccessColumn {
		access.Level = AccessFull
	}
	return access
}

func FilterColumnsByPermission(columnNames []string, access *TableColumnAccess) []string {
	if access.Level == AccessFull {
		return columnNames
	}
	if access.Level == AccessNone {
		return []string{}
	}

	filtered := make([]string, 0, len(columnNames))
	for _, col := range columnNames {
		if access.AllowedColumns[col] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func FilterColumnsForTables(columnNames []string, connId string, tableRefs []TableRef, authorization string) []string {
	if !config.Cfg.IsRemote {
		return columnNames
	}

	userPower := admin.GetUserPower(authorization)
	if userPower == nil {
		return []string{}
	}

	anyColumnLevel := false
	for _, t := range tableRefs {
		access := GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		if access.Level == AccessNone {
			return []string{}
		}
		if access.Level == AccessColumn {
			anyColumnLevel = true
		}
	}

	if !anyColumnLevel {
		return columnNames
	}

	allowedSet := make(map[string]bool)
	for _, t := range tableRefs {
		access := GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		if access.Level == AccessFull {
			for _, col := range columnNames {
				allowedSet[col] = true
			}
		} else if access.Level == AccessColumn {
			for _, col := range columnNames {
				if access.AllowedColumns[col] {
					allowedSet[col] = true
				}
			}
		}
	}

	filtered := make([]string, 0, len(columnNames))
	for _, col := range columnNames {
		if allowedSet[col] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func CheckTablePermission(connId, schemaName, tableName, authorization string) {
	if !config.Cfg.IsRemote {
		return
	}
	admin.CheckSchemaAccess(connId, schemaName, authorization)
	admin.CheckTableAccess(connId, schemaName, tableName, authorization)
}

func CheckTableWritePermission(connId string, schemaName string, tableName string, columns []string, authorization string) {
	if !config.Cfg.IsRemote {
		return
	}
	admin.CheckSchemaAccess(connId, schemaName, authorization)
	admin.CheckTableAccess(connId, schemaName, tableName, authorization)
}

func StripComments(sql string) string {
	re1 := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	sql = re1.ReplaceAllString(sql, " ")
	re2 := regexp.MustCompile(`--[^\n]*`)
	sql = re2.ReplaceAllString(sql, " ")
	re3 := regexp.MustCompile(`#[^\n]*`)
	sql = re3.ReplaceAllString(sql, " ")
	return sql
}

func ExtractTablesFromSQL(sql string) []string {
	sql = StripComments(sql)
	tables := make(map[string]bool)

	primaryRegex := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+((?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)\.)?(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+))`)
	commaRegex := regexp.MustCompile(`\s*,\s*((?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)\.)?(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+))`)
	metadataRegex := regexp.MustCompile(`(?i)\b(?:DESCRIBE|DESC|SHOW\s+CREATE\s+TABLE)\s+((?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)\.)?(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+))`)
	cteRegex := regexp.MustCompile(`(?i)\bWITH\s+(\w+)\s+AS\s*\(`)

	cteNames := make(map[string]bool)
	for _, match := range cteRegex.FindAllStringSubmatch(sql, -1) {
		if len(match) > 1 {
			cteNames[strings.ToLower(match[1])] = true
		}
	}

	for _, idx := range primaryRegex.FindAllStringSubmatchIndex(sql, -1) {
		if len(idx) >= 4 {
			tableName := stripBackticks(sql[idx[2]:idx[3]])
			if !isSQLKeyword(tableName) && !cteNames[strings.ToLower(tableName)] {
				tables[tableName] = true
			}

			afterMatch := sql[idx[1]:]
			stopRegex := regexp.MustCompile(`(?i)\b(?:WHERE|GROUP\s+BY|ORDER\s+BY|HAVING|LIMIT|OFFSET|UNION|INTERSECT|EXCEPT|VALUES|SET|ON)\b`)
			if stopMatch := stopRegex.FindStringIndex(afterMatch); stopMatch != nil {
				afterMatch = afterMatch[:stopMatch[0]]
			}

			afterMatch = skipTableAlias(afterMatch)

			for {
				trimmed := strings.TrimLeft(afterMatch, " \t\n\r")
				if !strings.HasPrefix(trimmed, ",") {
					break
				}
				commaMatch := commaRegex.FindStringSubmatch(trimmed)
				if len(commaMatch) < 2 {
					break
				}
				commaTableName := stripBackticks(commaMatch[1])
				if !isSQLKeyword(commaTableName) && !cteNames[strings.ToLower(commaTableName)] {
					remainingAfterTable := trimmed[len(commaMatch[0]):]
					if len(remainingAfterTable) == 0 || remainingAfterTable[0] != '(' {
						tables[commaTableName] = true
					}
				}
				afterMatch = skipTableAlias(trimmed[len(commaMatch[0]):])
			}
		}
	}

	for _, match := range metadataRegex.FindAllStringSubmatch(sql, -1) {
		if len(match) > 1 {
			tableName := stripBackticks(match[1])
			if !isSQLKeyword(tableName) {
				tables[tableName] = true
			}
		}
	}

	result := make([]string, 0, len(tables))
	for table := range tables {
		result = append(result, table)
	}
	return result
}

func skipTableAlias(s string) string {
	s = strings.TrimLeft(s, " \t\n\r")
	asRegex := regexp.MustCompile(`(?i)^AS\s+\w+`)
	if loc := asRegex.FindStringIndex(s); loc != nil {
		return s[loc[1]:]
	}
	if len(s) > 0 && s[0] != ',' && s[0] != '(' && s[0] != ')' {
		identRegex := regexp.MustCompile(`^\w+`)
		if loc := identRegex.FindStringIndex(s); loc != nil {
			word := s[:loc[1]]
			if !isSQLKeyword(word) {
				return s[loc[1]:]
			}
		}
	}
	return s
}

func stripBackticks(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '`' && s[len(s)-1] == '`') ||
			(s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func isSQLKeyword(s string) bool {
	keywords := map[string]bool{
		"DUAL": true, "AS": true, "SET": true, "SELECT": true,
		"FROM": true, "WHERE": true, "JOIN": true, "INNER": true,
		"LEFT": true, "RIGHT": true, "OUTER": true, "ON": true,
		"GROUP": true, "BY": true, "ORDER": true, "HAVING": true,
		"LIMIT": true, "OFFSET": true, "UNION": true, "ALL": true,
		"INSERT": true, "INTO": true, "VALUES": true, "UPDATE": true,
		"DELETE": true, "CREATE": true, "ALTER": true, "DROP": true,
		"TABLE": true, "INDEX": true, "VIEW": true, "DATABASE": true,
		"SCHEMA": true, "COLUMN": true, "KEY": true, "PRIMARY": true,
		"FOREIGN": true, "REFERENCES": true, "CONSTRAINT": true,
		"AND": true, "OR": true, "NOT": true, "IN": true, "EXISTS": true,
		"BETWEEN": true, "LIKE": true, "IS": true, "NULL": true,
		"ASC": true, "DESC": true, "DISTINCT": true, "COUNT": true,
		"SUM": true, "AVG": true, "MIN": true, "MAX": true,
	}
	return keywords[strings.ToUpper(s)]
}

type SQLPermissionResult struct {
	Allowed        bool
	DeniedTables   []string
	DeniedColumns  []string
	AllowedColumns []string
	Message        string
}

func CheckUserCanModify(authorization string) bool {
	if !config.Cfg.IsRemote {
		return true
	}
	user := admin.GetUser(authorization)
	if user == nil {
		return false
	}
	roles := admin.FindUserRoles(user.Id)
	for _, role := range roles {
		if role.AllowModify > 0 {
			return true
		}
	}
	return false
}

func CheckSQLFullPermission(sqlStr, connId, schema, authorization string) *SQLPermissionResult {
	analysis := AnalyzeSQL(sqlStr, schema)
	return CheckAnalysisPermission(analysis, connId, authorization)
}

func CheckAnalysisPermission(analysis *SQLAnalysis, connId, authorization string) *SQLPermissionResult {
	result := &SQLPermissionResult{Allowed: true}

	if len(analysis.WriteTables) > 0 || len(analysis.WriteColumns) > 0 {
		if !CheckUserCanModify(authorization) {
			result.Allowed = false
			result.Message = "当前角色禁止修改数据，无法执行写操作"
			return result
		}
	}

	for _, t := range analysis.ReadTables {
		access := GetTableAccessDowngraded(connId, t.Schema, t.Name, authorization)
		if access.Level == AccessNone {
			result.Allowed = false
			result.DeniedTables = append(result.DeniedTables, t.Name)
		}
	}

	for _, t := range analysis.WriteTables {
		access := GetTableAccessDowngraded(connId, t.Schema, t.Name, authorization)
		if access.Level == AccessNone {
			result.Allowed = false
			result.DeniedTables = append(result.DeniedTables, t.Name)
		}
	}

	if !result.Allowed {
		result.Message = fmt.Sprintf("无权访问表: %s", strings.Join(result.DeniedTables, ", "))
		return result
	}

	return result
}

type SelectColumn struct {
	TableAlias string
	ColumnName string
	IsStar     bool
	Expression string
}

func ExtractSelectColumns(sql string) []SelectColumn {
	upper := strings.ToUpper(strings.TrimSpace(sql))

	if strings.HasPrefix(upper, "WITH") {
		lastSelect := strings.LastIndex(upper, "SELECT")
		if lastSelect > 0 {
			sql = sql[lastSelect:]
			upper = strings.ToUpper(sql)
		}
	}

	if !strings.HasPrefix(upper, "SELECT") {
		return nil
	}

	selectBody := extractSelectBody(sql)
	if selectBody == "" {
		return nil
	}

	trimmed := strings.TrimSpace(selectBody)
	if trimmed == "*" {
		return []SelectColumn{{IsStar: true, Expression: "*"}}
	}

	parts := splitByCommaRespectParens(trimmed)
	var cols []SelectColumn

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.HasSuffix(part, ".*") {
			cols = append(cols, SelectColumn{
				TableAlias: strings.TrimSuffix(part, ".*"),
				IsStar:     true,
				Expression: part,
			})
			continue
		}

		col := removeAlias(part)

		if isFunctionCall(col) {
			innerCols := extractColumnsFromExpression(col)
			cols = append(cols, innerCols...)
			continue
		}

		if isConstant(col) {
			continue
		}

		sc := parseColumnRef(col)
		sc.Expression = part
		cols = append(cols, sc)
	}

	return cols
}

func extractSelectBody(sql string) string {
	upper := strings.ToUpper(sql)

	start := 6
	rest := strings.TrimSpace(sql[start:])
	upperRest := strings.ToUpper(rest)
	if strings.HasPrefix(upperRest, "DISTINCT ") {
		start += 9 + (len(sql[start:]) - len(rest))
		rest = strings.TrimSpace(sql[start:])
	} else if strings.HasPrefix(upperRest, "ALL ") {
		start += 4 + (len(sql[start:]) - len(rest))
		rest = strings.TrimSpace(sql[start:])
	}

	fromIdx := findTopLevelKeyword(upper[start:], "FROM")
	if fromIdx == -1 {
		return rest
	}

	return strings.TrimSpace(sql[start : start+fromIdx])
}

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

func removeAlias(expr string) string {
	upper := strings.ToUpper(strings.TrimSpace(expr))

	asIdx := findTopLevelKeyword(upper, " AS ")
	if asIdx >= 0 {
		return strings.TrimSpace(expr[:asIdx])
	}

	return strings.TrimSpace(expr)
}

func isFunctionCall(expr string) bool {
	trimmed := strings.TrimSpace(expr)
	re := regexp.MustCompile(`(?i)^\w+\s*\(`)
	return re.MatchString(trimmed)
}

func extractColumnsFromExpression(expr string) []SelectColumn {
	var cols []SelectColumn
	re := regexp.MustCompile(`(?i)(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)`)
	matches := re.FindAllString(expr, -1)
	for _, m := range matches {
		sc := parseColumnRef(m)
		if !isSQLKeyword(sc.ColumnName) && sc.ColumnName != "*" {
			cols = append(cols, sc)
		}
	}

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

func isConstant(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if matched, _ := regexp.MatchString(`^-?\d+(\.\d+)?$`, s); matched {
		return true
	}
	if (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) ||
		(strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) {
		return true
	}
	if strings.EqualFold(s, "NULL") || strings.EqualFold(s, "TRUE") || strings.EqualFold(s, "FALSE") {
		return true
	}
	return false
}

func parseColumnRef(ref string) SelectColumn {
	ref = strings.TrimSpace(ref)

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

func splitDottedIdentifier(s string) []string {
	var parts []string
	current := strings.Builder{}
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c == '`' || c == '"') && !inQuote {
			inQuote = true
			quoteChar = c
			continue
		}
		if inQuote && c == quoteChar {
			inQuote = false
			quoteChar = 0
			continue
		}
		if c == '.' && !inQuote {
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

func resolveAliasToTable(alias string, tables []TableRef) string {
	alias = stripBackticks(alias)
	for _, t := range tables {
		if strings.EqualFold(t.Name, alias) {
			return t.Name
		}
	}
	return alias
}

func getAccessForTable(connId, tableName string, tables []TableRef, authorization string) *TableColumnAccess {
	for _, t := range tables {
		if strings.EqualFold(t.Name, tableName) {
			return GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		}
	}
	return nil
}

func checkColumnInAnyTable(connId, columnName string, tables []TableRef, authorization string) bool {
	for _, t := range tables {
		access := GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		if access.Level == AccessColumn {
			if !access.AllowedColumns[columnName] {
				return true
			}
		}
	}
	return false
}

func FilterSchemaByColumnPermission(ddl string, tableName, connId, schema, authorization string) string {
	access := GetTableColumnAccess(connId, schema, tableName, authorization)
	if access.Level == AccessFull || access.Level == AccessNone {
		if access.Level == AccessNone {
			return ""
		}
		return ddl
	}

	lines := strings.Split(ddl, "\n")
	var filtered []string
	columnDefRegex := regexp.MustCompile("(?i)^\\s+[`\"']?(\\w+)[`\"']?\\s+")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperTrimmed := strings.ToUpper(trimmed)

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

		match := columnDefRegex.FindStringSubmatch(line)
		if len(match) >= 2 {
			colName := match[1]
			if access.AllowedColumns[colName] {
				filtered = append(filtered, line)
			}
		} else {
			filtered = append(filtered, line)
		}
	}

	return strings.Join(filtered, "\n")
}
