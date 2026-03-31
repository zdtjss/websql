package admin

import (
	"bytes"
	"errors"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"golang.org/x/exp/slices"
)

func SaveConn(c *gin.Context) {
	CheckAdminPower(c)
	cfg := &ConnCfg{}
	utils.UnmarshalJson(c.Request.Body, cfg)
	if cfg.Id == "" {
		stmt, _ := config.Mngtdb.Prepare("insert into t_conn (id, name, db_type, parent_id, user, pwd, url) values (?, ?, ?, ?, ?, ?, ?)")
		stmt.Exec(utils.RandomStr(), cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, utils.AESEncode(cfg.Pwd), cfg.Url)
	} else {
		if cfg.Pwd == "" {
			stmt, _ := config.Mngtdb.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, url = ? where id = ?")
			stmt.Exec(cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, cfg.Url, cfg.Id)
		} else {
			stmt, _ := config.Mngtdb.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, pwd = ?, url = ? where id = ?")
			stmt.Exec(cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, utils.AESEncode(cfg.Pwd), cfg.Url, cfg.Id)
		}
		config.RealseConn(convertToDBParam(cfg))
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
	c.JSON(200, gin.H{"code": 200, "msg": "连接成功"})
}

func DelConn(c *gin.Context) {
	CheckAdminPower(c)
	config.Mngtdb.Exec("delete from t_conn where id = ?", c.Query("id"))
	utils.WriteJson(c.Writer, "")
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
			data = findByParent(key, userPower)
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
		tree[i] = &Tree{Label: cfg.Name, Id: cfg.Id, Type: TREE_NODE_TYPE_CONN}
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
		cfgList[idx].Pwd = ""
	}
	utils.WriteJson(c.Writer, cfgList)
}

func ListConnBase(c *gin.Context) {
	cfgList := []*ConnCfgBase{}
	err := config.Mngtdb.Select(&cfgList, "select id,name,parent_id from t_conn")
	logutils.PanicErr(err)
	utils.WriteJson(c.Writer, cfgList)
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
	if config.Cfg.IsRemote && !slices.Contains(userPower.Power, id) {
		logutils.PanicErr(errors.New("无权访问"))
	}
	cfgList := []ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", id)
	logutils.PanicErr(err)
	cfgList[0].Pwd = utils.AESDecode(cfgList[0].Pwd)
	return config.GetConn(convertToDBParam(&cfgList[0]))
}

func convertToDBParam(cfg *ConnCfg) *config.DBParam {
	return &config.DBParam{Id: cfg.Id, Name: cfg.Name, DbType: cfg.DbType, User: cfg.User, Pwd: cfg.Pwd, Url: cfg.Url}
}

type ConnCfg struct {
	Id         string  `json:"id"`
	DbType     string  `json:"dbType" db:"db_type"`
	ParentId   string  `json:"parentId" db:"parent_id"`
	ParentName *string `json:"parentName" db:"parent_name"`
	Name       string  `json:"name"`
	User       string  `json:"user"`
	Pwd        string  `json:"pwd"`
	Url        string  `json:"url"`
}

type ConnCfgBase struct {
	Id       string `json:"id"`
	Name     string `json:"name"`
	ParentId string `json:"parentId" db:"parent_id"`
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
