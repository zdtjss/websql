package admin

import (
	"bytes"
	"errors"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"go-web/utils/store"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
	"golang.org/x/exp/slices"
)

func SaveConn(w http.ResponseWriter, r *http.Request) {
	CheckPower(r)
	cfg := &ConnCfg{}
	utils.UnmarshalJson(r.Body, cfg)
	if cfg.Id == 0 {
		stmt, _ := config.Mngtdb.Prepare("insert into t_conn (id, name, db_type, parent_id, user, pwd, url) values (?, ?, ?, ?, ?, ?, ?)")
		stmt.Exec(utils.RandomInt64(), &cfg.Name, &cfg.DbType, &cfg.ParentId, &cfg.User, utils.AESEncode(cfg.Pwd), &cfg.Url)
	} else {
		if cfg.Pwd == "" {
			stmt, _ := config.Mngtdb.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, url = ? where id = ?")
			stmt.Exec(&cfg.Name, &cfg.DbType, &cfg.ParentId, &cfg.User, &cfg.Url, &cfg.Id)
		} else {
			stmt, _ := config.Mngtdb.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, pwd = ?, url = ? where id = ?")
			stmt.Exec(&cfg.Name, &cfg.DbType, &cfg.ParentId, &cfg.User, utils.AESEncode(cfg.Pwd), &cfg.Url, &cfg.Id)
		}
		config.RealseConn(convertToDBParam(cfg))
	}
	utils.WriteJson(w, "")
}

func DelConn(w http.ResponseWriter, r *http.Request) {
	CheckPower(r)
	r.ParseForm()
	id := utils.AtoUint64(r.FormValue("id"))
	config.Mngtdb.Exec("delete from t_conn where id = ?", id)
	utils.WriteJson(w, "")
}

func ShowTree(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	connId := utils.AtoUint64(r.FormValue("connId"))
	key := r.Form.Get("key")
	curType := r.Form.Get("type")
	level := r.Form.Get("level")
	authorization := r.Header.Get("Authorization")
	var userPower UserPower
	store.GetItem(authorization, &userPower)

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
	utils.WriteJson(w, data)
}

func listConn(parentId string, userPower UserPower) []*Tree {
	if parentId == "" {
		return nil
	}
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_conn")
	if strings.EqualFold(parentId, "noneParent") {
		sql.WriteString(" where (parent_id = 0 or parent_id is null)")
	} else if parentId != "" {
		param = append(param, parentId)
		sql.WriteString(" where parent_id = ?")
	}
	appendPmsn(&sql, "id", &param, userPower)
	cfgList := []ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, sql.String(), param...)
	logutils.Panicln(err)
	tree := make([]*Tree, len(cfgList))
	for i, cfg := range cfgList {
		tree[i] = &Tree{Label: cfg.Name, Id: cfg.Id, Type: TREE_NODE_TYPE_CONN}
	}
	return tree
}

func ListConn2(w http.ResponseWriter, r *http.Request) {
	CheckPower(r)
	cfgList := []ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select c.*,t.label parent_name from t_conn c left join t_tree t on c.parent_id = t.id")
	logutils.Panicln(err)
	for idx := range cfgList {
		cfgList[idx].Pwd = ""
	}
	utils.WriteJson(w, cfgList)
}

func ListConnBase(w http.ResponseWriter, r *http.Request) {
	cfgList := []*ConnCfgBase{}
	err := config.Mngtdb.Select(&cfgList, "select id,name,parent_id from t_conn")
	logutils.Panicln(err)
	utils.WriteJson(w, cfgList)
}

func listConnBase() map[uint64][]*ConnCfgBase {
	cfgList := []*ConnCfgBase{}
	err := config.Mngtdb.Select(&cfgList, "select id,name,parent_id from t_conn")
	logutils.Panicln(err)
	rolePowerMap := make(map[uint64][]*ConnCfgBase, len(cfgList))
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

func GetConn(id uint64, authorization string) *sqlx.DB {
	var userPower UserPower
	store.GetItem(authorization, &userPower)
	if !slices.Contains(userPower.Power, id) {
		logutils.Panicln(errors.New("无权访问"))
	}
	cfgList := []ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", id)
	logutils.Panicln(err)
	cfgList[0].Pwd = utils.AESDecode(cfgList[0].Pwd)
	return config.GetConn(convertToDBParam(&cfgList[0]))
}

func convertToDBParam(cfg *ConnCfg) *config.DBParam {
	return &config.DBParam{Id: cfg.Id, Name: cfg.Name, DbType: cfg.DbType, User: cfg.User, Pwd: cfg.Pwd, Url: cfg.Url}
}

type ConnCfg struct {
	Id         uint64  `json:"id"`
	DbType     string  `json:"dbType" db:"db_type"`
	ParentId   uint64  `json:"parentId" db:"parent_id"`
	ParentName *string `json:"parentName" db:"parent_name"`
	Name       string  `json:"name"`
	User       string  `json:"user"`
	Pwd        string  `json:"pwd"`
	Url        string  `json:"url"`
}

type ConnCfgBase struct {
	Id       uint64 `json:"id"`
	Name     string `json:"name"`
	ParentId uint64 `json:"parentId" db:"parent_id"`
}

type Tree struct {
	Id       uint64         `json:"id"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
	Parent   uint64         `json:"parent"`
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
