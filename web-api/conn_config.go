package webapi

import (
	"database/sql"
	"net/http"
)

// 打开数据库，如果不存在，则创建
var db, _ = sql.Open("sqlite3", "./nway.sqlite3.db")

func SaveConn(w http.ResponseWriter, r *http.Request) {

}

func doInsert(cfg *ConnCfg) {

	initConfigTable()

	stmt, _ := db.Prepare("insert into t_config_dbconn (name, user, pwd, url) values (?, ?, ?, ?)")
	stmt.Exec(&cfg.Name, &cfg.User, &cfg.Pwd, &cfg.Url)
}

func doInsert(cfg *ConnCfg) {

	stmt, _ := db.Prepare("insert into t_config_dbconn (name, user, pwd, url) values (?, ?, ?, ?)")
	stmt.Exec(&cfg.Name, &cfg.User, &cfg.Pwd, &cfg.Url)
}

func initConfigTable() {

	//创建表
	sql_table := `
		CREATE TABLE IF NOT EXISTS t_config_dbconn (
			uid INTEGER PRIMARY KEY AUTOINCREMENT,
			name VARCHAR(64) NULL,
			user VARCHAR(64) NULL,
			pwd VARCHAR(128) NULL,
			url VARCHAR(512) NULL
		);
		`
	db.Exec(sql_table)
}

type ConnCfg struct {
	Name string
	User string
	Pwd  string
	Url  string
}
