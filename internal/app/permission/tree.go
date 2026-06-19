package permission

import (
	"bytes"
	"log"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/database"
	"websql/internal/dialect"
	"websql/internal/logger"
)

func FilterConnsWithPermission(parentId string, userPower *admin.UserPower) []*conn.Tree {
	if userPower == nil || len(userPower.Power) == 0 {
		return []*conn.Tree{}
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
	appendPmsnStrict(&sql, "id", &param, userPower)
	cfgList := []conn.ConnCfg{}
	err := database.Mngtdb.Select(&cfgList, sql.String(), param...)
	if err != nil {
		log.Printf("[FilterConnsWithPermission] 查询连接列表失败: %v", err)
		return nil
	}

	tree := make([]*conn.Tree, 0, len(cfgList))
	for _, cfg := range cfgList {
		label := ""
		if cfg.Name != nil {
			label = *cfg.Name
		}
		tree = append(tree, &conn.Tree{Label: label, Id: cfg.Id, Type: conn.TREE_NODE_TYPE_CONN})
	}
	return tree
}

func appendPmsnStrict(sql *bytes.Buffer, col string, param *[]any, userPower *admin.UserPower) {
	powerCount := len(userPower.Power)
	sql.WriteString(" and ")
	if powerCount == 0 {
		sql.WriteString(" 1 = 2 ")
		return
	}
	sql.WriteString(col)
	sql.WriteString(" in ( ")
	sql.WriteString(strings.Repeat("?,", powerCount)[0 : powerCount*2-1])
	sql.WriteString(") ")
	for i := 0; i < powerCount; i++ {
		*param = append(*param, userPower.Power[i])
	}
}

func FilterSchemasWithPermission(connId, authorization string) []*conn.Tree {
	schemaName := ""
	dc := conn.GetConn(connId, authorization)
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listSchema"])
	if err != nil {
		log.Printf("[FilterSchemasWithPermission] 查询schema列表失败: %v", err)
		return nil
	}
	allSchemas := make([]*conn.Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		allSchemas = append(allSchemas, &conn.Tree{Label: schemaName, Type: conn.TREE_NODE_TYPE_SCHEMA, Data: map[string]any{"dbType": dc.DriverName()}})
	}

	userPower := admin.GetUserPower(authorization)
	if userPower == nil {
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
			return allSchemas
		}
		for schema := range r.BySchema {
			allowedSchemas[schema] = true
		}
	}

	filtered := make([]*conn.Tree, 0, len(allSchemas))
	for _, schema := range allSchemas {
		if allowedSchemas[schema.Label] {
			filtered = append(filtered, schema)
		}
	}
	return filtered
}

func FilterTablesWithPermission(key string, schema, authorization string) []*conn.Tree {
	tableName, tableType, tableComment := "", "", ""
	dc := conn.GetConn(key, authorization)

	tableName2, columnName, columnComment := "", "", ""
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schema)
	if err != nil {
		log.Printf("[FilterTablesWithPermission] 查询列信息失败: %v", err)
		return nil
	}
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

	userPower := admin.GetUserPower(authorization)
	if userPower == nil || len(userPower.Power) == 0 {
		return []*conn.Tree{}
	}

	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return []*conn.Tree{}
	}

	byRole := admin.GroupPowerDetailsByRole(powerDetails, key)

	allowedTables := make(map[string]bool)
	for _, roleDetails := range byRole {
		r := admin.ResolveRolePermissions(roleDetails)
		if r.CanAccessAllTablesInSchema(schema) {
			return allTables
		}
		sp := r.BySchema[schema]
		if sp != nil {
			for tableName := range sp.ByTable {
				allowedTables[tableName] = true
			}
		}
	}

	filtered := make([]*conn.Tree, 0, len(allTables))
	for _, table := range allTables {
		if allowedTables[table.Label] {
			filtered = append(filtered, table)
		}
	}

	return filtered
}

func FilterDirTreeWithPermission(parentId string, userPower *admin.UserPower) []*conn.Tree {

	allDirs := findByParent(parentId, userPower)
	if len(allDirs) == 0 {
		return allDirs
	}

	if userPower == nil || len(userPower.Power) == 0 {
		return []*conn.Tree{}
	}

	connParam := []any{}
	connSQL := bytes.Buffer{}
	connSQL.WriteString("select id, parent_id from t_conn where 1 = 1 ")
	appendPmsnStrict(&connSQL, "id", &connParam, userPower)

	type connParent struct {
		Id       string `db:"id"`
		ParentId string `db:"parent_id"`
	}
	connList := []connParent{}
	err := database.Mngtdb.Select(&connList, connSQL.String(), connParam...)
	if err != nil {
		log.Printf("[FilterDirTreeWithPermission] 查询连接列表失败: %v", err)
		return nil
	}

	dirsWithConn := make(map[string]bool)
	for _, c := range connList {
		if c.ParentId != "" {
			dirsWithConn[c.ParentId] = true
		}
	}

	allTreeNodes := []*DirTree{}
	database.Mngtdb.Select(&allTreeNodes, "select * from t_tree")
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

	filtered := make([]*conn.Tree, 0, len(allDirs))
	for _, dir := range allDirs {
		if dirsWithConn[dir.Id] {
			filtered = append(filtered, dir)
		}
	}
	return filtered
}

type DirTree struct {
	Id       string     `json:"id" db:"id"`
	Label    string     `json:"label" db:"label"`
	Parent   string     `json:"parent" db:"parent"`
	Children []*DirTree `json:"children"`
}

func findByParent(parentId string, userPower *admin.UserPower) []*conn.Tree {
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_tree where ")
	if parentId == "" {
		sql.WriteString(" (parent is null or parent = '')")
	} else {
		param = append(param, parentId)
		sql.WriteString(" parent = ?")
	}
	treeList := []*DirTree{}
	err := database.Mngtdb.Select(&treeList, sql.String(), param...)
	if err != nil {
		log.Printf("[findByParent] 查询目录树失败: %v", err)
		return nil
	}
	tree := make([]*conn.Tree, len(treeList))
	for i, cfg := range treeList {
		tree[i] = &conn.Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: conn.TREE_NODE_TYPE_DIR}
	}
	return tree
}
