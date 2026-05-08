package config

import (
	"go-web/logutils"
	"log"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"
)

var Mngtdb *sqlx.DB
var (
	DBMap   map[string]*sqlx.DB = make(map[string]*sqlx.DB)
	dbMapMu sync.RWMutex
)

func InitMngtDbConn() {
	Cfg = ReadConfig()
	sqlxDb, err := sqlx.Connect(Cfg.DB.DriverName, Cfg.DB.DataSourceName)
	if err != nil {
		panic(err)
	}
	if Cfg.DB.DriverName == "sqlite" {
		sqlxDb.SetMaxOpenConns(1)
	}
	Mngtdb = sqlxDb
}

func GetConn(param *DBParam) *sqlx.DB {
	key := createKey(param)

	dbMapMu.RLock()
	val, ok := DBMap[key]
	dbMapMu.RUnlock()
	if ok {
		return val
	}

	dbMapMu.Lock()
	defer dbMapMu.Unlock()

	if val, ok := DBMap[key]; ok {
		return val
	}

	db, err := sqlx.Connect(param.DbType, *makeDsn(param))
	if err != nil {
		logutils.PanicErrf("连接数据库失败", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	DBMap[key] = db
	log.Printf("数据库连接成功, env = %s, db = %s", param.Name, param.User)

	go func() {
		t := time.NewTicker(1 * time.Hour)
		defer t.Stop()
		for range t.C {
			err := db.Ping()
			if err != nil {
				dbMapMu.Lock()
				if current, ok := DBMap[key]; ok && current == db {
					delete(DBMap, key)
				}
				dbMapMu.Unlock()
				return
			}
		}
	}()

	return db
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
	dbMapMu.Lock()
	delete(DBMap, key)
	dbMapMu.Unlock()
}

func createKey(param *DBParam) string {
	return param.Id
}

type DBParam struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	User      string `json:"user"`
	Pwd       string `json:"pwd"`
	Url       string `json:"url"`
	DbType    string `json:"dbType"`
	DbSchema  string `json:"dbSchema"`
	DbVersion string `json:"dbVersion"`
}
