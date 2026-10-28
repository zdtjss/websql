package agent

// SELECT 列的来源校验：按方言解析 alias→table，逐列确定真实来源。
//
// 安全背景：
//   - 旧实现按「候选表集合」判断列权限，`SELECT b.id FROM a JOIN b` 中 b.id
//     可能借用 a.id 的授权；
//   - 旧实现用「任一表存在同名授权列」的并集逻辑，JOIN 同名列会被放行。
//
// 新实现：
//   - 别名必须能解析到真实表；裸列只在单表场景可确定来源；
//   - 来源不明或歧义时，涉及列级受限表的查询一律拒绝（fail-closed）；
//   - 只有当本次查询涉及的表全部为 full 级别时才跳过列校验（无规则可违反）。

import (
	"fmt"

	appperm "websql/internal/app/permission"
)

// columnCheckOptions 列校验选项。
type columnCheckOptions struct {
	// RejectStar 拒绝对列级受限表使用 SELECT *（导出链路必须为 true）
	RejectStar bool
	// RequireKnownOrigin 来源不明时拒绝（受限用户默认 true）
	RequireKnownOrigin bool
}

// authorizeSelectedColumns 校验 SELECT 列表中的每一列是否被授权访问。
func (m *PermissionMiddleware) authorizeSelectedColumns(sql string, tables []string, opts columnCheckOptions) error {
	return authorizeColumnsInSQL(m.Scope, sql, tables, opts)
}

// authorizeColumnsInSQL 是列校验的共用实现（中间件与导出链路共用）。
func authorizeColumnsInSQL(scope *PermissionScope, sql string, tables []string, opts columnCheckOptions) error {
	if scope == nil || scope.SkipChecks() || scope.AllSchemasFull {
		return nil
	}

	// 先判断本次查询是否涉及列级受限表：没有受限表就不存在可违反的列规则，
	// 不必因「来源不明」误伤普通 JOIN / 派生表查询。
	restricted := false
	for _, table := range tables {
		if scope.GetTableAccessLevel(trimIdent(table)) == "column" {
			restricted = true
			break
		}
	}
	if !restricted {
		return nil
	}

	cols, _ := appperm.ResolveSelectColumns(sql)

	var deniedColumns []string
	for _, c := range cols {
		if c.IsStar {
			if !opts.RejectStar {
				continue // query_data 由结果列过滤兜底
			}
			table := trimIdent(c.TableName)
			if table != "" {
				if scope.GetTableAccessLevel(table) == "column" {
					return &PermissionError{
						Message: fmt.Sprintf("表 %s 仅开放部分字段，必须显式列出允许字段，禁止使用 SELECT *", table),
						Objects: []string{table},
					}
				}
				continue
			}
			// 无别名 SELECT *（或 a.* 无法解析）：命中任一列级受限表均拒绝
			for _, t := range tables {
				t = trimIdent(t)
				if scope.GetTableAccessLevel(t) == "column" {
					return &PermissionError{
						Message: fmt.Sprintf("表 %s 仅开放部分字段，必须显式列出允许字段，禁止使用 SELECT *", t),
						Objects: []string{t},
					}
				}
			}
			continue
		}

		table := trimIdent(c.TableName)
		if table == "" {
			if opts.RequireKnownOrigin {
				return &PermissionError{
					Message: fmt.Sprintf("无法确定字段 %s 的来源表（可能存在多表同名列或派生表），请显式使用表别名后重试", c.ColumnName),
					Objects: []string{c.ColumnName},
				}
			}
			continue
		}
		if scope.GetTableAccessLevel(table) != "column" {
			continue
		}
		if !scope.IsColumnAllowed(table, c.ColumnName) {
			deniedColumns = append(deniedColumns, table+"."+c.ColumnName)
		}
	}
	if len(deniedColumns) > 0 {
		return &PermissionError{
			Message: fmt.Sprintf("无权访问字段 %v", deniedColumns),
			Objects: deniedColumns,
		}
	}
	return nil
}
