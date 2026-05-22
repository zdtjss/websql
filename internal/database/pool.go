package database

import (
	"encoding/json"
	"fmt"
	logutils "websql/internal/logger"
	"websql/internal/config"
	"log"
	"strconv"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"
)

var Mngtdb *sqlx.DB
var (
	DBMap   = make(map[string]*sqlx.DB)
	dbMapMu sync.RWMutex
)

func InitMngtDbConn() {
	config.Cfg = config.ReadConfig()
	sqlxDb, err := sqlx.Connect(config.Cfg.DB.DriverName, config.Cfg.DB.DataSourceName)
	if err != nil {
		panic(err)
	}
	if config.Cfg.DB.DriverName == "sqlite" {
		sqlxDb.SetMaxOpenConns(2)
		sqlxDb.SetMaxIdleConns(2)
		sqlxDb.SetConnMaxLifetime(0)
		initSQLitePragma(sqlxDb)
	} else {
		mngtMaxOpen := config.Cfg.DB.MaxOpenConns
		if mngtMaxOpen <= 0 {
			mngtMaxOpen = 20
		}
		mngtMaxIdle := config.Cfg.DB.MaxIdleConns
		if mngtMaxIdle <= 0 {
			mngtMaxIdle = 10
		}
		sqlxDb.SetMaxOpenConns(mngtMaxOpen)
		sqlxDb.SetMaxIdleConns(mngtMaxIdle)
		sqlxDb.SetConnMaxLifetime(30 * time.Minute)
	}
	Mngtdb = sqlxDb
}

func initSQLitePragma(db *sqlx.DB) {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-64000",
		"PRAGMA temp_store=MEMORY",
	}
	for _, p := range pragmas {
		db.Exec(p)
	}
}

func GetConn(param *DBParam) *sqlx.DB {
	key := createKey(param)

	dbMapMu.RLock()
	val, ok := DBMap[key]
	dbMapMu.RUnlock()
	if ok {
		return val
	}

	db, err := sqlx.Connect(param.DbType, makeDsn(param))
	if err != nil {
		logutils.PanicErrf("连接数据库失败", err)
	}
	maxOpen := 50
	if config.Cfg.DB.MaxOpenConns > 0 {
		maxOpen = config.Cfg.DB.MaxOpenConns
	}
	maxIdle := 25
	if config.Cfg.DB.MaxIdleConns > 0 {
		maxIdle = config.Cfg.DB.MaxIdleConns
	}
	connMaxLife := 10 * time.Minute
	if config.Cfg.DB.ConnMaxLifeMin > 0 {
		connMaxLife = time.Duration(config.Cfg.DB.ConnMaxLifeMin) * time.Minute
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(connMaxLife)
	db.SetConnMaxIdleTime(5 * time.Minute)

	dbMapMu.Lock()
	if existing, ok := DBMap[key]; ok {
		dbMapMu.Unlock()
		db.Close()
		return existing
	}
	DBMap[key] = db
	dbMapMu.Unlock()

	log.Printf("数据库连接成功， env = %s, db = %s", param.Name, param.User)

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

func makeDsn(param *DBParam) string {
	switch param.DbType {
	case "oracle":
		return "oracle://" + param.User + ":" + param.Pwd + "@" + param.Url
	case "mysql":
		return fmt.Sprintf("%s:%s@%s", param.User, param.Pwd, param.Url)
	default:
		return param.User + ":" + param.Pwd + "@" + param.Url
	}
}

func RealseConn(param *DBParam) {
	key := createKey(param)
	dbMapMu.Lock()
	if db, ok := DBMap[key]; ok {
		db.Close()
	}
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

func LoadConfigFromDB() {
	if Mngtdb == nil {
		return
	}

	loadSystemConfigValue("system.outterUser", &config.Cfg.OutterUser)
	loadSystemConfigValue("system.allowedIP", &config.Cfg.AllowedIP)
	loadSystemConfigValue("ai.provider", &config.Cfg.AI.Provider)
	loadSystemConfigValue("ai.baseUrl", &config.Cfg.AI.BaseURL)
	loadSystemConfigValue("ai.model", &config.Cfg.AI.Model)
	loadSystemConfigValue("ai.apiKey", &config.Cfg.AI.ApiKey)

	loadSystemConfigValue("system.redisAddr", &config.Cfg.Redis.Addr)
	loadSystemConfigValue("system.redisPassword", &config.Cfg.Redis.Password)
	var redisDBStr string
	loadSystemConfigValue("system.redisDB", &redisDBStr)
	if redisDBStr != "" {
		if dbVal, err := strconv.Atoi(redisDBStr); err == nil {
			config.Cfg.Redis.DB = dbVal
		}
	}
}

func loadSystemConfigValue(key string, target any) {
	var value string
	err := Mngtdb.Get(&value, "select config_value from t_system_config where config_key = ?", key)
	if err != nil || value == "" {
		return
	}

	switch t := target.(type) {
	case *string:
		*t = value
	case *[]string:
		var arr []string
		err := json.Unmarshal([]byte(value), &arr)
		if err == nil {
			*t = arr
		}
	}
}