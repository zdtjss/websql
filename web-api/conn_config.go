package webapi

import (
	"bytes"
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
	utils.WriteJson(w, "")
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

func listConn(treeNode string) []*Tree {
	if treeNode == "" {
		return nil
	}
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_config_dbconn")
	if strings.EqualFold(treeNode, "noneParent") {
		sql.WriteString(" where tree_node = '' or tree_node is null")
	} else if treeNode != "" {
		param = append(param, treeNode)
		sql.WriteString(" where tree_node = ?")
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
	err := db.Select(&cfgList, "select * from t_config_dbconn")
	utils.Panicln(err)
	utils.WriteJson(w, cfgList)
}

func listSchema(key string) []*Tree {
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

func listTable(key, schema string) []*Tree {
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

func listColumns(key, table string) []*Tree {
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

func listAllColumns(key, schema string) []*Tree {
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

func doInsert(cfg *ConnCfg) {

	initConfigTable()

	stmt, _ := db.Prepare("insert into t_config_dbconn (name, db_type, tree_node, user, pwd, url) values (?, ?, ?, ?, ?, ?)")
	stmt.Exec(&cfg.Name, &cfg.DbType, &cfg.TreeNode, &cfg.User, &cfg.Pwd, &cfg.Url)
}

func doUpdate(cfg *ConnCfg) {
	stmt, _ := db.Prepare("update t_config_dbconn set name = ?, db_type = ?, tree_node = ?, user = ?, pwd = ?, url = ? where id = ?")
	stmt.Exec(&cfg.Name, &cfg.DbType, &cfg.TreeNode, &cfg.User, &cfg.Pwd, &cfg.Url, &cfg.Id)
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

func getConn(id string) *sqlx.DB {
	cfgList := []ConnCfg{}
	iid, _ := strconv.Atoi(id)
	err := db.Select(&cfgList, "select * from t_config_dbconn where id = ?", iid)
	utils.Panicln(err)
	cfg := cfgList[0]
	return config.GetConn(&config.DBParam{Id: cfg.Id, Name: cfg.Name, DbType: cfg.DbType, User: cfg.User, Pwd: cfg.Pwd, Url: cfg.Url})
}

// 初始化表结构
func InitTable() {
	initConfigTable()
	initTreeTable()
}

func initConfigTable() {

	//创建表
	sql_table := `
		CREATE TABLE IF NOT EXISTS t_config_dbconn (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			db_type VARCHAR(64) NULL,
			tree_node text(16),
			name VARCHAR(64) NULL,
			user VARCHAR(64) NULL,
			pwd VARCHAR(128) NULL,
			url VARCHAR(512) NULL
		);
		`
	db.Exec(sql_table)
}

type ConnCfg struct {
	Id       string `json:"id"`
	DbType   string `json:"dbType" db:"db_type"`
	TreeNode string `json:"treeNode" db:"tree_node"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Pwd      string `json:"pwd"`
	Url      string `json:"url"`
}

type Tree struct {
	Id       string         `json:"id"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
	Parent   string
	Children []*Tree `json:"children"`
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
