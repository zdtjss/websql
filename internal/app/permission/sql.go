package permission

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/config"
	"websql/internal/pkg/sqlguard"
)

type SQLAnalysis struct {
	OperationType string
	ReadTables    []TableRef
	WriteTables   []TableRef
	WriteColumns  []ColumnRef
	OriginalSQL   string
}

type TableRef struct {
	Schema string
	Name   string
}

type ColumnRef struct {
	TableName  string
	ColumnName string
}

type ColumnAccessLevel string

const (
	AccessFull   ColumnAccessLevel = "full"
	AccessColumn ColumnAccessLevel = "column"
	AccessNone   ColumnAccessLevel = "none"
)

type TableColumnAccess struct {
	Level          ColumnAccessLevel
	AllowedColumns map[string]bool
}

func AnalyzeSQL(sqlStr string, defaultSchema string) *SQLAnalysis {
	trimmed := strings.TrimSpace(sqlStr)
	analysis := &SQLAnalysis{
		OperationType: extractOperationType(trimmed),
		OriginalSQL:   trimmed,
	}

	allTables := ExtractTablesFromSQL(trimmed)

	switch analysis.OperationType {
	case "SELECT":
		for _, t := range allTables {
			schema, name := resolveTableName(t, defaultSchema)
			analysis.ReadTables = append(analysis.ReadTables, TableRef{Schema: schema, Name: name})
		}
	case "INSERT":
		target, cols := extractInsertTarget(trimmed, defaultSchema)
		if target != nil {
			analysis.WriteTables = append(analysis.WriteTables, *target)
			analysis.WriteColumns = cols
		}
		for _, t := range allTables {
			schema, name := resolveTableName(t, defaultSchema)
			if target == nil || schema != target.Schema || name != target.Name {
				analysis.ReadTables = append(analysis.ReadTables, TableRef{Schema: schema, Name: name})
			}
		}
	case "UPDATE":
		targets, cols := extractUpdateTargets(trimmed, defaultSchema)
		if len(targets) > 0 {
			analysis.WriteTables = append(analysis.WriteTables, targets...)
			analysis.WriteColumns = cols
		}
		for _, t := range allTables {
			schema, name := resolveTableName(t, defaultSchema)
			if !containsTableRef(targets, schema, name) {
				analysis.ReadTables = append(analysis.ReadTables, TableRef{Schema: schema, Name: name})
			}
		}
	case "DELETE":
		target := extractDeleteTarget(trimmed, defaultSchema)
		if target != nil {
			analysis.WriteTables = append(analysis.WriteTables, *target)
		}
	case "DDL":
		target := extractDDLTarget(trimmed, defaultSchema)
		if target != nil {
			analysis.WriteTables = append(analysis.WriteTables, *target)
		}
	}

	return analysis
}

// extractOperationType 判定语句类型（fail-closed 口径）。
//
// 安全性说明：历史实现用「关键字+空格」前缀匹配且不剥离注释，存在两类绕过：
//  1. `DELETE\tFROM t`（制表符/换行分隔）不命中前缀，被判为 UNKNOWN；
//  2. `WITH/*c*/x AS (...) DELETE FROM t`、`DELETE/*c*/FROM t` 因注释插在
//     关键字中间无法识别首关键字，CTE 写操作也被当成 SELECT。
//
// 现改为：先剥离注释 → 取词级首关键字 → WITH 语句额外整句扫描写关键字
// （与 AI 路径共用 sqlguard 的同一份判定实现）。
func extractOperationType(sql string) string {
	// WriteKind：普通语句取首关键字；WITH 语句返回整句扫描到的首个写关键字
	switch sqlguard.WriteKind(sql) {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "VALUES", "TABLE":
		return "SELECT"
	case "INSERT", "REPLACE", "MERGE":
		return "INSERT"
	case "UPDATE":
		return "UPDATE"
	case "DELETE":
		return "DELETE"
	case "WITH":
		// 不含写关键字的 CTE：按只读查询处理
		return "SELECT"
	case "ALTER", "DROP", "CREATE", "TRUNCATE", "RENAME":
		return "DDL"
	default:
		return "UNKNOWN"
	}
}

func resolveTableName(name, defaultSchema string) (string, string) {
	name = strings.Trim(name, "`")
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		return strings.Trim(parts[0], "`"), strings.Trim(parts[1], "`")
	}
	return defaultSchema, name
}

func extractInsertTarget(sql string, defaultSchema string) (*TableRef, []ColumnRef) {
	re := regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?`)
	match := re.FindString(sql)
	if match == "" {
		return nil, nil
	}
	tablePart := regexp.MustCompile(`(?i)\bINTO\s+(.+)`).FindStringSubmatch(match)
	if len(tablePart) < 2 {
		return nil, nil
	}
	tableName := strings.TrimSpace(tablePart[1])
	schema, name := resolveTableName(tableName, defaultSchema)
	target := &TableRef{Schema: schema, Name: name}

	colRe := regexp.MustCompile(`(?i)\bINTO\s+(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?\s*\(([^)]+)\)`)
	colMatch := colRe.FindStringSubmatch(sql)
	if len(colMatch) >= 2 {
		colStr := colMatch[1]
		colNames := strings.Split(colStr, ",")
		var cols []ColumnRef
		for _, c := range colNames {
			c = strings.TrimSpace(c)
			c = strings.Trim(c, "`")
			if c != "" {
				cols = append(cols, ColumnRef{TableName: name, ColumnName: c})
			}
		}
		return target, cols
	}

	return target, nil
}

// extractUpdateTargets 解析 UPDATE 的全部写入表与 SET 列归属。
//
// 支持：`UPDATE t`、`UPDATE s.t`、`UPDATE a AS x`、`UPDATE a, b`、
// `UPDATE a JOIN b ON ...`、`UPDATE a LEFT JOIN b AS y ON ...`。
// 无法确定列归属（多表且未使用别名/表名限定）时返回 TableName 为空，
// 由调用方 fail-closed 拒绝。
func extractUpdateTargets(sql string, defaultSchema string) ([]TableRef, []ColumnRef) {
	updateRe := regexp.MustCompile(`(?i)\bUPDATE\b`)
	updateLoc := updateRe.FindStringIndex(sql)
	if updateLoc == nil {
		return nil, nil
	}
	rest := sql[updateLoc[1]:]
	setRe := regexp.MustCompile(`(?i)\bSET\b`)
	setLoc := setRe.FindStringIndex(rest)
	if setLoc == nil {
		return nil, nil
	}
	targetText := rest[:setLoc[0]]
	setText := rest[setLoc[1]:]
	if end := findTopLevelClause(setText, []string{"WHERE", "ORDER", "LIMIT", "RETURNING"}); end >= 0 {
		setText = setText[:end]
	}

	var tables []TableRef
	aliases := make(map[string]TableRef)
	addTarget := func(segment string) {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return
		}
		for {
			trimmed := trimJoinModifier(segment)
			if trimmed == segment {
				break
			}
			segment = trimmed
		}
		if end := findTopLevelClause(segment, []string{"ON", "USING"}); end >= 0 {
			segment = segment[:end]
		}
		fields := strings.Fields(segment)
		if len(fields) == 0 {
			return
		}
		schema, name := resolveTableName(trimUpdateIdent(fields[0]), defaultSchema)
		if name == "" {
			return
		}
		ref := TableRef{Schema: schema, Name: name}
		tables = append(tables, ref)
		aliases[strings.ToLower(name)] = ref
		if len(fields) >= 2 {
			alias := fields[1]
			if strings.EqualFold(alias, "AS") && len(fields) >= 3 {
				alias = fields[2]
			}
			alias = trimUpdateIdent(alias)
			if alias != "" && !isJoinModifier(alias) {
				aliases[strings.ToLower(alias)] = ref
			}
		}
	}
	for _, part := range splitUpdateTargetSegments(targetText) {
		addTarget(part)
	}
	if len(tables) == 0 {
		return nil, nil
	}

	cols := make([]ColumnRef, 0, 4)
	for _, assignment := range splitTopLevelCommas(setText) {
		assignment = strings.TrimSpace(assignment)
		if assignment == "" {
			continue
		}
		lhs, _, ok := strings.Cut(assignment, "=")
		if !ok {
			continue
		}
		lhs = strings.TrimSpace(lhs)
		parts := strings.Split(lhs, ".")
		colName := trimUpdateIdent(parts[len(parts)-1])
		if colName == "" {
			continue
		}
		tableName := ""
		if len(parts) >= 2 {
			qualifier := trimUpdateIdent(parts[len(parts)-2])
			if ref, ok := aliases[strings.ToLower(qualifier)]; ok {
				tableName = ref.Name
			}
		} else if len(tables) == 1 {
			tableName = tables[0].Name
		}
		cols = append(cols, ColumnRef{TableName: tableName, ColumnName: colName})
	}
	return tables, cols
}

func containsTableRef(refs []TableRef, schema, name string) bool {
	for _, ref := range refs {
		if ref.Schema == schema && ref.Name == name {
			return true
		}
	}
	return false
}

func trimUpdateIdent(s string) string {
	return strings.Trim(strings.TrimSpace(s), "`\"[]")
}

func isJoinModifier(word string) bool {
	switch strings.ToUpper(word) {
	case "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "OUTER", "CROSS", "NATURAL", "STRAIGHT_JOIN", "ON", "USING", "AS", "SET":
		return true
	}
	return false
}

func trimJoinModifier(segment string) string {
	fields := strings.Fields(segment)
	if len(fields) == 0 || !isJoinModifier(fields[0]) || strings.EqualFold(fields[0], "ON") || strings.EqualFold(fields[0], "USING") {
		return segment
	}
	idx := strings.Index(segment, fields[0])
	return strings.TrimSpace(segment[idx+len(fields[0]):])
}

// splitUpdateTargetSegments 按顶层逗号与 JOIN 关键字切分目标表段。
func splitUpdateTargetSegments(s string) []string {
	var parts []string
	start, depth := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
			continue
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
			continue
		}
		if depth != 0 || i+4 > len(s) {
			continue
		}
		if strings.EqualFold(s[i:i+4], "JOIN") && isIdentBoundary(s, i-1) && isIdentBoundary(s, i+4) {
			parts = append(parts, s[start:i])
			start = i + 4
			i += 3
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func splitTopLevelCommas(s string) []string {
	var parts []string
	start, depth := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func isIdentBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' || c == '#')
}

// findTopLevelClause 返回顶层（括号/引号外）第一个关键字的位置，未命中返回 -1。
func findTopLevelClause(s string, keywords []string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
			continue
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth != 0 || !isIdentStart(c) {
			continue
		}
		if i > 0 && !isIdentBoundary(s, i-1) {
			continue
		}
		j := i
		for j < len(s) && isIdentChar(s[j]) {
			j++
		}
		word := strings.ToUpper(s[i:j])
		for _, keyword := range keywords {
			if word == keyword {
				return i
			}
		}
		i = j - 1
	}
	return -1
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9' || c == '$' || c == '#'
}

func extractDeleteTarget(sql string, defaultSchema string) *TableRef {
	re := regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`)
	match := re.FindStringSubmatch(sql)
	if len(match) >= 2 {
		tableName := strings.TrimSpace(match[1])
		schema, name := resolveTableName(tableName, defaultSchema)
		return &TableRef{Schema: schema, Name: name}
	}

	re2 := regexp.MustCompile(`(?i)\bDELETE\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?\s+FROM)`)
	match2 := re2.FindStringSubmatch(sql)
	if len(match2) >= 2 {
		tablePart := match2[1]
		tablePart = strings.TrimSuffix(strings.TrimSpace(tablePart), " FROM")
		tablePart = strings.TrimSuffix(strings.TrimSpace(tablePart), " from")
		tableName := strings.TrimSpace(tablePart)
		schema, name := resolveTableName(tableName, defaultSchema)
		return &TableRef{Schema: schema, Name: name}
	}

	return nil
}

func extractDDLTarget(sql string, defaultSchema string) *TableRef {
	patterns := []string{
		`(?i)\bALTER\s+TABLE\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
		`(?i)\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
		`(?i)\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
		`(?i)\bTRUNCATE\s+(?:TABLE\s+)?((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
		`(?i)\bRENAME\s+TABLE\s+((?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|\w+))?)`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		match := re.FindStringSubmatch(sql)
		if len(match) >= 2 {
			tableName := strings.TrimSpace(match[1])
			schema, name := resolveTableName(tableName, defaultSchema)
			return &TableRef{Schema: schema, Name: name}
		}
	}
	return nil
}

type RoleTableAccess struct {
	Level          ColumnAccessLevel
	AllowedColumns map[string]bool
}

func resolveRoleTableAccess(roleDetails []*admin.PowerDetail, schemaName, tableName string) *RoleTableAccess {
	r := admin.ResolveRolePermissions(roleDetails)
	sp := r.BySchema[schemaName]

	// 判断该 schema 下是否存在更具体的限制（table 级或 column 级配置）
	// 如果存在，则 conn/schema 级权限降级，以具体配置为准
	schemaHasRestriction := sp != nil && sp.HasRestriction()

	if r.HasConnLevel && !schemaHasRestriction {
		return &RoleTableAccess{Level: AccessFull}
	}
	if sp != nil && sp.HasSchemaLevel && !schemaHasRestriction {
		return &RoleTableAccess{Level: AccessFull}
	}

	if sp == nil {
		return &RoleTableAccess{Level: AccessNone}
	}
	tp := sp.ByTable[tableName]
	if tp == nil {
		// 当前表没有任何配置
		// 如果有 conn/schema 级权限但 schema 下有其他表的限制，当前表不在限制列表中
		// → 不允许访问（因为限制意味着"只允许访问配置的表"）
		return &RoleTableAccess{Level: AccessNone}
	}
	// 最具体优先：如果有 column 级配置，即使同时有 table 级也以 column 级为准
	if len(tp.Columns) > 0 {
		return &RoleTableAccess{Level: AccessColumn, AllowedColumns: tp.Columns}
	}
	if tp.HasTableLevel {
		return &RoleTableAccess{Level: AccessFull}
	}
	return &RoleTableAccess{Level: AccessNone}
}

func GetTableColumnAccess(connId, schemaName, tableName, authorization string) *TableColumnAccess {
	if config.IsLocalMode() {
		return &TableColumnAccess{Level: AccessFull}
	}

	userPower := admin.GetUserPower(authorization)
	if userPower == nil {
		return &TableColumnAccess{Level: AccessNone}
	}

	// 优先查决策缓存，避免重复解析权限规则（命中即返回）
	if cached, ok := globalDecisionCache.getDecision(userPower.UserId, connId, schemaName, tableName); ok {
		return &TableColumnAccess{
			Level:          cached.level,
			AllowedColumns: copyAllowedColumns(cached.allowedCols),
		}
	}

	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		globalDecisionCache.setDecision(userPower.UserId, connId, schemaName, tableName,
			&decisionCacheEntry{level: AccessNone, allowedCols: nil})
		return &TableColumnAccess{Level: AccessNone}
	}

	byRole := admin.GroupPowerDetailsByRole(powerDetails, connId)

	bestAccess := &TableColumnAccess{Level: AccessNone}
	for _, roleDetails := range byRole {
		roleAccess := resolveRoleTableAccess(roleDetails, schemaName, tableName)
		if roleAccess.Level == AccessFull {
			// 命中 full 级权限，缓存并返回
			globalDecisionCache.setDecision(userPower.UserId, connId, schemaName, tableName,
				&decisionCacheEntry{level: AccessFull, allowedCols: nil})
			return &TableColumnAccess{Level: AccessFull}
		}
		if roleAccess.Level == AccessColumn {
			if bestAccess.Level == AccessNone {
				bestAccess = &TableColumnAccess{Level: AccessColumn, AllowedColumns: make(map[string]bool)}
			}
			for col := range roleAccess.AllowedColumns {
				bestAccess.AllowedColumns[col] = true
			}
		}
	}

	// 缓存决策结果（column 级保存允许列的副本，none 级保存 nil）
	var cachedCols map[string]bool
	if bestAccess.Level == AccessColumn {
		cachedCols = make(map[string]bool, len(bestAccess.AllowedColumns))
		for k, v := range bestAccess.AllowedColumns {
			cachedCols[k] = v
		}
	}
	globalDecisionCache.setDecision(userPower.UserId, connId, schemaName, tableName,
		&decisionCacheEntry{level: bestAccess.Level, allowedCols: cachedCols})

	return bestAccess
}

// copyAllowedColumns 复制缓存的允许列集合，避免外部修改污染缓存。
func copyAllowedColumns(src map[string]bool) map[string]bool {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func GetTableAccessDowngraded(connId, schemaName, tableName, authorization string) *TableColumnAccess {
	access := GetTableColumnAccess(connId, schemaName, tableName, authorization)
	if access.Level == AccessColumn {
		access.Level = AccessFull
	}
	return access
}

func CheckTablePermission(connId, schemaName, tableName, authorization string) {
	if config.IsLocalMode() {
		return
	}
	admin.CheckSchemaAccess(connId, schemaName, authorization)
	admin.CheckTableAccess(connId, schemaName, tableName, authorization)
}

func CheckTableWritePermission(connId string, schemaName string, tableName string, columns []string, authorization string) {
	if config.IsLocalMode() {
		return
	}
	CheckTablePermission(connId, schemaName, tableName, authorization)
	if !CheckUserCanModify(authorization) {
		panic(errors.New("当前角色禁止修改数据"))
	}
}

// StripComments 剥离 SQL 注释。
// 实现已下沉到 internal/pkg/sqlguard.StripComments（项目内唯一实现），
// 修复历史正则实现的缺陷：字符串字面量内的 -- / # 不再被误当作注释剥离。
func StripComments(sql string) string {
	return sqlguard.StripComments(sql)
}

// sqlIdentFragment 与 tableRefFragment SQL 标识符/限定表名的正则片段。
//
// 必须用 \p{L}/\p{N}而不是 \w：Go 正则的 \w 仅匹配 ASCII，
// 中文表名（如 FROM 订单）会提取不到表，导致表级权限校验与脱敏判定拿到空集合（误判/绕过）。
const (
	sqlIdentFragment = "(?:`[^`]+`|\"[^\"]+\"|[\\p{L}\\p{N}_$#]+)"
	tableRefFragment = "(?:(?:" + sqlIdentFragment + "\\.)?" + sqlIdentFragment + ")"
)

func ExtractTablesFromSQL(sql string) []string {
	sql = StripComments(sql)
	tables := make(map[string]bool)

	primaryRegex := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+(` + tableRefFragment + `)`)
	commaRegex := regexp.MustCompile(`\s*,\s*(` + tableRefFragment + `)`)
	metadataRegex := regexp.MustCompile(`(?i)\b(?:DESCRIBE|DESC|SHOW\s+CREATE\s+TABLE)\s+(` + tableRefFragment + `)`)
	// 增强 CTE 识别：支持递归 CTE（WITH RECURSIVE name AS）和多个 CTE（name1 AS (...), name2 AS (...)）
	cteRegex := regexp.MustCompile(`(?i)\bWITH\s+(?:RECURSIVE\s+)?(\w+)\s+(?:\([^)]*\)\s+)?AS\s*\(`)
	cteCommaRegex := regexp.MustCompile(`(?i)\)\s*,\s*(\w+)\s+(?:\([^)]*\)\s+)?AS\s*\(`)

	cteNames := make(map[string]bool)
	for _, match := range cteRegex.FindAllStringSubmatch(sql, -1) {
		if len(match) > 1 {
			cteNames[strings.ToLower(match[1])] = true
		}
	}
	// 提取逗号分隔的后续 CTE 名称
	for _, match := range cteCommaRegex.FindAllStringSubmatch(sql, -1) {
		if len(match) > 1 {
			cteNames[strings.ToLower(match[1])] = true
		}
	}

	for _, idx := range primaryRegex.FindAllStringSubmatchIndex(sql, -1) {
		if len(idx) >= 4 {
			tableName := stripBackticks(sql[idx[2]:idx[3]])
			if !isSQLKeyword(tableName) && !cteNames[strings.ToLower(tableName)] && !isSQLBuiltinSchema(tableName) {
				tables[tableName] = true
			}

			afterMatch := sql[idx[1]:]
			stopRegex := regexp.MustCompile(`(?i)\b(?:WHERE|GROUP\s+BY|ORDER\s+BY|HAVING|LIMIT|OFFSET|UNION|INTERSECT|EXCEPT|VALUES|SET|ON)\b`)
			if stopMatch := stopRegex.FindStringIndex(afterMatch); stopMatch != nil {
				afterMatch = afterMatch[:stopMatch[0]]
			}

			afterMatch = skipTableAlias(afterMatch)

			for {
				trimmed := strings.TrimLeft(afterMatch, " \t\n\r")
				if !strings.HasPrefix(trimmed, ",") {
					break
				}
				commaMatch := commaRegex.FindStringSubmatch(trimmed)
				if len(commaMatch) < 2 {
					break
				}
				commaTableName := stripBackticks(commaMatch[1])
				if !isSQLKeyword(commaTableName) && !cteNames[strings.ToLower(commaTableName)] && !isSQLBuiltinSchema(commaTableName) {
					remainingAfterTable := trimmed[len(commaMatch[0]):]
					if len(remainingAfterTable) == 0 || remainingAfterTable[0] != '(' {
						tables[commaTableName] = true
					}
				}
				afterMatch = skipTableAlias(trimmed[len(commaMatch[0]):])
			}
		}
	}

	for _, match := range metadataRegex.FindAllStringSubmatch(sql, -1) {
		if len(match) > 1 {
			tableName := stripBackticks(match[1])
			if !isSQLKeyword(tableName) {
				tables[tableName] = true
			}
		}
	}

	result := make([]string, 0, len(tables))
	for table := range tables {
		result = append(result, table)
	}
	return result
}

// isSQLBuiltinSchema 判断是否为系统内置 schema（不需要权限检查）。
// 避免将 information_schema.tables 等系统表引用误识别为用户表。
func isSQLBuiltinSchema(name string) bool {
	lower := strings.ToLower(name)
	// 处理 schema.table 格式，只检查 schema 部分
	if dotIdx := strings.Index(lower, "."); dotIdx >= 0 {
		lower = lower[:dotIdx]
	}
	builtinSchemas := map[string]bool{
		"information_schema": true,
		"performance_schema": true,
		"mysql":              true,
		"sys":                true,
		"pg_catalog":         true,
		"sqlite_master":      true,
	}
	return builtinSchemas[lower]
}

func skipTableAlias(s string) string {
	s = strings.TrimLeft(s, " \t\n\r")
	asRegex := regexp.MustCompile(`(?i)^AS\s+\w+`)
	if loc := asRegex.FindStringIndex(s); loc != nil {
		return s[loc[1]:]
	}
	if len(s) > 0 && s[0] != ',' && s[0] != '(' && s[0] != ')' {
		identRegex := regexp.MustCompile(`^\w+`)
		if loc := identRegex.FindStringIndex(s); loc != nil {
			word := s[:loc[1]]
			if !isSQLKeyword(word) {
				return s[loc[1]:]
			}
		}
	}
	return s
}

func stripBackticks(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '`' && s[len(s)-1] == '`') ||
			(s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func isSQLKeyword(s string) bool {
	keywords := map[string]bool{
		"DUAL": true, "AS": true, "SET": true, "SELECT": true,
		"FROM": true, "WHERE": true, "JOIN": true, "INNER": true,
		"LEFT": true, "RIGHT": true, "OUTER": true, "ON": true,
		"GROUP": true, "BY": true, "ORDER": true, "HAVING": true,
		"LIMIT": true, "OFFSET": true, "UNION": true, "ALL": true,
		"INSERT": true, "INTO": true, "VALUES": true, "UPDATE": true,
		"DELETE": true, "CREATE": true, "ALTER": true, "DROP": true,
		"TABLE": true, "INDEX": true, "VIEW": true, "DATABASE": true,
		"SCHEMA": true, "COLUMN": true, "KEY": true, "PRIMARY": true,
		"FOREIGN": true, "REFERENCES": true, "CONSTRAINT": true,
		"AND": true, "OR": true, "NOT": true, "IN": true, "EXISTS": true,
		"BETWEEN": true, "LIKE": true, "IS": true, "NULL": true,
		"ASC": true, "DESC": true, "DISTINCT": true, "COUNT": true,
		"SUM": true, "AVG": true, "MIN": true, "MAX": true,
	}
	return keywords[strings.ToUpper(s)]
}

type SQLPermissionResult struct {
	Allowed        bool
	DeniedTables   []string
	DeniedColumns  []string
	AllowedColumns []string
	Message        string
}

func CheckUserCanModify(authorization string) bool {
	if config.IsLocalMode() {
		return true
	}
	user := admin.GetUser(authorization)
	if user == nil {
		return false
	}
	roles := admin.FindUserRoles(user.Id)
	for _, role := range roles {
		if role.AllowModify > 0 {
			return true
		}
	}
	return false
}

// requiresModifyPermission 判断语句是否必须持有「数据修改权限（allow_modify）」。
//
// 口径（fail-closed）：仅「明确只读」的语句（SELECT / SHOW / DESCRIBE / EXPLAIN /
// 不含写关键字的 WITH）可跳过修改权限校验；其余语句——包括解析出的写表/写列、
// CTE 包裹的写操作、注释变体、以及解析器无法识别的语句——一律要求修改权限，
// 防止「解析失败 → 校验被跳过 → 写操作被放行」的权限绕过。
func requiresModifyPermission(analysis *SQLAnalysis) bool {
	if analysis == nil {
		return true
	}
	if len(analysis.WriteTables) > 0 || len(analysis.WriteColumns) > 0 {
		return true
	}
	return !sqlguard.IsReadOnlyStatement(analysis.OriginalSQL)
}

// truncateSQL 截断日志中的 SQL 文本（按 rune，避免截断出非法 UTF-8）。
func truncateSQL(sql string) string {
	sql = strings.ReplaceAll(sql, "\n", " ")
	runes := []rune(sql)
	if len(runes) > 200 {
		return string(runes[:200]) + "..."
	}
	return sql
}

func CheckSQLFullPermission(sqlStr, connId, schema, authorization string) *SQLPermissionResult {
	analysis := AnalyzeSQL(sqlStr, schema)
	return CheckAnalysisPermission(analysis, connId, authorization)
}

// CheckBatchSQLPermission 批量检查多条 SQL 的权限，内部只查询一次用户权限数据
// 返回第一个不允许的结果，如果全部允许则返回 nil
func CheckBatchSQLPermission(sqlList []string, connId, schema, authorization string) *SQLPermissionResult {
	if config.IsLocalMode() {
		return nil
	}

	// 一次性查询用户权限信息
	userPower := admin.GetUserPower(authorization)
	if userPower == nil {
		return &SQLPermissionResult{Allowed: false, Message: "无权访问"}
	}
	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return &SQLPermissionResult{Allowed: false, Message: "无权访问"}
	}
	byRole := admin.GroupPowerDetailsByRole(powerDetails, connId)

	// 检查是否有写权限（只检查一次）
	hasWritePermission := false
	writeCheckDone := false

	for _, sqlStr := range sqlList {
		analysis := AnalyzeSQL(sqlStr, schema)

		// 写操作权限检查（延迟到第一次遇到写操作时）
		if requiresModifyPermission(analysis) {
			if !writeCheckDone {
				hasWritePermission = CheckUserCanModify(authorization)
				writeCheckDone = true
			}
			if !hasWritePermission {
				log.Printf("[PermCheck] 批量写权限拒绝 - conn=%s, user=%s, sql=%s\n", connId, userPower.UserId, truncateSQL(sqlStr))
				return &SQLPermissionResult{
					Allowed: false,
					Message: "当前角色禁止修改数据，无法执行写操作",
				}
			}
		}

		// 检查读表权限
		for _, t := range analysis.ReadTables {
			if !checkTableAccessWithRoles(byRole, t.Schema, t.Name) {
				log.Printf("[PermCheck] 批量表权限拒绝 - conn=%s, user=%s, table=%s\n", connId, userPower.UserId, t.Name)
				return &SQLPermissionResult{
					Allowed:      false,
					DeniedTables: []string{t.Name},
					Message:      fmt.Sprintf("无权访问表: %s", t.Name),
				}
			}
		}

		// 检查写表权限
		for _, t := range analysis.WriteTables {
			if !checkTableAccessWithRoles(byRole, t.Schema, t.Name) {
				log.Printf("[PermCheck] 批量表权限拒绝 - conn=%s, user=%s, table=%s\n", connId, userPower.UserId, t.Name)
				return &SQLPermissionResult{
					Allowed:      false,
					DeniedTables: []string{t.Name},
					Message:      fmt.Sprintf("无权访问表: %s", t.Name),
				}
			}
		}
	}

	return nil
}

func CheckAnalysisPermission(analysis *SQLAnalysis, connId, authorization string) *SQLPermissionResult {
	result := &SQLPermissionResult{Allowed: true}

	// 写操作的角色级修改权限检查（allow_modify 开关，fail-closed 口径）
	if requiresModifyPermission(analysis) {
		if !CheckUserCanModify(authorization) {
			result.Allowed = false
			result.Message = "当前角色禁止修改数据，无法执行写操作"
			log.Printf("[PermCheck] 写权限拒绝 - conn=%s, user=%s, writeTables=%v, sql=%s\n",
				connId, authorization, analysis.WriteTables, truncateSQL(analysis.OriginalSQL))
			return result
		}
	}

	// 【设计说明】经典模式下不检查 WriteColumns 的列级权限。
	// 原因：用户在 SQL 编辑器中编写的 SQL 可能非常复杂（子查询、CTE、多表 JOIN、
	// 别名、表达式等），基于正则的 SQL 解析器无法可靠地判断每个字段的归属表。
	// 列级权限检查仅在大模型 Agent 模式下生效（通过 PermissionScope.IsColumnAllowed
	// 和 FilterResultColumns 实现），因为 Agent 模式有 AI 辅助的语义理解和双重防线。
	// 因此，经典模式下列级权限统一降级为表级权限（参见 GetTableAccessDowngraded）。

	if config.IsLocalMode() {
		return result
	}

	// 一次性查询用户权限详情（admin 包内已做 powerCache 缓存，通常不触发 DB 查询）
	userPower := admin.GetUserPower(authorization)
	if userPower == nil {
		result.Allowed = false
		result.Message = "无权访问"
		log.Printf("[PermCheck] 拒绝 - conn=%s, reason=用户权限为空\n", connId)
		return result
	}
	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		result.Allowed = false
		result.Message = "无权访问"
		log.Printf("[PermCheck] 拒绝 - conn=%s, user=%s, reason=权限详情为空\n", connId, userPower.UserId)
		return result
	}
	byRole := admin.GroupPowerDetailsByRole(powerDetails, connId)

	// 检查读表权限
	for _, t := range analysis.ReadTables {
		if !checkTableAccessWithRoles(byRole, t.Schema, t.Name) {
			result.Allowed = false
			result.DeniedTables = append(result.DeniedTables, t.Name)
		}
	}

	// 检查写表权限
	for _, t := range analysis.WriteTables {
		if !checkTableAccessWithRoles(byRole, t.Schema, t.Name) {
			result.Allowed = false
			result.DeniedTables = append(result.DeniedTables, t.Name)
		}
	}

	if !result.Allowed {
		result.Message = fmt.Sprintf("无权访问表: %s", strings.Join(result.DeniedTables, ", "))
		log.Printf("[PermCheck] 表权限拒绝 - conn=%s, user=%s, deniedTables=%v\n", connId, userPower.UserId, result.DeniedTables)
		return result
	}

	return result
}

// checkTableAccessWithRoles 使用已解析的角色权限检查表访问权限（避免重复查询数据库）
func checkTableAccessWithRoles(byRole map[string][]*admin.PowerDetail, schemaName, tableName string) bool {
	for _, roleDetails := range byRole {
		roleAccess := resolveRoleTableAccess(roleDetails, schemaName, tableName)
		// 经典模式下列级降级为表级，只要不是 AccessNone 就算有权限
		if roleAccess.Level != AccessNone {
			return true
		}
	}
	return false
}

type SelectColumn struct {
	TableAlias string
	// TableName 方言解析后的真实来源表（由 ResolveSelectColumns 填充）。
	// 为空表示无法确定来源（未登记别名、多表裸列、派生表）。
	TableName string
	// TableSchema 来源表所属 Schema（解析成功且唯一时填充）。
	TableSchema string
	ColumnName  string
	IsStar      bool
	Expression  string
}

func ExtractSelectColumns(sql string) []SelectColumn {
	upper := strings.ToUpper(strings.TrimSpace(sql))

	if strings.HasPrefix(upper, "WITH") {
		lastSelect := strings.LastIndex(upper, "SELECT")
		if lastSelect > 0 {
			sql = sql[lastSelect:]
			upper = strings.ToUpper(sql)
		}
	}

	if !strings.HasPrefix(upper, "SELECT") {
		return nil
	}

	selectBody := extractSelectBody(sql)
	if selectBody == "" {
		return nil
	}

	trimmed := strings.TrimSpace(selectBody)
	if trimmed == "*" {
		return []SelectColumn{{IsStar: true, Expression: "*"}}
	}

	parts := splitByCommaRespectParens(trimmed)
	var cols []SelectColumn

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.HasSuffix(part, ".*") {
			cols = append(cols, SelectColumn{
				TableAlias: strings.TrimSuffix(part, ".*"),
				IsStar:     true,
				Expression: part,
			})
			continue
		}

		col := removeAlias(part)

		if isFunctionCall(col) {
			innerCols := extractColumnsFromExpression(col)
			cols = append(cols, innerCols...)
			continue
		}

		if isConstant(col) {
			continue
		}

		sc := parseColumnRef(col)
		sc.Expression = part
		cols = append(cols, sc)
	}

	return cols
}

func extractSelectBody(sql string) string {
	upper := strings.ToUpper(sql)

	start := 6
	rest := strings.TrimSpace(sql[start:])
	upperRest := strings.ToUpper(rest)
	if strings.HasPrefix(upperRest, "DISTINCT ") {
		start += 9 + (len(sql[start:]) - len(rest))
		rest = strings.TrimSpace(sql[start:])
	} else if strings.HasPrefix(upperRest, "ALL ") {
		start += 4 + (len(sql[start:]) - len(rest))
		rest = strings.TrimSpace(sql[start:])
	}

	fromIdx := findTopLevelKeyword(upper[start:], "FROM")
	if fromIdx == -1 {
		return rest
	}

	return strings.TrimSpace(sql[start : start+fromIdx])
}

func findTopLevelKeyword(sql, keyword string) int {
	depth := 0
	upper := strings.ToUpper(sql)
	kwLen := len(keyword)

	for i := 0; i <= len(upper)-kwLen; i++ {
		switch upper[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && upper[i:i+kwLen] == keyword {
				before := i == 0 || !isIdentChar(upper[i-1])
				after := i+kwLen >= len(upper) || !isIdentChar(upper[i+kwLen])
				if before && after {
					return i
				}
			}
		}
	}
	return -1
}

func splitByCommaRespectParens(s string) []string {
	var parts []string
	depth := 0
	start := 0
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
		} else if c == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
		} else if !inSingleQuote && !inDoubleQuote {
			switch c {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			case ',':
				if depth == 0 {
					parts = append(parts, s[start:i])
					start = i + 1
				}
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// selectAliasRE 匹配 SELECT 表达式的显式别名（贪婪前缀取最后一个 AS）。
var selectAliasRE = regexp.MustCompile(`(?is)^(.*)\s+as\s+\S+\s*$`)

func removeAlias(expr string) string {
	trimmed := strings.TrimSpace(expr)
	if m := selectAliasRE.FindStringSubmatch(trimmed); m != nil {
		return strings.TrimSpace(m[1])
	}
	return trimmed
}

func isFunctionCall(expr string) bool {
	trimmed := strings.TrimSpace(expr)
	re := regexp.MustCompile(`(?i)^\w+\s*\(`)
	return re.MatchString(trimmed)
}

func extractColumnsFromExpression(expr string) []SelectColumn {
	var cols []SelectColumn
	re := regexp.MustCompile(`(?i)(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|"[^"]+"|\w+)`)
	matches := re.FindAllString(expr, -1)
	for _, m := range matches {
		sc := parseColumnRef(m)
		if !isSQLKeyword(sc.ColumnName) && sc.ColumnName != "*" {
			cols = append(cols, sc)
		}
	}

	parenStart := strings.Index(expr, "(")
	parenEnd := strings.LastIndex(expr, ")")
	if parenStart >= 0 && parenEnd > parenStart {
		inner := expr[parenStart+1 : parenEnd]
		parts := splitByCommaRespectParens(inner)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "*" || p == "" || isConstant(p) {
				continue
			}
			if !strings.Contains(p, "(") && !strings.Contains(p, ".") {
				cleaned := stripBackticks(p)
				if !isSQLKeyword(cleaned) {
					cols = append(cols, SelectColumn{ColumnName: cleaned, Expression: p})
				}
			}
		}
	}

	return cols
}

func isConstant(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if matched, _ := regexp.MatchString(`^-?\d+(\.\d+)?$`, s); matched {
		return true
	}
	if (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) ||
		(strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) {
		return true
	}
	if strings.EqualFold(s, "NULL") || strings.EqualFold(s, "TRUE") || strings.EqualFold(s, "FALSE") {
		return true
	}
	return false
}

func parseColumnRef(ref string) SelectColumn {
	ref = strings.TrimSpace(ref)

	parts := splitDottedIdentifier(ref)
	switch len(parts) {
	case 3:
		return SelectColumn{TableAlias: parts[1], ColumnName: parts[2]}
	case 2:
		return SelectColumn{TableAlias: parts[0], ColumnName: parts[1]}
	default:
		return SelectColumn{ColumnName: stripBackticks(ref)}
	}
}

func splitDottedIdentifier(s string) []string {
	var parts []string
	current := strings.Builder{}
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c == '`' || c == '"') && !inQuote {
			inQuote = true
			quoteChar = c
			continue
		}
		if inQuote && c == quoteChar {
			inQuote = false
			quoteChar = 0
			continue
		}
		if c == '.' && !inQuote {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(c)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}
