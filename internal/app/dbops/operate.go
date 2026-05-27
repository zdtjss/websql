package dbops

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/database"
	"websql/internal/dialect"
	"websql/internal/logger"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type metaCacheEntry struct {
	columnMap   map[string]string
	primaryKeys []string
	expiresAt   time.Time
}

type metaCache struct {
	mu      sync.RWMutex
	entries map[string]*metaCacheEntry
}

var tableMetaCache = &metaCache{
	entries: make(map[string]*metaCacheEntry, 256),
}

const metaCacheTTL = 5 * time.Minute

func init() {
	go func() {
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
	}()
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

func ListSchema(key string, authorization string) []*conn.Tree {
	schemaName := ""
	dc := conn.GetConn(key, authorization)
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listSchema"])
	logger.PanicErr(err)
	allSchemas := make([]*conn.Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		allSchemas = append(allSchemas, &conn.Tree{Label: schemaName, Type: conn.TREE_NODE_TYPE_SCHEMA, Data: map[string]any{"dbType": dc.DriverName()}})
	}
	return filterSchemasByPermission(allSchemas, key, authorization)
}

func filterSchemasByPermission(schemas []*conn.Tree, connId, authorization string) []*conn.Tree {
	userPower := admin.GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		return []*conn.Tree{}
	}
	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return []*conn.Tree{}
	}
	allowedSchemas := make(map[string]bool)
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			return schemas
		case "schema":
			if p.SchemaName != nil {
				allowedSchemas[*p.SchemaName] = true
			}
		case "table":
			if p.SchemaName != nil {
				allowedSchemas[*p.SchemaName] = true
			}
		case "column":
			if p.SchemaName != nil {
				allowedSchemas[*p.SchemaName] = true
			}
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

func ListTable(key string, schema, authorization string) []*conn.Tree {
	tableName, tableType, tableComment := "", "", ""
	dc := conn.GetConn(key, authorization)

	admin.CheckSchemaAccess(key, schema, authorization)

	tableName2, columnName, columnComment := "", "", ""
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	logger.PanicErr(err)
	tableColumns := make([]map[string]string, 0)
	for row.Next() {
		columnComment = ""
		row.Scan(&tableName2, &columnName, &columnComment)
		tableColumns = append(tableColumns, map[string]string{"tableName": tableName2, "columnName": columnName, "columnComment": columnComment})
	}

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

	row, err = dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listTable"], schema)
	logger.PrintErr(err)
	allTables := make([]*conn.Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableType, &tableComment)
		treeNode := &conn.Tree{Label: tableName, Data: map[string]any{"text": tableComment, "columns": grouped[tableName]}, Type: conn.TREE_NODE_TYPE_TABLE}
		if dc.DriverName() == "mysql" || dc.DriverName() == "mariadb" {
			switch tableType {
			case "VIEW":
				treeNode.Type = "view"
			case "BASE TABLE":
				treeNode.Type = "table"
			}
		} else if dc.DriverName() == "oracle" {
			treeNode.Type = strings.ToLower(tableType)
		}
		allTables = append(allTables, treeNode)
	}
	filteredTables := filterTreeTablesByPermission(allTables, key, schema, authorization)

	return filteredTables
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
	allowedTables := make(map[string]bool)
	hasConnLevel := false
	hasSchemaLevel := false
	hasTableOrColumnLevel := false
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			hasConnLevel = true
		case "schema":
			if p.SchemaName != nil && *p.SchemaName == schema {
				hasSchemaLevel = true
			}
		case "table":
			if p.SchemaName != nil && *p.SchemaName == schema && p.TableName != nil {
				allowedTables[*p.TableName] = true
				hasTableOrColumnLevel = true
			}
		case "column":
			if p.SchemaName != nil && *p.SchemaName == schema && p.TableName != nil {
				allowedTables[*p.TableName] = true
				hasTableOrColumnLevel = true
			}
		}
	}
	if hasConnLevel && !hasTableOrColumnLevel {
		return tables
	}
	if hasSchemaLevel && !hasTableOrColumnLevel {
		return tables
	}
	filtered := make([]*conn.Tree, 0)
	for _, t := range tables {
		if allowedTables[t.Label] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func ListColumns(connId string, table, schema, authorization string) []*conn.Tree {
	columnName, columnComment := "", ""
	dc := conn.GetConn(connId, authorization)
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listColumns"], table)
	logger.PrintErr(err)
	tree := make([]*conn.Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &conn.Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: conn.TREE_NODE_TYPE_COLUMN})
	}

	if schema == "" {
		schema = getCurrentSchema(dc)
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

func getCurrentSchema(dc *sqlx.DB) string {
	var schema string
	switch dc.DriverName() {
	case "mysql", "mariadb":
		row := dc.QueryRow("SELECT DATABASE()")
		row.Scan(&schema)
	case "oracle":
		row := dc.QueryRow("SELECT USER FROM DUAL")
		row.Scan(&schema)
		schema = strings.ToUpper(schema)
	case "sqlite":
		schema = "main"
	default:
		schema = ""
	}
	return schema
}

func ListAllColumns(key string, schema, authorization string) []*conn.Tree {
	columnName, columnComment := "", ""
	dc := conn.GetConn(key, authorization)
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	logger.PanicErr(err)
	tree := make([]*conn.Tree, 0)
	for row.Next() {
		columnComment = ""
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &conn.Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: conn.TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func ListTableColumns(connIdParam string, tableName, schema, authorization string) []map[string]any {
	dc := conn.GetConn(connIdParam, authorization)
	rows, err := dc.Queryx(dialect.SQL_DIALECT[dc.DriverName()]["listTableColumns"], schema, tableName)
	logger.PanicErr(err)
	result, err := database.GetResultRows(dc.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取列信息失败", err)
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

func ListTableFat(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	connIdVal := c.Query("connId")
	schema := c.Query("schema")

	if schema == "" && connIdVal != "" {
		dc := conn.GetConn(connIdVal, authorization)
		switch dc.DriverName() {
		case "mysql", "mariadb":
			dc.Get(&schema, "SELECT DATABASE()")
		case "oracle":
			dc.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		case "sqlite":
			schema = "main"
		}
	}

	tables := QueryTableInfo(connIdVal, schema, authorization)
	userPower := admin.GetUserPower(authorization)
	filteredTables := conn.FilterTablesByPermission(tables, connIdVal, schema, userPower)
	c.JSON(200, filteredTables)
}

func QueryTableInfo(key string, schema, authorization string) []*conn.Table {
	tables := make([]*conn.Table, 0)
	dc := conn.GetConn(key, authorization)

	if dc == nil {
		panic(errors.New("数据库连接失败"))
	}

	var querySQL string
	if schema != "" {
		querySQL = dialect.SQL_DIALECT[dc.DriverName()]["listTable"]
	} else {
		switch dc.DriverName() {
		case "mysql", "mariadb":
			querySQL = "SELECT table_name, table_type, table_comment FROM information_schema.tables WHERE table_schema = DATABASE()"
		case "oracle":
			querySQL = "SELECT table_name, table_type, NULL FROM user_tab_comments UNION ALL SELECT view_name, 'VIEW', NULL FROM user_views ORDER BY table_name"
		case "sqlite":
			querySQL = "SELECT name, type, NULL FROM sqlite_master WHERE type IN ('table','view')"
		default:
			querySQL = dialect.SQL_DIALECT[dc.DriverName()]["listTable"]
		}
	}

	stmt, err := dc.Prepare(querySQL)
	logger.PanicErr(err)

	var rs *sql.Rows
	var err2 error
	if schema != "" {
		if dc.DriverName() == "sqlite" {
			rs, err2 = stmt.Query()
		} else {
			rs, err2 = stmt.Query(schema)
		}
	} else {
		rs, err2 = stmt.Query()
	}
	logger.PanicErr(err2)

	var tableName, tableType, tableComment string
	for rs.Next() {
		tableComment = ""
		rs.Scan(&tableName, &tableType, &tableComment)
		table := &conn.Table{Name: tableName, Comment: tableComment}
		tables = append(tables, table)
	}
	return tables
}

func ColumnMap(table, schema string, conn *sqlx.DB) map[string]string {
	columnMap := make(map[string]string)
	stmt, err := conn.Prepare(dialect.SQL_DIALECT[conn.DriverName()]["ColumnMap"])
	logger.PrintErr(err)
	table = strings.TrimPrefix(table, schema+".")
	if conn.DriverName() == "oracle" {
		schema = strings.ToUpper(schema)
		table = strings.ToUpper(table)
	}
	rs, err2 := stmt.Query(table, schema)
	logger.PrintErr(err2)
	var name, comment string
	for rs.Next() {
		comment = ""
		rs.Scan(&name, &comment)
		columnMap[name] = comment
	}
	return columnMap
}

func ColumnMapFiltered(table, schema, connId, authorization string, dc *sqlx.DB) map[string]string {
	cacheKey := metaCacheKey(connId, schema, table)
	if cached, ok := tableMetaCache.getColumnMap(cacheKey); ok {
		access := permission.GetTableAccessDowngraded(connId, schema, table, authorization)
		if access.Level == permission.AccessNone {
			return map[string]string{}
		}
		return cached
	}

	fullMap := ColumnMap(table, schema, dc)

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

func QueryPrimaryKey(schema, table string, tx *sqlx.Tx) ([]string, error) {
	primaryKeys := make([]string, 0)
	stmt, err := tx.Prepare(dialect.SQL_DIALECT[tx.DriverName()]["QueryPrimaryKey"])
	logger.PrintErr(err)
	rs, err2 := stmt.Query(schema, table)
	logger.PrintErr(err2)
	var name string
	for rs.Next() {
		rs.Scan(&name)
		primaryKeys = append(primaryKeys, name)
	}
	if len(primaryKeys) == 0 {
		msg := fmt.Sprintf("%s 没有主键", table)
		log.Println(msg)
		tx.Rollback()
		return nil, errors.New(msg)
	}
	return primaryKeys, nil
}

func QueryPrimaryKeyCached(connId, schema, table string, dc *sqlx.DB) []string {
	cacheKey := metaCacheKey(connId, schema, table)
	if cached, ok := tableMetaCache.getPrimaryKeys(cacheKey); ok {
		return cached
	}

	primaryKeys := make([]string, 0)
	stmt, err := dc.Prepare(dialect.SQL_DIALECT[dc.DriverName()]["QueryPrimaryKey"])
	if err != nil {
		return primaryKeys
	}
	schemaVal := schema
	tableVal := table
	if dc.DriverName() == "oracle" {
		schemaVal = strings.ToUpper(schema)
		tableVal = strings.ToUpper(table)
	}
	rs, err2 := stmt.Query(schemaVal, tableVal)
	if err2 != nil {
		return primaryKeys
	}
	var name string
	for rs.Next() {
		rs.Scan(&name)
		primaryKeys = append(primaryKeys, name)
	}

	var cachedColMap map[string]string
	if entry, ok := tableMetaCache.getColumnMap(cacheKey); ok {
		cachedColMap = entry
	}
	tableMetaCache.set(cacheKey, cachedColMap, primaryKeys)

	return primaryKeys
}

func TableOptions(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	param := conn.ColumnsQuery{}
	c.ShouldBindJSON(&param)
	permission.CheckTablePermission(param.ConnId, param.Schema, param.TableName, authorization)
	dc := conn.GetConn(param.ConnId, authorization)
	d := dialect.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := d["tableOptions"]
	if !ok {
		jsonutil.WriteJson(c.Writer, map[string]any{})
		return
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, param.TableName)
	} else {
		args = append(args, param.Schema, param.TableName)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	logger.PanicErr(err)
	data, err := database.GetResultRows(dc.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取表详情失败", err)
		c.JSON(200, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	if len(data) > 0 {
		jsonutil.WriteJson(c.Writer, data[0])
	} else {
		jsonutil.WriteJson(c.Writer, map[string]any{})
	}
}

func TableStatistics(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	param := conn.ColumnsQuery{}
	c.ShouldBindJSON(&param)
	permission.CheckTablePermission(param.ConnId, param.Schema, param.TableName, authorization)
	dc := conn.GetConn(param.ConnId, authorization)
	d := dialect.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := d["tableStatistics"]
	if !ok {
		jsonutil.WriteJson(c.Writer, map[string]any{})
		return
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, param.TableName)
	} else {
		args = append(args, param.Schema, param.TableName)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	logger.PanicErr(err)
	data, err := database.GetResultRows(dc.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取表统计信息失败", err)
		c.JSON(200, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	if len(data) > 0 {
		jsonutil.WriteJson(c.Writer, data[0])
	} else {
		jsonutil.WriteJson(c.Writer, map[string]any{})
	}
}

func ListIndexes(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	param := conn.ColumnsQuery{}
	c.ShouldBindJSON(&param)
	permission.CheckTablePermission(param.ConnId, param.Schema, param.TableName, authorization)
	dc := conn.GetConn(param.ConnId, authorization)
	d := dialect.SQL_DIALECT[dc.DriverName()]
	sqlStr, ok := d["listIndexes"]
	if !ok {
		jsonutil.WriteJson(c.Writer, []map[string]any{})
		return
	}
	args := []any{}
	if dc.DriverName() == "oracle" {
		args = append(args, param.TableName)
	} else {
		args = append(args, param.Schema, param.TableName)
	}
	rows, err := dc.Queryx(sqlStr, args...)
	logger.PanicErr(err)
	data, err := database.GetResultRows(dc.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取索引信息失败", err)
		c.JSON(200, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	jsonutil.WriteJson(c.Writer, data)
}
