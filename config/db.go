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
	db, err := sqlx.Connect("mysql", Cfg.DB[param.Db][param.Env])
	if err != nil {
		panic("连接数据库失败，err :" + err.Error())
	}
	db.SetMaxOpenConns(5)
	DBMap[createKey(param)] = db
	log.Printf("数据库连接成功, env = %s, db = %s", param.Env, param.Db)
}

func createKey(param *DBParam) string {
	return param.Db + "_" + param.Env
}

type DBParam struct {
	Db  string
	Env string
}
