package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"websql/internal/audit"
	appperm "websql/internal/app/permission"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

var (
	rePrimaryTable  = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+(?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)\.)?(` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)`)
	reCommaTable    = regexp.MustCompile(`\s*,\s*(?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)\.)?(` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)`)
	reMetadataTable = regexp.MustCompile(`(?i)\b(?:DESCRIBE|DESC|SHOW\s+CREATE\s+TABLE)\s+(?:(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)\.)?(` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)`)
	reCTE           = regexp.MustCompile(`(?i)\bWITH\s+(\w+)\s+AS\s*\(`)
	reStopClause    = regexp.MustCompile(`(?i)\b(?:WHERE|GROUP\s+BY|ORDER\s+BY|HAVING|LIMIT|OFFSET|UNION|INTERSECT|EXCEPT|VALUES|SET|ON|LATERAL)\b`)
	reAsAlias       = regexp.MustCompile(`(?i)^AS\s+\w+`)
	reIdent         = regexp.MustCompile(`^\w+`)
)

func extractTablesFromSQL(sql string) []string {
	tables := make(map[string]bool)

	cteNames := make(map[string]bool)
	for _, match := range reCTE.FindAllStringSubmatch(sql, -1) {
		if len(match) > 1 {
			cteNames[strings.ToLower(match[1])] = true
		}
	}

	for _, idx := range rePrimaryTable.FindAllStringSubmatchIndex(sql, -1) {
		if len(idx) >= 4 {
			tableName := stripBackticks(sql[idx[2]:idx[3]])
			if isValidTableNameExtract(tableName) && !cteNames[strings.ToLower(tableName)] {
				tables[tableName] = true
			}

			afterMatch := sql[idx[1]:]
			if stopMatch := reStopClause.FindStringIndex(afterMatch); stopMatch != nil {
				afterMatch = afterMatch[:stopMatch[0]]
			}

			afterMatch = skipTableAlias(afterMatch)

			for {
				trimmed := strings.TrimLeft(afterMatch, " \t\n\r")
				if !strings.HasPrefix(trimmed, ",") {
					break
				}
				commaMatch := reCommaTable.FindStringSubmatch(trimmed)
				if len(commaMatch) < 2 {
					break
				}
				commaTableName := stripBackticks(commaMatch[1])
				if isValidTableNameExtract(commaTableName) && !cteNames[strings.ToLower(commaTableName)] {
					remainingAfterTable := trimmed[len(commaMatch[0]):]
					if len(remainingAfterTable) == 0 || remainingAfterTable[0] != '(' {
						tables[commaTableName] = true
					}
				}
				afterMatch = skipTableAlias(trimmed[len(commaMatch[0]):])
			}
		}
	}

	for _, match := range reMetadataTable.FindAllStringSubmatch(sql, -1) {
		if len(match) > 1 {
			tableName := stripBackticks(match[1])
			if isValidTableNameExtract(tableName) {
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

func isValidTableNameExtract(name string) bool {
	if name == "" || isSQLKeyword(name) {
		return false
	}
	if len(name) > 0 && name[0] >= '0' && name[0] <= '9' {
		return false
	}
	return true
}

func skipTableAlias(s string) string {
	s = strings.TrimLeft(s, " \t\n\r")
	if loc := reAsAlias.FindStringIndex(s); loc != nil {
		return s[loc[1]:]
	}
	if len(s) > 0 && s[0] != ',' && s[0] != '(' && s[0] != ')' {
		if loc := reIdent.FindStringIndex(s); loc != nil {
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

type PermissionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	Scope     *PermissionScope
	PermAgent tool.BaseTool
}

func (m *PermissionMiddleware) logPrefix() string {
	return fmt.Sprintf("[PermMW] user=%s conn=%s", m.Scope.UserID, m.Scope.ConnID)
}

func (m *PermissionMiddleware) logDeny(toolName, reason string, objects []string) {
	log.Printf("%s [拒绝] tool=%s reason=%s objects=%v\n", m.logPrefix(), toolName, reason, objects)
	m.auditPermDenied(toolName, reason, objects)
}

func (m *PermissionMiddleware) logAllow(toolName, via string) {
	log.Printf("%s [放行] tool=%s via=%s\n", m.logPrefix(), toolName, via)
}

func (m *PermissionMiddleware) logInfo(toolName, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s [信息] tool=%s %s\n", m.logPrefix(), toolName, msg)
}

func (m *PermissionMiddleware) auditPermDenied(toolName, reason string, objects []string) {
	detail := fmt.Sprintf("tool=%s reason=%s objects=%v", toolName, reason, objects)
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:    "agent",
		ToolName:  toolName,
		SQLText:   detail,
		SQLType:   "PERM_DENIED",
		RiskLevel: "high",
		Status:    "denied",
		ConnID:    m.Scope.ConnID,
		UserID:    m.Scope.UserID,
		ErrorMsg:  detail,
	})
}

func (m *PermissionMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if m.Scope.SkipChecks() {
		m.logAllow(tCtx.Name, "skip(conn_full)")
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		switch tCtx.Name {
		case "get_table_schema":
			return m.checkSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "list_tables":
			return m.postFilterSync(ctx, argumentsInJSON, endpoint, "list_tables", m.filterListTablesResult, opts...)
		case "query_data":
			return m.checkQueryAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "exec_sql":
			return m.checkExecAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "export_excel", "export_excel_with_chart", "export_analysis_image", "export_analysis_docx", "export_ppt":
			return m.checkExportAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "import_data":
			return m.checkImportAccess(ctx, argumentsInJSON, endpoint, opts...)
		default:
			m.logAllow(tCtx.Name, "unmonitored_tool")
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
		m.logAllow(tCtx.Name+"(stream)", "skip(conn_full)")
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		switch tCtx.Name {
		case "get_table_schema":
			return m.checkStreamSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "list_tables":
			return m.postFilterStream(ctx, argumentsInJSON, endpoint, "list_tables", m.filterListTablesResult, opts...)
		case "query_data", "exec_sql", "export_excel", "export_excel_with_chart", "export_analysis_image", "export_analysis_docx", "export_ppt":
			return m.checkStreamSQLAccess(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
		default:
			m.logAllow(tCtx.Name+"(stream)", "unmonitored_tool")
			return endpoint(ctx, argumentsInJSON, opts...)
		}
	}, nil
}

func (m *PermissionMiddleware) checkSQLPermissionViaAgent(ctx context.Context, sql, toolName string) (*PermDecisionOutput, error) {
	if m.PermAgent == nil {
		return nil, errors.New("permission agent not initialized")
	}
	return callPermissionAgent(ctx, m.PermAgent, sql, toolName)
}

func (m *PermissionMiddleware) extractSQLFromArgs(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return ""
	}
	sqlStr, ok := raw["sql"].(string)
	if !ok || strings.TrimSpace(sqlStr) == "" {
		return ""
	}
	return strings.TrimSpace(sqlStr)
}

func (m *PermissionMiddleware) buildPermError(decision *PermDecisionOutput) *PermissionError {
	var objects []string
	objects = append(objects, decision.DeniedTables...)
	objects = append(objects, decision.DeniedColumns...)
	return &PermissionError{
		Message: decision.Reason,
		Objects: objects,
	}
}

func (m *PermissionMiddleware) denyEmptySQL(toolName string, args string) error {
	m.logDeny(toolName, "无法解析SQL参数，拒绝执行", nil)
	log.Printf("%s [安全] 原始参数(截断) - args=%s\n", m.logPrefix(), truncateForLog(args))
	return &PermissionError{
		Message: "权限检查失败：无法解析SQL语句，已拒绝执行",
		Objects: nil,
	}
}

func (m *PermissionMiddleware) checkSchemaAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input SchemaInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		m.logDeny("get_table_schema", "参数解析失败", nil)
		return "", err
	}

	m.logInfo("get_table_schema", "请求表=%v", input.Tables)

	filtered := make([]string, 0)
	deniedTables := make([]string, 0)
	for _, table := range input.Tables {
		if m.Scope.IsTableAllowedIgnoreCase(table) {
			filtered = append(filtered, table)
		} else {
			deniedTables = append(deniedTables, table)
		}
	}

	if len(deniedTables) > 0 {
		m.logDeny("get_table_schema", "部分表无权限", deniedTables)
	}
	if len(filtered) == 0 {
		deniedMsg := fmt.Sprintf("权限不足：您无权访问以下表 %v，请使用您有权限的表重新生成查询。", deniedTables)
		output := SchemaOutput{Schema: deniedMsg}
		outputJSON, _ := json.Marshal(output)
		return string(outputJSON), nil
	}

	m.logAllow("get_table_schema", "scope_filter")
	m.logInfo("get_table_schema", "过滤后表=%v", filtered)

	input.Tables = filtered
	newArgs, _ := json.Marshal(input)
	result, err := endpoint(ctx, string(newArgs), opts...)
	if err != nil {
		return "", err
	}

	hasColumnRestrictions := false
	for _, table := range filtered {
		if m.Scope.GetTableAccessLevel(table) == "column" {
			hasColumnRestrictions = true
			break
		}
	}

	var schemaResult string
	if hasColumnRestrictions {
		m.logInfo("get_table_schema", "执行DDL列级过滤")

		var output SchemaOutput
		if err := json.Unmarshal([]byte(result), &output); err != nil {
			m.logDeny("get_table_schema", "结果JSON解析失败，拒绝返回未过滤DDL", nil)
			safeOutput, _ := json.Marshal(SchemaOutput{Schema: ""})
			return string(safeOutput), nil
		}

		if output.Schema != "" {
			filteredSchema := filterDDLByScope(output.Schema, filtered, m.Scope)
			if filteredSchema != "" {
				output.Schema = filteredSchema
			}
		}
		outputJSON, _ := json.Marshal(output)
		schemaResult = string(outputJSON)
	} else {
		schemaResult = result
	}

	if len(deniedTables) > 0 {
		var output SchemaOutput
		if err := json.Unmarshal([]byte(schemaResult), &output); err != nil {
			deniedMsg := fmt.Sprintf("权限不足：您无权访问以下表 %v，请使用您有权限的表重新生成查询。", deniedTables)
			output = SchemaOutput{Schema: deniedMsg}
			outputJSON, _ := json.Marshal(output)
			return string(outputJSON), nil
		}
		output.Schema += fmt.Sprintf("\n\n注意：您无权访问以下表 %v，请勿在SQL中引用这些表，请使用您有权限的表重新生成查询。", deniedTables)
		outputJSON, _ := json.Marshal(output)
		return string(outputJSON), nil
	}

	return schemaResult, nil
}

func (m *PermissionMiddleware) postFilterSync(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, toolName string, filter func(string) string, opts ...tool.Option) (string, error) {
	if m.Scope.HasFullSchemaAccess || m.Scope.SkipChecks() {
		m.logAllow(toolName, "full_access")
		return endpoint(ctx, args, opts...)
	}
	result, err := endpoint(ctx, args, opts...)
	if err != nil {
		return "", err
	}
	m.logAllow(toolName, "post_filter")
	return filter(result), nil
}

func (m *PermissionMiddleware) postFilterStream(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, toolName string, filter func(string) string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	if m.Scope.HasFullSchemaAccess || m.Scope.SkipChecks() {
		m.logAllow(toolName+"(stream)", "full_access")
		return endpoint(ctx, args, opts...)
	}
	reader, err := endpoint(ctx, args, opts...)
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
	m.logAllow(toolName+"(stream)", "post_filter")
	return schema.StreamReaderFromArray([]string{filter(sb.String())}), nil
}

func (m *PermissionMiddleware) filterListTablesResult(result string) string {
	var output ListTablesOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		m.logDeny("list_tables", "结果JSON解析失败，拒绝返回未过滤数据", nil)
		safeOutput, _ := json.Marshal(ListTablesOutput{Tables: []TableInfo{}, Count: 0})
		return string(safeOutput)
	}

	filtered := make([]TableInfo, 0, len(output.Tables))
	for _, t := range output.Tables {
		if m.Scope.IsTableAllowedIgnoreCase(t.TableName) {
			filtered = append(filtered, t)
		}
	}

	removedCount := len(output.Tables) - len(filtered)
	if removedCount > 0 {
		m.logInfo("list_tables", "过滤无权限表 - 原始=%d, 保留=%d, 移除=%d", len(output.Tables), len(filtered), removedCount)
	}

	output.Tables = filtered
	output.Count = len(filtered)
	outputJSON, _ := json.Marshal(output)
	return string(outputJSON)
}

func (m *PermissionMiddleware) checkQueryAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	sql := m.extractSQLFromArgs(args)
	if sql == "" {
		return "", m.denyEmptySQL("query_data", args)
	}

	m.logInfo("query_data", "sql=%s", truncateForLog(sql))

	if m.Scope.HasFullSchemaAccess {
		m.logAllow("query_data", "schema_full(fast)")
		tables := extractTablesFromSQL(sql)
		result, err := endpoint(ctx, args, opts...)
		if err != nil {
			return "", err
		}
		return m.applyQueryResultFilter(result, tables)
	}

	decision, err := m.checkSQLPermissionViaAgent(ctx, sql, "query_data")
	if err != nil {
		log.Printf("%s [降级] query_data Agent失败→程序化检查 - err=%v\n", m.logPrefix(), err)
		return m.checkQueryAccessFallback(ctx, args, endpoint, opts...)
	}

	if !decision.Allowed {
		m.logDeny("query_data", decision.Reason, nil)
		return "", m.buildPermError(decision)
	}

	m.logAllow("query_data", "agent")
	tables := extractTablesFromSQL(sql)
	result, err := endpoint(ctx, args, opts...)
	if err != nil {
		return "", err
	}
	return m.applyQueryResultFilter(result, tables)
}

func (m *PermissionMiddleware) checkQueryAccessFallback(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input QueryInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		m.logDeny("query_data", "参数解析失败", nil)
		return "", err
	}

	tables := extractTablesFromSQL(input.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			m.logDeny("query_data", "无权访问表", []string{table})
			return "", &PermissionError{Message: "无权访问表", Objects: []string{table}}
		}
	}

	if err := m.checkSelectColumnAccess(input.SQL, tables, "query_data"); err != nil {
		return "", err
	}

	m.logAllow("query_data", "fallback(programmatic)")
	result, err := endpoint(ctx, args, opts...)
	if err != nil {
		return "", err
	}
	return m.applyQueryResultFilter(result, tables)
}

func (m *PermissionMiddleware) applyQueryResultFilter(result string, tables []string) (string, error) {
	var output QueryOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		return result, nil
	}
	beforeColCount := len(output.Columns)
	beforeRowCount := len(output.Data)
	output.Columns, output.Data = m.Scope.FilterResultColumns(output.Columns, output.Data, tables)
	output.Count = len(output.Data)
	removedCount := beforeColCount - len(output.Columns)
	if removedCount > 0 {
		log.Printf("%s [过滤] query_data 结果集列过滤 - 输入列数=%d, 输出列数=%d, 移除=%d, 输入行数=%d, 输出行数=%d\n",
			m.logPrefix(), beforeColCount, len(output.Columns), removedCount, beforeRowCount, len(output.Data))
	}
	outputJSON, _ := json.Marshal(output)
	return string(outputJSON), nil
}

func (m *PermissionMiddleware) checkExecAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	sql := m.extractSQLFromArgs(args)
	if sql == "" {
		return "", m.denyEmptySQL("exec_sql", args)
	}

	m.logInfo("exec_sql", "sql=%s", truncateForLog(sql))

	if m.Scope.HasFullSchemaAccess {
		m.logAllow("exec_sql", "schema_full(fast)")
		return endpoint(ctx, args, opts...)
	}

	decision, err := m.checkSQLPermissionViaAgent(ctx, sql, "exec_sql")
	if err != nil {
		log.Printf("%s [降级] exec_sql Agent失败→程序化检查 - err=%v\n", m.logPrefix(), err)
		return m.checkExecAccessFallback(ctx, args, endpoint, opts...)
	}

	if !decision.Allowed {
		m.logDeny("exec_sql", decision.Reason, nil)
		return "", m.buildPermError(decision)
	}

	m.logAllow("exec_sql", "agent")
	return endpoint(ctx, args, opts...)
}

func (m *PermissionMiddleware) checkExecAccessFallback(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input ExecInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		m.logDeny("exec_sql", "参数解析失败", nil)
		return "", err
	}

	for _, table := range extractTablesFromSQL(input.SQL) {
		if !m.Scope.IsTableAllowed(table) {
			m.logDeny("exec_sql", "无权访问表", []string{table})
			return "", &PermissionError{Message: fmt.Sprintf("无权访问表 %s", table), Objects: []string{table}}
		}
	}

	analysis := appperm.AnalyzeSQL(input.SQL, m.Scope.SchemaName)
	if len(analysis.WriteColumns) > 0 && len(analysis.WriteTables) > 0 {
		writeTableName := analysis.WriteTables[0].Name
		if m.Scope.GetTableAccessLevel(writeTableName) == "column" {
			for _, col := range analysis.WriteColumns {
				if !m.Scope.IsColumnAllowed(writeTableName, col.ColumnName) {
					m.logDeny("exec_sql", "无权操作字段", []string{writeTableName + "." + col.ColumnName})
					return "", &PermissionError{
						Message: fmt.Sprintf("无权操作字段 %s.%s", writeTableName, col.ColumnName),
						Objects: []string{writeTableName + "." + col.ColumnName},
					}
				}
			}
		}
	}

	m.logAllow("exec_sql", "fallback(programmatic)")
	return endpoint(ctx, args, opts...)
}

func (m *PermissionMiddleware) checkExportAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	sql := m.extractSQLFromArgs(args)
	if sql == "" {
		return "", m.denyEmptySQL("export", args)
	}

	m.logInfo("export", "sql=%s", truncateForLog(sql))

	if m.Scope.HasFullSchemaAccess {
		m.logAllow("export", "schema_full(fast)")
		return endpoint(ctx, args, opts...)
	}

	decision, err := m.checkSQLPermissionViaAgent(ctx, sql, "export")
	if err != nil {
		log.Printf("%s [降级] export Agent失败→程序化检查 - err=%v\n", m.logPrefix(), err)
		return m.checkExportAccessFallback(ctx, args, endpoint, opts...)
	}

	if !decision.Allowed {
		m.logDeny("export", decision.Reason, nil)
		return "", m.buildPermError(decision)
	}

	m.logAllow("export", "agent")
	return endpoint(ctx, args, opts...)
}

func (m *PermissionMiddleware) checkExportAccessFallback(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input ExportInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		m.logDeny("export", "参数解析失败", nil)
		return "", err
	}

	tables := extractTablesFromSQL(input.SQL)
	for _, table := range tables {
		if !m.Scope.IsTableAllowed(table) {
			m.logDeny("export", "无权访问表", []string{table})
			return "", &PermissionError{Message: fmt.Sprintf("无权访问表 %s", table), Objects: []string{table}}
		}
	}

	if err := m.checkSelectColumnAccess(input.SQL, tables, "export"); err != nil {
		return "", err
	}

	m.logAllow("export", "fallback(programmatic)")
	return endpoint(ctx, args, opts...)
}

func (m *PermissionMiddleware) checkImportAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	var input ImportDataInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		m.logDeny("import_data", "参数解析失败", nil)
		return "", err
	}

	m.logInfo("import_data", "table=%s, mode=%s, mappingKeys=%v", input.TableName, input.Mode, func() []string {
		keys := make([]string, 0, len(input.Mapping))
		for k := range input.Mapping {
			keys = append(keys, k)
		}
		return keys
	}())

	if input.TableName == "" {
		m.logInfo("import_data", "无目标表名，跳过表级检查")
		return endpoint(ctx, args, opts...)
	}

	if !m.Scope.IsTableAllowed(input.TableName) {
		m.logDeny("import_data", "无权访问表", []string{input.TableName})
		return "", &PermissionError{Message: fmt.Sprintf("无权访问表 %s", input.TableName), Objects: []string{input.TableName}}
	}

	if m.Scope.GetTableAccessLevel(input.TableName) == "column" {
		if len(input.Mapping) > 0 {
			for _, dbCol := range input.Mapping {
				if !m.Scope.IsColumnAllowed(input.TableName, dbCol) {
					m.logDeny("import_data", "无权写入字段", []string{input.TableName + "." + dbCol})
					return "", &PermissionError{
						Message: fmt.Sprintf("无权写入字段 %s.%s", input.TableName, dbCol),
						Objects: []string{input.TableName + "." + dbCol},
					}
				}
			}
		}
	}

	m.logAllow("import_data", "scope_check")
	return endpoint(ctx, args, opts...)
}

func filterDDLByScope(ddl string, tables []string, scope *PermissionScope) string {
	lines := strings.Split(ddl, "\n")
	var filtered []string
	columnDefRegex := regexp.MustCompile("(?i)^\\s+[`\"']?(\\w+)[`\"']?\\s+")
	createTableRegex := regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[`\"']?(\\w+)[`\"']?")

	currentTable := ""
	removedColCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperTrimmed := strings.ToUpper(trimmed)

		if strings.HasPrefix(upperTrimmed, "CREATE ") {
			if match := createTableRegex.FindStringSubmatch(line); len(match) >= 2 {
				currentTable = match[1]
			}
			filtered = append(filtered, line)
			continue
		}

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

		match := columnDefRegex.FindStringSubmatch(line)
		if len(match) >= 2 {
			colName := match[1]
			if currentTable != "" {
				accessLevel := scope.GetTableAccessLevel(currentTable)
				if accessLevel == "full" {
					filtered = append(filtered, line)
				} else if accessLevel == "column" {
					if scope.IsColumnAllowed(currentTable, colName) {
						filtered = append(filtered, line)
					} else {
						removedColCount++
					}
				} else {
					removedColCount++
				}
			} else {
				filtered = append(filtered, line)
			}
		} else {
			filtered = append(filtered, line)
		}
	}

	if removedColCount > 0 {
		log.Printf("[PermScope:DDLFilter] DDL列过滤 - user=%s, 移除列数=%d\n", scope.UserID, removedColCount)
	}

	return strings.Join(filtered, "\n")
}

func (m *PermissionMiddleware) checkStreamSchemaAccess(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, opts ...tool.Option) (*schema.StreamReader[string], error) {
	var input SchemaInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		m.logDeny("get_table_schema(stream)", "参数解析失败", nil)
		return nil, err
	}

	m.logInfo("get_table_schema(stream)", "请求表=%v", input.Tables)

	filtered := make([]string, 0)
	deniedTables := make([]string, 0)
	for _, table := range input.Tables {
		if m.Scope.IsTableAllowedIgnoreCase(table) {
			filtered = append(filtered, table)
		} else {
			deniedTables = append(deniedTables, table)
		}
	}

	if len(deniedTables) > 0 {
		m.logDeny("get_table_schema(stream)", "部分表无权限", deniedTables)
	}
	if len(filtered) == 0 {
		deniedMsg := fmt.Sprintf("权限不足：您无权访问以下表 %v，请使用您有权限的表重新生成查询。", deniedTables)
		sr, sw := schema.Pipe[string](1)
		output := SchemaOutput{Schema: deniedMsg}
		outputJSON, _ := json.Marshal(output)
		sw.Send(string(outputJSON), nil)
		sw.Close()
		return sr, nil
	}

	m.logAllow("get_table_schema(stream)", "scope_filter")
	m.logInfo("get_table_schema(stream)", "过滤后表=%v", filtered)

	input.Tables = filtered
	newArgs, _ := json.Marshal(input)

	hasColumnRestrictions := false
	for _, table := range filtered {
		if m.Scope.GetTableAccessLevel(table) == "column" {
			hasColumnRestrictions = true
			break
		}
	}

	var schemaResult string
	if hasColumnRestrictions {
		m.logInfo("get_table_schema(stream)", "执行DDL列级过滤")

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
			m.logDeny("get_table_schema(stream)", "结果JSON解析失败，拒绝返回未过滤DDL", nil)
			safeOutput, _ := json.Marshal(SchemaOutput{Schema: ""})
			return schema.StreamReaderFromArray([]string{string(safeOutput)}), nil
		}
		if output.Schema != "" {
			output.Schema = filterDDLByScope(output.Schema, filtered, m.Scope)
		}
		outputJSON, _ := json.Marshal(output)
		schemaResult = string(outputJSON)
	} else {
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
		schemaResult = sb.String()
	}

	if len(deniedTables) > 0 {
		var output SchemaOutput
		if err := json.Unmarshal([]byte(schemaResult), &output); err != nil {
			deniedMsg := fmt.Sprintf("权限不足：您无权访问以下表 %v，请使用您有权限的表重新生成查询。", deniedTables)
			output = SchemaOutput{Schema: deniedMsg}
			outputJSON, _ := json.Marshal(output)
			return schema.StreamReaderFromArray([]string{string(outputJSON)}), nil
		}
		output.Schema += fmt.Sprintf("\n\n注意：您无权访问以下表 %v，请勿在SQL中引用这些表，请使用您有权限的表重新生成查询。", deniedTables)
		outputJSON, _ := json.Marshal(output)
		return schema.StreamReaderFromArray([]string{string(outputJSON)}), nil
	}

	return schema.StreamReaderFromArray([]string{schemaResult}), nil
}

type permCheckResult struct {
	allowed    bool
	reason     string
	deniedObjs []string
	tables     []string
	needFilter bool
}

func (m *PermissionMiddleware) getPermCheckFunc(toolName string) func(ctx context.Context, sql string) (*permCheckResult, error) {
	return func(ctx context.Context, sql string) (*permCheckResult, error) {
		if m.Scope.HasFullSchemaAccess {
			tables := extractTablesFromSQL(sql)
			needFilter := toolName == "query_data" && m.hasColumnRestrictions(tables)
			return &permCheckResult{allowed: true, tables: tables, needFilter: needFilter}, nil
		}

		agentToolName := toolName
		if toolName != "query_data" && toolName != "exec_sql" {
			agentToolName = "export"
		}

		decision, err := m.checkSQLPermissionViaAgent(ctx, sql, agentToolName)
		if err != nil {
			return nil, err
		}

		if !decision.Allowed {
			return &permCheckResult{
				allowed:    false,
				reason:     decision.Reason,
				deniedObjs: append(decision.DeniedTables, decision.DeniedColumns...),
			}, nil
		}

		tables := extractTablesFromSQL(sql)
		needFilter := toolName == "query_data" && m.hasColumnRestrictions(tables)
		return &permCheckResult{allowed: true, tables: tables, needFilter: needFilter}, nil
	}
}

func (m *PermissionMiddleware) hasColumnRestrictions(tables []string) bool {
	for _, table := range tables {
		if m.Scope.GetTableAccessLevel(table) == "column" {
			return true
		}
	}
	return false
}

func (m *PermissionMiddleware) checkSelectColumnAccess(sql string, tables []string, toolName string) error {
	selectCols := appperm.ExtractSelectColumns(appperm.StripComments(strings.TrimSpace(sql)))
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
			displayName := colName
			if sc.TableAlias != "" {
				displayName = sc.TableAlias + "." + colName
			}
			m.logDeny(toolName, "无权访问字段", []string{displayName})
			return &PermissionError{
				Message: fmt.Sprintf("无权访问字段 %s", displayName),
				Objects: []string{displayName},
			}
		}
	}
	return nil
}

func (m *PermissionMiddleware) checkStreamSQLAccess(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, toolName string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	sql := m.extractSQLFromArgs(args)

	if sql == "" {
		m.logDeny(toolName+"(stream)", "无法解析SQL参数，拒绝执行", nil)
		log.Printf("%s [安全] 原始参数(截断) - args=%s\n", m.logPrefix(), truncateForLog(args))
		return streamFromStr("权限检查失败：无法解析SQL语句，已拒绝执行"), nil
	}

	m.logInfo(toolName+"(stream)", "sql=%s", truncateForLog(sql))

	checkFunc := m.getPermCheckFunc(toolName)
	result, err := checkFunc(ctx, sql)
	if err != nil {
		log.Printf("%s [降级] %s(stream) Agent失败→程序化检查 - err=%v\n", m.logPrefix(), toolName, err)
		return m.checkStreamSQLAccessFallback(ctx, args, endpoint, toolName, opts...)
	}

	if !result.allowed {
		m.logDeny(toolName+"(stream)", result.reason, result.deniedObjs)
		return streamFromStr(fmt.Sprintf("权限不足：%s", result.reason)), nil
	}

	m.logAllow(toolName+"(stream)", "agent")

	reader, err := endpoint(ctx, args, opts...)
	if err != nil {
		return nil, err
	}

	if result.needFilter {
		return m.applyStreamQueryResultFilter(reader, result.tables), nil
	}

	return reader, nil
}

func streamFromStr(s string) *schema.StreamReader[string] {
	sr, sw := schema.Pipe[string](1)
	sw.Send(s, nil)
	sw.Close()
	return sr
}

func (m *PermissionMiddleware) checkStreamSQLAccessFallback(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, toolName string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		m.logDeny(toolName+"(stream)", "参数解析失败", nil)
		return nil, err
	}

	sqlStr, _ := raw["sql"].(string)
	tables := extractTablesFromSQL(sqlStr)
	if sqlStr != "" {
		for _, table := range tables {
			if !m.Scope.IsTableAllowed(table) {
				m.logDeny(toolName+"(stream)", "无权访问表", []string{table})
				return streamFromStr(fmt.Sprintf("无权访问表：%s", table)), nil
			}
		}

		if err := m.checkSelectColumnAccess(sqlStr, tables, toolName+"(stream)"); err != nil {
			return streamFromStr(err.Error()), nil
		}
	}

	m.logAllow(toolName+"(stream)", "fallback(programmatic)")
	reader, err := endpoint(ctx, args, opts...)
	if err != nil {
		return nil, err
	}

	if toolName == "query_data" && m.hasColumnRestrictions(tables) {
		return m.applyStreamQueryResultFilter(reader, tables), nil
	}

	return reader, nil
}

func (m *PermissionMiddleware) applyStreamQueryResultFilter(reader *schema.StreamReader[string], tables []string) *schema.StreamReader[string] {
	var sb strings.Builder
	for {
		chunk, recvErr := reader.Recv()
		if recvErr != nil {
			break
		}
		sb.WriteString(chunk)
	}
	rawResult := sb.String()

	var output QueryOutput
	if err := json.Unmarshal([]byte(rawResult), &output); err != nil {
		return schema.StreamReaderFromArray([]string{rawResult})
	}

	beforeColCount := len(output.Columns)
	beforeRowCount := len(output.Data)
	output.Columns, output.Data = m.Scope.FilterResultColumns(output.Columns, output.Data, tables)
	output.Count = len(output.Data)

	removedCount := beforeColCount - len(output.Columns)
	if removedCount > 0 {
		log.Printf("%s [过滤] query_data(stream) 结果集列过滤 - 输入列数=%d, 输出列数=%d, 移除=%d, 输入行数=%d, 输出行数=%d\n",
			m.logPrefix(), beforeColCount, len(output.Columns), removedCount, beforeRowCount, len(output.Data))
	}

	outputJSON, _ := json.Marshal(output)
	return schema.StreamReaderFromArray([]string{string(outputJSON)})
}

func truncateForLog(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return strings.ReplaceAll(s, "\n", " ")
}