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
		// 权限键统一为「schema.table」：裸表名在跨 schema 场景会相互覆盖
		// （如 A.orders 的表级授权会解除 B.orders 的列级限制），必须带 schema 维度
		for s := range schemaSet {
			if scope.FullAccessSchemas[s] {
				continue
			}
			sp := r.BySchema[s]
			if sp == nil {
				continue
			}
			for tableName, tp := range sp.ByTable {
				key := scopeKey(s, tableName)
				// 最具体优先：如果有 column 级配置，即使同时有 table 级也以 column 级为准
				if len(tp.Columns) > 0 {
					for col := range tp.Columns {
						if scope.AllowedColumns[key] == nil {
							scope.AllowedColumns[key] = make(map[string]bool)
						}
						scope.AllowedColumns[key][col] = true // col 已由 ParseColumnName 转为小写
					}
				} else if tp.HasTableLevel {
					scope.AllowedTables[key] = true
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

// scopeKey 构造权限键「schema.table」（权限配置与判定共用同一规则，保留原始大小写）。
func scopeKey(schema, table string) string {
	schema = trimIdent(schema)
	table = trimIdent(table)
	if schema == "" {
		return table
	}
	if table == "" {
		return schema
	}
	return schema + "." + table
}

// trimIdent 去掉标识符两侧的引号/反引号/方括号与空白
func trimIdent(s string) string {
	return strings.Trim(strings.TrimSpace(s), "`\"[]")
}

// qualifyKey 把权限键归一化为「schema.table」（三段式取后两段，保留大小写）
func qualifyKey(key string) string {
	key = trimIdent(key)
	parts := strings.Split(key, ".")
	if len(parts) >= 3 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return key
}

// keyTablePart 返回权限键中的表名部分（保留大小写）
func keyTablePart(key string) string {
	key = qualifyKey(key)
	if idx := strings.LastIndex(key, "."); idx >= 0 {
		return key[idx+1:]
	}
	return key
}

// scopeKeys 返回该表名命中的权限配置键（表名部分严格大小写匹配，延续历史 fail-closed 口径）。
//
// 语义：裸表名在「多 schema 会话」下可能命中多个 schema 的同名表配置，
// 调用方按「所有命中的键都允许才算允许」（交集，fail-closed）判定，
// 避免一个 schema 的表级授权解除另一个 schema 同名表的列级限制。
// 无任何配置时回退到裸键（历史上裸键即表名，查不到即拒绝）。
func (s *PermissionScope) scopeKeys(table string) []string {
	return s.collectKeys(table, false)
}

// scopeKeysFold 大小写不敏感版本：用于容忍 DB 元数据与权限配置的大小写差异
// （如 Oracle 返回大写表名、MySQL 在部分平台保留建表时大小写）。
// 仅用于「只可能增加限制」的判定路径（list_tables 过滤、结果列过滤）与显式 IgnoreCase 调用。
func (s *PermissionScope) scopeKeysFold(table string) []string {
	return s.collectKeys(table, true)
}

func (s *PermissionScope) collectKeys(table string, fold bool) []string {
	t := trimIdent(table)
	if t == "" {
		return nil
	}
	eq := func(a, b string) bool {
		if fold {
			return strings.EqualFold(a, b)
		}
		return a == b
	}

	keys := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}

	if strings.Contains(t, ".") {
		// 已限定 schema（如 db1.orders）：按 schema.table 精确匹配
		want := qualifyKey(t)
		for k := range s.AllowedTables {
			if eq(qualifyKey(k), want) {
				add(k)
			}
		}
		for k := range s.AllowedColumns {
			if eq(qualifyKey(k), want) {
				add(k)
			}
		}
		if len(keys) == 0 {
			add(want)
		}
		return keys
	}

	// 裸表名：收集所有 schema 下的同名配置键
	for k := range s.AllowedTables {
		if eq(keyTablePart(k), t) {
			add(k)
		}
	}
	for k := range s.AllowedColumns {
		if eq(keyTablePart(k), t) {
			add(k)
		}
	}
	if len(keys) == 0 {
		add(t)
	}
	return keys
}

// tableLevel 基于命中的权限键集合判定访问级别（全部键都允许才算允许，交集 fail-closed）
func (s *PermissionScope) tableLevel(keys []string) string {
	if len(keys) == 0 {
		return "none"
	}
	hasColumn := false
	for _, k := range keys {
		if s.AllowedTables[k] {
			continue // 该 schema 为表级（全列）授权
		}
		if len(s.AllowedColumns[k]) > 0 {
			hasColumn = true
			continue
		}
		return "none" // 任一命中的 schema 无该表授权 → 拒绝
	}
	if hasColumn {
		return "column"
	}
	return "full"
}

// columnAllowed 基于命中的权限键集合判定列权限（全部键都允许该列才允许）
func (s *PermissionScope) columnAllowed(keys []string, column string) bool {
	if len(keys) == 0 {
		return false
	}
	col := strings.ToLower(column)
	for _, k := range keys {
		if s.AllowedTables[k] {
			continue // 该 schema 为表级（全列）授权
		}
		if s.AllowedColumns[k][col] {
			continue
		}
		return false // 任一命中的 schema 未授权该列 → 拒绝（fail-closed）
	}
	return true
}

// resolveKeys 返回结果过滤使用的键集合（大小写不敏感，只会增加限制方向）
func (s *PermissionScope) resolveKeys(table string) []string {
	if keys := s.scopeKeys(table); len(keys) > 0 {
		if s.tableLevel(keys) != "none" {
			return keys
		}
	}
	return s.scopeKeysFold(table)
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
	return s.tableLevel(s.scopeKeys(table)) != "none"
}

// IsTableAllowedIgnoreCase 大小写不敏感的表访问判定：
// 用于表名来自 DB 元数据的场景（list_tables 结果过滤、get_table_schema 请求表名）。
func (s *PermissionScope) IsTableAllowedIgnoreCase(table string) bool {
	if s.SkipChecks() || s.AllSchemasFull {
		return true
	}
	if level := s.tableLevel(s.scopeKeys(table)); level != "none" {
		return true
	}
	return s.tableLevel(s.scopeKeysFold(table)) != "none"
}

func (s *PermissionScope) IsColumnAllowed(table, column string) bool {
	if s.SkipChecks() || s.AllSchemasFull {
		return true
	}
	return s.columnAllowed(s.scopeKeys(table), column)
}

func (s *PermissionScope) GetTableAccessLevel(table string) string {
	if s.SkipChecks() || s.AllSchemasFull {
		return "full"
	}
	return s.tableLevel(s.scopeKeys(table))
}

func (s *PermissionScope) FilterResultColumns(columns []string, data []map[string]any, tables []string) ([]string, []map[string]any) {
	if s.SkipChecks() {
		return columns, data
	}

	hasRestrictions := false
	for _, table := range tables {
		if s.tableLevel(s.resolveKeys(table)) == "column" {
			hasRestrictions = true
			break
		}
	}
	if !hasRestrictions {
		return columns, data
	}

	// 按「schema 键交集」收集允许列：多 schema 会话下同名表可能命中多个权限键，
	// 必须所有命中的键都允许该列才保留（fail-closed）；跨表则取并集
	allowedCols := make(map[string]bool)
	for _, table := range tables {
		keys := s.resolveKeys(table)
		if s.tableLevel(keys) != "column" {
			continue
		}
		tableCols := make(map[string]bool)
		firstKey := true
		for _, k := range keys {
			if s.AllowedTables[k] {
				continue // 该 schema 为表级（全列）授权，不构成限制
			}
			if firstKey {
				for col := range s.AllowedColumns[k] {
					tableCols[strings.ToLower(col)] = true
				}
				firstKey = false
				continue
			}
			for col := range tableCols {
				if !s.AllowedColumns[k][col] {
					delete(tableCols, col)
				}
			}
		}
		for col := range tableCols {
			allowedCols[col] = true
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
