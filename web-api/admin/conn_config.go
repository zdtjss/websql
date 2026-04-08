package admin

import (
	"bytes"
	"errors"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

func SaveConn(c *gin.Context) {
	CheckAdminPower(c)
	cfg := &ConnCfg{}
	utils.UnmarshalJson(c.Request.Body, cfg)

	dbParam := convertToDBParam(cfg)
	db := config.GetConn(dbParam)

	dbSchema, dbVersion := getDbVersionAndSchema(db, cfg.DbType)

	if cfg.Id == "" {
		stmt, _ := config.Mngtdb.Prepare("insert into t_conn (id, name, db_type, parent_id, user, pwd, url, db_schema, db_version) values (?, ?, ?, ?, ?, ?, ?, ?, ?)")
		pwdEncoded := ""
		if cfg.Pwd != nil {
			pwdEncoded = utils.AESEncode(*cfg.Pwd)
		}
		stmt.Exec(utils.RandomStr(), cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, pwdEncoded, cfg.Url, dbSchema, dbVersion)
	} else {
		if cfg.Pwd == nil || *cfg.Pwd == "" {
			stmt, _ := config.Mngtdb.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, url = ?, db_schema = ?, db_version = ? where id = ?")
			stmt.Exec(cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, cfg.Url, dbSchema, dbVersion, cfg.Id)
		} else {
			stmt, _ := config.Mngtdb.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, pwd = ?, url = ?, db_schema = ?, db_version = ? where id = ?")
			pwdEncoded := utils.AESEncode(*cfg.Pwd)
			stmt.Exec(cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, pwdEncoded, cfg.Url, dbSchema, dbVersion, cfg.Id)
		}
		config.RealseConn(dbParam)
	}
	utils.WriteJson(c.Writer, "")
}

func TestDbConn(c *gin.Context) {
	CheckAdminPower(c)
	cfg := &ConnCfg{}
	utils.UnmarshalJson(c.Request.Body, cfg)

	dbParam := convertToDBParam(cfg)
	db := config.GetConn(dbParam)

	err := db.Ping()
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "连接失败：" + err.Error()})
		return
	}

	dbSchema, dbVersion := getDbVersionAndSchema(db, cfg.DbType)

	c.JSON(200, gin.H{
		"code":      200,
		"msg":       "连接成功",
		"dbSchema":  dbSchema,
		"dbVersion": dbVersion,
	})
}

func DelConn(c *gin.Context) {
	CheckAdminPower(c)
	config.Mngtdb.Exec("delete from t_conn where id = ?", c.Query("id"))
	utils.WriteJson(c.Writer, "")
}

func getDbVersionAndSchema(db *sqlx.DB, dbType string) (string, string) {
	var versionSQL string
	var schemaSQL string

	switch dbType {
	case "mysql", "mariadb":
		versionSQL = "SELECT VERSION()"
		schemaSQL = "SELECT DATABASE()"
	case "oracle":
		versionSQL = "SELECT BANNER FROM V$VERSION WHERE ROWNUM = 1"
		schemaSQL = "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL"
	case "sqlite":
		versionSQL = "SELECT SQLITE_VERSION()"
		schemaSQL = "SELECT 'main'"
	default:
		versionSQL = "SELECT VERSION()"
		schemaSQL = "SELECT DATABASE()"
	}

	version := ""
	schema := ""

	if err := db.Get(&version, versionSQL); err != nil {
		version = ""
	}

	if err := db.Get(&schema, schemaSQL); err != nil {
		schema = ""
	}

	return schema, version
}

func ShowTree(c *gin.Context) {
	connId := c.Query("connId")
	key := c.Query("key")
	curType := c.Query("type")
	level := c.Query("level")
	authorization := c.GetHeader("Authorization")
	userPower := GetUserPower(authorization)

	nextType := getNextType(curType)

	var data = make([]*Tree, 0)
	switch nextType {
	case TREE_NODE_TYPE_DIR:
		if !strings.EqualFold(curType, TREE_NODE_TYPE_COLUMN) {
			// 第一层级：目录 + 顶层链接
			// 目录需要特殊处理：只显示包含用户有权限链接的目录
			data = findByParentWithPermission(key, userPower)
			if len(data) == 0 || data[0] == nil {
				data = listConn(key, userPower)
			}
			if level == "0" {
				data = append(data, listConn("noneParent", userPower)...)
			}
		}
	case TREE_NODE_TYPE_CONN:
		data = listConn(key, userPower)
	case TREE_NODE_TYPE_SCHEMA:
		data = listSchema(connId, authorization)
	case TREE_NODE_TYPE_TABLE:
		data = listTable(connId, key, authorization)
	case TREE_NODE_TYPE_COLUMN:
		data = listColumns(connId, key, authorization)
	case TREE_NODE_TYPE_ALLCOLUMN:
		data = listAllColumns(connId, key, authorization)
	}
	utils.WriteJson(c.Writer, data)
}

func ListTableColumns(c *gin.Context) {
	authorization := c.GetHeader("Authorization")

	param := ColumnsQuery{}
	c.ShouldBindJSON(&param)

	columns := listTableColumns(param.ConnId, param.TableName, param.Schema, authorization)
	utils.WriteJson(c.Writer, utils.SnakeToCamel(columns))

}

type ColumnsQuery struct {
	ConnId    string `json:"connId"`
	Schema    string `json:"schema"`
	TableName string `json:"tableName"`
}

func listConn(parentId string, userPower *UserPower) []*Tree {
	if parentId == "" {
		return nil
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
	tree := make([]*Tree, len(cfgList))
	for i, cfg := range cfgList {
		label := ""
		if cfg.Name != nil {
			label = *cfg.Name
		}
		tree[i] = &Tree{Label: label, Id: cfg.Id, Type: TREE_NODE_TYPE_CONN}
	}
	return tree
}

func ListConn2(c *gin.Context) {
	CheckAdminPower(c)

	name := c.Query("name")
	parentId := c.Query("parentId")

	cfgList := []ConnCfg{}

	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select c.*,t.label parent_name from t_conn c left join t_tree t on c.parent_id = t.id where 1 = 1 ")
	if name != "" {
		sql.WriteString(" and c.name like '%" + name + "%'")
	} else if parentId != "" {
		sql.WriteString(" and c.parent_id = ?")

		if parentId == "none" {
			param = append(param, "")
		} else {
			param = append(param, parentId)
		}

	}

	err := config.Mngtdb.Select(&cfgList, sql.String(), param...)
	logutils.PanicErr(err)
	for idx := range cfgList {
		cfgList[idx].Pwd = nil
	}
	utils.WriteJson(c.Writer, cfgList)
}

func ListConnBase(c *gin.Context) {
	cfgList := []*ConnCfgBase{}
	err := config.Mngtdb.Select(&cfgList, "select id,name,parent_id from t_conn")
	logutils.PanicErr(err)
	utils.WriteJson(c.Writer, cfgList)
}

func ListUserConn(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	userPower := GetUserPower(authorization)

	type UserConnDTO struct {
		ConnId   string  `json:"connId" db:"id"`
		Name     string  `json:"name" db:"name"`
		DbSchema *string `json:"dbSchema" db:"db_schema"`
		DirName  *string `json:"dirName" db:"dir_name"`
	}

	dtoList := []UserConnDTO{}
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select c.id, c.name, c.db_schema, t.label as dir_name from t_conn c left join t_tree t on c.parent_id = t.id where 1 = 1 ")
	appendPmsn(&sql, "c.id", &param, userPower)

	sql.WriteString(" order by t.label,c.name ")
	err := config.Mngtdb.Select(&dtoList, sql.String(), param...)
	logutils.PanicErr(err)

	utils.WriteJson(c.Writer, dtoList)
}

func ListTableNames(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	connId := c.Query("connId")
	schema := c.Query("schema")

	if connId == "" {
		utils.WriteJson(c.Writer, []any{})
		return
	}

	// 获取用户权限
	userPower := GetUserPower(authorization)

	// 查询所有表
	tables := queryTableInfo(connId, schema, authorization)

	// 根据用户权限过滤表
	filteredTables := filterTablesByPermission(tables, connId, schema, userPower)

	// 返回包含表名和注释的对象列表
	type TableNameDTO struct {
		Name    string `json:"name"`
		Comment string `json:"comment"`
	}
	result := make([]TableNameDTO, len(filteredTables))
	for i, table := range filteredTables {
		result[i] = TableNameDTO{Name: table.Name, Comment: table.Comment}
	}

	utils.WriteJson(c.Writer, result)
}

// filterTablesByPermission 根据用户权限过滤表列表
func filterTablesByPermission(tables []*Table, connId, schema string, userPower *UserPower) []*Table {
	// 如果用户没有权限限制（管理员），返回所有表
	if userPower == nil || len(userPower.Power) == 0 {
		log.Printf("filterTablesByPermission: no power 限制，返回所有表")
		return tables
	}

	// 获取用户的所有权限详情
	powerDetails := findUserPowerDetails(userPower.UserId)
	log.Printf("filterTablesByPermission: powerDetails count=%d", len(powerDetails))
	for i, p := range powerDetails {
		schemaName := ""
		if p.SchemaName != nil {
			schemaName = *p.SchemaName
		}
		tableName := ""
		if p.TableName != nil {
			tableName = *p.TableName
		}
		log.Printf("filterTablesByPermission: power[%d] level=%s, connId=%s, schema=%s, table=%s",
			i, p.Level, p.ConnId, schemaName, tableName)
	}
	if len(powerDetails) == 0 {
		return []*Table{}
	}

	// 对每个表检查权限
	filtered := make([]*Table, 0)
	for _, table := range tables {
		param := &PowerCheckParam{
			ConnId:     connId,
			SchemaName: schema,
			TableName:  table.Name,
		}

		// 使用 checkPower 检查是否有该表的访问权限
		hasAccess := checkPowerByParam(powerDetails, param)
		log.Printf("filterTablesByPermission: table=%s, hasAccess=%v", table.Name, hasAccess)

		if hasAccess {
			filtered = append(filtered, table)
		}
	}

	return filtered
}

// checkPowerByParam 根据权限详情检查是否有访问权限
func checkPowerByParam(powerDetails []*PowerDetail, param *PowerCheckParam) bool {
	// 收集各级权限
	hasConnPermission := false
	hasSchemaPermission := false
	hasTablePermission := false
	hasColumnPermission := false

	// 标记是否配置了下级权限（只统计当前层级下的）
	hasAnySchemaInThisConn := false  // 当前 conn 下是否配置了任何 schema
	hasAnyTableInThisSchema := false // 当前 schema 下是否配置了任何 table
	hasAnyColumnInThisTable := false // 当前 table 下是否配置了任何 column

	for _, power := range powerDetails {
		if power.ConnId != param.ConnId {
			continue
		}

		switch power.Level {
		case "conn":
			hasConnPermission = true
			log.Printf("checkPowerByParam: 有 conn 权限 connId=%s", power.ConnId)
		case "schema":
			if power.SchemaName != nil {
				// 统计当前 conn 下是否有 schema 配置
				hasAnySchemaInThisConn = true
				log.Printf("checkPowerByParam: 有 schema 权限 connId=%s, schema=%s", power.ConnId, *power.SchemaName)
				// 检查是否有当前 schema 的权限（如果请求的 schema 为空，则匹配所有）
				if param.SchemaName == "" || *power.SchemaName == param.SchemaName {
					hasSchemaPermission = true
					log.Printf("checkPowerByParam: 匹配当前 schema=%s", param.SchemaName)
				}
			}
		case "table":
			if power.SchemaName != nil && power.TableName != nil {
				// 统计当前 schema 下是否有 table 配置（如果请求的 schema 为空，则匹配所有）
				if param.SchemaName == "" || *power.SchemaName == param.SchemaName {
					hasAnyTableInThisSchema = true
					log.Printf("checkPowerByParam: 有 table 权限 schema=%s, table=%s", *power.SchemaName, *power.TableName)
					// 检查是否有当前 table 的权限
					if param.TableName == "" || *power.TableName == param.TableName {
						hasTablePermission = true
						log.Printf("checkPowerByParam: 匹配当前 table=%s", param.TableName)
					}
				}
			}
		case "column":
			if power.SchemaName != nil && power.TableName != nil && power.ColumnName != nil {
				// 统计当前 table 下是否有 column 配置（如果请求的 schema 为空，则匹配所有）
				if param.SchemaName == "" || (*power.SchemaName == param.SchemaName && *power.TableName == param.TableName) {
					hasAnyColumnInThisTable = true
					log.Printf("checkPowerByParam: 有 column 权限 table=%s, column=%s", *power.TableName, *power.ColumnName)
					// 检查是否有当前 column 的权限
					if *power.ColumnName != "" {
						hasColumnPermission = true
					}
				}
			}
		}
	}

	log.Printf("checkPowerByParam: hasConn=%v, hasSchema=%v, hasTable=%v, hasColumn=%v, hasAnySchema=%v, hasAnyTable=%v, hasAnyColumn=%v",
		hasConnPermission, hasSchemaPermission, hasTablePermission, hasColumnPermission,
		hasAnySchemaInThisConn, hasAnyTableInThisSchema, hasAnyColumnInThisTable)

	// 权限判断逻辑（向下继承）

	// 1. 检查 conn 级权限
	if hasConnPermission {
		// 如果 conn 下没有配置任何 schema，则有所有 schema 权限
		if !hasAnySchemaInThisConn {
			log.Printf("checkPowerByParam: conn 级通过（无 schema 配置）")
			return true
		}
		// 如果配置了 schema，检查是否有当前 schema 的权限
		if hasSchemaPermission {
			// 特殊情况：如果请求的 schema 为空（列举所有表），需要继续检查 table 级权限
			if param.SchemaName == "" && hasAnyTableInThisSchema && !hasTablePermission {
				// 有 schema 权限，但 schema 下配置了 table 权限，且当前表不在权限列表中
				// 这种情况不应该通过，让后续逻辑判断
				log.Printf("checkPowerByParam: schema 级权限但配置了 table，继续检查")
			} else {
				log.Printf("checkPowerByParam: conn 级通过（有 schema 权限）")
				return true
			}
		}
	}

	// 2. 检查 schema 级权限
	if hasSchemaPermission {
		// 如果 schema 下没有配置任何 table，则有所有 table 权限
		if !hasAnyTableInThisSchema {
			log.Printf("checkPowerByParam: schema 级通过（无 table 配置）")
			return true
		}
		// 如果配置了 table，检查是否有当前 table 的权限
		if hasTablePermission {
			log.Printf("checkPowerByParam: schema 级通过（有 table 权限）")
			return true
		}
	}

	// 3. 检查 table 级权限
	if hasTablePermission {
		// 如果 table 下没有配置任何 column，则有所有 column 权限（即有表权限）
		if !hasAnyColumnInThisTable {
			log.Printf("checkPowerByParam: table 级通过（无 column 配置）")
			return true
		}
		// 如果配置了 column，只要有 column 权限就有表权限
		if hasColumnPermission {
			log.Printf("checkPowerByParam: table 级通过（有 column 权限）")
			return true
		}
	}

	// 4. 检查 column 级权限
	if hasColumnPermission {
		log.Printf("checkPowerByParam: column 级通过")
		return true
	}

	log.Printf("checkPowerByParam: 拒绝访问")
	return false
}

func listConnBase() map[string][]*ConnCfgBase {
	cfgList := []*ConnCfgBase{}
	err := config.Mngtdb.Select(&cfgList, "select id,name,parent_id from t_conn")
	logutils.PanicErr(err)
	rolePowerMap := make(map[string][]*ConnCfgBase, len(cfgList))
	for _, conn := range cfgList {
		v, ok := rolePowerMap[conn.ParentId]
		if !ok {
			v = []*ConnCfgBase{}
		}
		v = append(v, conn)
		rolePowerMap[conn.ParentId] = v
	}
	return rolePowerMap
}

func getNextType(curType string) string {
	t := TREE_NODE_TYPE_DIR
	switch curType {
	case TREE_NODE_TYPE_DIR:
		t = TREE_NODE_TYPE_DIR
	case TREE_NODE_TYPE_CONN:
		t = TREE_NODE_TYPE_SCHEMA
	case TREE_NODE_TYPE_SCHEMA:
		t = TREE_NODE_TYPE_TABLE
	case TREE_NODE_TYPE_TABLE:
		t = TREE_NODE_TYPE_COLUMN
	case TREE_NODE_TYPE_ALLCOLUMN:
		t = TREE_NODE_TYPE_ALLCOLUMN
	}
	return t
}

func GetConn(id string, authorization string) *sqlx.DB {
	userPower := GetUserPower(authorization)
	if config.Cfg.IsRemote {
		param := &PowerCheckParam{
			ConnId: id,
		}
		if !checkPower(userPower, param) {
			logutils.PanicErr(errors.New("无权访问"))
		}
	}
	cfgList := []ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", id)
	logutils.PanicErr(err)

	// 解码密码
	pwd := ""
	if cfgList[0].Pwd != nil {
		pwd = utils.AESDecode(*cfgList[0].Pwd)
	}
	cfgList[0].Pwd = &pwd

	return config.GetConn(convertToDBParam(&cfgList[0]))
}

func convertToDBParam(cfg *ConnCfg) *config.DBParam {
	dbSchema := ""
	if cfg.DbSchema != nil {
		dbSchema = *cfg.DbSchema
	}
	dbVersion := ""
	if cfg.DbVersion != nil {
		dbVersion = *cfg.DbVersion
	}
	name := ""
	if cfg.Name != nil {
		name = *cfg.Name
	}
	user := ""
	if cfg.User != nil {
		user = *cfg.User
	}
	pwd := ""
	if cfg.Pwd != nil {
		pwd = *cfg.PwdTYPE_CONN:
		t = TREE_NODE_TYPE_SCHEMA
	case TREE_NODE_TYPE_SCHEMA:
		t = TREE_NODE_TYPE_TABLE
	case TREE_NODE_TYPE_TABLE:
		t = TREE_NODE_TYPE_COLUMN
	case TREE_NODE_TYPE_ALLCOLUMN:
		t = TREE_NODE_TYPE_ALLCOLUMN
	}
	return t
}

func GetConn(id string, authorization string) *sqlx.DB {
	userPower := GetUserPower(authorization)
	if config.Cfg.IsRemote {
		param := &PowerCheckParam{
			ConnId: id,
		}
		if !checkPower(userPower, param) {
			logutils.PanicErr(errors.New("无权访问"))
		}
	}
	cfgList := []ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", id)
	logutils.PanicErr(err)

	// 解码密码
	pwd := ""
	if cfgList[0].Pwd != nil {
		pwd = utils.AESDecode(*cfgList[0].Pwd)
	}
	cfgList[0].Pwd = &pwd

	return config.GetConn(convertToDBParam(&cfgList[0]))
}

func convertToDBParam(cfg *ConnCfg) *config.DBParam {
	dbSchema := ""
	if cfg.DbSchema != nil {
		dbSchema = *cfg.DbSchema
	}
	dbVersion := ""
	if cfg.DbVersion != nil {
		dbVersion = *cfg.DbVersion
	}
	name := ""
	if cfg.Name != nil {
		name = *cfg.Name
	}
	user := ""
	if cfg.User != nil {
		user = *cfg.User
	}
	pwd := ""
	if cfg.Pwd != nil {
		pwd = *cfg.Pwd
	}
	url := ""
	if cfg.Url != nil {
		url = *cfg.Url
	}
	return &config.DBParam{Id: cfg.Id, Name: name, DbType: cfg.DbType, User: user, Pwd: pwd, Url: url, DbSchema: dbSchema, DbVersion: dbVersion}
}

type ConnCfg struct {
	Id         string  `json:"id"`
	DbType     string  `json:"dbType" db:"db_type"`
	ParentId   string  `json:"parentId" db:"parent_id"`
	ParentName *string `json:"parentName" db:"parent_name"`
	Name       *string `json:"name"`
	User       *string `json:"user"`
	Pwd        *string `json:"pwd"`
	Url        *string `json:"url"`
	DbSchema   *string `json:"dbSchema" db:"db_schema"`
	DbVersion  *string `json:"dbVersion" db:"db_version"`
}

type ConnCfgBase struct {
	Id       string  `json:"id"`
	Name     *string `json:"name"`
	ParentId string  `json:"parentId" db:"parent_id"`
}

type Tree struct {
	Id       string         `json:"id"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
	Parent   string         `json:"parent"`
	Children []*Tree        `json:"children"`
}

const (
	// dir
	TREE_NODE_TYPE_DIR = "dir"
	// conn
	TREE_NODE_TYPE_CONN = "conn"
	// schema
	TREE_NODE_TYPE_SCHEMA = "schema"
	// table
	TREE_NODE_TYPE_TABLE = "table"
	// column
	TREE_NODE_TYPE_COLUMN = "column"
	// all column
	TREE_NODE_TYPE_ALLCOLUMN = "all_column"
)
