package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"websql/internal/config"
	"websql/internal/pkg/safego"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"
)

// init 注册 mariadb 驱动别名。
// MariaDB 与 MySQL 协议兼容，go-sql-driver/mysql 驱动可直接用于连接 MariaDB。
// 通过注册别名，使 sqlx.Connect("mariadb", ...) 可正常工作，
// 且 *sqlx.DB.DriverName() 返回 "mariadb"，便于在 dialect map 中区分 MySQL 与 MariaDB。
func init() {
	sql.Register("mariadb", &mysql.MySQLDriver{})
}

// Deprecated: 使用 Container，将在阶段 4 移除
var Mngtdb *sqlx.DB

var (
	DBMap       = make(map[string]*sqlx.DB)
	dbMapMu     sync.RWMutex
	dbLastUsed  = make(map[string]time.Time)
	dbCancels   = make(map[string]context.CancelFunc) // 每个 DB 连接的健康检查 cancel
	maxPoolSize = 100                                 // 最大连接池数量
)

func InitMngtDbConn() {
	// 仅在调用方未预先加载配置时才读取，避免覆盖桌面入口已设置的 IsRemote/IsDesktop 标志。
	if config.Cfg == nil {
		config.Cfg = config.ReadConfig()
	}
	sqlxDb, err := sqlx.Connect(config.Cfg.DB.DriverName, config.Cfg.DB.DataSourceName)
	if err != nil {
		panic(err)
	}
	if config.Cfg.DB.DriverName == "sqlite" {
		sqlxDb.SetMaxOpenConns(10)
		sqlxDb.SetMaxIdleConns(3)
		sqlxDb.SetConnMaxLifetime(0)
		sqlxDb.SetConnMaxIdleTime(5 * time.Minute)
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
		"PRAGMA busy_timeout=30000",
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

	dbMapMu.Lock()
	val, ok := DBMap[key]
	if ok {
		dbLastUsed[key] = time.Now()
	}
	dbMapMu.Unlock()
	if ok {
		return val
	}

	db, err := sqlx.Connect(param.DbType, makeDsn(param))
	if err != nil {
		log.Printf("连接数据库失败 - err=%v, param=%+v\n", err, param)
		return nil
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

	// LRU 淘汰：当连接池数量超过上限时，关闭最久未使用的连接
	if len(DBMap) >= maxPoolSize {
		evictLRULocked()
	}

	// 创建 context 控制健康检查 goroutine 生命周期
	ctx, cancel := context.WithCancel(context.Background())
	dbCancels[key] = cancel

	DBMap[key] = db
	dbLastUsed[key] = time.Now()
	dbMapMu.Unlock()

	log.Printf("数据库连接成功， env = %s, db = %s", param.Name, param.User)

	go func() {
		defer safego.Recover("db-healthcheck")
		startHealthCheck(ctx, key, db)
	}()

	return db
}

// startHealthCheck 定期检查数据库连接健康状态
// 通过 context 控制生命周期，连接被淘汰或服务关闭时取消 context 即可停止 goroutine
func startHealthCheck(ctx context.Context, key string, db *sqlx.DB) {
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			err := db.Ping()
			if err != nil {
				// 健康检查失败：关闭并移除连接池，避免连接泄漏
				dbMapMu.Lock()
				if current, ok := DBMap[key]; ok && current == db {
					closeConnLocked(key)
				}
				dbMapMu.Unlock()
				return
			}
			// 更新最后使用时间（健康检查也算活跃）
			dbMapMu.Lock()
			if _, ok := DBMap[key]; ok {
				dbLastUsed[key] = time.Now()
			}
			dbMapMu.Unlock()
		}
	}
}

// evictLRULocked 淘汰最久未使用的连接，调用时必须持有 dbMapMu 写锁
func evictLRULocked() {
	// 淘汰 10% 的连接，避免频繁淘汰
	evictCount := maxPoolSize / 10
	if evictCount < 1 {
		evictCount = 1
	}

	for i := 0; i < evictCount; i++ {
		if len(DBMap) == 0 {
			break
		}
		oldestKey := ""
		oldestTime := time.Now()
		for k, t := range dbLastUsed {
			if t.Before(oldestTime) {
				oldestTime = t
				oldestKey = k
			}
		}
		if oldestKey != "" {
			closeConnLocked(oldestKey)
			log.Printf("连接池 LRU 淘汰: key=%s, lastUsed=%v", oldestKey, oldestTime)
		}
	}
}

// closeConnLocked 关闭指定 key 的连接并取消其健康检查 goroutine
// 调用时必须持有 dbMapMu 写锁
func closeConnLocked(key string) {
	if db, ok := DBMap[key]; ok {
		db.Close()
	}
	if cancel, ok := dbCancels[key]; ok {
		cancel()
		delete(dbCancels, key)
	}
	delete(DBMap, key)
	delete(dbLastUsed, key)
}

func makeDsn(param *DBParam) string {
	switch param.DbType {
	case "oracle":
		dsn := "oracle://" + param.User + ":" + param.Pwd + "@" + param.Url
		// 强制指定客户端字符集为 AL32UTF8，确保服务器返回 UTF-8 编码数据，避免中文乱码
		if strings.Contains(dsn, "?") {
			dsn += "&charset=AL32UTF8"
		} else {
			dsn += "?charset=AL32UTF8"
		}
		return dsn
	case "mysql", "mariadb":
		// MariaDB 与 MySQL 协议兼容，DSN 格式一致
		return fmt.Sprintf("%s:%s@%s", param.User, param.Pwd, param.Url)
	case "sqlite", "sqlite3":
		return param.Url
	default:
		return param.User + ":" + param.Pwd + "@" + param.Url
	}
}

func ReleaseConn(param *DBParam) {
	key := createKey(param)
	dbMapMu.Lock()
	closeConnLocked(key)
	dbMapMu.Unlock()
}

// CloseAllConns 关闭所有数据库连接池（服务关闭时调用）
func CloseAllConns() {
	dbMapMu.Lock()
	defer dbMapMu.Unlock()
	for key := range DBMap {
		closeConnLocked(key)
	}
	log.Printf("[DBPool] 所有连接已关闭\n")
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

func IsRetryableErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "is locked") {
		return true
	}
	if strings.Contains(msg, "Deadlock found") ||
		strings.Contains(msg, "try restarting transaction") ||
		strings.Contains(msg, "1213") ||
		strings.Contains(msg, "1205") {
		return true
	}
	return false
}

func RetryOnBusy(fn func() error, maxRetries int, baseDelay time.Duration) error {
	var err error
	for i := 0; i <= maxRetries; i++ {
		err = fn()
		if err == nil || !IsRetryableErr(err) {
			return err
		}
		if i < maxRetries {
			delay := baseDelay * time.Duration(1<<uint(i))
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
			time.Sleep(delay)
		}
	}
	return err
}

func MngtdbExec(query string, args ...any) error {
	return RetryOnBusy(func() error {
		_, err := Mngtdb.Exec(query, args...)
		return err
	}, 3, 50*time.Millisecond)
}

func MngtdbBeginx() (*sqlx.Tx, error) {
	var tx *sqlx.Tx
	err := RetryOnBusy(func() error {
		var err error
		tx, err = Mngtdb.Beginx()
		return err
	}, 3, 50*time.Millisecond)
	return tx, err
}
