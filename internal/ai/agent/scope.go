package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"

	admin "websql/internal/app/admin"
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
	// AllowModify 现有全局数据修改能力（经典 SQL + AI + 代码/文件工具），保持原角色并集语义。
	AllowModify bool
	// AIAllowModify 是否存在「同一角色同时授予 allow_modify 与 ai_allow_modify」的 AI 写能力。
	// 禁止把角色 A 的 allow_modify 与角色 B 的 ai_allow_modify 拼成新能力。
	AIAllowModify bool
	// AIPolicyEnforce 为 true 时 AI 写能力取 AllowModify && AIAllowModify 双门槛；
	// 兼容期为 false，直接取 AllowModify，保证升级前后行为一致。
	AIPolicyEnforce bool
	// Revision 构建该 Scope 时的权限修订号，用于运行中权限变更检测。
	Revision uint64
	// Profile 运行时 Profile（远程严格 / 桌面完整）。
	Profile RuntimeProfile
}

type PermissionError struct {
	Message string
	Objects []string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("%s: %v", e.Message, e.Objects)
}

func BuildPermissionScope(userId, connId string, schemaNames []string) *PermissionScope {
	profile := CurrentRuntimeProfile()
	scope := &PermissionScope{
		UserID:            userId,
		ConnID:            connId,
		SchemaName:        firstNonEmpty(schemaNames),
		IsRemote:          profile.Remote(),
		FullAccessSchemas: make(map[string]bool),
		AllowedTables:     make(map[string]bool),
		AllowedColumns:    make(map[string]map[string]bool),
		AllowModify:       true,
		AIAllowModify:     true,
		AIPolicyEnforce:   true,
		Profile:           profile,
		Revision:          admin.CurrentPermissionRevision(),
	}

	if !scope.IsRemote {
		log.Printf("[PermScope] 非远程模式，跳过权限检查 - user=%s\n", userId)
		return scope
	}

	// AI 双门槛：兼容期（AIPolicyEnforce=false）CanAIModifyData 直接取 AllowModify，
	// 保证升级前后行为一致；开关打开后同一角色必须双允许。
	scope.AIPolicyEnforce = admin.AIPolicyEnforced()
	scope.AllowModify = false
	scope.AIAllowModify = false
	applyRoleCapabilities(scope, admin.FindUserRoles(userId))

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

// applyRoleCapabilities 计算角色能力：先在每个角色内部取双与，再跨角色取并集。
//
// 这保证「角色 A 的 allow_modify + 角色 B 的 ai_allow_modify」不会拼出
// 任何单一角色都未授予的 AI 写能力。
func applyRoleCapabilities(scope *PermissionScope, roles []*admin.Role) {
	// 双与与并集语义统一由 admin 包实现（role_capability.go），
	// 避免配置层与运行层对「A(1,0)+B(0,1)」这类组合给出不同结论。
	scope.AllowModify = admin.RolesAllowModify(roles)
	scope.AIAllowModify = admin.RolesAllowAIModify(roles)
}

// CanAIModifyData 判断 AI 数据写工具（exec_sql 及未来 CapabilityAIWrite 工具）是否可用。
//
// 兼容期（AIPolicyEnforce=false）沿用 AllowModify，避免升级瞬间改变现有行为。
func (s *PermissionScope) CanAIModifyData() bool {
	if s == nil {
		return false
	}
	if !s.AIPolicyEnforce {
		return s.AllowModify
	}
	return s.AllowModify && s.AIAllowModify
}

// CloneWithRestrictedWrites 返回一个禁止本轮 AI 写操作的副本
// （输入中风险时使用，不影响共享 Scope 与其他并发运行）。
func (s *PermissionScope) CloneWithRestrictedWrites() *PermissionScope {
	if s == nil {
		return nil
	}
	cp := *s
	cp.AllowModify = false
	cp.AIAllowModify = false
	return &cp
}

// ScopeDigest 计算权限快照摘要（与具体密码/密钥无关），用于：
//   - 会话消息的安全溯源；
//   - Resume checkpoint 绑定；
//   - Agent 缓存 key。
func (s *PermissionScope) ScopeDigest() string {
	if s == nil {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "u=%s\nc=%s\ns=%s\nr=%t\nfull=%t\nall=%t\nam=%t\nai=%t\nenf=%t\nrev=%d\nprofile=%d\n",
		s.UserID, s.ConnID, s.SchemaName, s.IsRemote, s.HasFullConnAccess, s.AllSchemasFull,
		s.AllowModify, s.AIAllowModify, s.AIPolicyEnforce, s.Revision, uint8(s.Profile))

	schemaNames := make([]string, 0, len(s.FullAccessSchemas))
	for name := range s.FullAccessSchemas {
		schemaNames = append(schemaNames, name)
	}
	sort.Strings(schemaNames)
	for _, name := range schemaNames {
		fmt.Fprintf(h, "fs=%s\n", name)
	}

	tables := make([]string, 0, len(s.AllowedTables))
	for t := range s.AllowedTables {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	for _, t := range tables {
		fmt.Fprintf(h, "t=%s\n", t)
	}

	colTables := make([]string, 0, len(s.AllowedColumns))
	for t := range s.AllowedColumns {
		colTables = append(colTables, t)
	}
	sort.Strings(colTables)
	for _, t := range colTables {
		cols := make([]string, 0, len(s.AllowedColumns[t]))
		for c := range s.AllowedColumns[t] {
			cols = append(cols, c)
		}
		sort.Strings(cols)
		fmt.Fprintf(h, "ct=%s:%s\n", t, strings.Join(cols, ","))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// IsWriteRestrictedByInput 判断本轮是否因输入风险被收缩了写能力。
func (s *PermissionScope) IsWriteRestrictedByInput() bool {
	return s != nil && !s.AllowModify
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

// scopeKeyParts 权限键的两段（schema / table），保留原始大小写
type scopeKeyParts struct {
	schema string
	table  string
}

// splitScopeKey 拆分权限键：去引号/空白，三段式（db.schema.table）取后两段
func splitScopeKey(key string) scopeKeyParts {
	key = trimIdent(key)
	parts := strings.Split(key, ".")
	switch len(parts) {
	case 1:
		return scopeKeyParts{table: parts[0]}
	case 2:
		return scopeKeyParts{schema: parts[0], table: parts[1]}
	default:
		return scopeKeyParts{schema: parts[len(parts)-2], table: parts[len(parts)-1]}
	}
}

// scopeKeys 返回该表名命中的权限配置键（表名部分严格大小写匹配，延续历史 fail-closed 口径）。
//
// 语义：裸表名在「多 schema 会话」下可能命中多个 schema 的同名表配置，
// 调用方按「所有命中的键都允许才算允许」（交集，fail-closed）判定，
// 避免一个 schema 的表级授权解除另一个 schema 同名表的列级限制。
// 无任何配置时回退到查询表名本身（历史上裸键即表名，查不到即拒绝）。
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
	qualified := strings.Contains(t, ".")
	want := splitScopeKey(t)
	// 空表名或形如 ".orders" 的畸形限定名：不匹配任何配置（fail-closed）
	if want.table == "" || (qualified && want.schema == "") {
		return nil
	}
	eq := func(a, b string) bool {
		if fold {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	// 限定名（db1.orders）：schema 与表名都要匹配；裸表名：只比表名，保留该表名下所有 schema 的配置键
	match := func(k string) bool {
		p := splitScopeKey(k)
		if !qualified {
			return eq(p.table, want.table)
		}
		return eq(p.schema, want.schema) && eq(p.table, want.table)
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

	for k := range s.AllowedTables {
		if match(k) {
			add(k)
		}
	}
	for k := range s.AllowedColumns {
		if match(k) {
			add(k)
		}
	}
	if len(keys) == 0 {
		add(scopeKey(want.schema, want.table))
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

// IsTableAllowedStrict 严格大小写的表访问判定（与 Authorizer 同语义）。
//
// 供无法取得 RunSecurityContext 的路径（如跨库工具的两侧 Scope）使用：
// AllSchemasFull 只对构建时完整评估过的 Schema 放行，不回退 IgnoreCase。
// schema 为空表示未限定 Schema：按“同名表多 schema 交集”判定（fail-closed）。
func (s *PermissionScope) IsTableAllowedStrict(schema, table string) bool {
	if s.SkipChecks() {
		return true
	}
	effectiveSchema := schema
	if effectiveSchema == "" {
		effectiveSchema = s.SchemaName
	}
	if s.AllSchemasFull {
		if effectiveSchema == "" {
			// 未限定表名且所有评估 Schema 均为完整访问：可确定落在已授权 Schema 内
			return true
		}
		return s.FullAccessSchemas[effectiveSchema]
	}
	if effectiveSchema != "" && s.FullAccessSchemas[effectiveSchema] {
		return true
	}
	return s.tableLevel(s.scopeKeys(scopeKey(schema, table))) != "none"
}

// IsTableRefAllowed 按 SQL/工具参数中的表引用（可能带 schema 限定）判定。
//
// 未限定表名保留原始（不自动补默认 Schema）：历史上裸表名表示
// “任意同名 Schema 的交集”，补默认 Schema 会把旧权限键判为无权限。
func (s *PermissionScope) IsTableRefAllowed(ref string) bool {
	t := trimIdent(ref)
	if t == "" {
		return false
	}
	if strings.Contains(t, ".") {
		parts := splitScopeKey(t)
		return s.IsTableAllowedStrict(parts.schema, parts.table)
	}
	return s.IsTableAllowedStrict("", t)
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

	// 结果过滤只需「判定是否存在列级限制」：统一用大小写不敏感的键匹配，
	// 方向只会更严（多过滤列），不会放宽
	hasRestrictions := false
	for _, table := range tables {
		if s.tableLevel(s.scopeKeysFold(table)) == "column" {
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
		keys := s.scopeKeysFold(table)
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
		if !s.CanAIModifyData() {
			return "\n\n## 数据权限（违反将被拦截并记录安全事件）\n⛔ **禁止修改数据（含 AI 数据写操作）**。不得生成或执行 INSERT/UPDATE/DELETE/ALTER/DROP/CREATE/TRUNCATE。用户要求修改时告知：当前角色未开放 AI 数据修改权限，请联系管理员。\n"
		}
		return ""
	}

	if s.AllSchemasFull {
		schemaNames := make([]string, 0, len(s.FullAccessSchemas))
		for name := range s.FullAccessSchemas {
			schemaNames = append(schemaNames, name)
		}
		schemaDesc := strings.Join(schemaNames, ", ")
		if !s.CanAIModifyData() {
			return fmt.Sprintf("\n\n## 数据权限（违反将被拦截）\n✅ Schema [%s] 全部表和字段可访问\n⛔ **禁止修改数据（含 AI 数据写操作）**。不得生成或执行任何写操作 SQL。用户要求修改时告知：当前角色未开放 AI 数据修改权限。\n❌ 禁止访问未列出的 Schema。\n", schemaDesc)
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
	sb.WriteString("⚠️ 权限拒绝时直接向用户说明缺少哪张表/哪个字段的权限并给出替代方案，**禁止**换表名变体、注释包装、导出链路内嵌 SQL 等方式尝试绕过。\n")

	if !s.CanAIModifyData() {
		sb.WriteString("⛔ **禁止修改数据（含 AI 数据写操作）**。不得生成或执行任何写操作 SQL。\n")
	}

	return sb.String()
}
