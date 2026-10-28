package agent

// 输出列来源映射：把查询结果集里的列名（可能是别名）映射回原始
// conn/schema/table/column，供列级脱敏规则匹配。
//
// 关键安全点：`SELECT password AS x FROM users` 必须按 users.password 的规则脱敏，
// 不能因为输出列叫 x 就匹配不到规则。

import (
	"encoding/json"
	"strings"

	appperm "websql/internal/app/permission"
)

// resolveOutputColumnOrigins 解析结果列到原始资源的映射。
//
// 返回的 map 只包含「来源可确定」的输出列；来源不明的列不会出现在 map 中，
// 由 MaskQueryResult 在存在生效规则时按 redact 处理。
func resolveOutputColumnOrigins(sql, payload string, scope *PermissionScope) map[string]ResourceRef {
	if scope == nil {
		return nil
	}
	var out QueryOutput
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return nil
	}
	return resolveColumnOriginsForNames(sql, out.Columns, scope)
}

// resolveColumnOriginsForNames 按输出列名解析来源（供导出表格等非 JSON 场景使用）。
func resolveColumnOriginsForNames(sql string, names []string, scope *PermissionScope) map[string]ResourceRef {
	if scope == nil {
		return nil
	}
	cols, _ := appperm.ResolveSelectColumns(sql)
	byAlias := make(map[string]appperm.SelectColumn, len(cols))
	byName := make(map[string]appperm.SelectColumn, len(cols))
	for _, c := range cols {
		if c.IsStar || c.ColumnName == "" {
			continue
		}
		if alias := explicitColumnAlias(c.Expression); alias != "" {
			byAlias[strings.ToLower(alias)] = c
		}
		byName[strings.ToLower(c.ColumnName)] = c
	}

	origins := make(map[string]ResourceRef, len(names))
	for _, name := range names {
		key := strings.ToLower(name)
		c, ok := byAlias[key]
		if !ok {
			c, ok = byName[key]
		}
		if !ok || c.TableName == "" {
			continue
		}
		schema := c.TableSchema
		if schema == "" {
			schema = scope.SchemaName
		}
		origins[name] = ResourceRef{
			ConnID: scope.ConnID,
			Schema: schema,
			Table:  c.TableName,
			Column: c.ColumnName,
		}
	}
	return origins
}

// explicitColumnAlias 从 SELECT 表达式提取显式别名。
//
// 支持 `expr AS alias` 与 `col alias`；无法确定时返回空串。
func explicitColumnAlias(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	upper := strings.ToUpper(expr)
	if idx := strings.LastIndex(upper, " AS "); idx > 0 {
		return strings.Trim(strings.TrimSpace(expr[idx+4:]), "`\"[]")
	}
	// 隐式别名：`a.password x`（两个 token，且首段不是函数调用）
	if strings.Contains(expr, "(") {
		return ""
	}
	fields := strings.Fields(expr)
	if len(fields) == 2 && !strings.Contains(fields[0], ".") {
		return strings.Trim(fields[1], "`\"[]")
	}
	if len(fields) == 2 && strings.Contains(fields[0], ".") {
		return strings.Trim(fields[1], "`\"[]")
	}
	return ""
}
