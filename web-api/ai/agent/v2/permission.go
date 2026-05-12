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
	"go-web/utils"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

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
		log.Printf("[PermScope] 非远程模式，跳过权限检查 - user=%s\n", userId)
		return scope
	}

	powerList := admin.FindUserPowerDetails(userId)
	log.Printf("[PermScope] 用户权限记录数=%d - user=%s, conn=%s\n", len(powerList), userId, connId)

	hasConnPerm := false
	hasSchemaPerm := false
	hasTableOrColumnForSchema := false
	tableCount := 0
	columnCount := 0

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
				tableCount++
			}
		case "column":
			if (schemaName == "" || pSchema == schemaName) && pTable != "" && pColumn != "" {
				hasTableOrColumnForSchema = true
				if !scope.AllowedTables[pTable] {
					if scope.AllowedColumns[pTable] == nil {
						scope.AllowedColumns[pTable] = make(map[string]bool)
					}
					scope.AllowedColumns[pTable][pColumn] = true
					columnCount++
				}
			}
		}
	}

	if hasConnPerm && !hasTableOrColumnForSchema {
		scope.HasFullConnAccess = true
		log.Printf("[PermScope] 连接级完整权限 - user=%s, conn=%s\n", userId, connId)
		return scope
	}
	if hasSchemaPerm && !hasTableOrColumnForSchema {
		scope.HasFullSchemaAccess = true
		log.Printf("[PermScope] Schema级完整权限 - user=%s, conn=%s, schema=%s\n", userId, connId, schemaName)
		return scope
	}

	if hasConnPerm {
		log.Printf("[PermScope] 连接级权限(降级) - user=%s, conn=%s, 有具体表/字段权限\n", userId, connId)
	}
	log.Printf("[PermScope] 权限范围 - user=%s, conn=%s, schema=%s, tables=%d, columnTables=%d(columns=%d)\n",
		userId, connId, schemaName, len(scope.AllowedTables), len(scope.AllowedColumns), columnCount)

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
			if strings.EqualFold(t, table) {
				for col := range cols {
					allowedCols[strings.ToLower(col)] = true
				}
			}
		}
	}

	filteredCols := make([]string, 0)
	removedCols := make([]string, 0)
	for _, col := range columns {
		if allowedCols[strings.ToLower(col)] {
			filteredCols = append(filteredCols, col)
		} else {
			removedCols = append(removedCols, col)
		}
	}

	if len(removedCols) > 0 {
		log.Printf("[PermScope:Filter] 结果集列过滤 - user=%s, conn=%s, 移除列=%v, 保留列=%v\n",
			s.UserID, s.ConnID, removedCols, filteredCols)
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
// PermissionMiddleware（安全红线）
//
// 核心安全原则：
//   1. 默认拒绝：任何不确定的情况一律拒绝，绝不"兜底放行"
//   2. 双重防线：Agent判断(第一道) + 结果集过滤(第二道兜底)
//   3. 全程可审：每个决策点都有日志，拒绝操作写入审计表
//   4. 不可绕过：SQL为空时拒绝执行（防止解析失败导致的绕过）
// ──────────────────────────────────────────────

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
	auditID := utils.RandomStr()
	detail := fmt.Sprintf("tool=%s reason=%s objects=%v", toolName, reason, objects)
	InsertSQLAudit(auditID, m.Scope.UserID, "", m.Scope.ConnID, "",
		detail, "PERM_DENIED", "high", "denied", 0, detail)
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
		return nil, fmt.Errorf("permission agent not initialized")
	}
	return callPermissionAgent(ctx, m.PermAgent, sql, toolName)
}

// extractSQLFromArgs 从工具参数JSON中提取SQL字段。
// 安全原则：提取失败返回空字符串，调用方必须拒绝——绝不假设SQL不存在。
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

// denyEmptySQL 当无法从参数中提取SQL时，拒绝执行。
// 安全红线：绝不在无法确定SQL内容的情况下放行。
func (m *PermissionMiddleware) denyEmptySQL(toolName string, args string) error {
	m.logDeny(toolName, "无法解析SQL参数，拒绝执行", nil)
	log.Printf("%s [安全] 原始参数(截断) - args=%s\n", m.logPrefix(), truncateForLog(args))
	return &PermissionError{
		Message: "权限检查失败：无法解析SQL语句，已拒绝执行",
		Objects: nil,
	}
}

// ──────────────────────────────────────────────
// get_table_schema 权限检查
// ──────────────────────────────────────────────

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
		if m.Scope.IsTableAllowed(table) {
			filtered = append(filtered, table)
		} else {
			deniedTables = append(deniedTables, table)
		}
	}

	if len(deniedTables) > 0 {
		m.logDeny("get_table_schema", "部分表无权限", deniedTables)
	}
	if len(filtered) == 0 {
		output := SchemaOutput{Schema: "提示：请提供正确的表名。您传入的名称无法访问。"}
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

	if !hasColumnRestrictions {
		return result, nil
	}

	m.logInfo("get_table_schema", "执行DDL列级过滤")

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

// ──────────────────────────────────────────────
// query_data 权限检查
// ──────────────────────────────────────────────

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
				displayName := colName
				if sc.TableAlias != "" {
					displayName = sc.TableAlias + "." + colName
				}
				m.logDeny("query_data", "无权访问字段", []string{displayName})
				return "", &PermissionError{
					Message: fmt.Sprintf("无权访问字段 %s", displayName),
					Objects: []string{displayName},
				}
			}
		}
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

// ──────────────────────────────────────────────
// exec_sql 权限检查
// ──────────────────────────────────────────────

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

	analysis := admin.AnalyzeSQL(input.SQL, m.Scope.SchemaName)
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

// ──────────────────────────────────────────────
// export_* 权限检查
// ──────────────────────────────────────────────

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
				m.logDeny("export", "无权导出字段", []string{colName})
				return "", &PermissionError{
					Message: fmt.Sprintf("无权导出字段 %s", colName),
					Objects: []string{colName},
				}
			}
		}
	}

	m.logAllow("export", "fallback(programmatic)")
	return endpoint(ctx, args, opts...)
}

// ──────────────────────────────────────────────
// import_data 权限检查
// ──────────────────────────────────────────────

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

// ──────────────────────────────────────────────
// DDL 列级过滤
// ──────────────────────────────────────────────

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

// ──────────────────────────────────────────────
// 流式权限检查
// ──────────────────────────────────────────────

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
		if m.Scope.IsTableAllowed(table) {
			filtered = append(filtered, table)
		} else {
			deniedTables = append(deniedTables, table)
		}
	}

	if len(deniedTables) > 0 {
		m.logDeny("get_table_schema(stream)", "部分表无权限", deniedTables)
	}
	if len(filtered) == 0 {
		sr, sw := schema.Pipe[string](1)
		sw.Send("提示：请提供正确的表名。您传入的名称无法访问。", nil)
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

	if !hasColumnRestrictions {
		return endpoint(ctx, string(newArgs), opts...)
	}

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
		return schema.StreamReaderFromArray([]string{rawResult}), nil
	}
	if output.Schema != "" {
		output.Schema = filterDDLByScope(output.Schema, filtered, m.Scope)
	}
	outputJSON, _ := json.Marshal(output)
	return schema.StreamReaderFromArray([]string{string(outputJSON)}), nil
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
	tables := []string{}
	if sqlStr != "" {
		tables = extractTablesFromSQL(sqlStr)
		for _, table := range tables {
			if !m.Scope.IsTableAllowed(table) {
				m.logDeny(toolName+"(stream)", "无权访问表", []string{table})
				return streamFromStr(fmt.Sprintf("无权访问表：%s", table)), nil
			}
		}

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
				m.logDeny(toolName+"(stream)", "无权访问字段", []string{sc.ColumnName})
				return streamFromStr(fmt.Sprintf("无权访问字段：%s", sc.ColumnName)), nil
			}
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