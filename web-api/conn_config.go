package webapi

import (
	"go-web/config"
	"go-web/utils"
	"net/http"
	"strconv"
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
	if cfg.Id == "" {
		doInsert(cfg)
	} else {
		doUpdate(cfg)
	}
}

func DelConn(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id := r.Form.Get("id")
	db.Exec("delete from t_config_dbconn where id = ?", id)
	utils.WriteJson(w, "")
}

func ShowTree(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	connId := r.Form.Get("connId")
	key := r.Form.Get("key")
	curType := r.Form.Get("type")
	nextType := getNextType(curType)

	var data []*Tree
	switch nextType {
	case TREE_NODE_TYPE_CONN:
		if !strings.EqualFold(curType, TREE_NODE_TYPE_COLUMN) {
			data = listConn()
		} else {
			data = make([]*Tree, 0)
		}
	case TREE_NODE_TYPE_SCHEMA:
		data = listSchema(connId)
	case TREE_NODE_TYPE_TABLE:
		data = listTable(connId, key)
	case TREE_NODE_TYPE_COLUMN:
		data = listColumns(connId, key)
	}
	utils.WriteJson(w, data)
}

func listConn() []*Tree {

	type NodeData struct {
		Id string `json:"id"`
	}

	cfgList := []ConnCfg{}
	err := db.Select(&cfgList, "select * from t_config_dbconn")
	utils.Println(err)
	tree := make([]*Tree, len(cfgList))
	for i, cfg := range cfgList {
		tree[i] = &Tree{Label: cfg.Name, Data: NodeData{Id: cfg.Id}, Type: TREE_NODE_TYPE_CONN}
	}
	return tree
}

func ListConn2(w http.ResponseWriter, r *http.Request) {
	cfgList := []ConnCfg{}
	err := db.Select(&cfgList, "select * from t_config_dbconn")
	utils.Panicln(err)
	utils.WriteJson(w, cfgList)
}

func listSchema(key string) []*Tree {
	schemaName := ""
	row, err := getConn(key).Query("select schema_name from information_schema.schemata")
	utils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&schemaName)
		tree = append(tree, &Tree{Label: schemaName, Type: TREE_NODE_TYPE_SCHEMA})
	}
	return tree
}

func listTable(key, schema string) []*Tree {

	tableName, tableComment := "", ""
	row, err := getConn(key).Query("select TABLE_NAME,table_comment from information_schema.tables WHERE table_schema = ?", schema)
	utils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&tableName, &tableComment)
		tree = append(tree, &Tree{Label: tableName, Data: map[string]string{"text": tableComment}, Type: TREE_NODE_TYPE_TABLE})
	}
	return tree
}

func listColumns(key, table string) []*Tree {
	columnName, columnComment := "", ""
	row, err := getConn(key).Query("select concat(column_name,'  ', column_type) column_name,COLUMN_COMMENT from information_schema.COLUMNS where TABLE_NAME = ? order by ORDINAL_POSITION", table)
	utils.Println(err)
	tree := make([]*Tree, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		tree = append(tree, &Tree{Label: columnName, Data: map[string]string{"text": columnComment}, Type: TREE_NODE_TYPE_COLUMN})
	}
	return tree
}

func doInsert(cfg *ConnCfg) {

	initConfigTable()

	stmt, _ := db.Prepare("insert into t_config_dbconn (name, user, pwd, url) values (?, ?, ?, ?)")
	stmt.Exec(&cfg.Name, &cfg.User, &cfg.Pwd, &cfg.Url)
}

func doUpdate(cfg *ConnCfg) {
	stmt, _ := db.Prepare("update t_config_dbconn set name = ?, user = ?, pwd = ?, url = ? where id = ?")
	stmt.Exec(&cfg.Name, &cfg.User, &cfg.Pwd, &cfg.Url, &cfg.Id)
}

func getNextType(curType string) string {
	t := TREE_NODE_TYPE_CONN
	switch curType {
	case TREE_NODE_TYPE_CONN:
		t = TREE_NODE_TYPE_SCHEMA
	case TREE_NODE_TYPE_SCHEMA:
		t = TREE_NODE_TYPE_TABLE
	case TREE_NODE_TYPE_TABLE:
		t = TREE_NODE_TYPE_COLUMN
	}
	return t
}

func getConn(id string) *sqlx.DB {
	cfgList := []ConnCfg{}
	iid, _ := strconv.Atoi(id)
	err := db.Select(&cfgList, "select * from t_config_dbconn where id = ?", iid)
	utils.Panicln(err)
	cfg := cfgList[0]
	return config.GetConn(&config.DBParam{Id: cfg.Id, Name: cfg.Name, User: cfg.User, Pwd: cfg.Pwd, Url: cfg.Url})
}

func initConfigTable() {

	//创建表
	sql_table := `
		CREATE TABLE IF NOT EXISTS t_config_dbconn (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name VARCHAR(64) NULL,
			user VARCHAR(64) NULL,
			pwd VARCHAR(128) NULL,
			url VARCHAR(512) NULL
		);
		`
	db.Exec(sql_table)
}

type ConnCfg struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	User string `json:"user"`
	Pwd  string `json:"pwd"`
	Url  string `json:"url"`
}

type Tree struct {
	Label    string `json:"label"`
	Type     string `json:"type"`
	Data     any    `json:"data"`
	Children []Tree `json:"children"`
}

const (
	// conn
	TREE_NODE_TYPE_CONN = "conn"
	// schema
	TREE_NODE_TYPE_SCHEMA = "schema"
	// table
	TREE_NODE_TYPE_TABLE = "table"
	// column
	TREE_NODE_TYPE_COLUMN = "column"
)
