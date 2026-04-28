package admin

import (
	"go-web/config"
	"regexp"
	"strings"
)

type SQLAnalysis struct {
	OperationType string
	ReadTables    []TableRef
	WriteTables   []TableRef
	WriteColumns  []ColumnRef
	OriginalSQL   string // 原始 SQL（供列级权限检查使用）
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

// CheckSQLPermission 旧版权限检查（仅表级 + 写列级）
// Deprecated: 请使用 CheckAnalysisPermission 进行完整的表+列权限校验
func CheckSQLPermission(analysis *SQLAnalysis, connId, authorization string) {
	if !config.Cfg.IsRemote {
		return
	}

	for _, t := range analysis.ReadTables {
		checkSchemaAccess(connId, t.Schema, authorization)
		checkTableAccess(connId, t.Schema, t.Name, authorization)
	}

	for _, t := range analysis.WriteTables {
		checkSchemaAccess(connId, t.Schema, authorization)
		checkTableAccess(connId, t.Schema, t.Name, authorization)
	}

	if len(analysis.WriteColumns) > 0 {
		for _, c := range analysis.WriteColumns {
			checkColumnAccess(connId, analysis.WriteTables[0].Schema, c.TableName, c.ColumnName, authorization)
		}
	}
}

func GetTableColumnAccess(connId, schemaName, tableName, authorization string) *TableColumnAccess {
	if !config.Cfg.IsRemote {
		return &TableColumnAccess{Level: AccessFull}
	}

	userPower := GetUserPower(authorization)
	if userPower == nil {
		return &TableColumnAccess{Level: AccessNone}
	}
	if userPower.UserId == config.AdminId {
		return &TableColumnAccess{Level: AccessFull}
	}

	powerDetails := findUserPowerDetails(userPower.UserId)
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
					allowedCols[*p.ColumnName] = true
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

	userPower := GetUserPower(authorization)
	if userPower == nil {
		return []string{}
	}
	if userPower.UserId == config.AdminId {
		return columnNames
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
	checkSchemaAccess(connId, schemaName, authorization)
	checkTableAccess(connId, schemaName, tableName, authorization)
}

func CheckTableWritePermission(connId string, schemaName string, tableName string, columns []string, authorization string) {
	if !config.Cfg.IsRemote {
		return
	}
	checkSchemaAccess(connId, schemaName, authorization)
	checkTableAccess(connId, schemaName, tableName, authorization)
	for _, col := range columns {
		checkColumnAccess(connId, schemaName, tableName, col, authorization)
	}
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

	primaryRegex := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+((?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\.)?(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))`)
	commaRegex := regexp.MustCompile(`\s*,\s*((?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\.)?(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))`)
	metadataRegex := regexp.MustCompile(`(?i)\b(?:DESCRIBE|DESC|SHOW\s+CREATE\s+TABLE)\s+((?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\.)?(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))`)
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
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1]
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
