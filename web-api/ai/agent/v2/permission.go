package agentv2

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"go-web/config"
	admin "go-web/web-api/admin"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ──────────────────────────────────────────────
// PermissionScope 权限范围
// ──────────────────────────────────────────────

type PermissionScope struct {
	UserID              string
	ConnID              string
	SchemaName          string
	IsRemote            bool
	HasFullConnAccess   bool
	HasFullSchemaAccess bool
	AllowedTables       map[string]bool
	AllowedColumns      map[string]map[string]bool
}

type PermissionError struct {
	Message string
	Objects []string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("%s: %v", e.Message, e.Objects)
}

// BuildPermissionScope 构建权限范围
// 权限层级：conn → schema → table → column
// 最具体优先原则：当同一schema下存在table/column级权限时，schema级权限不生效
func BuildPermissionScope(userId, connId, schemaName string) *PermissionScope {
	scope := &PermissionScope{
		UserID:         userId,
		ConnID:         connId,
		SchemaName:     schemaName,
		IsRemote:       config.Cfg.IsRemote,
		AllowedTables:  make(map[string]bool),
		AllowedColumns: make(map[string]map[string]bool),
	}

	if !scope.IsRemote {
		return scope
	}

	powerList := admin.FindUserPowerDetails(userId)

	hasConnPerm := false
	hasSchemaPerm := false
	hasTableOrColumnForSchema := false

	for _, power := range powerList {
		if power.ConnId != connId {
			continue
		}

		pSchema := ""
		if power.SchemaName != nil {
			pSchema = *power.SchemaName
		}
		pTable := ""
		if power.TableName != nil {
			pTable = *power.TableName
		}
		pColumn := ""
		if power.ColumnName != nil {
			pColumn = *power.ColumnName
		}

		switch power.Level {
		case "conn":
			hasConnPerm = true
		case "schema":
			if schemaName == "" || pSchema == schemaName {
				hasSchemaPerm = true
			}
		case "table":
			if (schemaName == "" || pSchema == schemaName) && pTable != "" {
				scope.AllowedTables[pTable] = true
				hasTableOrColumnForSchema = true
			}
		case "column":
			if (schemaName == "" || pSchema == schemaName) && pTable != "" && pColumn != "" {
				hasTableOrColumnForSchema = true
				if !scope.AllowedTables[pTable] {
					if scope.AllowedColumns[pTable] == nil {
						scope.AllowedColumns[pTable] = make(map[string]bool)
					}
					scope.AllowedColumns[pTable][pColumn] = true
				}
			}
		}
	}

	if hasConnPerm && !hasTableOrColumnForSchema {
		scope.HasFullConnAccess = true
		return scope
	}
	if hasSchemaPerm && !hasTableOrColumnForSchema {
		scope.HasFullSchemaAccess = true
		return scope
	}

	return scope
}

func (s *PermissionScope) SkipChecks() bool {
	return !s.IsRemote || s.HasFullConnAccess
}

func (s *PermissionScope) HasAnyAccess() bool {
	return s.HasFullConnAccess || s.HasFullSchemaAccess || len(s.AllowedTables) > 0 || len(s.AllowedColumns) > 0
}

func (s *PermissionScope) IsTableAllowed(table string) bool {
	if s.SkipChecks() || s.HasFullSchemaAccess {
		return true
	}
	return s.AllowedTables[table] || len(s.AllowedColumns[table]) > 0
}

func (s *PermissionScope) IsColumnAllowed(table, column string) bool {
	if s.SkipChecks() || s.HasFullSchemaAccess || s.AllowedTables[table] {
		return true
	}
	if s.AllowedColumns[table] != nil {
		return s.AllowedColumns[table][column]
	}
	return false
}

func (s *PermissionScope) GetTableAccessLevel(table string) string {
	if s.SkipChecks() || s.HasFullSchemaAccess || s.AllowedTables[table] {
		return "full"
	}
	if len(s.AllowedColumns[table]) > 0 {
		return "column"
	}
	return "none"
}

// FilterResultColumns 过滤结果列（列级权限）
func (s *PermissionScope) FilterResultColumns(columns []string, data []map[string]any, tables []string) ([]string, []map[string]any) {
	if s.SkipChecks() {
		return columns, data
	}

	hasRestrictions := false
	for _, table := range tables {
		if s.GetTableAccessLevel(table) == "column" {
			hasRestrictions = true
			break
		}
	}
	if !hasRestrictions {
		return columns, data
	}

	allowedCols := make(map[string]bool)
	for table, cols := range s.AllowedColumns {
		for _, t := range tables {
			if t == table {
				for col := range cols {
					allowedCols[col] = true
				}
			}
		}
	}

	filteredCols := make([]string, 0)
	for _, col := range columns {
		if allowedCols[col] {
			filteredCols = append(filteredCols, col)
		}
	}

	filteredData := make([]map[string]any, 0, len(data))
	for _, row := range data {
		filteredRow := make(map[string]any)
		for _, col := range filteredCols {
			if val, ok := row[col]; ok {
				filteredRow[col] = val
			}
		}
		filteredData = append(filteredData, filteredRow)
	}

	return filteredCols, filteredData
}

// DescribeForPrompt 生成权限描述用于系统提示词
func (s *PermissionScope) DescribeForPrompt() string {
	if !s.IsRemote || s.HasFullConnAccess {
		return ""
	}

	if s.HasFullSchemaAccess {
		return fmt.Sprintf("\n\n## 数据权限\n拥有 Schema %s 的完整访问权限。禁止访问其他 Schema。", s.SchemaName)
	}

	var sb strings.Builder
	sb.WriteString("\n\n## 数据权限（最高优先级）\n")
	sb.WriteString("绝对禁止使用、提及任何未授权表的信息。\n\n")

	if len(s.AllowedTables) > 0 {
		tables := make([]string, 0, len(s.AllowedTables))
		for t := range s.AllowedTables {
			tables = append(tables, t)
		}
		fmt.Fprintf(&sb, "表级权限（可访问所有字段）：%s\n\n", strings.Join(tables, ", "))
	}

	if len(s.AllowedColumns) > 0 {
		for table, cols := range s.AllowedColumns {
			if s.AllowedTables[table] {
				continue
			}
			colList := make([]string, 0, len(cols))
			for col := range cols {
				colList = append(colList, col)
			}
			fmt.Fprintf(&sb, "字段级权限 - 表 `%s`：仅允许 [%s]，其他字段禁止使用\n", table, strings.Join(colList, ", "))
		}
	}

	return sb.String()
}

// ──────────────────────────────────────────────
// SQL 表名提取
// ──────────────────────────────────────────────

func extractTablesFromSQL(sql string) []string {
	tables := make(map[string]bool)

	primaryRegex := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+(?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\.)?(` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)`)
	commaRegex := regexp.MustCompile(`\s*,\s*(?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\.)?(` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)`)
	metadataRegex := regexp.MustCompile(`(?i)\b(?:DESCRIBE|DESC|SHOW\s+CREATE\s+TABLE)\s+(?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)\.)?(` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)`)
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

// ──────────────────────────────────────────────
// PermissionMiddleware 权限中间件
// ──────────────────────────────────────────────

type PermissionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	Scope *PermissionScope
}

func (m *PermissionMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if m.Scope.SkipChecks() {
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		switch tCtx.Name {
		case "get_table_schema":
			return m.checkSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "query_data":
			return m.checkQueryAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "exec_sql":
			return m.checkExecAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "export_excel", "export_excel_with_chart", "export_analysis_image", "export_analysis_docx", "export_ppt":
			return m.checkExportAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "import_data":
			return m.checkImportAccess(ctx, argumentsInJSON, endpoint, opts...)
		default:
			return endpoint(ctx, argumentsInJSON, opts...)
		}
	}, nil
}

func (m *PermissionMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if m.Scope.SkipChecks() {
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		// 对流式调用做同样的权限检查
		switch tCtx.Name {
		case "get_table_schema":
			return m.checkStreamSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "query_data", "exec_sql", "export_excel", "export_excel_with_chart", "export_analysis_image", "export_analysis_docx", "export_ppt":
			return m.checkStreamSQLAccess(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
		default:
			return endpoint(ctx, argumentsInJSON, opts...)
		}
	}, nil
}

func (m *PermissionMiddleware) checkSchemaAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input SchemaInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", err
	}

	filtered := make([]string, 0)
	for _, table := range input.Tables {
		if m.Scope.IsTableAllowed(table) {
			filtered = append(filtered, table)
		}
	}

	if len(filtered) == 0 {
		output := SchemaOutput{Schema: "提示：请提供正确的表名。您传入的名称无法访问。"}
		outputJSON, _ := json.Marshal(output)
		return string(outputJSON), nil
	}

	input.Tables = filtered
	newArgs, _ := json.Marshal(input)
	result, err := endpoint(ctx, string(newArgs), opts...)
	if err != nil {
		return "", err
	}

	// 对返回的 DDL 按列级权限过滤
	hasColumnRestrictions := false
	for _, table := range filtered {
		if m.Scope.GetTableAccessLevel(table) == "column" {
			hasColumnRestrictions = true
			break
		}
	}

	if !hasColumnRestrictions {
		return result, nil
	}

	var output SchemaOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		return result, nil
	}

	if output.Schema != "" {
		filteredSchema := filterDDLByScope(output.Schema, filtered, m.Scope)
		if filteredSchema != "" {
			output.Schema = filteredSchema
			outputJSON, _ := json.Marshal(output)
			return string(outputJSON), nil
		}
	}

	return result, nil
}

func (m *PermissionMiddleware) checkQueryAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input QueryInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", err
	}

	// 表级权限检查
	tables := extractTablesFromSQL(input.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			return "", &PermissionError{Message: "无权访问表", Objects: []string{table}}
		}
	}

	// 列级权限检查（SELECT 中显式引用的列）
	selectCols := admin.ExtractSelectColumns(admin.StripComments(strings.TrimSpace(input.SQL)))
	if len(selectCols) > 0 {
		for _, sc := range selectCols {
			if sc.IsStar {
				continue // SELECT * 由结果集过滤兜底
			}
			colName := sc.ColumnName
			// 检查所有有列级限制的表
			// 策略：只有当所有有列级限制的表都不允许该列时才拒绝
			// 原因：无法可靠地将别名映射到真实表名，且结果集过滤是最终兜底
			allDenied := false
			hasColumnLevelTable := false
			for _, table := range tables {
				if m.Scope.GetTableAccessLevel(table) == "column" {
					hasColumnLevelTable = true
					if m.Scope.IsColumnAllowed(table, colName) {
						allDenied = false
						break // 至少有一个表允许，放行
					}
					allDenied = true
				}
			}
			if hasColumnLevelTable && allDenied {
				displayName := colName
				if sc.TableAlias != "" {
					displayName = sc.TableAlias + "." + colName
				}
				return "", &PermissionError{
					Message: fmt.Sprintf("无权访问字段 %s", displayName),
					Objects: []string{displayName},
				}
			}
		}
	}

	result, err := endpoint(ctx, args, opts...)
	if err != nil {
		return "", err
	}

	// 列级过滤（结果集兜底保护）
	var output QueryOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		return result, nil
	}

	hasColumnRestrictions := false
	for _, table := range tables {
		if m.Scope.GetTableAccessLevel(table) == "column" {
			hasColumnRestrictions = true
			break
		}
	}
	if !hasColumnRestrictions {
		return result, nil
	}

	output.Columns, output.Data = m.Scope.FilterResultColumns(output.Columns, output.Data, tables)
	output.Count = len(output.Data)
	outputJSON, _ := json.Marshal(output)
	return string(outputJSON), nil
}

func (m *PermissionMiddleware) checkExecAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input ExecInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", err
	}

	// 表级权限检查
	for _, table := range extractTablesFromSQL(input.SQL) {
		if !m.Scope.IsTableAllowed(table) {
			return "", &PermissionError{Message: fmt.Sprintf("无权访问表 %s", table), Objects: []string{table}}
		}
	}

	// 列级权限检查（INSERT/UPDATE 中操作的列）
	analysis := admin.AnalyzeSQL(input.SQL, m.Scope.SchemaName)
	if len(analysis.WriteColumns) > 0 && len(analysis.WriteTables) > 0 {
		writeTableName := analysis.WriteTables[0].Name
		if m.Scope.GetTableAccessLevel(writeTableName) == "column" {
			for _, col := range analysis.WriteColumns {
				if !m.Scope.IsColumnAllowed(writeTableName, col.ColumnName) {
					return "", &PermissionError{
						Message: fmt.Sprintf("无权操作字段 %s.%s", writeTableName, col.ColumnName),
						Objects: []string{writeTableName + "." + col.ColumnName},
					}
				}
			}
		}
	}

	return endpoint(ctx, args, opts...)
}

func (m *PermissionMiddleware) checkExportAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input ExportInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", err
	}

	tables := extractTablesFromSQL(input.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			return "", &PermissionError{Message: fmt.Sprintf("无权访问表 %s", table), Objects: []string{table}}
		}
	}

	// 列级权限检查（SELECT 中显式引用的列）
	selectCols := admin.ExtractSelectColumns(admin.StripComments(strings.TrimSpace(input.SQL)))
	if len(selectCols) > 0 {
		for _, sc := range selectCols {
			if sc.IsStar {
				continue
			}
			colName := sc.ColumnName
			allDenied := false
			hasColumnLevelTable := false
			for _, table := range tables {
				if m.Scope.GetTableAccessLevel(table) == "column" {
					hasColumnLevelTable = true
					if m.Scope.IsColumnAllowed(table, colName) {
						allDenied = false
						break
					}
					allDenied = true
				}
			}
			if hasColumnLevelTable && allDenied {
				return "", &PermissionError{
					Message: fmt.Sprintf("无权导出字段 %s", colName),
					Objects: []string{colName},
				}
			}
		}
	}

	return endpoint(ctx, args, opts...)
}

func (m *PermissionMiddleware) checkImportAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input ImportDataInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", err
	}

	if input.TableName != "" && !m.Scope.IsTableAllowed(input.TableName) {
		return "", &PermissionError{Message: fmt.Sprintf("无权访问表 %s", input.TableName), Objects: []string{input.TableName}}
	}

	// 列级权限检查：如果有映射，检查目标列是否有写权限
	if input.TableName != "" && m.Scope.GetTableAccessLevel(input.TableName) == "column" {
		if len(input.Mapping) > 0 {
			for _, dbCol := range input.Mapping {
				if !m.Scope.IsColumnAllowed(input.TableName, dbCol) {
					return "", &PermissionError{
						Message: fmt.Sprintf("无权写入字段 %s.%s", input.TableName, dbCol),
						Objects: []string{input.TableName + "." + dbCol},
					}
				}
			}
		}
	}

	return endpoint(ctx, args, opts...)
}

// filterDDLByScope 根据 PermissionScope 过滤 DDL 中的未授权列
func filterDDLByScope(ddl string, tables []string, scope *PermissionScope) string {
	lines := strings.Split(ddl, "\n")
	var filtered []string
	columnDefRegex := regexp.MustCompile("(?i)^\\s+[`\"']?(\\w+)[`\"']?\\s+")
	createTableRegex := regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[`\"']?(\\w+)[`\"']?")

	currentTable := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperTrimmed := strings.ToUpper(trimmed)

		// 检测 CREATE TABLE 行，更新当前表名
		if strings.HasPrefix(upperTrimmed, "CREATE ") {
			if match := createTableRegex.FindStringSubmatch(line); len(match) >= 2 {
				currentTable = match[1]
			}
			filtered = append(filtered, line)
			continue
		}

		// 保留非列定义行
		if strings.HasPrefix(upperTrimmed, ")") ||
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
			// 根据当前表名检查列权限
			if currentTable != "" {
				accessLevel := scope.GetTableAccessLevel(currentTable)
				if accessLevel == "full" {
					filtered = append(filtered, line)
				} else if accessLevel == "column" {
					if scope.IsColumnAllowed(currentTable, colName) {
						filtered = append(filtered, line)
					}
					// 未授权列：跳过
				}
				// accessLevel == "none"：跳过
			} else {
				// 无法确定当前表，保守保留
				filtered = append(filtered, line)
			}
		} else {
			// 无法解析的行保留
			filtered = append(filtered, line)
		}
	}

	return strings.Join(filtered, "\n")
}

func (m *PermissionMiddleware) checkStreamSchemaAccess(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, opts ...tool.Option) (*schema.StreamReader[string], error) {
	var input SchemaInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return nil, err
	}

	filtered := make([]string, 0)
	for _, table := range input.Tables {
		if m.Scope.IsTableAllowed(table) {
			filtered = append(filtered, table)
		}
	}

	if len(filtered) == 0 {
		sr, sw := schema.Pipe[string](1)
		sw.Send("提示：请提供正确的表名。您传入的名称无法访问。", nil)
		sw.Close()
		return sr, nil
	}

	input.Tables = filtered
	newArgs, _ := json.Marshal(input)

	// 检查是否有列级限制
	hasColumnRestrictions := false
	for _, table := range filtered {
		if m.Scope.GetTableAccessLevel(table) == "column" {
			hasColumnRestrictions = true
			break
		}
	}

	if !hasColumnRestrictions {
		return endpoint(ctx, string(newArgs), opts...)
	}

	// 有列级限制时，需要消费流式结果并过滤 DDL
	reader, err := endpoint(ctx, string(newArgs), opts...)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for {
		chunk, recvErr := reader.Recv()
		if recvErr != nil {
			break
		}
		sb.WriteString(chunk)
	}
	rawResult := sb.String()

	var output SchemaOutput
	if err := json.Unmarshal([]byte(rawResult), &output); err != nil {
		return schema.StreamReaderFromArray([]string{rawResult}), nil
	}
	if output.Schema != "" {
		output.Schema = filterDDLByScope(output.Schema, filtered, m.Scope)
	}
	outputJSON, _ := json.Marshal(output)
	return schema.StreamReaderFromArray([]string{string(outputJSON)}), nil
}

func (m *PermissionMiddleware) checkStreamSQLAccess(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, toolName string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	// 提取 SQL 字段
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return nil, err
	}

	sqlStr, _ := raw["sql"].(string)
	if sqlStr != "" {
		tables := extractTablesFromSQL(sqlStr)
		for _, table := range tables {
			if !m.Scope.IsTableAllowed(table) {
				log.Printf("[PermissionMiddleware:Stream] 表权限检查失败 - tool=%s, table=%s\n", toolName, table)
				sr, sw := schema.Pipe[string](1)
				sw.Send(fmt.Sprintf("无权访问表：%s", table), nil)
				sw.Close()
				return sr, nil
			}
		}

		// 列级权限检查
		selectCols := admin.ExtractSelectColumns(admin.StripComments(strings.TrimSpace(sqlStr)))
		for _, sc := range selectCols {
			if sc.IsStar {
				continue
			}
			allDenied := false
			hasColumnLevelTable := false
			for _, table := range tables {
				if m.Scope.GetTableAccessLevel(table) == "column" {
					hasColumnLevelTable = true
					if m.Scope.IsColumnAllowed(table, sc.ColumnName) {
						allDenied = false
						break
					}
					allDenied = true
				}
			}
			if hasColumnLevelTable && allDenied {
				log.Printf("[PermissionMiddleware:Stream] 列权限检查失败 - tool=%s, column=%s\n", toolName, sc.ColumnName)
				sr, sw := schema.Pipe[string](1)
				sw.Send(fmt.Sprintf("无权访问字段：%s", sc.ColumnName), nil)
				sw.Close()
				return sr, nil
			}
		}
	}

	return endpoint(ctx, args, opts...)
}
