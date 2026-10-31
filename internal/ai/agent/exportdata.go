package agent

// 报告导出的数据安全边界：把 Report IR 中的 SQL 绑定块（```chart sql / ```table sql）
// 经「只读校验 + 连接范围 + 表级/列级权限」后取数回填。
//
// export 包不访问数据库、不做权限判断（无法反向依赖 agent 包）；DataSource 由本包注入。
// 权限中间件在工具执行前会对 content 内嵌 SQL 做同一套校验（第一道防线），
// 此处是执行边界上的第二道防线（fail-closed）——即使中间件漏检，未授权数据也取不到。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"websql/internal/ai/agent/export"
	"websql/internal/ai/agent/export/report"
	appperm "websql/internal/app/permission"
	"websql/internal/pkg/sqlguard"
)

// maxReportBindRows 报告内嵌数据（明细表/图表）取数上限，防止误拉取大结果集。
const maxReportBindRows = 500

// newReportDataSource 构建报告数据源。
//
// 连接范围限定为「默认连接 + 会话选中 schema 所属连接」；
// 每个 ref 使用**目标连接自己的 Scope** 校验与脱敏，不能沿用默认连接的 Scope。
func newReportDataSource(defaultConnID, userID string, schemas []SchemaRef) export.DataSource {
	connLookup := buildConnLookup(schemas)
	allowedConns := make(map[string]bool)
	if defaultConnID != "" {
		allowedConns[strings.ToUpper(defaultConnID)] = true
	}
	for _, s := range schemas {
		if s.ConnID != "" {
			allowedConns[strings.ToUpper(s.ConnID)] = true
		}
	}

	return func(ctx context.Context, ref *report.DataRef) (*report.Table, error) {
		if ref == nil || strings.TrimSpace(ref.SQL) == "" {
			return nil, fmt.Errorf("SQL 为空")
		}
		targetConnID := resolveConnID(defaultConnID, ref.ConnID, connLookup)
		if len(allowedConns) > 0 && !allowedConns[strings.ToUpper(targetConnID)] {
			return nil, &PermissionError{
				Message: fmt.Sprintf("连接 %s 不在本次会话授权范围内，无法取数", ref.ConnID),
				Objects: []string{ref.ConnID},
			}
		}
		sec, secErr := RunSecurityFrom(ctx)
		if secErr != nil {
			return nil, secErr
		}
		scope, scopeErr := sec.ScopeFor(targetConnID)
		if scopeErr != nil {
			return nil, scopeErr
		}
		if err := validateExportReadSQL(scope, ref.SQL); err != nil {
			return nil, err
		}
		conn, _, connErr := safeGetConn(targetConnID, userID)
		if connErr != nil {
			return nil, connErr
		}
		if conn == nil {
			return nil, fmt.Errorf("连接不可用：%s", targetConnID)
		}
		limit := ref.Limit
		if limit <= 0 || limit > maxReportBindRows {
			limit = maxReportBindRows
		}
		qr, err := export.QueryForExportLimit(conn, ref.SQL, limit)
		if err != nil {
			return nil, err
		}
		table := export.QueryResultToTable(qr, limit)
		if table == nil {
			return nil, nil
		}

		// 导出结果会直接落盘，没有结果过滤兜底：先按「输出列 → 原始列」脱敏，再交给渲染器。
		masker := GetMaskerProvider().Get()
		if masker == nil {
			recordDegraded("masker", "export_blocked")
			return nil, &PermissionError{Message: "脱敏规则不可用，已阻止导出数据"}
		}
		origins := resolveColumnOriginsForNames(ref.SQL, table.Headers, scope)
		table.Rows = masker.MaskTable(table.Headers, table.Rows, origins, appperm.ExtractTablesFromSQL(ref.SQL))
		if masker.HasRules() {
			recordMaskEvent(StageExport, "report_table")
		}
		return table, nil
	}
}

// maskExportArgs 对导出工具参数中的正文与结构化编辑内容做文本脱敏。
//
// 覆盖：content / summary / issue / changes、export_skill 的 payload（嵌套 JSON）、
// operations[].content、operations[].patch.replace、patches[].replace。
// 导出文件内容不经 SSE 审计，必须在渲染前单独处理；脱敏器不可用时返回错误
// （fail-closed，不以原文落盘）。
func (m *PermissionMiddleware) maskExportArgs(ctx context.Context, args string) (string, error) {
	sec, err := RunSecurityFrom(ctx)
	if err != nil {
		return "", err
	}
	if sec.Masker == nil {
		return "", &PermissionError{Message: "脱敏规则不可用，已阻止导出"}
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return args, nil
	}
	changed := false
	mask := func(text string) string {
		masked := sec.Masker.MaskText(text)
		if masked != text {
			changed = true
		}
		return masked
	}

	for _, key := range []string{"content", "summary", "issue", "changes"} {
		if text, ok := raw[key].(string); ok && text != "" {
			raw[key] = mask(text)
		}
	}

	// export_skill：payload 是 JSON 字符串，必须递归处理所有字符串叶子。
	if payload, ok := raw["payload"].(string); ok && strings.TrimSpace(payload) != "" {
		masked, maskErr := export.MaskJSONStrings(payload, mask)
		if maskErr != nil {
			return "", &PermissionError{Message: "Skill payload 脱敏失败，已阻止导出", Objects: []string{maskErr.Error()}}
		}
		raw["payload"] = masked
	}

	// repair_export 的结构化编辑：operations[].content / operations[].patch.replace / patches[].replace。
	for _, key := range []string{"operations", "patches"} {
		items, ok := raw[key].([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := obj["content"].(string); ok && text != "" {
				obj["content"] = mask(text)
			}
			if patch, ok := obj["patch"].(map[string]any); ok {
				if text, ok := patch["replace"].(string); ok && text != "" {
					patch["replace"] = mask(text)
				}
			}
			if text, ok := obj["replace"].(string); ok && text != "" {
				obj["replace"] = mask(text)
			}
		}
	}

	if !changed {
		return args, nil
	}
	recordMaskEvent(StageExport, "export_args")
	out, marshalErr := json.Marshal(raw)
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(out), nil
}

// hasExportTextArg 判断导出参数是否包含需要脱敏的正文或结构化编辑内容。
func hasExportTextArg(args string) bool {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return false
	}
	for _, key := range []string{"content", "summary", "issue", "changes", "payload"} {
		if text, ok := raw[key].(string); ok && strings.TrimSpace(text) != "" {
			return true
		}
	}
	for _, key := range []string{"operations", "patches"} {
		if items, ok := raw[key].([]any); ok && len(items) > 0 {
			return true
		}
	}
	return false
}

// exportByCodeAllowed 判断高级导出是否允许注册/执行。
//
// 条件：代码执行能力（allow_modify + AI 数据写）+ 完整连接权限 + Python 可用 +
// OS 级隔离可用 + 不存在生效脱敏规则。
// 前两项缺一不可：直连脚本会绕过应用层表/列权限，且会执行任意 Python 代码。
func exportByCodeAllowed(scope *PermissionScope) bool {
	if scope == nil || !scope.AllowModify || !scope.CanAIModifyData() || !scope.SkipChecks() {
		return false
	}
	if !export.IsPythonAvailable() || !export.CodegenIsolationAvailable() {
		return false
	}
	masker := GetMaskerProvider().Get()
	if masker == nil {
		return false // 规则存储不可用：按最严格策略不开放
	}
	return !masker.HasRules()
}

// validateExportReadSQL 导出 SQL 的程序化校验（不依赖权限 Agent，确定性）：
//  1. 必须为只读语句（SELECT/SHOW/DESCRIBE/EXPLAIN/无写 CTE），导出链路无审批与事务语义；
//  2. 涉及的表必须全部在授权范围内；
//  3. column 级权限表必须显式列出允许字段，禁止 SELECT *（导出结果直接落盘，无结果过滤兜底）。
func validateExportReadSQL(scope *PermissionScope, sql string) error {
	if scope.SkipChecks() {
		return nil
	}
	if !sqlguard.IsReadOnlyStatement(sql) {
		return &PermissionError{
			Message: "导出仅支持只读查询（SELECT/SHOW/DESCRIBE/EXPLAIN/WITH），不允许写操作",
			Objects: []string{},
		}
	}
	tables := appperm.ExtractTablesFromSQL(sql)
	var denied []string
	for _, table := range tables {
		if !scope.IsTableAllowedIgnoreCase(table) {
			denied = append(denied, table)
		}
	}
	if len(denied) > 0 {
		return &PermissionError{
			Message: fmt.Sprintf("无权访问表 %v，请改用您有权限的表重新生成查询。", denied),
			Objects: denied,
		}
	}
	return checkSelectColumnsStrict(scope, sql, tables, true)
}

// checkSelectColumnsStrict 列级校验（供权限中间件与导出数据源共用）。
// rejectStar=true 时，column 级权限表上的 SELECT * 一律拒绝。
//
// 实现已统一到 authorizeColumnsInSQL：按方言解析 alias→table，逐列确定真实来源，
// 来源不明时拒绝（导出没有结果过滤兜底，必须严格）。
func checkSelectColumnsStrict(scope *PermissionScope, sql string, tables []string, rejectStar bool) error {
	return authorizeColumnsInSQL(scope, sql, tables, columnCheckOptions{
		RejectStar:         rejectStar,
		RequireKnownOrigin: true,
	})
}
