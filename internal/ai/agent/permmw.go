package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	conn "websql/internal/app/conn"
	appperm "websql/internal/app/permission"
	"websql/internal/audit"
	"websql/internal/pkg/sqlguard"
	"websql/internal/pkg/strutil"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type PermissionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	Scope     *PermissionScope
	PermAgent tool.BaseTool
	// ConnID / Schemas 为本次会话被授权的连接集合：
	// 工具参数里的 connId 只允许命中「默认连接」或「会话选中的 schema 所属连接」，
	// 避免模型（或提示注入）把查询切到未授权的连接上，而表/列校验仍按默认连接的 Scope 判定。
	ConnID  string
	Schemas []SchemaRef
}

// checkConnScope 校验工具参数中的 connId 是否在本次会话授权范围内。
// 返回 nil 表示允许；否则返回拒绝原因。
func (m *PermissionMiddleware) checkConnScope(args string) error {
	if m.Scope.SkipChecks() || len(m.Schemas) == 0 && m.ConnID == "" {
		return nil
	}
	raw := extractConnIDFromArgs(args)
	if raw == "" {
		return nil // 未指定 → 使用默认连接，必然在授权范围内
	}
	allowed := map[string]bool{}
	if m.ConnID != "" {
		allowed[strings.ToUpper(m.ConnID)] = true
	}
	// schema 名 → 连接 ID（与 tools.go 的 resolveConnID 同一规则）
	schemaConn := map[string]string{}
	for _, s := range m.Schemas {
		if s.ConnID == "" {
			continue
		}
		allowed[strings.ToUpper(s.ConnID)] = true
		schemaConn[strings.ToUpper(s.Schema)] = s.ConnID
	}
	target := raw
	if connID, ok := schemaConn[strings.ToUpper(raw)]; ok {
		target = connID
	}
	if allowed[strings.ToUpper(target)] {
		return nil
	}
	log.Printf("%s [拒绝] connId=%s 不在会话授权连接范围内（授权：%v）\n", m.logPrefix(), raw, keysOf(allowed))
	return fmt.Errorf("连接 %s 不在本次会话授权范围内，请仅使用会话已选择的连接（%v）", raw, keysOf(allowed))
}

// extractConnIDFromArgs 从工具参数 JSON 中提取 connId（大小写兼容）。
func extractConnIDFromArgs(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return ""
	}
	for _, key := range []string{"connId", "connID", "conn_id"} {
		if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
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

	// 如果不允许修改数据，从工具列表中移除写操作/命令执行工具
	if !m.Scope.AllowModify {
		filtered := make([]*schema.ToolInfo, 0, len(state.ToolInfos))
		for _, ti := range state.ToolInfos {
			if isPrivilegedWriteTool(ti.Name) {
				log.Printf("%s [ToolInfos] 移除写操作/命令执行工具 - tool=%s\n", m.logPrefix(), ti.Name)
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

// privilegedWriteTools 需要「数据修改权限（AllowModify）」才能使用的工具。
//
// 除数据写入（exec_sql）外，还包括：
//   - write_file / edit_file：任意文件落盘，可被提示注入用于写出恶意脚本或覆盖配置
//   - execute：任意命令执行（Skills 的 python 渲染入口）
//
// 只读角色必须一并失去这些能力，否则可通过执行代码/写盘绕过数据权限约束。
var privilegedWriteTools = map[string]bool{
	"exec_sql":   true,
	"write_file": true,
	"edit_file":  true,
	"execute":    true,
}

func isPrivilegedWriteTool(name string) bool {
	return privilegedWriteTools[name]
}

// fsWriteTools 文件写入类工具：路径已由 export 包沙箱限制，但「写了什么文件」必须留痕，
// 否则提示注入写出脚本/覆盖文件的行为在审计中不可见。
var fsWriteTools = map[string]bool{"write_file": true, "edit_file": true}

// readOnlyUnmonitoredTools 免权限策略的只读工具白名单。
//
// 这些工具不访问数据库、不落盘，也不依赖 PermissionScope：
//   - get_current_date_info：返回当前日期时间
//   - read_file_data：读取本人上传的暂存文件（工具内部做 fileId 归属校验）
//   - ls / read_file / glob / grep：Eino Filesystem 中间件的只读文件工具，
//     路径由 export.OSFilesystemBackend 沙箱限制（工作区 / exports / skills 只读 / 临时目录），
//     其中 write_file / edit_file / execute 已在其他分支按写权限处理
//
// 【安全约定】工具分发采用 fail-closed：未在 switch 中登记、也不在本白名单里的
// 工具一律拒绝。新增数据/文件/命令类工具时必须显式登记权限策略，
// 禁止依赖「默认放行」——否则新增工具会在无任何权限校验的情况下暴露给模型。
var readOnlyUnmonitoredTools = map[string]bool{
	"get_current_date_info": true,
	"read_file_data":        true,
	"ls":                    true,
	"read_file":             true,
	"glob":                  true,
	"grep":                  true,
}

// unregisteredToolError 构造未登记工具的统一拒绝错误
func unregisteredToolError(name string) *PermissionError {
	return &PermissionError{
		Message: fmt.Sprintf("工具 %s 未登记权限策略，已被安全策略拒绝执行", name),
		Objects: []string{name},
	}
}

func isFsWriteTool(name string) bool {
	return fsWriteTools[name]
}

// withFsWriteAudit 对文件写入类工具补审计记录（工具名 + 目标路径 + 成败 + 耗时）。
func (m *PermissionMiddleware) withFsWriteAudit(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, toolName string, opts ...tool.Option) (string, error) {
	start := time.Now()
	result, err := endpoint(ctx, args, opts...)
	status, errMsg := "success", ""
	if err != nil {
		status, errMsg = "failed", err.Error()
	}
	connName, schemaName := conn.GetConnInfo(m.Scope.ConnID)
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:     "agent",
		ToolName:   toolName,
		SQLText:    extractFilePathFromArgs(args),
		SQLType:    "FS_WRITE",
		RiskLevel:  "medium",
		Status:     status,
		ConnID:     m.Scope.ConnID,
		ConnName:   connName,
		SchemaName: schemaName,
		UserID:     m.Scope.UserID,
		ErrorMsg:   errMsg,
		ExecTimeMs: int(time.Since(start).Milliseconds()),
	})
	m.logAllow(toolName, "fs_write(audited)")
	return result, err
}

// extractFilePathFromArgs 从文件工具参数中提取目标路径（兼容 filePath / file_path / path 三种命名）。
// 解析失败时退化为参数前 200 字符，保证审计可读且不会把大段文件内容入库。
func extractFilePathFromArgs(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return strutil.TruncateBytes(args, 200)
	}
	for _, key := range []string{"filePath", "file_path", "path"} {
		if v, ok := raw[key].(string); ok && v != "" {
			return v
		}
	}
	return strutil.TruncateBytes(args, 200)
}

// permErrorObjects 安全提取 PermissionError 携带的对象列表。
// 使用 errors.As 而非裸类型断言，避免断言失败 panic 打断工具调用链路。
func permErrorObjects(err error) []string {
	var pe *PermissionError
	if errors.As(err, &pe) {
		return pe.Objects
	}
	return nil
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
	connName, schemaName := conn.GetConnInfo(m.Scope.ConnID)
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:     "agent",
		ToolName:   toolName,
		SQLText:    detail,
		SQLType:    "PERM_DENIED",
		RiskLevel:  "high",
		Status:     "denied",
		ConnID:     m.Scope.ConnID,
		ConnName:   connName,
		SchemaName: schemaName,
		UserID:     m.Scope.UserID,
		ErrorMsg:   detail,
	})
}

func (m *PermissionMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if m.Scope.SkipChecks() {
		if !m.Scope.AllowModify && isPrivilegedWriteTool(tCtx.Name) {
			m.logDeny(tCtx.Name, "角色禁止修改数据", nil)
			return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
				return "", &PermissionError{
					Message: "当前角色禁止修改数据（含写操作与命令执行），无法执行该工具",
					Objects: []string{},
				}
			}, nil
		}
		m.logAllow(tCtx.Name, "skip(conn_full)")
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		if !m.Scope.AllowModify && isPrivilegedWriteTool(tCtx.Name) {
			m.logDeny(tCtx.Name, "角色禁止修改数据", nil)
			return "", &PermissionError{
				Message: "当前角色禁止修改数据（含写操作与命令执行），无法执行该工具",
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
		case "cross_db_join":
			// 跨库 join 工具：结构化参数（非自由 SQL），程序化校验两侧表/列权限后放行
			return m.checkCrossDBJoinAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "exec_sql":
			return m.checkExecAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "export_excel", "export_excel_with_chart", "export_analysis_docx", "export_ppt", "export_html":
			return m.checkExportAccess(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
		case "skill":
			// skill 工具仅读取 SKILL.md 文件，无安全风险，直接放行
			m.logAllow(tCtx.Name, "skill_read_only")
			return endpoint(ctx, argumentsInJSON, opts...)
		case "execute":
			// execute 工具执行 shell 命令，安全校验由 OSFilesystemBackend.validateCommand 负责
			// 此处增加审计记录 + 间接危险命令检测
			return m.checkExecuteAccess(ctx, argumentsInJSON, endpoint, opts...)
		default:
			if isFsWriteTool(tCtx.Name) {
				return m.withFsWriteAudit(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
			}
			if readOnlyUnmonitoredTools[tCtx.Name] {
				m.logAllow(tCtx.Name, "read_only_allowlist")
				return endpoint(ctx, argumentsInJSON, opts...)
			}
			m.logDeny(tCtx.Name, "工具未登记权限策略(fail-closed)", nil)
			return "", unregisteredToolError(tCtx.Name)
		}
	}, nil
}

func (m *PermissionMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if m.Scope.SkipChecks() {
		if !m.Scope.AllowModify && isPrivilegedWriteTool(tCtx.Name) {
			m.logDeny(tCtx.Name+"(stream)", "角色禁止修改数据", nil)
			return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
				return nil, &PermissionError{
					Message: "当前角色禁止修改数据（含写操作与命令执行），无法执行该工具",
					Objects: []string{},
				}
			}, nil
		}
		m.logAllow(tCtx.Name+"(stream)", "skip(conn_full)")
		return endpoint, nil
	}

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		if !m.Scope.AllowModify && isPrivilegedWriteTool(tCtx.Name) {
			m.logDeny(tCtx.Name+"(stream)", "角色禁止修改数据", nil)
			return nil, &PermissionError{
				Message: "当前角色禁止修改数据（含写操作与命令执行），无法执行该工具",
				Objects: []string{},
			}
		}
		switch tCtx.Name {
		case "get_table_schema":
			return m.checkStreamSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "list_tables":
			return m.postFilterStream(ctx, argumentsInJSON, endpoint, "list_tables", m.filterListTablesResult, opts...)
		case "query_data", "exec_sql", "export_excel", "export_excel_with_chart", "export_analysis_docx", "export_ppt", "export_html":
			return m.checkStreamSQLAccess(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
		case "skill":
			// skill 工具仅读取 SKILL.md 文件，无安全风险，直接放行
			m.logAllow(tCtx.Name+"(stream)", "skill_read_only")
			return endpoint(ctx, argumentsInJSON, opts...)
		case "execute":
			// execute 工具执行 shell 命令，安全校验由 OSFilesystemBackend.validateCommand 负责
			// 审计记录 + 间接危险命令检测（流式版本）
			return m.checkStreamExecuteAccess(ctx, argumentsInJSON, endpoint, opts...)
		default:
			// 只读白名单工具与文件写入类工具（写权限已由上方 privilegedWriteTools 拦截、
			// 路径已由 export 沙箱限制）直接放行；其余未登记工具一律拒绝
			if readOnlyUnmonitoredTools[tCtx.Name] || isFsWriteTool(tCtx.Name) {
				m.logAllow(tCtx.Name+"(stream)", "read_only_allowlist")
				return endpoint(ctx, argumentsInJSON, opts...)
			}
			m.logDeny(tCtx.Name+"(stream)", "工具未登记权限策略(fail-closed)", nil)
			return nil, unregisteredToolError(tCtx.Name)
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
	if err := m.checkConnScope(args); err != nil {
		m.logDeny("query_data", err.Error(), nil)
		return "", &PermissionError{Message: err.Error(), Objects: []string{}}
	}
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

	// 双重验证：Permission Agent（LLM）允许后，追加程序化表级校验。
	// 与 exec_sql 的 doubleCheckHighRiskExec 同一设计目的——防止 LLM 误判或
	// 被 SQL 内嵌文本（注释/字面量）注入诱导放行导致越权读取。
	if err := m.doubleCheckReadAccess(sql, tables); err != nil {
		objects := permErrorObjects(err)
		m.logDeny("query_data", "双重验证拦截(agent允许但程序化拒绝)", objects)
		m.auditPermDenied("query_data", "double_check_blocked: "+err.Error(), objects)
		return "", err
	}

	m.logAllow("query_data", "agent+double_check")
	result, err := endpoint(ctx, args, opts...)
	if err != nil {
		return "", err
	}
	return m.applyQueryResultFilter(result, tables)
}

// doubleCheckReadAccess 对 query_data 做程序化表级双重验证。
//
// 设计原则与 doubleCheckHighRiskExec 一致：
//   - 仅做表级验证（轻量且确定），列级由 Permission Agent + 结果列过滤协同完成
//   - 提取不到表名时（如 SELECT 1 / SHOW TABLES）无法做表级验证，
//     记录日志并放行，由 applyQueryResultFilter 的 fail-closed 策略兜底
func (m *PermissionMiddleware) doubleCheckReadAccess(sql string, tables []string) error {
	if len(tables) == 0 {
		tables = appperm.ExtractTablesFromSQL(sql)
	}
	if len(tables) == 0 {
		m.logInfo("query_data", "双重验证：无法从 SQL 提取表名，跳过表级校验（结果列过滤兜底）")
		return nil
	}

	var deniedTables []string
	for _, table := range tables {
		if !m.Scope.IsTableAllowedIgnoreCase(table) {
			deniedTables = append(deniedTables, table)
		}
	}
	if len(deniedTables) > 0 {
		log.Printf("%s [双重验证] query_data Permission Agent 允许但程序化检查拦截 - sql=%s, deniedTables=%v\n",
			m.logPrefix(), truncateForLog(sql), deniedTables)
		return &PermissionError{
			Message: fmt.Sprintf("双重验证拦截：无权限访问表 %v，请改用您有权限的表重新生成查询。", deniedTables),
			Objects: deniedTables,
		}
	}
	return nil
}

// checkCrossDBJoinAccess 校验 cross_db_join 工具的表级/列级权限。
// 该工具为结构化参数（非自由 SQL），无需 Permission Agent 语义分析，
// 程序化校验两侧表与显式引用的列即可（与 query_data fallback 同一套权限模型）。
func (m *PermissionMiddleware) checkCrossDBJoinAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	if m.Scope.SkipChecks() {
		m.logAllow("cross_db_join", "skip(conn_full)")
		return endpoint(ctx, args, opts...)
	}

	var input CrossDBJoinInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		m.logDeny("cross_db_join", "参数解析失败", nil)
		return "", err
	}

	// 表级校验（支持 schema.table 形式，取表名部分判断）
	var deniedTables []string
	for _, t := range []struct {
		table string
		label string
	}{{input.LeftTable, "leftTable"}, {input.RightTable, "rightTable"}} {
		if t.table == "" {
			continue
		}
		_, tableName := splitSchemaTable(t.table, "")
		if !m.Scope.IsTableAllowedIgnoreCase(tableName) {
			deniedTables = append(deniedTables, tableName)
		}
	}
	if len(deniedTables) > 0 {
		m.logDeny("cross_db_join", "无权访问表", deniedTables)
		return "", &PermissionError{
			Message: fmt.Sprintf("权限不足：无权访问表 %v，请使用您有权限的表重新发起关联分析。", deniedTables),
			Objects: deniedTables,
		}
	}

	// 列级校验：显式 select 列、join key、metrics 列
	if err := m.checkCrossDBJoinColumns(&input); err != nil {
		if pe, ok := err.(*PermissionError); ok {
			m.logDeny("cross_db_join", "无权访问字段", pe.Objects)
		} else {
			m.logDeny("cross_db_join", "列权限校验失败", nil)
		}
		return "", err
	}

	m.logAllow("cross_db_join", "scope_filter")
	return endpoint(ctx, args, opts...)
}

// checkCrossDBJoinColumns 校验 cross_db_join 中显式引用的列（column 级权限表）。
// 注意：column 级权限表必须显式列出 select 列（禁止省略或 *），否则工具内部会
// 取全列并绕过列级过滤（query_data 有结果列过滤兜底，本工具没有）。
func (m *PermissionMiddleware) checkCrossDBJoinColumns(input *CrossDBJoinInput) error {
	check := func(table, col string) error {
		if col == "" || col == "*" {
			return nil
		}
		if m.Scope.GetTableAccessLevel(table) == "column" && !m.Scope.IsColumnAllowed(table, col) {
			return &PermissionError{
				Message: fmt.Sprintf("无权访问字段 %s.%s", table, col),
				Objects: []string{table + "." + col},
			}
		}
		return nil
	}

	_, leftTable := splitSchemaTable(input.LeftTable, "")
	_, rightTable := splitSchemaTable(input.RightTable, "")

	// column 级权限表禁止隐式全列/星号（防列级过滤绕过）
	for _, t := range []struct {
		table      string
		selectCols []string
		label      string
	}{{leftTable, input.LeftSelect, "leftSelect"}, {rightTable, input.RightSelect, "rightSelect"}} {
		if m.Scope.GetTableAccessLevel(t.table) != "column" {
			continue
		}
		if len(t.selectCols) == 0 || containsStarColumn(t.selectCols) {
			return &PermissionError{
				Message: fmt.Sprintf("表 %s 存在列级权限限制，必须通过 %s 显式列出有权限的列（禁止省略或使用 *）", t.table, t.label),
				Objects: []string{t.table},
			}
		}
	}

	// join key（单列或多列数组）
	leftKeys, err := resolveJoinKeys(input.LeftKeys, input.LeftKey)
	if err != nil {
		return err
	}
	rightKeys, err := resolveJoinKeys(input.RightKeys, input.RightKey)
	if err != nil {
		return err
	}
	for _, k := range leftKeys {
		if err := check(leftTable, k); err != nil {
			return err
		}
	}
	for _, k := range rightKeys {
		if err := check(rightTable, k); err != nil {
			return err
		}
	}
	for _, c := range input.LeftSelect {
		if err := check(leftTable, c); err != nil {
			return err
		}
	}
	for _, c := range input.RightSelect {
		if err := check(rightTable, c); err != nil {
			return err
		}
	}

	// where 过滤条件：先做语法白名单校验，再校验引用的列
	for _, w := range []struct {
		where string
		table string
	}{{input.LeftWhere, leftTable}, {input.RightWhere, rightTable}} {
		if strings.TrimSpace(w.where) == "" {
			continue
		}
		if err := validateWhereClause(w.where); err != nil {
			return &PermissionError{
				Message: err.Error(),
				Objects: []string{w.table},
			}
		}
		for _, col := range extractWhereColumns(w.where) {
			if err := check(w.table, col); err != nil {
				return err
			}
		}
	}

	for _, mt := range input.Metrics {
		prefix, col := splitMetricColumn(mt.Column)
		switch prefix {
		case "left":
			if err := check(leftTable, col); err != nil {
				return err
			}
		case "right":
			if err := check(rightTable, col); err != nil {
				return err
			}
		}
	}
	return nil
}

// containsStarColumn 判断列列表中是否包含 "*"
func containsStarColumn(cols []string) bool {
	for _, c := range cols {
		if c == "*" {
			return true
		}
	}
	return false
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
	if err := m.checkConnScope(args); err != nil {
		m.logDeny("exec_sql", err.Error(), nil)
		return "", &PermissionError{Message: err.Error(), Objects: []string{}}
	}
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

	// 双重验证：Permission Agent 允许后，对高风险写操作（DDL + 无 WHERE 的 DELETE/UPDATE）
	// 追加程序化表级校验。防止 Permission Agent（LLM）误放行导致越权操作。
	// 表级校验后统一追加列级复核（checkWriteColumnAccess），LLM 不承担唯一防线。
	if containsDangerousSQL(sql) {
		if err := m.doubleCheckHighRiskExec(sql, tables); err != nil {
			objects := permErrorObjects(err)
			m.logDeny("exec_sql", "双重验证拦截(agent允许但程序化拒绝)", objects)
			m.auditPermDenied("exec_sql", "double_check_blocked: "+err.Error(), objects)
			return "", err
		}
		m.logAllow("exec_sql", "agent+double_check")
	}

	// 列级复核：与降级路径同一逻辑，防止 LLM 误判导致向未授权字段写入
	if err := m.checkWriteColumnAccess(sql); err != nil {
		objects := permErrorObjects(err)
		m.logDeny("exec_sql", "列级校验拦截(agent允许但程序化拒绝)", objects)
		m.auditPermDenied("exec_sql", "column_check_blocked: "+err.Error(), objects)
		return "", err
	}

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

	if err := m.checkWriteColumnAccess(input.SQL); err != nil {
		return "", err
	}

	m.logAllow("exec_sql", "fallback(programmatic)")
	return endpoint(ctx, args, opts...)
}

// doubleCheckHighRiskExec 对高风险写操作做程序化表级双重验证。
// 当 Permission Agent（LLM）返回 allowed=true 时，此函数对 DDL/危险 DML 追加
// 最低限度的程序化验证（仅验证表级访问权限），防止 LLM 误判导致越权。
//
// 设计原则：
//   - 仅对高风险操作生效（由 containsDangerousSQL 判断）
//   - 仅做表级验证（轻量且可靠），不重复 Agent 已完成的列级/语义分析
//   - 发现任何一个无权限表即拒绝，不做部分允许
func (m *PermissionMiddleware) doubleCheckHighRiskExec(sql string, tables []string) error {
	if len(tables) == 0 {
		tables = appperm.ExtractTablesFromSQL(sql)
	}
	if len(tables) == 0 {
		// 无法提取表名（例如 SET/USE 等语句），退化放行
		return nil
	}

	var deniedTables []string
	for _, table := range tables {
		if !m.Scope.IsTableAllowedIgnoreCase(table) {
			deniedTables = append(deniedTables, table)
		}
	}

	if len(deniedTables) > 0 {
		log.Printf("%s [双重验证] Permission Agent 允许但程序化检查拦截 - sql=%s, deniedTables=%v\n",
			m.logPrefix(), truncateForLog(sql), deniedTables)
		return &PermissionError{
			Message: fmt.Sprintf("双重验证拦截：Permission Agent 判定允许，但程序化检查发现无权访问表 %v", deniedTables),
			Objects: deniedTables,
		}
	}
	return nil
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

	// 导出必须只读：导出链路没有审批与事务语义，写语句一律拒绝
	// （同时阻止 CTE 包裹的写操作借导出链路执行）
	if !sqlguard.IsReadOnlyStatement(sql) {
		m.logDeny(toolName, "导出仅支持只读查询", nil)
		return "", &PermissionError{
			Message: "导出仅支持只读查询（SELECT/SHOW/DESCRIBE/EXPLAIN/WITH），不允许写操作",
			Objects: []string{},
		}
	}

	m.logInfo("export", "sql=%s", truncateForLog(sql))
	tables := appperm.ExtractTablesFromSQL(sql)

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

	// 双重验证：与 query_data 保持一致，Permission Agent 允许后追加程序化
	// 表级 + 列级校验。导出结果直接落盘、没有结果过滤兜底，必须执行前拦截。
	if err := m.doubleCheckReadAccess(sql, tables); err != nil {
		objects := permErrorObjects(err)
		m.logDeny(toolName, "双重验证拦截(agent允许但程序化拒绝)", objects)
		m.auditPermDenied(toolName, "double_check_blocked: "+err.Error(), objects)
		return "", err
	}
	if err := m.checkSelectColumnAccessStrict(sql, tables, toolName); err != nil {
		objects := permErrorObjects(err)
		m.logDeny(toolName, "列级校验拦截(agent允许但程序化拒绝)", objects)
		m.auditPermDenied(toolName, "column_check_blocked: "+err.Error(), objects)
		return "", err
	}

	m.logAllow(toolName, "agent+double_check")
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

	if err := m.checkSelectColumnAccessStrict(input.SQL, tables, "export"); err != nil {
		return "", err
	}

	m.logAllow("export", "fallback(programmatic)")
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
	return m.checkSelectColumnAccessWithStar(sql, tables, toolName, false)
}

// checkSelectColumnAccessStrict 列级校验的严格版本：额外拒绝 column 级表上的 SELECT *。
// 用途：导出类工具会把全部列落盘且没有结果过滤兜底，不能像 query_data 那样
// 依赖「结果列过滤」在返回前裁剪，必须在执行前拦截。
func (m *PermissionMiddleware) checkSelectColumnAccessStrict(sql string, tables []string, toolName string) error {
	return m.checkSelectColumnAccessWithStar(sql, tables, toolName, true)
}

func (m *PermissionMiddleware) checkSelectColumnAccessWithStar(sql string, tables []string, toolName string, rejectStar bool) error {
	selectCols := appperm.ExtractSelectColumns(appperm.StripComments(strings.TrimSpace(sql)))
	for _, sc := range selectCols {
		if sc.IsStar {
			if !rejectStar {
				continue
			}
			for _, table := range tables {
				if m.Scope.GetTableAccessLevel(table) == "column" {
					m.logDeny(toolName, "column 级表禁止 SELECT *（导出无结果过滤）", []string{table})
					return &PermissionError{
						Message: fmt.Sprintf("表 %s 仅开放部分字段，导出时必须显式列出允许字段，禁止使用 SELECT *", table),
						Objects: []string{table},
					}
				}
			}
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

// checkWriteColumnAccess 写操作的列级程序化校验。
//
// 用途：exec_sql 在 Permission Agent（LLM）允许后追加，与降级路径使用同一逻辑，
// 防止 Agent 误判导致向未授权字段写入（此前 Agent 路径仅做表级复核）。
func (m *PermissionMiddleware) checkWriteColumnAccess(sql string) error {
	analysis := appperm.AnalyzeSQL(sql, m.Scope.SchemaName)
	if len(analysis.WriteColumns) == 0 || len(analysis.WriteTables) == 0 {
		return nil
	}
	writeTableName := analysis.WriteTables[0].Name
	if m.Scope.GetTableAccessLevel(writeTableName) != "column" {
		return nil
	}
	deniedCols := make([]string, 0)
	for _, col := range analysis.WriteColumns {
		if !m.Scope.IsColumnAllowed(writeTableName, col.ColumnName) {
			deniedCols = append(deniedCols, writeTableName+"."+col.ColumnName)
		}
	}
	if len(deniedCols) > 0 {
		m.logDeny("exec_sql", "无权操作字段", deniedCols)
		return &PermissionError{
			Message: fmt.Sprintf("无权写入字段 %v", deniedCols),
			Objects: deniedCols,
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
		return strutil.TruncateBytes(s, 300) + "..."
	}
	return strings.ReplaceAll(s, "\n", " ")
}

// ──────────────────────────────────────────────
// execute 工具审计与安全增强
// ──────────────────────────────────────────────

// checkExecuteAccess 对 execute 工具增加审计记录和间接危险命令检测。
// OSFilesystemBackend.validateCommand 在执行层做第一道黑名单拦截，
// 此处在权限层做第二道间接攻击检测 + 完整审计记录。
func (m *PermissionMiddleware) checkExecuteAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	command := m.extractCommandFromArgs(args)

	// 间接危险命令检测
	if warning := detectIndirectDangerousCommand(command); warning != "" {
		m.logDeny("execute", "间接危险命令检测", []string{warning})
		m.auditExecute(command, "denied", warning)
		return "", &PermissionError{
			Message: fmt.Sprintf("execute 安全检查拦截：%s", warning),
			Objects: []string{command},
		}
	}

	// 审计记录（执行前）
	m.auditExecute(command, "executing", "")
	m.logAllow("execute", "audit_recorded")

	result, err := endpoint(ctx, args, opts...)

	// 审计记录（执行后更新状态）
	if err != nil {
		m.auditExecute(command, "failed", err.Error())
	}

	return result, err
}

// checkStreamExecuteAccess 流式版本的 execute 审计
func (m *PermissionMiddleware) checkStreamExecuteAccess(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, opts ...tool.Option) (*schema.StreamReader[string], error) {
	command := m.extractCommandFromArgs(args)

	// 间接危险命令检测
	if warning := detectIndirectDangerousCommand(command); warning != "" {
		m.logDeny("execute(stream)", "间接危险命令检测", []string{warning})
		m.auditExecute(command, "denied", warning)
		return nil, &PermissionError{
			Message: fmt.Sprintf("execute 安全检查拦截：%s", warning),
			Objects: []string{command},
		}
	}

	// 审计记录
	m.auditExecute(command, "executing", "")
	m.logAllow("execute(stream)", "audit_recorded")

	return endpoint(ctx, args, opts...)
}

// extractCommandFromArgs 从 execute 工具参数中提取命令字符串
func (m *PermissionMiddleware) extractCommandFromArgs(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return ""
	}
	if cmd, ok := raw["command"].(string); ok {
		return strings.TrimSpace(cmd)
	}
	// 兼容其他可能的参数名
	if cmd, ok := raw["cmd"].(string); ok {
		return strings.TrimSpace(cmd)
	}
	return ""
}

// auditExecute 记录 execute 工具的审计日志
func (m *PermissionMiddleware) auditExecute(command, status, errorMsg string) {
	connName, schemaName := conn.GetConnInfo(m.Scope.ConnID)

	riskLevel := "medium"
	if status == "denied" {
		riskLevel = "high"
	}

	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:     "agent",
		ToolName:   "execute",
		SQLText:    truncateForAudit(command),
		SQLType:    "SHELL",
		RiskLevel:  riskLevel,
		Status:     status,
		ConnID:     m.Scope.ConnID,
		ConnName:   connName,
		SchemaName: schemaName,
		UserID:     m.Scope.UserID,
		ErrorMsg:   errorMsg,
	})
}

// truncateForAudit 截断审计内容，避免巨大命令撑爆数据库字段
func truncateForAudit(s string) string {
	if len(s) > 2000 {
		return strutil.TruncateBytes(s, 2000) + "...(truncated)"
	}
	return s
}

// detectIndirectDangerousCommand 检测通过间接方式执行危险操作的命令。
// 补充 OSFilesystemBackend.defaultCommandValidator 的直接模式检测，
// 覆盖以下攻击向量：
//   - Python subprocess / os.system 执行 shell 命令
//   - base64 编码绕过
//   - 数据外传（python requests/urllib 发送数据）
//   - 数据库 CLI 直接操作
//
// 返回空字符串表示安全；返回非空字符串为拦截原因。
func detectIndirectDangerousCommand(command string) string {
	if command == "" {
		return ""
	}
	lower := strings.ToLower(command)

	// 1. Python 中通过 subprocess/os.system 间接执行 shell（可绕过前端黑名单）
	indirectExecPatterns := []struct {
		pattern string
		reason  string
	}{
		{"os.system(", "检测到 Python os.system() 调用，可能用于绕过命令校验"},
		{"subprocess.call(", "检测到 Python subprocess 调用，可能用于绕过命令校验"},
		{"subprocess.run(", "检测到 Python subprocess 调用，可能用于绕过命令校验"},
		{"subprocess.popen(", "检测到 Python subprocess 调用，可能用于绕过命令校验"},
		{"subprocess.check_output(", "检测到 Python subprocess 调用，可能用于绕过命令校验"},
		// base64 编码执行
		{"base64 -d", "检测到 base64 解码执行，可能用于隐藏恶意命令"},
		{"base64 --decode", "检测到 base64 解码执行，可能用于隐藏恶意命令"},
		// 数据外传
		{"requests.post(", "检测到 Python requests 外传数据"},
		{"requests.get(", "检测到 Python requests 发起网络请求"},
		{"urllib.request", "检测到 Python urllib 发起网络请求"},
		{"http.client", "检测到 Python http.client 发起网络请求"},
		{"socket.connect", "检测到 Python socket 连接"},
		// 数据库 CLI 直接操作（绕过权限中间件）
		{"mysql -e", "检测到直接使用数据库 CLI 执行 SQL，绕过权限控制"},
		{"mysql --execute", "检测到直接使用数据库 CLI 执行 SQL，绕过权限控制"},
		{"psql -c", "检测到直接使用数据库 CLI 执行 SQL，绕过权限控制"},
		{"sqlite3 ", "检测到直接使用数据库 CLI，绕过权限控制"},
		{"sqlplus", "检测到直接使用 Oracle CLI，绕过权限控制"},
		// eval/exec 动态执行
		{"eval(", "检测到 Python eval() 动态执行代码"},
		{"exec(", "检测到 Python exec() 动态执行代码"},
		// PowerShell 编码绕过
		{"-encodedcommand", "检测到 PowerShell 编码命令执行"},
		{"invoke-webrequest", "检测到 PowerShell 网络请求"},
		{"invoke-restmethod", "检测到 PowerShell 网络请求"},
		{"new-object net.webclient", "检测到 PowerShell 网络下载"},
	}

	for _, p := range indirectExecPatterns {
		if strings.Contains(lower, p.pattern) {
			return p.reason
		}
	}

	return ""
}
