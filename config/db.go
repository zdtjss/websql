package config

import (
	"log"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

var DBMap map[string]*sqlx.DB = make(map[string]*sqlx.DB)

func GetConn(param *DBParam) *sqlx.DB {
	val, ok := DBMap[createKey(param)]
	if ok {
		return val
	} else {
		initDBConn(param)
		return DBMap[createKey(param)]
	}
}

func initDBConn(param *DBParam) {
	log.Println("正在初始化数据库连接")
	db, err := sqlx.Connect("mysql", Cfg.DB[param.Db][param.Env])
	if err != nil {
		panic("数据库连接失败，err :" + err.Error())
	}
	db.SetMaxOpenConns(5)
	DBMap[createKey(param)] = db
}

func createKey(param *DBParam) string {
	return param.Db + "_" + param.Env
}

type DBParam struct {
	Db  string
	Env string
}
