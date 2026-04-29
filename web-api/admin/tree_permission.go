package admin

import (
	"bytes"
	"go-web/config"
	"go-web/logutils"
	dbutils "go-web/utils/db"
	"strings"
)

func filterConnsWithPermission(parentId string, userPower *UserPower) []*Tree {
	if !config.Cfg.IsRemote {
		return listConn(parentId, userPower)
	}
	if userPower == nil || len(userPower.Power) == 0 {
		if userPower != nil && userPower.UserId == config.AdminId {
			return listConn(parentId, userPower)
		}
		return []*Tree{}
	}

	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_conn")
	if strings.EqualFold(parentId, "noneParent") {
		sql.WriteString(" where (parent_id = '' or parent_id is null)")
	} else if parentId != "" {
		param = append(param, parentId)
		sql.WriteString(" where parent_id = ?")
	}
	appendPmsn(&sql, "id", &param, userPower)
	cfgList := []ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, sql.String(), param...)
	logutils.PanicErr(err)

	// tree_visible 过滤
	treeVisConn, _, _ := GetUserTreeVisibility(userPower.UserId)
	hasAnyTreeVis := len(treeVisConn) > 0

	tree := make([]*Tree, 0, len(cfgList))
	for _, cfg := range cfgList {
		if hasAnyTreeVis && !treeVisConn[cfg.Id] {
			continue
		}
		label := ""
		if cfg.Name != nil {
			label = *cfg.Name
		}
		tree = append(tree, &Tree{Label: label, Id: cfg.Id, Type: TREE_NODE_TYPE_CONN})
	}
	return tree
}

func filterSchemasWithPermission(connId, authorization string) []*Tree {
	schemaName := ""
	dc := GetConn(connId, authorization)
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listSchema"])
	logutils.PanicErr(err)
	allSchemas := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		allSchemas = append(allSchemas, &Tree{Label: schemaName, Type: TREE_NODE_TYPE_SCHEMA, Data: map[string]any{"dbType": dc.DriverName()}})
	}

	if !config.Cfg.IsRemote {
		return allSchemas
	}

	userPower := GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		if userPower != nil && userPower.UserId == config.AdminId {
			return allSchemas
		}
		return []*Tree{}
	}

	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		if userPower.UserId == config.AdminId {
			return allSchemas
		}
		return []*Tree{}
	}

	allowedSchemas := make(map[string]bool)
	hasConnLevel := false
	hasSchemaOrLowerLevel := false
	for _, p := range powerDetails {
		if p.ConnId != connId {
			continue
		}
		switch p.Level {
		case "conn":
			hasConnLevel = true
		case "schema":
			if p.SchemaName != nil {
				allowedSchemas[*p.SchemaName] = true
				hasSchemaOrLowerLevel = true
			}
		case "table", "column":
			if p.SchemaName != nil {
				allowedSchemas[*p.SchemaName] = true
				hasSchemaOrLowerLevel = true
			}
		}
	}

	if hasConnLevel && !hasSchemaOrLowerLevel {
		return filterAllByTreeVis(allSchemas, connId, userPower.UserId)
	}

	// tree_visible 过滤
	_, treeVisSchemas, _ := GetUserTreeVisibility(userPower.UserId)
	hasAnyTreeVis := len(treeVisSchemas) > 0

	filtered := make([]*Tree, 0, len(allSchemas))
	for _, schema := range allSchemas {
		if allowedSchemas[schema.Label] {
			if hasAnyTreeVis && !treeVisSchemas[connId+"::"+schema.Label] {
				continue
			}
			filtered = append(filtered, schema)
		}
	}
	return filtered
}

func filterTablesWithPermission(key string, schema, authorization string) []*Tree {
	tableName, tableType, tableComment := "", "", ""
	dc := GetConn(key, authorization)

	tableName2, columnName, columnComment := "", "", ""
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	logutils.PanicErr(err)
	tableColumns := make([]map[string]string, 0)
	for row.Next() {
		*&columnComment = ""
		row.Scan(&tableName2, &columnName, &columnComment)
		tableColumns = append(tableColumns, map[string]string{"tableName": tableName2, "columnName": columnName, "columnComment": columnComment})
	}

	grouped := make(map[string][]Column)

	for _, col := range tableColumns {
		tn := col["tableName"]
		if grouped[tn] == nil {
			grouped[tn] = make([]Column, 0)
		}
		fieldInfo := Column{
			Name:    col["columnName"],
			Comment: col["columnComment"],
		}
		grouped[tn] = append(grouped[tn], fieldInfo)
	}

	row, err = dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listTable"], schema)
	logutils.PrintErr(err)
	allTables := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableType, &tableComment)
		treeNode := &Tree{Label: tableName, Data: map[string]any{"text": tableComment, "columns": grouped[tableName]}, Type: TREE_NODE_TYPE_TABLE}
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

	if !config.Cfg.IsRemote {
		return allTables
	}

	userPower := GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		if userPower != nil && userPower.UserId == config.AdminId {
			return allTables
		}
		return []*Tree{}
	}

	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		if userPower.UserId == config.AdminId {
			return allTables
		}
		return []*Tree{}
	}

	allowedTables := make(map[string]bool)
	hasConnLevel := false
	hasSchemaLevel := false
	hasTableOrColumnLevel := false
	for _, p := range powerDetails {
		if p.ConnId != key {
			continue
		}
		switch p.Level {
		case "conn":
			hasConnLevel = true
		case "schema":
			if p.SchemaName != nil && *p.SchemaName == schema {
				hasSchemaLevel = true
			}
		case "table", "column":
			if p.SchemaName != nil && *p.SchemaName == schema && p.TableName != nil {
				allowedTables[*p.TableName] = true
				hasTableOrColumnLevel = true
			}
		}
	}

	if hasConnLevel && !hasTableOrColumnLevel {
		return filterTablesByTreeVis(allTables, key, schema, userPower.UserId)
	}
	if hasSchemaLevel && !hasTableOrColumnLevel {
		return filterTablesByTreeVis(allTables, key, schema, userPower.UserId)
	}

	// tree_visible 过滤
	_, treeVisSchemas, treeVisTables := GetUserTreeVisibility(userPower.UserId)
	hasAnyTreeVis := len(treeVisSchemas) > 0 || len(treeVisTables) > 0

	filtered := make([]*Tree, 0, len(allTables))
	for _, table := range allTables {
		if allowedTables[table.Label] {
			if hasAnyTreeVis && !treeVisTables[key+"::"+schema+"::"+table.Label] {
				continue
			}
			filtered = append(filtered, table)
		}
	}

	// 列级权限过滤：过滤表数据中的列信息
	for _, t := range filtered {
		if cols, ok := t.Data["columns"].([]Column); ok {
			access := GetTableColumnAccess(key, schema, t.Label, authorization)
			if access.Level == AccessColumn {
				filteredCols := make([]Column, 0, len(cols))
				for _, col := range cols {
					if access.AllowedColumns[col.Name] {
						filteredCols = append(filteredCols, col)
					}
				}
				t.Data["columns"] = filteredCols
			} else if access.Level == AccessNone {
				t.Data["columns"] = []Column{}
			}
		}
	}

	return filtered
}

func filterDirTreeWithPermission(parentId string, userPower *UserPower) []*Tree {
	if !config.Cfg.IsRemote {
		return findByParent(parentId, userPower)
	}

	allDirs := findByParent(parentId, userPower)
	if len(allDirs) == 0 {
		return allDirs
	}

	if userPower == nil || len(userPower.Power) == 0 {
		if userPower != nil && userPower.UserId == config.AdminId {
			return allDirs
		}
		return []*Tree{}
	}

	connParam := []any{}
	connSQL := bytes.Buffer{}
	connSQL.WriteString("select id, parent_id from t_conn where 1 = 1 ")
	appendPmsn(&connSQL, "id", &connParam, userPower)

	type connParent struct {
		Id       string `db:"id"`
		ParentId string `db:"parent_id"`
	}
	connList := []connParent{}
	err := config.Mngtdb.Select(&connList, connSQL.String(), connParam...)
	logutils.PanicErr(err)

	// tree_visible 过滤：只保留有树可见性的连接
	treeVisConn, _, _ := GetUserTreeVisibility(userPower.UserId)
	hasAnyTreeVis := len(treeVisConn) > 0

	dirsWithConn := make(map[string]bool)
	for _, conn := range connList {
		if hasAnyTreeVis && !treeVisConn[conn.Id] {
			continue
		}
		if conn.ParentId != "" {
			dirsWithConn[conn.ParentId] = true
		}
	}

	allTreeNodes := []*DirTree{}
	config.Mngtdb.Select(&allTreeNodes, "select * from t_tree")
	parentMap := make(map[string]string)
	for _, node := range allTreeNodes {
		parentMap[node.Id] = node.Parent
	}
	toPropagate := make([]string, 0, len(dirsWithConn))
	for dirId := range dirsWithConn {
		toPropagate = append(toPropagate, dirId)
	}
	for len(toPropagate) > 0 {
		var next []string
		for _, dirId := range toPropagate {
			if pid, ok := parentMap[dirId]; ok && pid != "" && !dirsWithConn[pid] {
				dirsWithConn[pid] = true
				next = append(next, pid)
			}
		}
		toPropagate = next
	}

	filtered := make([]*Tree, 0, len(allDirs))
	for _, dir := range allDirs {
		if dirsWithConn[dir.Id] {
			filtered = append(filtered, dir)
		}
	}
	return filtered
}

// filterAllByTreeVis 对全部资源的列表按 tree_visible 过滤
func filterAllByTreeVis(allItems []*Tree, connId string, userId string) []*Tree {
	conns, schemas, _ := GetUserTreeVisibility(userId)
	hasAnyTreeVis := len(conns) > 0 || len(schemas) > 0
	if !hasAnyTreeVis {
		return allItems
	}

	filtered := make([]*Tree, 0, len(allItems))
	for _, item := range allItems {
		key := connId + "::" + item.Label
		if schemas[key] {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		filtered = allItems
	}
	return filtered
}

// filterTablesByTreeVis 对全部表的列表按 tree_visible 过滤
func filterTablesByTreeVis(allTables []*Tree, connId, schema, userId string) []*Tree {
	_, _, tables := GetUserTreeVisibility(userId)
	hasAnyTreeVis := false
	for range tables {
		hasAnyTreeVis = true
		break
	}
	if !hasAnyTreeVis {
		return allTables
	}

	filtered := make([]*Tree, 0, len(allTables))
	for _, table := range allTables {
		key := connId + "::" + schema + "::" + table.Label
		if tables[key] {
			filtered = append(filtered, table)
		}
	}
	if len(filtered) == 0 {
		filtered = allTables
	}
	return filtered
}
