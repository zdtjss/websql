package conn

import (
	"bytes"
	"log"
	"strconv"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

func SaveConn(c *gin.Context) {
	admin.CheckAdminPower(c)
	cfg := &ConnCfg{}
	jsonutil.UnmarshalJson(c.Request.Body, cfg)

	dbParam := ConvertToDBParam(cfg)
	db := database.GetConn(dbParam)
	if db == nil {
		c.JSON(200, gin.H{"code": 500, "msg": "连接失败，无法打开数据库"})
		return
	}

	dbSchema, dbVersion := getDbVersionAndSchema(db, cfg.DbType)

	var savedId string
	if cfg.Id == "" {
		savedId = idgen.RandomStr()
		stmt, _ := database.Mngtdb.Prepare("insert into t_conn (id, name, db_type, parent_id, user, pwd, url, db_schema, db_version) values (?, ?, ?, ?, ?, ?, ?, ?, ?)")
		pwdEncoded := ""
		if cfg.Pwd != nil && *cfg.Pwd != "" {
			pwdEncoded = crypto.AESEncode(*cfg.Pwd)
		}
		stmt.Exec(savedId, cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, pwdEncoded, cfg.Url, dbSchema, dbVersion)
	} else {
		savedId = cfg.Id
		if cfg.Pwd == nil || *cfg.Pwd == "" {
			stmt, _ := database.Mngtdb.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, url = ?, db_schema = ?, db_version = ? where id = ?")
			stmt.Exec(cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, cfg.Url, dbSchema, dbVersion, cfg.Id)
		} else {
			stmt, _ := database.Mngtdb.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, pwd = ?, url = ?, db_schema = ?, db_version = ? where id = ?")
			pwdEncoded := crypto.AESEncode(*cfg.Pwd)
			stmt.Exec(cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, pwdEncoded, cfg.Url, dbSchema, dbVersion, cfg.Id)
		}
		database.RealseConn(dbParam)
	}

	saved := []ConnCfg{}
	err := database.Mngtdb.Select(&saved, "select c.*, t.label parent_name from t_conn c left join t_tree t on c.parent_id = t.id where c.id = ?", savedId)
	logger.PanicErr(err)
	if len(saved) > 0 {
		saved[0].Pwd = nil
		jsonutil.WriteJson(c.Writer, saved[0])
	} else {
		jsonutil.WriteJson(c.Writer, "")
	}
}

func TestDbConn(c *gin.Context) {
	admin.CheckAdminPower(c)
	cfg := &ConnCfg{}
	jsonutil.UnmarshalJson(c.Request.Body, cfg)

	dbParam := ConvertToDBParam(cfg)
	db := database.GetConn(dbParam)
	if db == nil {
		c.JSON(200, gin.H{"code": 500, "msg": "连接失败，无法打开数据库"})
		return
	}

	err := db.Ping()
	if err != nil {
		log.Printf("[TestDbConn] 数据库连接失败 - err=%v\n", err)
		c.JSON(200, gin.H{"code": 500, "msg": "连接失败，请检查数据库配置"})
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
	admin.CheckAdminPower(c)
	database.Mngtdb.Exec("delete from t_conn where id = ?", c.Query("id"))
	jsonutil.WriteJson(c.Writer, "")
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
	default:
		versionSQL = "SELECT VERSION()"
		schemaSQL = "SELECT DATABASE()"
	}

	version := ""
	schema := ""

	if err := db.Get(&version, versionSQL); err != nil {
		log.Printf("[getDbVersionAndSchema] 获取版本失败 - dbType=%s, err=%v\n", dbType, err)
		version = ""
	}

	if dbType == "sqlite" {
		schema = "main"
	} else if err := db.Get(&schema, schemaSQL); err != nil {
		log.Printf("[getDbVersionAndSchema] 获取schema失败 - dbType=%s, err=%v\n", dbType, err)
		schema = ""
	}

	return schema, version
}

func ListConn(parentId string, userPower *admin.UserPower) []*Tree {
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
	admin.AppendPmsn(&sql, "id", &param, userPower)
	cfgList := []ConnCfg{}
	err := database.Mngtdb.Select(&cfgList, sql.String(), param...)
	logger.PanicErr(err)
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
	admin.CheckAdminPower(c)

	name := c.Query("name")
	parentId := c.Query("parentId")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	cfgList := []ConnCfg{}

	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select c.*,t.label parent_name from t_conn c left join t_tree t on c.parent_id = t.id where 1 = 1 ")
	if name != "" {
		sql.WriteString(" and c.name like '%" + name + "%'")
	}
	if parentId != "" {
		if parentId == "none" {
			sql.WriteString(" and (c.parent_id = '' or c.parent_id is null)")
		} else {
			sql.WriteString(" and c.parent_id = ?")
			param = append(param, parentId)
		}
	}

	countSQL := "select count(*) from (" + sql.String() + ") as total_count"
	var total int
	err := database.Mngtdb.Get(&total, countSQL, param...)
	logger.PanicErr(err)

	sql.WriteString(" order by c.id limit ? offset ?")
	param = append(param, pageSize, (page-1)*pageSize)

	err = database.Mngtdb.Select(&cfgList, sql.String(), param...)
	logger.PanicErr(err)
	for idx := range cfgList {
		cfgList[idx].Pwd = nil
	}
	jsonutil.WriteJson(c.Writer, map[string]any{
		"data":     cfgList,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func ListConnBase(c *gin.Context) {
	cfgList := []*ConnCfgBase{}
	err := database.Mngtdb.Select(&cfgList, "select id,name,parent_id from t_conn")
	logger.PanicErr(err)
	jsonutil.WriteJson(c.Writer, cfgList)
}

func ListUserConn(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	userPower := admin.GetUserPower(authorization)

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
	admin.AppendPmsn(&sql, "c.id", &param, userPower)

	sql.WriteString(" order by t.label,c.name ")
	err := database.Mngtdb.Select(&dtoList, sql.String(), param...)
	logger.PanicErr(err)

	jsonutil.WriteJson(c.Writer, dtoList)
}

func FilterTablesByPermission(tables []*Table, connId, schema string, userPower *admin.UserPower) []*Table {
	if !config.Cfg.IsRemote {
		return tables
	}
	if userPower == nil || len(userPower.Power) == 0 {
		return []*Table{}
	}

	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return []*Table{}
	}

	filtered := make([]*Table, 0)
	for _, table := range tables {
		param := &admin.PowerCheckParam{
			ConnId:     connId,
			SchemaName: schema,
			TableName:  table.Name,
		}

		hasAccess := checkPowerByParam(powerDetails, param)

		if hasAccess {
			filtered = append(filtered, table)
		}
	}

	return filtered
}

func checkPowerByParam(powerDetails []*admin.PowerDetail, param *admin.PowerCheckParam) bool {
	if param.SchemaName == "" {
		for _, power := range powerDetails {
			if power.ConnId == param.ConnId && power.Level == "conn" {
				return true
			}
		}
		return false
	}

	hasConnPermission := false
	hasSchemaPermission := false
	hasTablePermission := false
	hasColumnPermission := false
	hasTableOrColumnForSchema := false

	for _, power := range powerDetails {
		if power.ConnId != param.ConnId {
			continue
		}

		switch power.Level {
		case "conn":
			hasConnPermission = true
		case "schema":
			if power.SchemaName != nil && *power.SchemaName == param.SchemaName {
				hasSchemaPermission = true
			}
		case "table":
			if power.SchemaName != nil && power.TableName != nil {
				if *power.SchemaName == param.SchemaName {
					hasTableOrColumnForSchema = true
					if *power.TableName == param.TableName {
						hasTablePermission = true
					}
				}
			}
		case "column":
			if power.SchemaName != nil && power.TableName != nil && power.ColumnName != nil {
				if *power.SchemaName == param.SchemaName {
					hasTableOrColumnForSchema = true
					if *power.TableName == param.TableName {
						hasColumnPermission = true
					}
				}
			}
		}
	}

	if hasConnPermission && !hasTableOrColumnForSchema {
		return true
	}

	if hasSchemaPermission && !hasTableOrColumnForSchema {
		return true
	}

	if hasTablePermission {
		return true
	}

	if hasColumnPermission {
		return true
	}

	return false
}

func GetConn(id string, authorization string) *sqlx.DB {
	userPower := admin.GetUserPower(authorization)
	if config.Cfg.IsRemote {
		if !admin.CheckConnAccess(userPower, id) {
			logger.PrintErrf("无权访问连接: %s", nil, id)
			return nil
		}
	}
	cfgList := []ConnCfg{}
	err := database.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", id)
	if err != nil {
		logger.PrintErrf("查询连接配置失败: %s", err, id)
		return nil
	}
	if len(cfgList) == 0 {
		logger.PrintErrf("连接配置不存在: %s", nil, id)
		return nil
	}

	pwd := ""
	if cfgList[0].Pwd != nil && cfgList[0].DbType != "sqlite" {
		pwd = crypto.AESDecode(*cfgList[0].Pwd)
	}
	cfgList[0].Pwd = &pwd

	db := database.GetConn(ConvertToDBParam(&cfgList[0]))
	if db == nil {
		logger.PrintErrf("数据库连接创建失败: %s", nil, id)
	}
	return db
}

func GetConnNoCheck(connId string) *sqlx.DB {
	if connId == "" {
		return nil
	}

	cfgList := []ConnCfg{}
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

func ConvertToDBParam(cfg *ConnCfg) *database.DBParam {
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
	return &database.DBParam{Id: cfg.Id, Name: name, DbType: cfg.DbType, User: user, Pwd: pwd, Url: url, DbSchema: dbSchema, DbVersion: dbVersion}
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
	Charset    *string `json:"charset" db:"charset"`
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

type Table struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

type Column struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

type ColumnsQuery struct {
	ConnId    string `json:"connId"`
	Schema    string `json:"schema"`
	TableName string `json:"tableName"`
}

const (
	TREE_NODE_TYPE_DIR       = "dir"
	TREE_NODE_TYPE_CONN      = "conn"
	TREE_NODE_TYPE_SCHEMA    = "schema"
	TREE_NODE_TYPE_TABLE     = "table"
	TREE_NODE_TYPE_COLUMN    = "column"
	TREE_NODE_TYPE_ALLCOLUMN = "all_column"
)
