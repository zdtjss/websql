package webapi

import (
	"bytes"
	"go-web/config"
	"go-web/utils"
	"net/http"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/jmoiron/sqlx"
)

// 打开数据库，如果不存在，则创建
var db *sqlx.DB

func init() {
	sqlxDb, err := sqlx.Connect("sqlite3", "./nway.sqlite3.db")
	utils.Panicln(err)
	db = sqlxDb
}

func SaveConn(w http.ResponseWriter, r *http.Request) {
	cfg := &ConnCfg{}
	utils.UnmarshalJson(r.Body, cfg)
	if cfg.Id == 0 {
		stmt, _ := db.Prepare("insert into t_conn (id, name, db_type, parent_id, user, pwd, url) values (?, ?, ?, ?, ?, ?, ?)")
		stmt.Exec(utils.RandomInt64(), &cfg.Name, &cfg.DbType, &cfg.ParentId, &cfg.User, &cfg.Pwd, &cfg.Url)
	} else {
		stmt, _ := db.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, pwd = ?, url = ? where id = ?")
		stmt.Exec(&cfg.Name, &cfg.DbType, &cfg.ParentId, &cfg.User, &cfg.Pwd, &cfg.Url, &cfg.Id)
		config.RealseConn(convertToDBParam(cfg))
	}
	utils.WriteJson(w, "")
}

func DelConn(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id := utils.AtoUint64(r.FormValue("id"))
	db.Exec("delete from t_conn where id = ?", id)
	utils.WriteJson(w, "")
}

func ShowTree(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	connId := utils.AtoUint64(r.FormValue("connId"))
	key := r.Form.Get("key")
	curType := r.Form.Get("type")
	level := r.Form.Get("level")
	nextType := getNextType(curType)

	var data []*Tree
	switch nextType {
	case TREE_NODE_TYPE_DIR:
		if !strings.EqualFold(curType, TREE_NODE_TYPE_COLUMN) {
			data = findByParent(key)
			if len(data) == 0 || data[0] == nil {
				data = listConn(key)
			}
			if level == "0" {
				data = append(data, listConn("noneParent")...)
			}
		} else {
			data = make([]*Tree, 0)
		}
	case TREE_NODE_TYPE_CONN:
		data = listConn(key)
	case TREE_NODE_TYPE_SCHEMA:
		data = listSchema(connId)
	case TREE_NODE_TYPE_TABLE:
		data = listTable(connId, key)
	case TREE_NODE_TYPE_COLUMN:
		data = listColumns(connId, key)
	case TREE_NODE_TYPE_ALLCOLUMN:
		data = listAllColumns(connId, key)
	}
	utils.WriteJson(w, data)
}

func ShowTree2(w http.ResponseWriter, r *http.Request) {

}

func listConn(parentId string) []*Tree {
	if parentId == "" {
		return nil
	}
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_conn")
	if strings.EqualFold(parentId, "noneParent") {
		sql.WriteString(" where parent_id = 0 or parent_id is null")
	} else if parentId != "" {
		param = append(param, parentId)
		sql.WriteString(" where parent_id = ?")
	}
	cfgList := []ConnCfg{}
	err := db.Select(&cfgList, sql.String(), param...)
	utils.Panicln(err)
	tree := make([]*Tree, len(cfgList))
	for i, cfg := range cfgList {
		tree[i] = &Tree{Label: cfg.Name, Id: cfg.Id, Type: TREE_NODE_TYPE_CONN}
	}
	return tree
}

func ListConn2(w http.ResponseWriter, r *http.Request) {
	cfgList := []ConnCfg{}
	err := db.Select(&cfgList, "select c.*,t.label parent_name from t_conn c left join t_tree t on c.parent_id = t.id")
	utils.Panicln(err)
	utils.WriteJson(w, cfgList)
}

func ListConnBase(w http.ResponseWriter, r *http.Request) {
	cfgList := []*ConnCfgBase{}
	err := db.Select(&cfgList, "select id,name,parent_id from t_conn")
	utils.Panicln(err)
	utils.WriteJson(w, cfgList)
}

func listConnBase() map[uint64][]*ConnCfgBase {
	cfgList := []*ConnCfgBase{}
	err := db.Select(&cfgList, "select id,name,parent_id from t_conn")
	utils.Panicln(err)
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

func listSchema(key uint64) []*Tree {
	schemaName := ""
	row, err := getConn(key).Query("select schema_name from information_schema.schemata")
	utils.Panicln(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		tree = append(tree, &Tree{Label: schemaName, Type: TREE_NODE_TYPE_SCHEMA})
	}
	return tree
}

func listTable(key uint64, schema string) []*Tree {
	tableName, tableComment := "", ""
	row, err := getConn(key).Query("select TABLE_NAME,table_comment from information_schema.tables WHERE table_schema = ?", schema)
	utils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableComment)
		tree = append(tree, &Tree{Label: tableName, Data: map[string]any{"text": tableComment}, Type: TREE_NODE_TYPE_TABLE})
	}
	return tree
}

func listColumns(key uint64, table string) []*Tree {
	columnName, columnComment := "", ""
	row, err := getConn(key).Query("select concat(column_name,'  ', column_type) column_name,COLUMN_COMMENT from information_schema.COLUMNS where TABLE_NAME = ? order by ORDINAL_POSITION", table)
	utils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func listAllColumns(key uint64, schema string) []*Tree {
	columnName, columnComment := "", ""
	row, err := getConn(key).Query("select column_name, COLUMN_COMMENT from information_schema.COLUMNS where table_schema = ?", schema)
	utils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]any{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
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

func getConn(id uint64) *sqlx.DB {
	cfgList := []ConnCfg{}
	err := db.Select(&cfgList, "select * from t_conn where id = ?", id)
	utils.Panicln(err)
	return config.GetConn(convertToDBParam(&cfgList[0]))
}

func convertToDBParam(cfg *ConnCfg) *config.DBParam {
	return &config.DBParam{Id: cfg.Id, Name: cfg.Name, DbType: cfg.DbType, User: cfg.User, Pwd: cfg.Pwd, Url: cfg.Url}
}

// 初始化表结构
func InitTable() {
	initConfigTable()
	initTreeTable()
}

func initConfigTable() {

	//创建表
	sql_table := `
		CREATE TABLE IF NOT EXISTS t_conn (
			id BIGINT PRIMARY KEY,
			db_type VARCHAR(64) NULL,
			parent_id BIGINT,
			name VARCHAR(64) NULL,
			user VARCHAR(64) NULL,
			pwd VARCHAR(128) NULL,
			url VARCHAR(512) NULL
		);
		`
	db.Exec(sql_table)
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
