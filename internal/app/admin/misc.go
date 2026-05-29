package admin

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/dialect"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type connCfgLocal struct {
	Id         string  `json:"id"`
	DbType     string  `json:"dbType" db:"db_type"`
	ParentId   string  `json:"parentId" db:"parent_id"`
	ParentName *string `json:"parentName" db:"parent_name"`
	Name       *string `json:"name"`
	Pwd        *string `json:"pwd"`
	User       *string `json:"user"`
	Url        *string `json:"url"`
	DbSchema   *string `json:"dbSchema" db:"db_schema"`
	DbVersion  *string `json:"dbVersion" db:"db_version"`
	Charset    *string `json:"charset" db:"charset"`
}

type PermissionNode struct {
	Id       string            `json:"id"`
	Label    string            `json:"label"`
	Type     string            `json:"type"`
	Level    string            `json:"level"`
	ParentId string            `json:"parentId,omitempty"`
	Checked  bool              `json:"checked,omitempty"`
	Data     map[string]any    `json:"data,omitempty"`
	Children []*PermissionNode `json:"children"`
}

func ShowBackupData(c *gin.Context) {
	CheckAdminPower(c)
	backupId := c.Query("backupId")
	stmt, err := database.Mngtdb.Preparex("select data from t_history where id = ?")
	logger.PrintErr(err)
	rowsx, err2 := stmt.Query(backupId)
	logger.PrintErr(err2)
	var backupData any
	if rowsx.Next() {
		rowsx.Scan(&backupData)
	}
	jsonutil.WriteJson(c.Writer, backupData)
}

func ListBackupData(c *gin.Context) {
	CheckAdminPower(c)
	user := GetUser(c.GetHeader("Authorization"))
	connId := c.Query("connId")

	var (
		countSQL  string
		querySQL  string
		countArgs []any
		queryArgs []any
	)

	baseWhere := "WHERE conn_id = ?"
	baseCountSQL := "SELECT COUNT(*) FROM t_history a " + baseWhere
	baseQuerySQL := "SELECT a.id, a.exec_sql, a.exec_time, a.operation_type FROM t_history a " + baseWhere +
		" ORDER BY a.exec_time DESC LIMIT ?, ?"

	if user.Id != config.AdminId {
		countSQL = "SELECT COUNT(*) FROM t_history a WHERE a.user = ? AND conn_id = ?"
		querySQL = "SELECT a.id, a.exec_sql, a.exec_time, a.operation_type FROM t_history a WHERE a.user = ? AND conn_id = ? ORDER BY exec_time DESC LIMIT ?,?"
		countArgs = []any{user.LoginName, connId}
	} else {
		countSQL = baseCountSQL
		querySQL = baseQuerySQL
		countArgs = []any{connId}
	}

	stmt, err := database.Mngtdb.Preparex(countSQL)
	logger.PrintErr(err)
	defer stmt.Close()

	var total int
	err = stmt.QueryRow(countArgs...).Scan(&total)
	logger.PrintErr(err)

	if total == 0 {
		jsonutil.WriteJson(c.Writer, map[string]any{"data": []map[string]any{}, "total": 0})
		return
	}

	current, _ := strconv.Atoi(c.DefaultQuery("current", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "12"))
	if current < 1 {
		current = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 12
	}
	offset := (current - 1) * pageSize

	if user.Id != config.AdminId {
		queryArgs = []any{user.LoginName, connId, offset, pageSize}
	} else {
		queryArgs = []any{connId, offset, pageSize}
	}

	stmt2, err := database.Mngtdb.Preparex(querySQL)
	logger.PrintErr(err)
	defer stmt2.Close()

	rows, err := stmt2.Queryx(queryArgs...)
	logger.PrintErr(err)
	defer rows.Close()

	data, err := database.GetResultRows(database.Mngtdb.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("查询操作日志失败", err)
		c.JSON(200, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	jsonutil.WriteJson(c.Writer, map[string]any{"data": data, "total": total})
}

func GetPermissionTree(c *gin.Context) {
	if config.Cfg.IsRemote {
		authorization := c.GetHeader("Authorization")
		user := GetUser(authorization)
		if user == nil || user.Id != config.AdminId {
			logger.PrintErr(errors.New("无权访问"))
		}
	}

	connId := c.Query("connId")
	schemaName := c.Query("schema")
	tableName := c.Query("table")
	level := c.Query("level")
	roleId := c.Query("roleId")
	authorization := c.GetHeader("Authorization")

	if level == "" {
		level = "conn"
	}

	data := []*PermissionNode{}

	switch level {
	case "conn":
		data = getConnTree(roleId)
	case "schema":
		if connId == "" {
			jsonutil.WriteJson(c.Writer, data)
			return
		}
		data = getSchemaTree(connId, authorization, roleId)
	case "table":
		if connId == "" || schemaName == "" {
			jsonutil.WriteJson(c.Writer, data)
			return
		}
		data = getTableTree(connId, schemaName, authorization, roleId)
	case "column":
		if connId == "" || schemaName == "" || tableName == "" {
			jsonutil.WriteJson(c.Writer, data)
			return
		}
		data = getColumnTree(connId, schemaName, tableName, authorization, roleId)
	}

	if data == nil {
		data = []*PermissionNode{}
	}

	jsonutil.WriteJson(c.Writer, data)
}

func getConnTree(roleId string) []*PermissionNode {
	type ConnWithDir struct {
		connCfgLocal
		ParentName *string `db:"parent_name"`
	}

	connList := []*ConnWithDir{}
	err := database.Mngtdb.Select(&connList, "select c.*, t.label as parent_name from t_conn c left join t_tree t on c.parent_id = t.id order by t.label, c.name")
	logger.PrintErr(err)

	var roleConnIds map[string]bool
	if roleId != "" {
		roleConnIds = getRoleConnPermissions(roleId)
	}

	dirMap := make(map[string][]*ConnWithDir)
	var noParentConns []*ConnWithDir

	for _, c := range connList {
		if c.ParentName != nil && *c.ParentName != "" {
			if dirMap[*c.ParentName] == nil {
				dirMap[*c.ParentName] = make([]*ConnWithDir, 0)
			}
			dirMap[*c.ParentName] = append(dirMap[*c.ParentName], c)
		} else {
			noParentConns = append(noParentConns, c)
		}
	}

	nodes := make([]*PermissionNode, 0)

	for dirName, conns := range dirMap {
		dirNode := &PermissionNode{
			Id:       fmt.Sprintf("dir::%s", dirName),
			Label:    dirName,
			Type:     "dir",
			Level:    "dir",
			ParentId: "",
			Checked:  false,
			Data: map[string]any{
				"type": "dir",
			},
			Children: make([]*PermissionNode, 0),
		}

		for _, c := range conns {
			checked := false
			if roleId != "" {
				checked = roleConnIds[c.Id]
			}

			name := ""
			if c.Name != nil {
				name = *c.Name
			}

			dirNode.Children = append(dirNode.Children, &PermissionNode{
				Id:       c.Id,
				Label:    name,
				Type:     "conn",
				Level:    "conn",
				ParentId: dirNode.Id,
				Checked:  checked,
				Data: map[string]any{
					"connId":     c.Id,
					"parentName": c.ParentName,
				},
				Children: nil,
			})
		}

		nodes = append(nodes, dirNode)
	}

	for _, c := range noParentConns {
		checked := false
		if roleId != "" {
			checked = roleConnIds[c.Id]
		}

		name := ""
		if c.Name != nil {
			name = *c.Name
		}

		nodes = append(nodes, &PermissionNode{
			Id:       c.Id,
			Label:    name,
			Type:     "conn",
			Level:    "conn",
			ParentId: c.ParentId,
			Checked:  checked,
			Data: map[string]any{
				"connId":     c.Id,
				"parentName": c.ParentName,
			},
			Children: nil,
		})
	}

	return nodes
}

func getRoleConnPermissions(roleId string) map[string]bool {
	connIds := make(map[string]bool)
	powerList := []*PowerDetail{}
	err := database.Mngtdb.Select(&powerList, "select conn_id from t_power where role_id = ? and power_level = 'conn'", roleId)
	logger.PrintErr(err)
	for _, power := range powerList {
		connIds[power.ConnId] = true
	}
	schemaPowers := []*PowerDetail{}
	err2 := database.Mngtdb.Select(&schemaPowers, "select conn_id from t_power where role_id = ? and power_level in ('schema','table','column')", roleId)
	logger.PrintErr(err2)
	for _, power := range schemaPowers {
		connIds[power.ConnId] = true
	}
	return connIds
}

func getSchemaTree(connId, authorization string, roleId string) []*PermissionNode {
	dc := getConnNoCheckInternal(connId)
	if dc == nil {
		return []*PermissionNode{}
	}

	var roleSchemaMap map[string]bool
	if roleId != "" {
		roleSchemaMap = getRoleSchemaPermissions(roleId, connId)
	}

	schemaName := ""
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listSchema"])
	if err != nil {
		logger.PrintErr(err)
		return []*PermissionNode{}
	}
	defer row.Close()

	nodes := make([]*PermissionNode, 0)
	for row.Next() {
		row.Scan(&schemaName)
		checked := false
		if roleId != "" {
			checked = roleSchemaMap[schemaName]
		}

		nodes = append(nodes, &PermissionNode{
			Id:       connId + "::" + schemaName,
			Label:    schemaName,
			Type:     "schema",
			Level:    "schema",
			ParentId: connId,
			Checked:  checked,
			Data: map[string]any{
				"connId":     connId,
				"schema":     schemaName,
				"schemaName": schemaName,
			},
		})
	}

	return nodes
}

func getRoleSchemaPermissions(roleId, connId string) map[string]bool {
	schemas := make(map[string]bool)
	powerList := []*PowerDetail{}
	err := database.Mngtdb.Select(&powerList,
		"select schema_name from t_power where role_id = ? and conn_id = ? and power_level = 'schema'", roleId, connId)
	logger.PrintErr(err)
	for _, power := range powerList {
		if power.SchemaName != nil {
			schemas[*power.SchemaName] = true
		}
	}
	subPowers := []*PowerDetail{}
	err2 := database.Mngtdb.Select(&subPowers,
		"select schema_name from t_power where role_id = ? and conn_id = ? and power_level in ('table','column')", roleId, connId)
	logger.PrintErr(err2)
	for _, power := range subPowers {
		if power.SchemaName != nil {
			schemas[*power.SchemaName] = true
		}
	}
	return schemas
}

func getTableTree(connId, schema, authorization string, roleId string) []*PermissionNode {
	dc := getConnNoCheckInternal(connId)
	if dc == nil {
		return []*PermissionNode{}
	}

	schemaName := schema
	if strings.Contains(schema, "::") {
		parts := strings.Split(schema, "::")
		if len(parts) >= 2 {
			schemaName = parts[1]
		}
	}

	var roleTableMap map[string]bool
	if roleId != "" {
		roleTableMap = getRoleTablePermissions(roleId, connId, schemaName)
	}

	tableName, columnName, columnComment := "", "", ""

	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schemaName)
	if err != nil {
		logger.PrintErr(err)
		return []*PermissionNode{}
	}
	defer row.Close()

	tableColumns := make([]map[string]string, 0)
	for row.Next() {
		columnComment = ""
		row.Scan(&tableName, &columnName, &columnComment)
		tableColumns = append(tableColumns, map[string]string{"tableName": tableName, "columnName": columnName, "columnComment": columnComment})
	}

	grouped := make(map[string][]map[string]string)
	for _, col := range tableColumns {
		tableName := col["tableName"]
		if grouped[tableName] == nil {
			grouped[tableName] = make([]map[string]string, 0)
		}
		grouped[tableName] = append(grouped[tableName], col)
	}

	tableType, tableComment := "", ""
	row, err = dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listTable"], schemaName)
	if err != nil {
		logger.PrintErr(err)
		return []*PermissionNode{}
	}
	defer row.Close()

	nodes := make([]*PermissionNode, 0)
	for row.Next() {
		row.Scan(&tableName, &tableType, &tableComment)
		nodeType := "table"
		if dc.DriverName() == "mysql" || dc.DriverName() == "mariadb" {
			switch tableType {
			case "VIEW":
				nodeType = "view"
			case "BASE TABLE":
				nodeType = "table"
			}
		} else if dc.DriverName() == "oracle" {
			nodeType = strings.ToLower(tableType)
		}

		checked := false
		if roleId != "" {
			checked = roleTableMap[tableName]
		}

		nodes = append(nodes, &PermissionNode{
			Id:       connId + "::" + schemaName + "::" + tableName,
			Label:    tableName,
			Type:     nodeType,
			Level:    "table",
			ParentId: connId + "::" + schemaName,
			Checked:  checked,
			Data: map[string]any{
				"connId":     connId,
				"schema":     schemaName,
				"schemaName": schemaName,
				"table":      tableName,
				"tableName":  tableName,
				"comment":    tableComment,
			},
		})
	}

	return nodes
}

func getRoleTablePermissions(roleId, connId, schemaName string) map[string]bool {
	tables := make(map[string]bool)
	powerList := []*PowerDetail{}
	err := database.Mngtdb.Select(&powerList,
		"select table_name from t_power where role_id = ? and conn_id = ? and schema_name = ? and power_level = 'table'", roleId, connId, schemaName)
	logger.PrintErr(err)
	for _, power := range powerList {
		if power.TableName != nil {
			tables[*power.TableName] = true
		}
	}
	subPowers := []*PowerDetail{}
	err2 := database.Mngtdb.Select(&subPowers,
		"select table_name from t_power where role_id = ? and conn_id = ? and schema_name = ? and power_level = 'column'", roleId, connId, schemaName)
	logger.PrintErr(err2)
	for _, power := range subPowers {
		if power.TableName != nil {
			tables[*power.TableName] = true
		}
	}
	return tables
}

func getColumnTree(connId, schema, table, authorization string, roleId string) []*PermissionNode {
	dc := getConnNoCheckInternal(connId)
	if dc == nil {
		return []*PermissionNode{}
	}

	schemaName := schema
	if strings.Contains(schema, "::") {
		parts := strings.Split(schema, "::")
		if len(parts) >= 2 {
			schemaName = parts[1]
		}
	}

	tableName := table
	if strings.Contains(table, "::") {
		parts := strings.Split(table, "::")
		if len(parts) >= 3 {
			tableName = parts[2]
		}
	}

	var roleColumnMap map[string]map[string]bool
	if roleId != "" {
		roleColumnMap = getRoleColumnPermissions(roleId, connId, schemaName, tableName)
	}

	columnName, columnComment := "", ""
	row, err := dc.Query(dialect.SQL_DIALECT[dc.DriverName()]["listColumns"], tableName)
	if err != nil {
		logger.PrintErr(err)
		return []*PermissionNode{}
	}
	defer row.Close()

	nodes := make([]*PermissionNode, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		checked := false
		if roleId != "" && roleColumnMap != nil && roleColumnMap[tableName][columnName] {
			checked = true
		}

		nodes = append(nodes, &PermissionNode{
			Id:       connId + "::" + schemaName + "::" + tableName + "::" + columnName,
			Label:    columnName,
			Type:     "column",
			Level:    "column",
			ParentId: connId + "::" + schemaName + "::" + tableName,
			Checked:  checked,
			Data: map[string]any{
				"connId":     connId,
				"schema":     schemaName,
				"schemaName": schemaName,
				"table":      tableName,
				"tableName":  tableName,
				"column":     columnName,
				"columnName": columnName,
				"comment":    columnComment,
			},
		})
	}

	return nodes
}

func getRoleColumnPermissions(roleId, connId, schemaName, tableName string) map[string]map[string]bool {
	columns := make(map[string]map[string]bool)
	columns[tableName] = make(map[string]bool)
	powerList := []*PowerDetail{}
	err := database.Mngtdb.Select(&powerList, "select column_name from t_power where role_id = ? and conn_id = ? and schema_name = ? and table_name = ? and power_level = 'column'", roleId, connId, schemaName, tableName)
	logger.PrintErr(err)
	for _, power := range powerList {
		if power.ColumnName != nil {
			columns[tableName][*power.ColumnName] = true
		}
	}
	return columns
}

func UserPermissions(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	user := GetUser(authorization)
	if user == nil {
		jsonutil.WriteJson(c.Writer, []string{})
		return
	}

	powerList := findUserPowerDetails(user.Id)

	permissionKeys := make([]string, 0)
	for _, power := range powerList {
		key := power.ConnId
		if power.SchemaName != nil && *power.SchemaName != "" {
			key += "::" + *power.SchemaName
		}
		if power.TableName != nil && *power.TableName != "" {
			key += "::" + *power.TableName
		}
		if power.ColumnName != nil && *power.ColumnName != "" {
			key += "::" + *power.ColumnName
		}
		permissionKeys = append(permissionKeys, key)
	}

	jsonutil.WriteJson(c.Writer, permissionKeys)
}

func CanUseClassicView(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	user := GetUser(authorization)
	if user == nil {
		jsonutil.WriteJson(c.Writer, gin.H{"allowed": false})
		return
	}

	allowed := false
	roles := []*Role{}
	err := database.Mngtdb.Select(&roles,
		"select r.id, r.view_classic from t_role r inner join t_user_role ur on r.id = ur.role_id where ur.user_id = ?", user.Id)
	if err != nil {
		logger.PrintErr(err)
		jsonutil.WriteJson(c.Writer, gin.H{"allowed": false})
		return
	}
	for _, role := range roles {
		if role.ViewClassic > 0 {
			allowed = true
			break
		}
	}
	jsonutil.WriteJson(c.Writer, gin.H{"allowed": allowed})
}

func CanModifyData(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	user := GetUser(authorization)
	if user == nil {
		jsonutil.WriteJson(c.Writer, gin.H{"allowed": false})
		return
	}

	allowed := false
	roles := []*Role{}
	err := database.Mngtdb.Select(&roles,
		"select r.id, r.name, r.allow_modify from t_role r inner join t_user_role ur on r.id = ur.role_id where ur.user_id = ?", user.Id)
	if err != nil {
		logger.PrintErr(err)
		jsonutil.WriteJson(c.Writer, gin.H{"allowed": false})
		return
	}
	for _, role := range roles {
		if role.AllowModify > 0 {
			allowed = true
			break
		}
	}
	jsonutil.WriteJson(c.Writer, gin.H{"allowed": allowed})
}

func getConnNoCheckInternal(connId string) *sqlx.DB {
	if connId == "" {
		return nil
	}

	cfgList := []connCfgLocal{}
	err := database.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connId)
	if err != nil || len(cfgList) == 0 {
		logger.PrintErr(err)
		return nil
	}

	pwd := ""
	if cfgList[0].Pwd != nil {
		pwd = crypto.AESDecode(*cfgList[0].Pwd)
	}

	name := ""
	if cfgList[0].Name != nil {
		name = *cfgList[0].Name
	}
	user := ""
	if cfgList[0].User != nil {
		user = *cfgList[0].User
	}
	url := ""
	if cfgList[0].Url != nil {
		url = *cfgList[0].Url
	}

	return database.GetConn(&database.DBParam{
		Id: cfgList[0].Id, Name: name, DbType: cfgList[0].DbType,
		User: user, Pwd: pwd, Url: url,
	})
}