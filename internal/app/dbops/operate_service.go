package dbops

import (
	"errors"
	"strings"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/database"
	"websql/internal/pkg/safego"

	"github.com/jmoiron/sqlx"
)

// OperateService 封装数据库操作相关的业务逻辑：连接获取、权限过滤、缓存管理等
type OperateService struct {
	repo OperateRepo
}

// NewOperateService 创建 OperateService 实例
func NewOperateService(repo OperateRepo) *OperateService {
	return &OperateService{repo: repo}
}

// 默认实例，保持对包级别函数的向后兼容
var (
	defaultOperateRepo    = NewOperateRepo(database.Mngtdb)
	defaultOperateService = NewOperateService(defaultOperateRepo)
)

// ===== 表元数据缓存 =====

var tableMetaCache = &metaCache{
	entries: make(map[string]*metaCacheEntry, 256),
}

const metaCacheTTL = 5 * time.Minute

func init() {
	safego.GoWithName("dbops-metacache-cleanup", func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			tableMetaCache.mu.Lock()
			now := time.Now()
			for k, v := range tableMetaCache.entries {
				if now.After(v.expiresAt) {
					delete(tableMetaCache.entries, k)
				}
			}
			tableMetaCache.mu.Unlock()
		}
	})
}

func metaCacheKey(connId, schema, table string) string {
	return connId + ":" + schema + ":" + table
}

func (c *metaCache) getColumnMap(key string) (map[string]string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) || entry.columnMap == nil {
		return nil, false
	}
	return entry.columnMap, true
}

func (c *metaCache) getPrimaryKeys(key string) ([]string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) || entry.primaryKeys == nil {
		return nil, false
	}
	return entry.primaryKeys, true
}

func (c *metaCache) set(key string, columnMap map[string]string, primaryKeys []string) {
	c.mu.Lock()
	if existing, ok := c.entries[key]; ok {
		if columnMap == nil {
			columnMap = existing.columnMap
		}
		if primaryKeys == nil {
			primaryKeys = existing.primaryKeys
		}
	}
	c.entries[key] = &metaCacheEntry{
		columnMap:   columnMap,
		primaryKeys: primaryKeys,
		expiresAt:   time.Now().Add(metaCacheTTL),
	}
	c.mu.Unlock()
}

// ===== 业务逻辑方法 =====

// ListSchema 列出数据库下的所有 schema（按权限过滤）
func (s *OperateService) ListSchema(key string, authorization string) []*conn.Tree {
	dc := conn.GetConn(key, authorization)
	if dc == nil {
		return []*conn.Tree{}
	}
	schemaNames := s.repo.ListSchemas(dc)
	allSchemas := make([]*conn.Tree, 0)
	for _, schemaName := range schemaNames {
		allSchemas = append(allSchemas, &conn.Tree{Label: schemaName, Type: conn.TREE_NODE_TYPE_SCHEMA, Data: map[string]any{"dbType": dc.DriverName()}})
	}
	return filterSchemasByPermission(allSchemas, key, authorization)
}

// ListTable 列出 schema 下的所有表（含列信息，按权限过滤）
func (s *OperateService) ListTable(key string, schema, authorization string) []*conn.Tree {
	dc := conn.GetConn(key, authorization)
	if dc == nil {
		return []*conn.Tree{}
	}

	admin.CheckSchemaAccess(key, schema, authorization)

	tableColumns := s.repo.ListAllColumnsForTable(dc, schema)

	grouped := make(map[string][]conn.Column)
	for _, col := range tableColumns {
		tn := col["tableName"]
		if grouped[tn] == nil {
			grouped[tn] = make([]conn.Column, 0)
		}
		fieldInfo := conn.Column{
			Name:    col["columnName"],
			Comment: col["columnComment"],
		}
		grouped[tn] = append(grouped[tn], fieldInfo)
	}

	tableRows := s.repo.ListTables(dc, schema)
	allTables := make([]*conn.Tree, 0)
	for _, t := range tableRows {
		treeNode := &conn.Tree{Label: t.Name, Data: map[string]any{"text": t.Comment, "columns": grouped[t.Name]}, Type: conn.TREE_NODE_TYPE_TABLE}
		if dc.DriverName() == "mysql" || dc.DriverName() == "mariadb" {
			switch t.Type {
			case "VIEW":
				treeNode.Type = "view"
			case "BASE TABLE":
				treeNode.Type = "table"
			}
		} else if dc.DriverName() == "oracle" {
			treeNode.Type = strings.ToLower(t.Type)
		}
		allTables = append(allTables, treeNode)
	}
	filteredTables := filterTreeTablesByPermission(allTables, key, schema, authorization)
	return filteredTables
}

// ListColumns 列出表的所有列（按权限过滤）
func (s *OperateService) ListColumns(connId string, table, schema, authorization string) []*conn.Tree {
	dc := conn.GetConn(connId, authorization)
	if dc == nil {
		return []*conn.Tree{}
	}
	columns := s.repo.ListColumnsForTable(dc, table)
	tree := make([]*conn.Tree, 0)
	for _, c := range columns {
		tree = append(tree, &conn.Tree{Label: c.Name, Data: map[string]any{"text": c.Comment}, Type: conn.TREE_NODE_TYPE_COLUMN})
	}

	if schema == "" {
		schema = s.repo.GetCurrentSchema(dc)
	}
	access := permission.GetTableAccessDowngraded(connId, schema, table, authorization)
	if access.Level == permission.AccessFull {
		return tree
	}
	if access.Level == permission.AccessNone {
		return []*conn.Tree{}
	}
	return tree
}

// ListAllColumns 列出 schema 下的所有列
func (s *OperateService) ListAllColumns(key string, schema, authorization string) []*conn.Tree {
	dc := conn.GetConn(key, authorization)
	if dc == nil {
		return []*conn.Tree{}
	}
	columns := s.repo.ListAllColumnsRaw(dc, schema)
	tree := make([]*conn.Tree, 0)
	for _, c := range columns {
		tree = append(tree, &conn.Tree{Label: c.Name, Data: map[string]any{"text": c.Comment}, Type: conn.TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

// ListTableColumns 列出表的列详情（按权限过滤）
func (s *OperateService) ListTableColumns(connIdParam string, tableName, schema, authorization string) []map[string]any {
	dc := conn.GetConn(connIdParam, authorization)
	if dc == nil {
		return []map[string]any{}
	}
	result, err := s.repo.ListTableColumnsRaw(dc, schema, tableName)
	if err != nil {
		return []map[string]any{}
	}

	access := permission.GetTableAccessDowngraded(connIdParam, schema, tableName, authorization)
	if access.Level == permission.AccessFull {
		return result
	}
	if access.Level == permission.AccessNone {
		return []map[string]any{}
	}
	return result
}

// QueryTableInfo 查询表信息列表
func (s *OperateService) QueryTableInfo(key string, schema, authorization string) []*conn.Table {
	dc := conn.GetConn(key, authorization)
	if dc == nil {
		return []*conn.Table{}
	}
	return s.repo.QueryTables(dc, schema)
}

// ColumnMap 查询表的列名与注释映射
func (s *OperateService) ColumnMap(table, schema string, dc *sqlx.DB) map[string]string {
	return s.repo.ColumnMap(dc, table, schema)
}

// ColumnMapFiltered 查询表的列名与注释映射（带缓存与权限过滤）
func (s *OperateService) ColumnMapFiltered(table, schema, connId, authorization string, dc *sqlx.DB) map[string]string {
	cacheKey := metaCacheKey(connId, schema, table)
	if cached, ok := tableMetaCache.getColumnMap(cacheKey); ok {
		access := permission.GetTableAccessDowngraded(connId, schema, table, authorization)
		if access.Level == permission.AccessNone {
			return map[string]string{}
		}
		return cached
	}

	fullMap := s.repo.ColumnMap(dc, table, schema)

	var pks []string
	if cachedPks, ok := tableMetaCache.getPrimaryKeys(cacheKey); ok {
		pks = cachedPks
	}
	tableMetaCache.set(cacheKey, fullMap, pks)

	access := permission.GetTableAccessDowngraded(connId, schema, table, authorization)
	if access.Level == permission.AccessNone {
		return map[string]string{}
	}
	return fullMap
}

// QueryPrimaryKey 查询表的主键（事务版本）
func (s *OperateService) QueryPrimaryKey(schema, table string, tx *sqlx.Tx) ([]string, error) {
	return s.repo.QueryPrimaryKeyWithTx(tx, schema, table)
}

// QueryPrimaryKeyCached 查询表的主键（带缓存）
func (s *OperateService) QueryPrimaryKeyCached(connId, schema, table string, dc *sqlx.DB) []string {
	cacheKey := metaCacheKey(connId, schema, table)
	if cached, ok := tableMetaCache.getPrimaryKeys(cacheKey); ok {
		return cached
	}

	primaryKeys := s.repo.QueryPrimaryKey(dc, schema, table)

	var cachedColMap map[string]string
	if entry, ok := tableMetaCache.getColumnMap(cacheKey); ok {
		cachedColMap = entry
	}
	tableMetaCache.set(cacheKey, cachedColMap, primaryKeys)

	return primaryKeys
}

// ListTableFat 列出表信息（含 schema 自动检测与权限过滤）
func (s *OperateService) ListTableFat(connId, schema, authorization string) []*conn.Table {
	if schema == "" && connId != "" {
		dc := conn.GetConn(connId, authorization)
		if dc != nil {
			schema = s.repo.GetCurrentSchemaForFat(dc)
		}
	}
	tables := s.QueryTableInfo(connId, schema, authorization)
	userPower := admin.GetUserPower(authorization)
	return conn.FilterTablesByPermission(tables, connId, schema, userPower)
}

// GetTableOptions 获取表选项信息
func (s *OperateService) GetTableOptions(connId, schema, table, authorization string) (map[string]any, error) {
	permission.CheckTablePermission(connId, schema, table, authorization)
	dc := conn.GetConn(connId, authorization)
	if dc == nil {
		return nil, errors.New("数据库连接失败")
	}
	data, err := s.repo.GetTableOptions(dc, schema, table)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		return data[0], nil
	}
	return map[string]any{}, nil
}

// GetTableStatistics 获取表统计信息
func (s *OperateService) GetTableStatistics(connId, schema, table, authorization string) (map[string]any, error) {
	permission.CheckTablePermission(connId, schema, table, authorization)
	dc := conn.GetConn(connId, authorization)
	if dc == nil {
		return nil, errors.New("数据库连接失败")
	}
	data, err := s.repo.GetTableStatistics(dc, schema, table)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		return data[0], nil
	}
	return map[string]any{}, nil
}

// ListIndexes 列出表的索引信息
func (s *OperateService) ListIndexes(connId, schema, table, authorization string) ([]map[string]any, error) {
	permission.CheckTablePermission(connId, schema, table, authorization)
	dc := conn.GetConn(connId, authorization)
	if dc == nil {
		return nil, errors.New("数据库连接失败")
	}
	data, err := s.repo.ListIndexes(dc, schema, table)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return []map[string]any{}, nil
	}
	return data, nil
}

// ===== 权限过滤辅助函数 =====

func filterSchemasByPermission(schemas []*conn.Tree, connId, authorization string) []*conn.Tree {
	userPower := admin.GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		return []*conn.Tree{}
	}
	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return []*conn.Tree{}
	}
	byRole := admin.GroupPowerDetailsByRole(powerDetails, connId)

	allowedSchemas := make(map[string]bool)
	for _, roleDetails := range byRole {
		r := admin.ResolveRolePermissions(roleDetails)
		if r.HasConnLevel {
			return schemas
		}
		for schema := range r.BySchema {
			allowedSchemas[schema] = true
		}
	}
	filtered := make([]*conn.Tree, 0)
	for _, s := range schemas {
		if allowedSchemas[s.Label] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func filterTreeTablesByPermission(tables []*conn.Tree, connId, schema, authorization string) []*conn.Tree {
	userPower := admin.GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		return []*conn.Tree{}
	}
	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return []*conn.Tree{}
	}
	byRole := admin.GroupPowerDetailsByRole(powerDetails, connId)

	allowedTables := make(map[string]bool)
	for _, roleDetails := range byRole {
		r := admin.ResolveRolePermissions(roleDetails)
		if r.CanAccessAllTablesInSchema(schema) {
			return tables
		}
		sp := r.BySchema[schema]
		if sp != nil {
			for tableName := range sp.ByTable {
				allowedTables[tableName] = true
			}
		}
	}
	filtered := make([]*conn.Tree, 0)
	for _, t := range tables {
		if allowedTables[t.Label] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// ===== 向后兼容的包级别委托函数 =====
// 这些函数被 treehandler / sql 等外部包调用，保持原有签名不变，委托到 defaultOperateService。

func ListSchema(key string, authorization string) []*conn.Tree {
	return defaultOperateService.ListSchema(key, authorization)
}

func ListTable(key string, schema, authorization string) []*conn.Tree {
	return defaultOperateService.ListTable(key, schema, authorization)
}

func ListColumns(connId string, table, schema, authorization string) []*conn.Tree {
	return defaultOperateService.ListColumns(connId, table, schema, authorization)
}

func ListAllColumns(key string, schema, authorization string) []*conn.Tree {
	return defaultOperateService.ListAllColumns(key, schema, authorization)
}

func ListTableColumns(connIdParam string, tableName, schema, authorization string) []map[string]any {
	return defaultOperateService.ListTableColumns(connIdParam, tableName, schema, authorization)
}

func QueryTableInfo(key string, schema, authorization string) []*conn.Table {
	return defaultOperateService.QueryTableInfo(key, schema, authorization)
}

func ColumnMap(table, schema string, conn *sqlx.DB) map[string]string {
	return defaultOperateService.ColumnMap(table, schema, conn)
}

func ColumnMapFiltered(table, schema, connId, authorization string, dc *sqlx.DB) map[string]string {
	return defaultOperateService.ColumnMapFiltered(table, schema, connId, authorization, dc)
}

func QueryPrimaryKey(schema, table string, tx *sqlx.Tx) ([]string, error) {
	return defaultOperateService.QueryPrimaryKey(schema, table, tx)
}

func QueryPrimaryKeyCached(connId, schema, table string, dc *sqlx.DB) []string {
	return defaultOperateService.QueryPrimaryKeyCached(connId, schema, table, dc)
}
