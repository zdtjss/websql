package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	appperm "websql/internal/app/permission"
	"websql/internal/audit"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type PermissionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	Scope     *PermissionScope
	PermAgent tool.BaseTool
}

// BeforeModelRewriteState 在模型调用前根据权限动态过滤 ToolInfos（Eino v0.9 新增）。
// 无权限的工具直接从模型可见列表中移除，减少无效的工具调用尝试。
func (m *PermissionMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if m.Scope.SkipChecks() && m.Scope.AllowModify {
		return ctx, state, nil // 完整权限，无需过滤
	}

	if state.ToolInfos == nil {
		return ctx, state, nil
	}

	// 如果不允许修改数据，从工具列表中移除写操作工具
	if !m.Scope.AllowModify {
		filtered := make([]*schema.ToolInfo, 0, len(state.ToolInfos))
		for _, ti := range state.ToolInfos {
			if ti.Name == "exec_sql" || ti.Name == "import_data" {
				log.Printf("%s [ToolInfos] 移除写操作工具 - tool=%s\n", m.logPrefix(), ti.Name)
				continue
			}
			filtered = append(filtered, ti)
		}
		state.ToolInfos = filtered
	}

	return ctx, state, nil
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
		if !m.Scope.AllowModify && (tCtx.Name == "exec_sql" || tCtx.Name == "import_data") {
			m.logDeny(tCtx.Name, "角色禁止修改数据", nil)
			return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
				return "", &PermissionError{
					Message: "当前角色禁止修改数据，无法执行写操作",
					Objects: []string{},
				}
			}, nil
		}
		m.logAllow(tCtx.Name, "skip(conn_full)")
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		if !m.Scope.AllowModify && (tCtx.Name == "exec_sql" || tCtx.Name == "import_data") {
			m.logDeny(tCtx.Name, "角色禁止修改数据", nil)
			return "", &PermissionError{
				Message: "当前角色禁止修改数据，无法执行写操作",
				Objects: []string{},
			}
		}
		switch tCtx.Name {
		case "get_table_schema":
			return m.checkSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "list_tables":
			return m.postFilterSync(ctx, argumentsInJSON, endpoint, "list_tables", m.filterListTablesResult, opts...)
		case "query_data":
			return m.checkQueryAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "exec_sql":
			return m.checkExecAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "export_excel", "export_excel_with_chart", "export_analysis_image", "export_analysis_docx", "export_ppt", "export_html":
			return m.checkExportAccess(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
		case "import_data":
			return m.checkImportAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "skill":
			// skill 工具仅读取 SKILL.md 文件，无安全风险，直接放行
			m.logAllow(tCtx.Name, "skill_read_only")
			return endpoint(ctx, argumentsInJSON, opts...)
		case "execute":
			// execute 工具执行 shell 命令，安全校验由 OSFilesystemBackend.validateCommand 负责
			// 此处记录审计日志后放行，命令黑名单校验在执行层拦截
			m.logAllow(tCtx.Name, "execute_validated_by_backend")
			return endpoint(ctx, argumentsInJSON, opts...)
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
		if !m.Scope.AllowModify && (tCtx.Name == "exec_sql" || tCtx.Name == "import_data") {
			m.logDeny(tCtx.Name+"(stream)", "角色禁止修改数据", nil)
			return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
				return nil, &PermissionError{
					Message: "当前角色禁止修改数据，无法执行写操作",
					Objects: []string{},
				}
			}, nil
		}
		m.logAllow(tCtx.Name+"(stream)", "skip(conn_full)")
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		if !m.Scope.AllowModify && (tCtx.Name == "exec_sql" || tCtx.Name == "import_data") {
			m.logDeny(tCtx.Name+"(stream)", "角色禁止修改数据", nil)
			return nil, &PermissionError{
				Message: "当前角色禁止修改数据，无法执行写操作",
				Objects: []string{},
			}
		}
		switch tCtx.Name {
		case "get_table_schema":
			return m.checkStreamSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "list_tables":
			return m.postFilterStream(ctx, argumentsInJSON, endpoint, "list_tables", m.filterListTablesResult, opts...)
		case "query_data", "exec_sql", "export_excel", "export_excel_with_chart", "export_analysis_image", "export_analysis_docx", "export_ppt", "export_html":
			return m.checkStreamSQLAccess(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
		case "skill":
			// skill 工具仅读取 SKILL.md 文件，无安全风险，直接放行
			m.logAllow(tCtx.Name+"(stream)", "skill_read_only")
			return endpoint(ctx, argumentsInJSON, opts...)
		case "execute":
			// execute 工具执行 shell 命令，安全校验由 OSFilesystemBackend.validateCommand 负责
			m.logAllow(tCtx.Name+"(stream)", "execute_validated_by_backend")
			return endpoint(ctx, argumentsInJSON, opts...)
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

// isContentModeExportTool 判断是否为支持 content 模式的导出工具
// 这些工具允许通过 content 参数直接传入 Markdown 内容（由 Agent 生成），无需 SQL 即可导出
func isContentModeExportTool(toolName string) bool {
	switch toolName {
	case "export_html", "export_ppt", "export_analysis_docx":
		return true
	}
	return false
}

// hasContentArg 检查参数中是否包含非空 content 字段
func hasContentArg(args string) bool {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return false
	}
	content, ok := raw["content"].(string)
	return ok && strings.TrimSpace(content) != ""
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

func (m *PermissionMiddleware) checkQueryAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	sql := m.extractSQLFromArgs(args)
	if sql == "" {
		return "", m.denyEmptySQL("query_data", args)
	}

	m.logInfo("query_data", "sql=%s", truncateForLog(sql))

	if m.Scope.AllSchemasFull {
		m.logAllow("query_data", "schema_full(fast)")
		tables := appperm.ExtractTablesFromSQL(sql)
		result, err := endpoint(ctx, args, opts...)
		if err != nil {
			return "", err
		}
		return m.applyQueryResultFilter(result, tables)
	}

	// 程序化预检：如果所有涉及的表都是 full 级别，直接放行，无需调用 Permission Agent
	tables := appperm.ExtractTablesFromSQL(sql)
	allTablesFullAccess := len(tables) > 0
	for _, table := range tables {
		level := m.Scope.GetTableAccessLevel(table)
		if level != "full" {
			allTablesFullAccess = false
			break
		}
	}
	if allTablesFullAccess {
		m.logAllow("query_data", "precheck(all_tables_full)")
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

	tables := appperm.ExtractTablesFromSQL(input.SQL)
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

func (m *PermissionMiddleware) checkExecAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	sql := m.extractSQLFromArgs(args)
	if sql == "" {
		return "", m.denyEmptySQL("exec_sql", args)
	}

	m.logInfo("exec_sql", "sql=%s", truncateForLog(sql))

	if m.Scope.AllSchemasFull {
		m.logAllow("exec_sql", "schema_full(fast)")
		return endpoint(ctx, args, opts...)
	}

	// 程序化预检：如果所有涉及的表都是 full 级别，直接放行
	tables := appperm.ExtractTablesFromSQL(sql)
	allTablesFullAccess := len(tables) > 0
	for _, table := range tables {
		level := m.Scope.GetTableAccessLevel(table)
		if level != "full" {
			allTablesFullAccess = false
			break
		}
	}
	if allTablesFullAccess {
		m.logAllow("exec_sql", "precheck(all_tables_full)")
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

	for _, table := range appperm.ExtractTablesFromSQL(input.SQL) {
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

func (m *PermissionMiddleware) checkExportAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, toolName string, opts ...tool.Option) (string, error) {
	sql := m.extractSQLFromArgs(args)
	if sql == "" {
		// content 模式：export_html/export_ppt/export_analysis_docx 允许通过 content 参数直接传入 Markdown，
		// 内容由 Agent 生成（基于已通过权限校验的查询结果），不直接访问数据库，无需 SQL 权限校验
		if isContentModeExportTool(toolName) && hasContentArg(args) {
			m.logAllow(toolName, "content_mode(no_sql)")
			return endpoint(ctx, args, opts...)
		}
		return "", m.denyEmptySQL("export", args)
	}

	m.logInfo("export", "sql=%s", truncateForLog(sql))

	if m.Scope.AllSchemasFull {
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

	tables := appperm.ExtractTablesFromSQL(input.SQL)
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
		if m.Scope.AllSchemasFull {
			tables := appperm.ExtractTablesFromSQL(sql)
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

		tables := appperm.ExtractTablesFromSQL(sql)
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
		// content 模式：export_html/export_ppt/export_analysis_docx 允许通过 content 参数直接传入 Markdown，
		// 内容由 Agent 生成（基于已通过权限校验的查询结果），不直接访问数据库，无需 SQL 权限校验
		if isContentModeExportTool(toolName) && hasContentArg(args) {
			m.logAllow(toolName+"(stream)", "content_mode(no_sql)")
			return endpoint(ctx, args, opts...)
		}
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
	tables := appperm.ExtractTablesFromSQL(sqlStr)
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

func truncateForLog(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return strings.ReplaceAll(s, "\n", " ")
}
