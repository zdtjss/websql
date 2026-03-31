package config

import (
	"go-web/logutils"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"
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
	if Cfg.DB.DriverName == "sqlite" {
		// https://pkg.go.dev/modernc.org/sqlite#section-readme
		// Q: How can I write to a database concurrently without getting the database is locked error (or SQLITE_BUSY)?
		// A: You can't. The C sqlite implementation does not allow concurrent writes, and this libary does not modify that behaviour.
		//	  You can, however, use DB.SetMaxOpenConns(1) so that only 1 connection is ever used by the DB, allowing concurrent access to DB without making the writes concurrent.
		//	  More information on issues #65 and #106.
		sqlxDb.SetMaxOpenConns(1)
	}
	Mngtdb = sqlxDb
}

// 此方法没有权限管理，不建议直接使用，请请使用 admin.GetConn
func GetConn(param *DBParam) *sqlx.DB {
	key := createKey(param)
	val, ok := DBMap[key]
	if ok {
		return val
	} else {
		initDBConn(param)
		conn := DBMap[key]
		t := time.NewTicker(1 * time.Hour)
		go func() {
			for {
				<-t.C
				err := conn.Ping()
				if err != nil {
					delete(DBMap, key)
				}
			}
		}()
		return conn
	}
}

func initDBConn(param *DBParam) {
	db, err := sqlx.Connect(param.DbType, *makeDsn(param))
	if err != nil {
		logutils.PanicErrf("连接数据库失败", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
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
	key := createKey(param)
	_, ok := DBMap[key]
	if ok {
		delete(DBMap, key)
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
