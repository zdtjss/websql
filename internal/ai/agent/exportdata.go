package agent

// 报告导出的数据安全边界：把 Report IR 中的 SQL 绑定块（```chart sql / ```table sql）
// 经「只读校验 + 连接范围 + 表级/列级权限」后取数回填。
//
// export 包不访问数据库、不做权限判断（无法反向依赖 agent 包）；DataSource 由本包注入。
// 权限中间件在工具执行前会对 content 内嵌 SQL 做同一套校验（第一道防线），
// 此处是执行边界上的第二道防线（fail-closed）——即使中间件漏检，未授权数据也取不到。

import (
	"context"
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
// 连接范围限定为「默认连接 + 会话选中 schema 所属连接」，与 checkConnScope 同一规则。
func newReportDataSource(defaultConnID, userID string, schemas []SchemaRef, scope *PermissionScope) export.DataSource {
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
		if !scope.SkipChecks() && len(allowedConns) > 0 && !allowedConns[strings.ToUpper(targetConnID)] {
			return nil, &PermissionError{
				Message: fmt.Sprintf("连接 %s 不在本次会话授权范围内，无法取数", ref.ConnID),
				Objects: []string{ref.ConnID},
			}
		}
		if err := validateExportReadSQL(scope, ref.SQL); err != nil {
			return nil, err
		}
		conn, _ := GetConn(targetConnID, userID)
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
		return export.QueryResultToTable(qr, limit), nil
	}
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
func checkSelectColumnsStrict(scope *PermissionScope, sql string, tables []string, rejectStar bool) error {
	selectCols := appperm.ExtractSelectColumns(appperm.StripComments(strings.TrimSpace(sql)))
	for _, sc := range selectCols {
		if sc.IsStar {
			if !rejectStar {
				continue
			}
			for _, table := range tables {
				if scope.GetTableAccessLevel(table) == "column" {
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
			if scope.GetTableAccessLevel(table) == "column" {
				hasColumnLevelTable = true
				if scope.IsColumnAllowed(table, colName) {
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
			return &PermissionError{
				Message: fmt.Sprintf("无权访问字段 %s", displayName),
				Objects: []string{displayName},
			}
		}
	}
	return nil
}
