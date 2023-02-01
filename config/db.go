package config

import (
	"log"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

var DBMap map[uint64]*sqlx.DB = make(map[uint64]*sqlx.DB)

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
	db, err := sqlx.Connect(param.DbType, param.User+":"+param.Pwd+"@"+param.Url)
	if err != nil {
		panic("连接数据库失败，err :" + err.Error())
	}
	db.SetMaxOpenConns(5)
	DBMap[createKey(param)] = db
	log.Printf("数据库连接成功, env = %s, db = %s", param.Name, param.User)
}

func RealseConn(param *DBParam) {
	val, ok := DBMap[createKey(param)]
	if ok {
		val.Close()
		delete(DBMap, createKey(param))
	}
}

func createKey(param *DBParam) uint64 {
	return param.Id
}

type DBParam struct {
	Id     uint64 `json:"id"`
	Name   string `json:"name"`
	User   string `json:"user"`
	Pwd    string `json:"pwd"`
	Url    string `json:"url"`
	DbType string `json:"dbType"`
}
