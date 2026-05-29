package agent

import (
	"fmt"
	"log"
	"strings"

	admin "websql/internal/app/admin"
	"websql/internal/config"
)

func parseColumnName(raw string) string {
	if idx := strings.Index(raw, "  "); idx > 0 {
		return strings.TrimSpace(raw[:idx])
	}
	return strings.TrimSpace(raw)
}

type PermissionScope struct {
	UserID              string
	ConnID              string
	SchemaName          string
	IsRemote            bool
	HasFullConnAccess   bool
	HasFullSchemaAccess bool
	AllowedTables       map[string]bool
	AllowedColumns      map[string]map[string]bool
	AllowModify         bool
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
		AllowModify:    true,
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
			if pSchema == schemaName {
				hasSchemaPerm = true
			}
		case "table":
			if pSchema == schemaName && pTable != "" {
				scope.AllowedTables[pTable] = true
				hasTableOrColumnForSchema = true
				tableCount++
			}
		case "column":
			if pSchema == schemaName && pTable != "" && pColumn != "" {
				hasTableOrColumnForSchema = true
				colName := parseColumnName(pColumn)
				if !scope.AllowedTables[pTable] {
					if scope.AllowedColumns[pTable] == nil {
						scope.AllowedColumns[pTable] = make(map[string]bool)
					}
					scope.AllowedColumns[pTable][colName] = true
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

	for table := range scope.AllowedColumns {
		if scope.AllowedTables[table] {
			delete(scope.AllowedTables, table)
			log.Printf("[PermScope] 权限降级 - 表 %s 同时存在表级和字段级权限，以字段级权限为准\n", table)
		}
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
		if !s.AllowModify {
			return "\n\n## 数据修改权限（最高优先级）\n当前角色**禁止修改数据**。你绝对不能生成或执行任何 INSERT、UPDATE、DELETE、ALTER、DROP、CREATE、TRUNCATE 等写操作 SQL。如果用户要求修改数据，请明确告知：您当前的角色没有数据修改权限，请联系管理员开通。\n"
		}
		return ""
	}

	if s.HasFullSchemaAccess {
		if !s.AllowModify {
			return fmt.Sprintf("\n\n## 数据权限\n拥有 Schema %s 的完整访问权限，但**禁止修改数据**。你绝对不能生成或执行任何 INSERT、UPDATE、DELETE、ALTER、DROP、CREATE、TRUNCATE 等写操作 SQL。如果用户要求修改数据，请明确告知：您当前的角色没有数据修改权限，请联系管理员开通。", s.SchemaName)
		}
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

	if !s.AllowModify {
		sb.WriteString("\n**禁止修改数据**：你绝对不能生成或执行任何 INSERT、UPDATE、DELETE、ALTER、DROP、CREATE、TRUNCATE 等写操作 SQL。如果用户要求修改数据，请明确告知：您当前的角色没有数据修改权限，请联系管理员开通。\n")
	}

	return sb.String()
}