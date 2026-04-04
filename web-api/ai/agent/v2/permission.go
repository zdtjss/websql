package agentv2

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"go-web/config"
	admin "go-web/web-api/admin"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// PermissionScope 权限范围
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

// PermissionError 权限错误
type PermissionError struct {
	Message string
	Objects []string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("%s: %v", e.Message, e.Objects)
}

// BuildPermissionScope 构建权限范围
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

	// 第一遍：收集上级权限（conn、schema、table）
	for _, power := range powerList {
		if power.ConnId != connId {
			continue
		}

		switch power.Level {
		case "conn":
			scope.HasFullConnAccess = true
		case "schema":
			if power.SchemaName != nil && *power.SchemaName == schemaName {
				scope.HasFullSchemaAccess = true
			}
		case "table":
			if power.SchemaName != nil && *power.SchemaName == schemaName && power.TableName != nil {
				scope.AllowedTables[*power.TableName] = true
			}
		}
	}

	// 第二遍：收集列级权限（只有未授权表级权限的表才需要列级权限）
	for _, power := range powerList {
		if power.ConnId != connId {
			continue
		}

		if power.Level == "column" {
			if power.SchemaName != nil && *power.SchemaName == schemaName && power.TableName != nil && power.ColumnName != nil {
				// 只有未授权表级权限的表才需要记录列级权限
				if !scope.AllowedTables[*power.TableName] {
					if scope.AllowedColumns[*power.TableName] == nil {
						scope.AllowedColumns[*power.TableName] = make(map[string]bool)
					}
					scope.AllowedColumns[*power.TableName][*power.ColumnName] = true
				}
			}
		}
	}

	return scope
}

// SkipChecks 是否跳过检查（非远程模式或拥有连接级权限）
func (s *PermissionScope) SkipChecks() bool {
	return !s.IsRemote || s.HasFullConnAccess
}

// HasAnyAccess 是否有任何访问权限
func (s *PermissionScope) HasAnyAccess() bool {
	if s.HasFullConnAccess || s.HasFullSchemaAccess {
		return true
	}
	return len(s.AllowedTables) > 0 || len(s.AllowedColumns) > 0
}

// IsTableAllowed 表是否被允许访问
func (s *PermissionScope) IsTableAllowed(table string) bool {
	if s.SkipChecks() {
		return true
	}
	if s.HasFullSchemaAccess {
		return true
	}
	if s.AllowedTables[table] {
		return true
	}
	if len(s.AllowedColumns[table]) > 0 {
		return true
	}
	return false
}

// IsColumnAllowed 列是否被允许访问
func (s *PermissionScope) IsColumnAllowed(table, column string) bool {
	if s.SkipChecks() {
		return true
	}
	if s.HasFullSchemaAccess || s.AllowedTables[table] {
		return true
	}
	if s.AllowedColumns[table] != nil {
		return s.AllowedColumns[table][column]
	}
	return false
}

// GetTableAccessLevel 获取表的访问级别
func (s *PermissionScope) GetTableAccessLevel(table string) string {
	if s.SkipChecks() || s.HasFullSchemaAccess || s.AllowedTables[table] {
		return "full"
	}
	if len(s.AllowedColumns[table]) > 0 {
		return "column"
	}
	return "none"
}

// FilterResultColumns 过滤结果列
func (s *PermissionScope) FilterResultColumns(columns []string, data []map[string]interface{}, tables []string) ([]string, []map[string]interface{}) {
	if s.SkipChecks() {
		return columns, data
	}

	tablesWithRestrictions := make(map[string]bool)
	for _, table := range tables {
		if s.GetTableAccessLevel(table) == "column" {
			tablesWithRestrictions[table] = true
		}
	}

	if len(tablesWithRestrictions) == 0 {
		return columns, data
	}

	allowedColumnSet := make(map[string]bool)
	for table, cols := range s.AllowedColumns {
		if tablesWithRestrictions[table] {
			for col := range cols {
				allowedColumnSet[col] = true
			}
		}
	}

	filteredColumns := make([]string, 0)
	columnIndex := make(map[string]int)

	for i, col := range columns {
		if allowedColumnSet[col] {
			filteredColumns = append(filteredColumns, col)
			columnIndex[col] = i
		}
	}

	filteredData := make([]map[string]interface{}, 0)
	for _, row := range data {
		filteredRow := make(map[string]interface{})
		for _, col := range filteredColumns {
			if val, ok := row[col]; ok {
				filteredRow[col] = val
			}
		}
		filteredData = append(filteredData, filteredRow)
	}

	return filteredColumns, filteredData
}

// DescribeForPrompt 生成权限描述用于系统提示词
func (s *PermissionScope) DescribeForPrompt() string {
	if !s.IsRemote {
		return ""
	}

	if s.HasFullConnAccess {
		return ""
	}

	if s.HasFullSchemaAccess {
		return fmt.Sprintf("\n\n## 🔒 数据权限约束\n你拥有数据库 **%s** 的完整访问权限，没有表或列限制。\n**重要**：你只能访问此 schema 内的对象，禁止访问其他 schema。", s.SchemaName)
	}

	var sb strings.Builder
	sb.WriteString("\n\n## 🔒 数据权限约束\n")
	sb.WriteString("**重要**：你必须严格遵守以下权限限制，不能使用未授权的表或列！\n\n")
	sb.WriteString("### 权限层级说明\n")
	sb.WriteString("- **表级权限**：拥有某表的表级权限 = 可以访问该表**所有字段**\n")
	sb.WriteString("- **字段级权限**：只有明确授权的字段才能访问，**其他字段禁止使用**\n\n")

	if len(s.AllowedTables) > 0 {
		sb.WriteString("### 可访问的表（表级权限 - 包含所有字段）\n")
		tables := make([]string, 0, len(s.AllowedTables))
		for table := range s.AllowedTables {
			tables = append(tables, table)
		}
		sb.WriteString(fmt.Sprintf("以下表已授权，你可以访问所有列：%s\n\n", strings.Join(tables, ", ")))
	}

	if len(s.AllowedColumns) > 0 {
		sb.WriteString("### 可访问的表（字段级权限 - 仅限指定字段）\n")
		for table, cols := range s.AllowedColumns {
			// 跳过已有表级权限的表
			if s.AllowedTables[table] {
				continue
			}
			colList := make([]string, 0, len(cols))
			for col := range cols {
				colList = append(colList, col)
			}
			sb.WriteString(fmt.Sprintf("- 表 `%s`：仅允许使用列 [%s] - **该表其他列禁止在 SELECT、WHERE、JOIN ON 中使用**\n", table, strings.Join(colList, ", ")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// extractTablesFromSQL 从 SQL 中提取表名
func extractTablesFromSQL(sql string) []string {
	tables := make(map[string]bool)

	primaryRegex := regexp.MustCompile(`(?i)(?:FROM|JOIN|INTO|UPDATE)\s+(?:` + "`" + `[^` + "`" + `]+` + "`" + `\.|\w+\.)?(` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)`)
	commaRegex := regexp.MustCompile(`,\s*(?:` + "`" + `[^` + "`" + `]+` + "`" + `\.|\w+\.)?(` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)`)
	metadataRegex := regexp.MustCompile(`(?i)(?:DESCRIBE|DESC|SHOW\s+CREATE\s+TABLE)\s+(` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)`)
	cteRegex := regexp.MustCompile(`(?i)WITH\s+(\w+)\s+AS\s*\(`)

	cteNames := make(map[string]bool)
	cteMatches := cteRegex.FindAllStringSubmatch(sql, -1)
	for _, match := range cteMatches {
		if len(match) > 1 {
			cteNames[strings.ToLower(match[1])] = true
		}
	}

	primaryMatches := primaryRegex.FindAllStringSubmatch(sql, -1)
	for _, match := range primaryMatches {
		if len(match) > 1 {
			tableName := stripBackticks(match[1])
			if !isSQLKeyword(tableName) && !cteNames[strings.ToLower(tableName)] {
				tables[tableName] = true
			}

			matchEnd := match[0]
			if len(match) > 1 {
				matchEnd = match[0]
			}
			afterMatch := sql[len(matchEnd):]
			commaMatches := commaRegex.FindAllStringSubmatch(afterMatch, -1)
			for _, commaMatch := range commaMatches {
				if len(commaMatch) > 1 {
					commaTableName := stripBackticks(commaMatch[1])
					if !isSQLKeyword(commaTableName) && !cteNames[strings.ToLower(commaTableName)] {
						tables[commaTableName] = true
					}
				}
			}
		}
	}

	metadataMatches := metadataRegex.FindAllStringSubmatch(sql, -1)
	for _, match := range metadataMatches {
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

// PermissionMiddleware 权限中间件
type PermissionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	Scope *PermissionScope
}

// WrapInvokableToolCall 包装同步工具调用
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
			return m.handleGetTableSchema(ctx, argumentsInJSON, endpoint, opts...)
		case "query_data":
			return m.handleQueryData(ctx, argumentsInJSON, endpoint, opts...)
		case "exec_sql":
			return m.handleExecSQL(ctx, argumentsInJSON, endpoint, opts...)
		case "export_excel", "export_excel_with_chart", "export_analysis_image", "export_analysis_docx":
			return m.handleExport(ctx, argumentsInJSON, endpoint, opts...)
		case "export_ppt":
			return endpoint(ctx, argumentsInJSON, opts...)
		default:
			return endpoint(ctx, argumentsInJSON, opts...)
		}
	}, nil
}

// WrapStreamableToolCall 包装流式工具调用
func (m *PermissionMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if m.Scope.SkipChecks() {
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		switch tCtx.Name {
		case "get_table_schema":
			return m.handleStreamGetTableSchema(ctx, argumentsInJSON, endpoint, opts...)
		case "query_data":
			return m.handleStreamQueryData(ctx, argumentsInJSON, endpoint, opts...)
		case "exec_sql":
			return m.handleStreamExecSQL(ctx, argumentsInJSON, endpoint, opts...)
		case "export_excel", "export_excel_with_chart", "export_analysis_image", "export_analysis_docx":
			return m.handleStreamExport(ctx, argumentsInJSON, endpoint, opts...)
		case "export_ppt":
			return endpoint(ctx, argumentsInJSON, opts...)
		default:
			return endpoint(ctx, argumentsInJSON, opts...)
		}
	}, nil
}

func (m *PermissionMiddleware) handleGetTableSchema(ctx context.Context, argumentsInJSON string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var schemaInput SchemaInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &schemaInput); err != nil {
		return "", err
	}

	filteredTables := make([]string, 0)
	for _, table := range schemaInput.Tables {
		if m.Scope.IsTableAllowed(table) {
			filteredTables = append(filteredTables, table)
		}
	}

	if len(filteredTables) == 0 {
		output := SchemaOutput{
			Schema: "无权访问请求的表",
		}
		outputJSON, _ := json.Marshal(output)
		return string(outputJSON), nil
	}

	schemaInput.Tables = filteredTables
	newArgs, _ := json.Marshal(schemaInput)
	return endpoint(ctx, string(newArgs), opts...)
}

func (m *PermissionMiddleware) handleStreamGetTableSchema(ctx context.Context, argumentsInJSON string, endpoint adk.StreamableToolCallEndpoint, opts ...tool.Option) (*schema.StreamReader[string], error) {
	var schemaInput SchemaInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &schemaInput); err != nil {
		return nil, err
	}

	filteredTables := make([]string, 0)
	for _, table := range schemaInput.Tables {
		if m.Scope.IsTableAllowed(table) {
			filteredTables = append(filteredTables, table)
		}
	}

	if len(filteredTables) == 0 {
		// 创建返回固定字符串的 StreamReader
		sr, sw := schema.Pipe[string](1)
		sw.Send("无权访问请求的表", nil)
		sw.Close()
		return sr, nil
	}

	schemaInput.Tables = filteredTables
	newArgs, _ := json.Marshal(schemaInput)
	return endpoint(ctx, string(newArgs), opts...)
}

func (m *PermissionMiddleware) handleQueryData(ctx context.Context, argumentsInJSON string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var queryInput QueryInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &queryInput); err != nil {
		return "", err
	}

	tables := extractTablesFromSQL(queryInput.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			return "", &PermissionError{
				Message: fmt.Sprintf("无权访问表 %s", table),
				Objects: []string{table},
			}
		}
	}

	result, err := endpoint(ctx, argumentsInJSON, opts...)
	if err != nil {
		return "", err
	}

	var queryOutput QueryOutput
	if err := json.Unmarshal([]byte(result), &queryOutput); err != nil {
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

	filteredColumns, filteredData := m.Scope.FilterResultColumns(queryOutput.Columns, queryOutput.Data, tables)
	queryOutput.Columns = filteredColumns
	queryOutput.Data = filteredData
	queryOutput.Count = len(filteredData)

	outputJSON, _ := json.Marshal(queryOutput)
	return string(outputJSON), nil
}

func (m *PermissionMiddleware) handleStreamQueryData(ctx context.Context, argumentsInJSON string, endpoint adk.StreamableToolCallEndpoint, opts ...tool.Option) (*schema.StreamReader[string], error) {
	var queryInput QueryInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &queryInput); err != nil {
		return nil, err
	}

	tables := extractTablesFromSQL(queryInput.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			sr, sw := schema.Pipe[string](1)
			sw.Send(fmt.Sprintf("无权访问表：%s", table), nil)
			sw.Close()
			return sr, nil
		}
	}

	return endpoint(ctx, argumentsInJSON, opts...)
}

func (m *PermissionMiddleware) handleExecSQL(ctx context.Context, argumentsInJSON string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var execInput ExecInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &execInput); err != nil {
		return "", err
	}

	tables := extractTablesFromSQL(execInput.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			return "", &PermissionError{
				Message: fmt.Sprintf("无权访问表 %s", table),
				Objects: []string{table},
			}
		}
	}

	return endpoint(ctx, argumentsInJSON, opts...)
}

func (m *PermissionMiddleware) handleStreamExecSQL(ctx context.Context, argumentsInJSON string, endpoint adk.StreamableToolCallEndpoint, opts ...tool.Option) (*schema.StreamReader[string], error) {
	var execInput ExecInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &execInput); err != nil {
		return nil, err
	}

	tables := extractTablesFromSQL(execInput.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			sr, sw := schema.Pipe[string](1)
			sw.Send(fmt.Sprintf("无权访问表：%s", table), nil)
			sw.Close()
			return sr, nil
		}
	}

	return endpoint(ctx, argumentsInJSON, opts...)
}

func (m *PermissionMiddleware) handleExport(ctx context.Context, argumentsInJSON string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var exportInput ExportInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &exportInput); err != nil {
		return "", err
	}

	tables := extractTablesFromSQL(exportInput.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			return "", &PermissionError{
				Message: fmt.Sprintf("无权访问表 %s", table),
				Objects: []string{table},
			}
		}
	}

	return endpoint(ctx, argumentsInJSON, opts...)
}

func (m *PermissionMiddleware) handleStreamExport(ctx context.Context, argumentsInJSON string, endpoint adk.StreamableToolCallEndpoint, opts ...tool.Option) (*schema.StreamReader[string], error) {
	var exportInput ExportInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &exportInput); err != nil {
		return nil, err
	}

	tables := extractTablesFromSQL(exportInput.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			sr, sw := schema.Pipe[string](1)
			sw.Send(fmt.Sprintf("无权访问表：%s", table), nil)
			sw.Close()
			return sr, nil
		}
	}

	return endpoint(ctx, argumentsInJSON, opts...)
}

// SQLWithPermissionInput 带权限检查的 SQL 输入
type SQLWithPermissionInput struct {
	SQL string `json:"sql"`
}

// extractSQLFromInput 从各种输入结构中提取 SQL
func extractSQLFromInput(input any) (string, bool) {
	switch v := input.(type) {
	case *QueryInput:
		return v.SQL, true
	case *ExecInput:
		return v.SQL, true
	case *ExportInput:
		return v.SQL, true
	case map[string]interface{}:
		if sql, ok := v["sql"].(string); ok {
			return sql, true
		}
	case *SQLWithPermissionInput:
		return v.SQL, true
	}
	return "", false
}

// jsonToMap 将 JSON 转换为 map
func jsonToMap(data any) (map[string]interface{}, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	err = json.Unmarshal(jsonData, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
