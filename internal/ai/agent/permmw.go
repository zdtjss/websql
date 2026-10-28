package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"websql/internal/ai/agent/export"
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
	return m.checkConnAllowed(extractConnIDFromArgs(args))
}

// checkConnAllowed 校验连接标识（连接 ID 或 schema 名）是否在本次会话授权范围内。
// 空标识表示使用默认连接（必然在授权范围内）。
func (m *PermissionMiddleware) checkConnAllowed(raw string) error {
	if m.Scope.SkipChecks() || len(m.Schemas) == 0 && m.ConnID == "" {
		return nil
	}
	raw = strings.TrimSpace(raw)
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
	return &PermissionError{
		Message: fmt.Sprintf("连接 %s 不在本次会话授权范围内，请仅使用会话已选择的连接（%v）", raw, keysOf(allowed)),
		Objects: []string{raw},
	}
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
//
// 与 Wrap*ToolCall 的能力校验共用同一 ToolPolicy：注册阶段裁剪 + 执行阶段双保险。
func (m *PermissionMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if state.ToolInfos == nil || m.Scope == nil {
		return ctx, state, nil
	}

	// 允许的写能力：AI 写双门槛 + 代码/文件既有 allow_modify
	allowAIWrite := m.Scope.CanAIModifyData()
	allowCodeExec := m.Scope.AllowModify
	if allowAIWrite && allowCodeExec && m.Scope.SkipChecks() {
		return ctx, state, nil // 完整权限且无能力收缩，无需过滤
	}

	filtered := make([]*schema.ToolInfo, 0, len(state.ToolInfos))
	for _, ti := range state.ToolInfos {
		if ti == nil {
			continue
		}
		policy, ok := lookupToolPolicy(normalizeToolName(ti.Name))
		if !ok {
			// 未登记工具不向模型展示（执行阶段同样 fail-closed）
			log.Printf("%s [ToolInfos] 移除未登记工具 - tool=%s\n", m.logPrefix(), ti.Name)
			continue
		}
		switch policy.Capability {
		case CapabilityAIWrite:
			if !allowAIWrite {
				log.Printf("%s [ToolInfos] 移除 AI 写工具 - tool=%s\n", m.logPrefix(), ti.Name)
				continue
			}
		case CapabilityCodeExecute:
			if !allowCodeExec {
				log.Printf("%s [ToolInfos] 移除代码/文件写工具 - tool=%s\n", m.logPrefix(), ti.Name)
				continue
			}
		}
		filtered = append(filtered, ti)
	}
	state.ToolInfos = filtered
	return ctx, state, nil
}

func (m *PermissionMiddleware) logPrefix() string {
	return fmt.Sprintf("[PermMW] user=%s conn=%s", m.Scope.UserID, m.Scope.ConnID)
}

// tableAllowedStrict 严格判定 SQL 中出现的表是否被授权（保留 schema 限定）。
//
// 与 Authorizer 同语义：不回退 IgnoreCase，AllSchemasFull 只对评估过的 Schema 放行。
func (m *PermissionMiddleware) tableAllowedStrict(table string) bool {
	return m.Scope.IsTableRefAllowed(table)
}

// verifyTablesStrict 在快速路径上仍逐表校验，避免 AllSchemasFull 放行评估集合外的 Schema。
func (m *PermissionMiddleware) verifyTablesStrict(toolName string, tables []string) error {
	for _, table := range tables {
		if !m.tableAllowedStrict(table) {
			m.logDeny(toolName, "快速路径表权限拦截", []string{table})
			return &PermissionError{
				Message: fmt.Sprintf("无权访问表 %s", table),
				Objects: []string{table},
			}
		}
	}
	return nil
}

// privilegedWriteTools 工具能力分类已统一到 toolpolicy.go（Capability）。
// 保留 fsWriteTools 仅用于文件写入审计（路径留痕），能力判定不再使用本表。
var fsWriteTools = map[string]bool{"write_file": true, "edit_file": true}

// readOnlyUnmonitoredTools 已由 toolpolicy.go 的 ToolPolicy 注册表取代。
// 保留注释供历史对照：未登记工具一律拒绝（fail-closed）。

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
	toolName := normalizeToolName(tCtx.Name)

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		// 未登记工具：执行阶段同样 fail-closed（不只依赖 ToolInfos 裁剪）。
		if _, ok := lookupToolPolicy(toolName); !ok {
			m.logDeny(tCtx.Name, "工具未登记权限策略(fail-closed)", nil)
			return "", unregisteredToolError(tCtx.Name)
		}
		// 按目标连接取 Scope + 校验安全依赖可用性；失败即拒绝。
		scoped, _, err := m.resolveRunScopedMiddleware(ctx, toolName, argumentsInJSON)
		if err != nil {
			m.logDeny(tCtx.Name, "运行安全上下文拒绝", permErrorObjects(err))
			return "", err
		}
		// 能力双保险：模型伪造 tool_call 也不能绕过。
		if err := scoped.denyByCapability(toolName); err != nil {
			m.logDeny(tCtx.Name, "能力校验拒绝", permErrorObjects(err))
			return "", err
		}

		switch toolName {
		case "get_table_schema":
			return scoped.checkSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "list_tables":
			return scoped.postFilterSync(ctx, argumentsInJSON, endpoint, "list_tables", scoped.filterListTablesResult, opts...)
		case "query_data":
			return scoped.checkQueryAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "cross_db_join":
			return scoped.checkCrossDBJoinAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "exec_sql":
			return scoped.checkExecAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "export_excel", "export_excel_with_chart", "export_analysis_docx", "export_ppt", "export_html":
			return scoped.checkExportAccess(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
		case "export_by_code":
			return scoped.checkExportByCodeAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "skill":
			scoped.logAllow(tCtx.Name, "skill_read_only")
			return endpoint(ctx, argumentsInJSON, opts...)
		case "execute":
			return scoped.checkExecuteAccess(ctx, argumentsInJSON, endpoint, opts...)
		default:
			if isFsWriteTool(toolName) {
				return scoped.withFsWriteAudit(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
			}
			if !isDataTool(toolName) {
				scoped.logAllow(tCtx.Name, "read_only_registered")
				return endpoint(ctx, argumentsInJSON, opts...)
			}
			// 已在注册表登记但未在分发中显式处理的数据库工具：拒绝，防止默认放行
			scoped.logDeny(tCtx.Name, "数据库工具未登记分发策略(fail-closed)", nil)
			return "", unregisteredToolError(tCtx.Name)
		}
	}, nil
}

func (m *PermissionMiddleware) WrapStreamableToolCall(
	ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	toolName := normalizeToolName(tCtx.Name)

	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		if _, ok := lookupToolPolicy(toolName); !ok {
			m.logDeny(tCtx.Name+"(stream)", "工具未登记权限策略(fail-closed)", nil)
			return nil, unregisteredToolError(tCtx.Name)
		}
		scoped, _, err := m.resolveRunScopedMiddleware(ctx, toolName, argumentsInJSON)
		if err != nil {
			m.logDeny(tCtx.Name+"(stream)", "运行安全上下文拒绝", permErrorObjects(err))
			return nil, err
		}
		if err := scoped.denyByCapability(toolName); err != nil {
			m.logDeny(tCtx.Name+"(stream)", "能力校验拒绝", permErrorObjects(err))
			return nil, err
		}

		switch toolName {
		case "get_table_schema":
			return scoped.checkStreamSchemaAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "list_tables":
			return scoped.postFilterStream(ctx, argumentsInJSON, endpoint, "list_tables", scoped.filterListTablesResult, opts...)
		case "query_data", "exec_sql", "export_excel", "export_excel_with_chart", "export_analysis_docx", "export_ppt", "export_html":
			return scoped.checkStreamSQLAccess(ctx, argumentsInJSON, endpoint, tCtx.Name, opts...)
		case "export_by_code":
			return scoped.checkStreamExportByCodeAccess(ctx, argumentsInJSON, endpoint, opts...)
		case "skill":
			scoped.logAllow(tCtx.Name+"(stream)", "skill_read_only")
			return endpoint(ctx, argumentsInJSON, opts...)
		case "execute":
			return scoped.checkStreamExecuteAccess(ctx, argumentsInJSON, endpoint, opts...)
		default:
			// 文件写入类已由能力校验拦截；只读文件工具直接放行
			if isFsWriteTool(toolName) || !isDataTool(toolName) {
				scoped.logAllow(tCtx.Name+"(stream)", "read_only_registered")
				return endpoint(ctx, argumentsInJSON, opts...)
			}
			scoped.logDeny(tCtx.Name+"(stream)", "数据库工具未登记分发策略(fail-closed)", nil)
			return nil, unregisteredToolError(tCtx.Name)
		}
	}, nil
}

// checkExportByCodeAccess 高级导出（export_by_code）权限校验。
//
// 该工具仅在「用户拥有连接完整权限」时才会注册（直连会绕过应用层表/列权限）；
// 此处再 fail-closed 兜底，并记录审计（代码执行类操作，RiskLevel=medium）。
func (m *PermissionMiddleware) checkExportByCodeAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	// 防御性重算：Agent 可能来自缓存，构建后新增的脱敏规则不应被旧实例绕过。
	if !exportByCodeAllowed(m.Scope) {
		m.logDeny("export_by_code", "高级导出条件不满足（完整权限 / Python / OS 隔离 / 无生效脱敏规则）", nil)
		return "", &PermissionError{
			Message: "高级导出仅对拥有完整连接权限、且不存在生效脱敏规则的用户开放",
			Objects: []string{},
		}
	}
	start := time.Now()
	result, err := endpoint(ctx, args, opts...)
	status, errMsg := "success", ""
	if err != nil {
		status, errMsg = "failed", err.Error()
	}
	connName, schemaName := conn.GetConnInfo(m.Scope.ConnID)
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:     "agent",
		ToolName:   "export_by_code",
		SQLType:    "CODEGEN",
		RiskLevel:  "medium",
		Status:     status,
		ConnID:     m.Scope.ConnID,
		ConnName:   connName,
		SchemaName: schemaName,
		UserID:     m.Scope.UserID,
		ErrorMsg:   errMsg,
		ExecTimeMs: int(time.Since(start).Milliseconds()),
	})
	m.logAllow("export_by_code", "codegen_ro_export")
	return result, err
}

// checkStreamExportByCodeAccess 高级导出的流式版本。
func (m *PermissionMiddleware) checkStreamExportByCodeAccess(ctx context.Context, args string, endpoint adk.StreamableToolCallEndpoint, opts ...tool.Option) (*schema.StreamReader[string], error) {
	if !exportByCodeAllowed(m.Scope) {
		m.logDeny("export_by_code(stream)", "高级导出条件不满足", nil)
		return nil, &PermissionError{
			Message: "高级导出仅对拥有完整连接权限、且不存在生效脱敏规则的用户开放",
			Objects: []string{},
		}
	}
	m.logAllow("export_by_code(stream)", "codegen_ro_export")
	return endpoint(ctx, args, opts...)
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

// contentArg 返回导出工具参数中的 content 正文（解析失败/缺失返回空串）。
func contentArg(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return ""
	}
	content, _ := raw["content"].(string)
	return content
}

// validateContentSQLBindings 扫描 content 正文中的 SQL 数据绑定（```chart sql / ```table sql）
// 并逐条做程序化校验：只读 + 连接范围 + 表级/列级权限。
//
// 背景：content 模式此前不校验 SQL 即放行，而新的报告正文允许内嵌 SQL 由服务端取数，
// 若不校验会形成「content 免检 + 内嵌 SQL 读任意表」的越权通道。
func (m *PermissionMiddleware) validateContentSQLBindings(toolName, content string) error {
	if m.Scope.SkipChecks() {
		return nil
	}
	refs := export.ExtractDataRefs(content)
	for _, ref := range refs {
		if err := m.checkConnAllowed(ref.ConnID); err != nil {
			m.logDeny(toolName, "内嵌SQL连接范围校验失败", []string{ref.ConnID})
			m.auditPermDenied(toolName, "embedded_sql_conn_blocked: "+err.Error(), []string{ref.ConnID})
			return err
		}
		if err := validateExportReadSQL(m.Scope, ref.SQL); err != nil {
			objects := permErrorObjects(err)
			m.logDeny(toolName, "内嵌SQL权限校验拦截", objects)
			m.auditPermDenied(toolName, "embedded_sql_blocked: "+err.Error(), objects)
			return err
		}
		m.logInfo(toolName, "内嵌SQL校验通过 - sql{%s}", safeSQLSummary(ref.SQL))
	}
	return nil
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
	log.Printf("%s [安全] 参数解析失败 - args{%s}\n", m.logPrefix(), safeTextSummary(args))
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
		if m.tableAllowedStrict(table) {
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

	m.logInfo("query_data", "sql{%s}", safeSQLSummary(sql))

	if m.Scope.AllSchemasFull {
		m.logAllow("query_data", "schema_full(fast)")
		tables := appperm.ExtractTablesFromSQL(sql)
		// 快速路径仍逐表校验：AllSchemasFull 只覆盖构建时评估过的 Schema。
		if err := m.verifyTablesStrict("query_data", tables); err != nil {
			return "", err
		}
		result, err := endpoint(ctx, args, opts...)
		if err != nil {
			return "", err
		}
		return m.applyQueryResultFilterMasked(ctx, sql, result, tables)
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
		return m.applyQueryResultFilterMasked(ctx, sql, result, tables)
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
	if err := m.doubleCheckReadAccess(ctx, sql, tables); err != nil {
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
	return m.applyQueryResultFilterMasked(ctx, sql, result, tables)
}

// doubleCheckReadAccess 对 query_data 做程序化表级双重验证。
//
// 设计原则与 doubleCheckHighRiskExec 一致：
//   - 使用 Authorizer 的严格大小写语义，不回退 IgnoreCase（IgnoreCase 仅用于只会更严格的结果过滤）；
//   - AllSchemasFull 只对构建时完整评估过的 Schema 放行；
//   - 提取不到表名时由无资源查询白名单判定。
func (m *PermissionMiddleware) doubleCheckReadAccess(ctx context.Context, sql string, tables []string) error {
	if len(tables) == 0 {
		tables = appperm.ExtractTablesFromSQL(sql)
	}
	if len(tables) == 0 {
		// 无法提取表名：受限用户只放行「已证明不访问任何资源」的常量查询（如 SELECT 1），
		// 其余一律拒绝，不能依赖「查完再过滤」兜底。
		if !m.Scope.SkipChecks() && !appperm.IsVerifiedNoResourceQuery(sql) {
			m.logDeny("query_data", "无法确认 SQL 的数据访问范围", nil)
			return &PermissionError{
				Message: "无法确认 SQL 的数据访问范围，已拒绝执行。请改写为明确的单表查询。",
				Objects: []string{},
			}
		}
		m.logInfo("query_data", "无法从 SQL 提取表名，按无资源查询白名单处理")
		return nil
	}

	var deniedTables []string
	for _, table := range tables {
		if !m.Scope.IsTableRefAllowed(table) {
			deniedTables = append(deniedTables, table)
		}
	}
	if len(deniedTables) > 0 {
		log.Printf("%s [双重验证] query_data Permission Agent 允许但程序化检查拦截 - sql{%s}, deniedTables=%v\n",
			m.logPrefix(), safeSQLSummary(sql), deniedTables)
		return &PermissionError{
			Message: fmt.Sprintf("双重验证拦截：无权限访问表 %v，请改用您有权限的表重新生成查询。", deniedTables),
			Objects: deniedTables,
		}
	}
	return nil
}

// checkCrossDBJoinAccess 校验 cross_db_join 工具的表级/列级权限。
//
// 两侧必须分别使用各自连接 Scope：禁止跨连接合并 allowlist，
// 否则 A 连接的授权会放宽 B 连接的表/列限制。
func (m *PermissionMiddleware) checkCrossDBJoinAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, opts ...tool.Option) (string, error) {
	sec, err := RunSecurityFrom(ctx)
	if err != nil {
		return "", err
	}
	var input CrossDBJoinInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		m.logDeny("cross_db_join", "参数解析失败", nil)
		return "", err
	}

	connLookup := buildConnLookup(m.Schemas)
	leftConnID := resolveConnID(sec.DefaultConnID, input.LeftConnID, connLookup)
	rightConnID := resolveConnID(sec.DefaultConnID, input.RightConnID, connLookup)
	leftScope, err := sec.ScopeFor(leftConnID)
	if err != nil {
		m.logDeny("cross_db_join", "左侧连接不在授权范围内", []string{leftConnID})
		return "", err
	}
	rightScope, err := sec.ScopeFor(rightConnID)
	if err != nil {
		m.logDeny("cross_db_join", "右侧连接不在授权范围内", []string{rightConnID})
		return "", err
	}

	if leftScope.SkipChecks() && rightScope.SkipChecks() {
		m.logAllow("cross_db_join", "skip(both_conn_full)")
		return endpoint(ctx, args, opts...)
	}

	// 表级校验（支持 schema.table 形式；严格大小写，不回退 IgnoreCase）
	var deniedTables []string
	for _, side := range []struct {
		table string
		scope *PermissionScope
	}{
		{input.LeftTable, leftScope},
		{input.RightTable, rightScope},
	} {
		if side.table == "" {
			continue
		}
		_, tableName := splitSchemaTable(side.table, side.scope.SchemaName)
		if !side.scope.IsTableRefAllowed(side.table) {
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

	// 列级校验：显式 select 列、join key、where 列、metrics 列，分别用各自连接 Scope
	if err := checkCrossDBJoinColumnsWithScopes(&input, leftScope, rightScope); err != nil {
		if pe, ok := err.(*PermissionError); ok {
			m.logDeny("cross_db_join", "无权访问字段", pe.Objects)
		} else {
			m.logDeny("cross_db_join", "列权限校验失败", nil)
		}
		return "", err
	}

	m.logAllow("cross_db_join", "scope_filter")
	result, err := endpoint(ctx, args, opts...)
	if err != nil {
		return "", err
	}
	return maskCrossDBJoinResult(sec, result, leftScope, rightScope)
}

// checkCrossDBJoinColumns 保留旧签名（默认两侧同 Scope），供内部复用与测试。
func (m *PermissionMiddleware) checkCrossDBJoinColumns(input *CrossDBJoinInput) error {
	return checkCrossDBJoinColumnsWithScopes(input, m.Scope, m.Scope)
}

// checkCrossDBJoinColumnsWithScopes 校验 cross_db_join 中显式引用的列（column 级权限表）。
// 注意：column 级权限表必须显式列出 select 列（禁止省略或 *），否则工具内部会
// 取全列并绕过列级过滤（query_data 有结果列过滤兜底，本工具没有）。
func checkCrossDBJoinColumnsWithScopes(input *CrossDBJoinInput, leftScope, rightScope *PermissionScope) error {
	check := func(scope *PermissionScope, table, col string) error {
		if col == "" || col == "*" {
			return nil
		}
		if scope.GetTableAccessLevel(table) == "column" && !scope.IsColumnAllowed(table, col) {
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
		scope      *PermissionScope
		selectCols []string
		label      string
	}{
		{leftTable, leftScope, input.LeftSelect, "leftSelect"},
		{rightTable, rightScope, input.RightSelect, "rightSelect"},
	} {
		if t.scope.GetTableAccessLevel(t.table) != "column" {
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
		if err := check(leftScope, leftTable, k); err != nil {
			return err
		}
	}
	for _, k := range rightKeys {
		if err := check(rightScope, rightTable, k); err != nil {
			return err
		}
	}
	for _, c := range input.LeftSelect {
		if err := check(leftScope, leftTable, c); err != nil {
			return err
		}
	}
	for _, c := range input.RightSelect {
		if err := check(rightScope, rightTable, c); err != nil {
			return err
		}
	}

	// where 过滤条件：先做语法白名单校验，再校验引用的列
	for _, w := range []struct {
		where string
		scope *PermissionScope
		table string
	}{
		{input.LeftWhere, leftScope, leftTable},
		{input.RightWhere, rightScope, rightTable},
	} {
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
			if err := check(w.scope, w.table, col); err != nil {
				return err
			}
		}
	}

	for _, mt := range input.Metrics {
		prefix, col := splitMetricColumn(mt.Column)
		switch prefix {
		case "left":
			if err := check(leftScope, leftTable, col); err != nil {
				return err
			}
		case "right":
			if err := check(rightScope, rightTable, col); err != nil {
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
		if !m.tableAllowedStrict(table) {
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
	return m.applyQueryResultFilterMasked(ctx, input.SQL, result, tables)
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

	m.logInfo("exec_sql", "sql{%s}", safeSQLSummary(sql))

	if m.Scope.AllSchemasFull {
		m.logAllow("exec_sql", "schema_full(fast)")
		if err := m.verifyTablesStrict("exec_sql", appperm.ExtractTablesFromSQL(sql)); err != nil {
			return "", err
		}
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
		if !m.tableAllowedStrict(table) {
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
		if !m.tableAllowedStrict(table) {
			deniedTables = append(deniedTables, table)
		}
	}

	if len(deniedTables) > 0 {
		log.Printf("%s [双重验证] Permission Agent 允许但程序化检查拦截 - sql=%s, deniedTables=%v\n",
			m.logPrefix(), safeSQLSummary(sql), deniedTables)
		return &PermissionError{
			Message: fmt.Sprintf("双重验证拦截：Permission Agent 判定允许，但程序化检查发现无权访问表 %v", deniedTables),
			Objects: deniedTables,
		}
	}
	return nil
}

func (m *PermissionMiddleware) checkExportAccess(ctx context.Context, args string, endpoint adk.InvokableToolCallEndpoint, toolName string, opts ...tool.Option) (string, error) {
	sql := m.extractSQLFromArgs(args)
	content := ""
	if isContentModeExportTool(toolName) {
		content = contentArg(args)
	}

	// content 模式的正文可能内嵌 SQL 绑定（```chart sql / ```table sql），这些 SQL
	// 会在工具内取数：必须与顶层 sql 走同一套权限校验，否则形成
	// 「content 免 SQL 校验 + 内嵌 SQL 取数」的越权通道（两参数同时存在时都要校验）。
	if strings.TrimSpace(content) != "" {
		if err := m.validateContentSQLBindings(toolName, content); err != nil {
			return "", err
		}
	}

	// 正文在渲染前再经文本脱敏：SSE 审计覆盖不到导出文件内容，
	// 脱敏失败则拒绝导出（不能以原文落盘）。
	checkedArgs := args
	if strings.TrimSpace(content) != "" {
		maskedArgs, err := m.maskExportArgs(ctx, args)
		if err != nil {
			m.logDeny(toolName, "导出正文脱敏失败", nil)
			return "", err
		}
		checkedArgs = maskedArgs
	}

	if sql == "" {
		// content 模式：export_html/export_ppt/export_analysis_docx 允许通过 content 参数直接传入 Markdown，
		// 内容由 Agent 生成（基于已通过权限校验的查询结果）；其中内嵌 SQL 已在上方完成校验
		if strings.TrimSpace(content) != "" {
			m.logAllow(toolName, "content_mode(no_sql)")
			return endpoint(ctx, checkedArgs, opts...)
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

	m.logInfo("export", "sql{%s}", safeSQLSummary(sql))
	tables := appperm.ExtractTablesFromSQL(sql)

	if m.Scope.AllSchemasFull {
		m.logAllow("export", "schema_full(fast)")
		if err := m.verifyTablesStrict("export", tables); err != nil {
			return "", err
		}
		return endpoint(ctx, checkedArgs, opts...)
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
	if err := m.doubleCheckReadAccess(ctx, sql, tables); err != nil {
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
	return endpoint(ctx, checkedArgs, opts...)
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
		if m.tableAllowedStrict(table) {
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
			// 快速路径仍逐表校验：AllSchemasFull 不能放行评估集合外的 Schema。
			if err := m.verifyTablesStrict(toolName, tables); err != nil {
				return nil, err
			}
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
	err := m.authorizeSelectedColumns(sql, tables, columnCheckOptions{
		RejectStar:         rejectStar,
		RequireKnownOrigin: true,
	})
	if err == nil {
		return nil
	}
	var pe *PermissionError
	if errors.As(err, &pe) {
		m.logDeny(toolName, pe.Message, pe.Objects)
	}
	return err
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
	content := ""
	if isContentModeExportTool(toolName) {
		content = contentArg(args)
	}

	// content 模式内嵌 SQL（```chart sql / ```table sql）与顶层 sql 使用同一套校验
	if strings.TrimSpace(content) != "" {
		if err := m.validateContentSQLBindings(toolName+"(stream)", content); err != nil {
			return streamFromStr(err.Error()), nil
		}
	}

	if sql == "" {
		// content 模式：export_html/export_ppt/export_analysis_docx 允许通过 content 参数直接传入 Markdown，
		// 内容由 Agent 生成（基于已通过权限校验的查询结果）；其中内嵌 SQL 已在上方完成校验
		if strings.TrimSpace(content) != "" {
			maskedArgs, maskErr := m.maskExportArgs(ctx, args)
			if maskErr != nil {
				m.logDeny(toolName+"(stream)", "导出正文脱敏失败", nil)
				return streamFromStr("导出失败：脱敏规则不可用"), nil
			}
			m.logAllow(toolName+"(stream)", "content_mode(no_sql)")
			return endpoint(ctx, maskedArgs, opts...)
		}
		m.logDeny(toolName+"(stream)", "无法解析SQL参数，拒绝执行", nil)
		log.Printf("%s [安全] 参数解析失败 - args{%s}\n", m.logPrefix(), safeTextSummary(args))
		return streamFromStr("权限检查失败：无法解析SQL语句，已拒绝执行"), nil
	}

	m.logInfo(toolName+"(stream)", "sql{%s}", safeSQLSummary(sql))

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
		return m.applyStreamQueryResultFilterMasked(ctx, sql, reader, result.tables), nil
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
			if !m.tableAllowedStrict(table) {
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
		return m.applyStreamQueryResultFilterMasked(ctx, sqlStr, reader, tables), nil
	}

	return reader, nil
}

// 日志中的 SQL / 参数一律使用 safeSQLSummary / safeTextSummary（只记录长度与哈希）。

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
