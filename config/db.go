package config

import (
	"go-web/logutils"
	"log"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"
	"github.com/jmoiron/sqlx"
)

// 打开数据库，如果不存在，则创建
var Mngtdb *sqlx.DB
var DBMap map[string]*sqlx.DB = make(map[string]*sqlx.DB)

func InitMngtDbConn() {
	Cfg = ReadConfig()
	sqlxDb, err := sqlx.Connect(Cfg.DB.DriverName, Cfg.DB.DataSourceName)
	if err != nil {
		panic(err)
	}
	Mngtdb = sqlxDb
}

// 此方法没有权限管理，不建议直接使用，请请使用admin.GetConn
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
	db, err := sqlx.Connect(param.DbType, *makeDsn(param))
	if err != nil {
		logutils.PanicErrf("连接数据库失败", err)
	}
	db.SetMaxOpenConns(5)
	DBMap[createKey(param)] = db
	log.Printf("数据库连接成功, env = %s, db = %s", param.Name, param.User)
}

func makeDsn(param *DBParam) *string {
	dsn := ""
	if param.DbType == "oracle" {
		dsn = "oracle://" + param.User + ":" + param.Pwd + "@" + param.Url
	} else {
		dsn = param.User + ":" + param.Pwd + "@" + param.Url
	}
	return &dsn
}

func RealseConn(param *DBParam) {
	conn, ok := DBMap[createKey(param)]
	if ok {
		conn.Close()
		delete(DBMap, createKey(param))
	}
}

func createKey(param *DBParam) string {
	return param.Id
}

type DBParam struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	User   string `json:"user"`
	Pwd    string `json:"pwd"`
	Url    string `json:"url"`
	DbType string `json:"dbType"`
}
