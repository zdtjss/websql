package agent

import (
	"fmt"
	"log"
	"strings"

	admin "websql/internal/app/admin"
	"websql/internal/config"
)

type PermissionScope struct {
	UserID            string
	ConnID            string
	SchemaName        string
	IsRemote          bool
	HasFullConnAccess bool
	FullAccessSchemas map[string]bool // per-schema 完整访问标记
	AllSchemasFull    bool            // 所有选中的 schema 均有完整访问权限（快速路径）
	AllowedTables     map[string]bool
	AllowedColumns    map[string]map[string]bool
	AllowModify       bool
}

type PermissionError struct {
	Message string
	Objects []string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("%s: %v", e.Message, e.Objects)
}

func BuildPermissionScope(userId, connId string, schemaNames []string) *PermissionScope {
	scope := &PermissionScope{
		UserID:            userId,
		ConnID:            connId,
		SchemaName:        firstNonEmpty(schemaNames),
		IsRemote:          !config.IsLocalMode(),
		FullAccessSchemas: make(map[string]bool),
		AllowedTables:     make(map[string]bool),
		AllowedColumns:    make(map[string]map[string]bool),
		AllowModify:       true,
	}

	if !scope.IsRemote {
		log.Printf("[PermScope] 非远程模式，跳过权限检查 - user=%s\n", userId)
		return scope
	}

	scope.AllowModify = false
	roles := admin.FindUserRoles(userId)
	for _, role := range roles {
		if role.AllowModify > 0 {
			scope.AllowModify = true
			break
		}
	}

	powerList := admin.FindUserPowerDetails(userId)
	log.Printf("[PermScope] 用户权限记录数=%d - user=%s, conn=%s\n", len(powerList), userId, connId)

	byRole := admin.GroupPowerDetailsByRole(powerList, connId)

	schemaSet := make(map[string]bool)
	for _, s := range schemaNames {
		if s != "" {
			schemaSet[s] = true
		}
	}

	// 逐角色解析权限，按 schema 维度收集完整访问和表/列级权限
	for _, roleDetails := range byRole {
		r := admin.ResolveRolePermissions(roleDetails)

		// conn 级权限：对所有没有限制的 schema 授予完整访问
		if r.HasConnLevel {
			for s := range schemaSet {
				if !scope.FullAccessSchemas[s] {
					sp := r.BySchema[s]
					if sp == nil || !sp.HasRestriction() {
						scope.FullAccessSchemas[s] = true
					}
				}
			}
		}

		// schema 级权限：对没有限制的 schema 授予完整访问
		for s := range schemaSet {
			if !scope.FullAccessSchemas[s] {
				sp := r.BySchema[s]
				if sp != nil && sp.HasSchemaLevel && !sp.HasRestriction() {
					scope.FullAccessSchemas[s] = true
				}
			}
		}

		// table/column 级权限：仅处理没有完整访问的 schema
		for s := range schemaSet {
			if scope.FullAccessSchemas[s] {
				continue
			}
			sp := r.BySchema[s]
			if sp == nil {
				continue
			}
			for tableName, tp := range sp.ByTable {
				// 最具体优先：如果有 column 级配置，即使同时有 table 级也以 column 级为准
				if len(tp.Columns) > 0 {
					for col := range tp.Columns {
						if scope.AllowedColumns[tableName] == nil {
							scope.AllowedColumns[tableName] = make(map[string]bool)
						}
						scope.AllowedColumns[tableName][col] = true // col 已由 ParseColumnName 转为小写
					}
				} else if tp.HasTableLevel {
					scope.AllowedTables[tableName] = true
				}
			}
		}
	}

	// 检查是否所有选中的 schema 均有完整访问权限（快速路径）
	if len(schemaSet) > 0 {
		scope.AllSchemasFull = true
		for s := range schemaSet {
			if !scope.FullAccessSchemas[s] {
				scope.AllSchemasFull = false
				break
			}
		}
	}

	// 判定 conn 级完整访问权限（最高级别快速路径）：
	// 条件：用户在某个角色中拥有 conn 级权限，且所有选中的 schema 均被授予完整访问
	// （即无任何 schema/table/column 级限制降级了 conn 级权限）。
	// 此标记启用后，SkipChecks() 返回 true，跳过 Permission Agent 调用和程序化检查，
	// 同时避免不必要的 Permission Agent 实例化（builder.go 中依赖此字段）。
	if scope.AllSchemasFull && len(scope.AllowedTables) == 0 && len(scope.AllowedColumns) == 0 {
		// 确认确实来源于 conn 级权限（而非单纯的多个 schema 级权限恰好覆盖全部）
		for _, roleDetails := range byRole {
			r := admin.ResolveRolePermissions(roleDetails)
			if r.HasConnLevel {
				scope.HasFullConnAccess = true
				break
			}
		}
	}

	log.Printf("[PermScope] 权限范围 - user=%s, conn=%s, schemas=%v, fullConn=%v, fullSchemas=%d, allFull=%v, tables=%d, columnTables=%d\n",
		userId, connId, schemaNames, scope.HasFullConnAccess, len(scope.FullAccessSchemas), scope.AllSchemasFull, len(scope.AllowedTables), len(scope.AllowedColumns))

	return scope
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func (s *PermissionScope) SkipChecks() bool {
	return !s.IsRemote || s.HasFullConnAccess
}

func (s *PermissionScope) HasAnyAccess() bool {
	if !s.IsRemote {
		return true
	}
	return s.HasFullConnAccess || len(s.FullAccessSchemas) > 0 || len(s.AllowedTables) > 0 || len(s.AllowedColumns) > 0
}

func (s *PermissionScope) IsTableAllowed(table string) bool {
	if s.SkipChecks() || s.AllSchemasFull {
		return true
	}
	return s.AllowedTables[table] || len(s.AllowedColumns[table]) > 0
}

func (s *PermissionScope) IsTableAllowedIgnoreCase(table string) bool {
	if s.IsTableAllowed(table) {
		return true
	}
	upper := strings.ToUpper(table)
	for t := range s.AllowedTables {
		if strings.ToUpper(t) == upper {
			return true
		}
	}
	for t := range s.AllowedColumns {
		if strings.ToUpper(t) == upper {
			return true
		}
	}
	return false
}

func (s *PermissionScope) IsColumnAllowed(table, column string) bool {
	if s.SkipChecks() || s.AllSchemasFull || s.AllowedTables[table] {
		return true
	}
	if cols, ok := s.AllowedColumns[table]; ok {
		return cols[strings.ToLower(column)]
	}
	return false
}

func (s *PermissionScope) GetTableAccessLevel(table string) string {
	if s.SkipChecks() || s.AllSchemasFull || s.AllowedTables[table] {
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
		if !s.AllowModify {
			return "\n\n## 数据权限（违反将被拦截并记录安全事件）\n⛔ **禁止修改数据**。不得生成或执行 INSERT/UPDATE/DELETE/ALTER/DROP/CREATE/TRUNCATE。用户要求修改时告知：当前角色无修改权限，请联系管理员。\n"
		}
		return ""
	}

	if s.AllSchemasFull {
		schemaNames := make([]string, 0, len(s.FullAccessSchemas))
		for name := range s.FullAccessSchemas {
			schemaNames = append(schemaNames, name)
		}
		schemaDesc := strings.Join(schemaNames, ", ")
		if !s.AllowModify {
			return fmt.Sprintf("\n\n## 数据权限（违反将被拦截）\n✅ Schema [%s] 全部表和字段可访问\n⛔ **禁止修改数据**。不得生成或执行任何写操作 SQL。用户要求修改时告知：当前角色无修改权限。\n❌ 禁止访问未列出的 Schema。\n", schemaDesc)
		}
		return fmt.Sprintf("\n\n## 数据权限（违反将被拦截）\n✅ Schema [%s] 全部表和字段可访问\n❌ 禁止访问未列出的 Schema。\n", schemaDesc)
	}

	var sb strings.Builder
	sb.WriteString("\n\n## 数据权限（违反将被拦截并记录安全事件）\n")
	sb.WriteString("你**仅被允许**访问以下表和字段。任何对未列出对象的查询将被系统立即拒绝。\n\n")

	// 表格化展示权限
	hasTablePerm := len(s.AllowedTables) > 0
	hasColPerm := len(s.AllowedColumns) > 0

	if hasTablePerm || hasColPerm {
		sb.WriteString("| 表名 | 访问级别 | 可用字段 |\n")
		sb.WriteString("|------|---------|--------|\n")

		for t := range s.AllowedTables {
			fmt.Fprintf(&sb, "| %s | full | 全部 |\n", t)
		}
		for table, cols := range s.AllowedColumns {
			if s.AllowedTables[table] {
				continue
			}
			colList := make([]string, 0, len(cols))
			for col := range cols {
				colList = append(colList, col)
			}
			fmt.Fprintf(&sb, "| %s | column | %s |\n", table, strings.Join(colList, ", "))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("⚠️ 对 column 级别的表使用 SELECT * **会被拒绝**，必须显式列出允许字段。\n")

	if !s.AllowModify {
		sb.WriteString("⛔ **禁止修改数据**。不得生成或执行任何写操作 SQL。\n")
	}

	return sb.String()
}
