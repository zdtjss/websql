package agent

// 原生导出的统一数据执行器。
//
// 背景：export_excel / export_excel_with_chart / export_docx / export_ppt / export_html
// 的顶层 `sql` 由 export 包直接查询并落盘，SSE 审计与结果列过滤都覆盖不到。
// 这里把取数收敛到 QueryRunner：只读校验 → 表权限 → 严格列校验 → 执行 →
// 结果列过滤 + 脱敏，保证「导出文件」与「查询结果」适用同一安全边界。

import (
	"context"
	"strings"

	"websql/internal/ai/agent/export"
	appperm "websql/internal/app/permission"
	"websql/internal/pkg/sqlguard"

	"github.com/jmoiron/sqlx"
)

// exportQueryRunner 实现 export.QueryRunner。
type exportQueryRunner struct {
	conn   *sqlx.DB
	scope  *PermissionScope
	userID string
}

func newExportQueryRunner(conn *sqlx.DB, scope *PermissionScope, userID string) *exportQueryRunner {
	return &exportQueryRunner{conn: conn, scope: scope, userID: userID}
}

// QueryForExport 在导出取数前完成权限校验，并对结果执行脱敏。
func (r *exportQueryRunner) QueryForExport(ctx context.Context, sqlText string, maxRows int) (*export.QueryResult, error) {
	if r == nil || r.scope == nil {
		return nil, &PermissionError{Message: "导出缺少权限上下文，已拒绝执行"}
	}
	sec, err := RunSecurityFrom(ctx)
	if err != nil {
		return nil, err
	}
	if !sec.DataToolsAllowed() {
		return nil, &PermissionError{Message: "数据访问安全依赖不可用，已禁止导出"}
	}
	if !sec.RevisionStillValid() {
		return nil, &PermissionError{Message: "权限已发生变化，本次导出已失效，请重新发起"}
	}

	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return nil, &PermissionError{Message: "导出缺少 SQL 语句"}
	}
	// 导出链路没有审批与事务语义：写语句一律拒绝（含 CTE 包裹的写操作）
	if !sqlguard.IsReadOnlyStatement(sqlText) {
		return nil, &PermissionError{
			Message: "导出仅支持只读查询（SELECT/SHOW/DESCRIBE/EXPLAIN/WITH），不允许写操作",
			Objects: []string{},
		}
	}

	tables := appperm.ExtractTablesFromSQL(sqlText)
	for _, table := range tables {
		if !r.scope.IsTableRefAllowed(table) {
			recordSecurityEvent(StageExport, "table", "deny")
			return nil, &PermissionError{
				Message: "导出涉及无权访问的数据表",
				Objects: []string{table},
			}
		}
	}
	// 导出结果直接落盘，没有结果过滤兜底：严格列校验（禁止 SELECT *、来源必须明确）
	if err := authorizeColumnsInSQL(r.scope, sqlText, tables, columnCheckOptions{
		RejectStar:         true,
		RequireKnownOrigin: true,
	}); err != nil {
		recordSecurityEvent(StageExport, "column", "deny")
		return nil, err
	}

	qr, err := export.QueryForExport(r.conn, sqlText)
	if err != nil {
		return nil, err
	}
	if qr == nil {
		return &export.QueryResult{}, nil
	}
	if maxRows > 0 && len(qr.Data) > maxRows {
		return nil, &PermissionError{Message: "导出数据量超过上限，请先聚合或缩小范围"}
	}

	// 先权限过滤、后脱敏：与 query_data 使用同一实现与规则。
	masker := sec.Masker
	if masker == nil {
		masker = NewNoopMasker()
	}
	out := QueryOutput{Columns: qr.Columns, Data: qr.Data, Count: len(qr.Data)}
	origins := resolveColumnOriginsForNames(sqlText, qr.Columns, r.scope)
	masker.MaskQueryOutput(&out, tables, origins, r.scope)
	// 候选策略 2 同样作用于文件结果：shadow 不改变导出内容，enforce 整列脱敏。
	if candidateColumns := candidateMaskHighRiskColumns(sec, out.Columns, origins, masker); len(candidateColumns) > 0 {
		redactQueryOutputColumns(&out, candidateColumns)
	}
	if masker.HasRules() {
		recordMaskEvent(StageExport, "native_export")
	}
	return &export.QueryResult{Columns: out.Columns, Data: out.Data}, nil
}
