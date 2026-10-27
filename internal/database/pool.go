package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"websql/internal/config"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/mask"
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

// Deprecated: 使用 database.OpenMngtDB() + app.NewContainerWithDB() 替代。
// 仅在过渡期保留：dbaccess.Holder 回退和 LoadConfigFromDB 仍依赖此变量。
// 计划在所有直接引用消除后删除。
var Mngtdb *sqlx.DB

var (
	DBMap       = make(map[string]*sqlx.DB)
	dbMapMu     sync.RWMutex
	dbLastUsed  = make(map[string]time.Time)
	dbCancels   = make(map[string]context.CancelFunc) // 每个 DB 连接的健康检查 cancel
	dbTimeouts  = make(map[string]time.Duration)      // 每个 DB 连接的语句超时
	maxPoolSize = 100                                 // 最大连接池数量
)

const (
	// defaultConnectTimeout 连接超时：数据库不可达时必须尽快失败，而不是挂住请求。
	defaultConnectTimeout = 10 * time.Second
	// defaultQueryTimeout 语句超时：保留原有 5 分钟行为，可由连接配置覆盖。
	defaultQueryTimeout = 5 * time.Minute
)

func InitMngtDbConn() {
	db, err := OpenMngtDB(config.Get())
	if err != nil {
		panic(err)
	}
	Mngtdb = db
}

// OpenMngtDB 根据配置创建并返回管理库连接。
// 这是 InitMngtDbConn 的纯函数版本：不设置全局变量，调用方自行决定持有方式。
// 用于 DI 容器等需要显式传递 *sqlx.DB 的场景。
func OpenMngtDB(cfg *config.Config) (*sqlx.DB, error) {
	if cfg == nil {
		cfg = config.ReadConfig()
		// 写回全局活跃配置：main 流程后续 config.Get() 依赖此处完成初始化；
		// 桌面入口已预加载配置时不会进入此分支，不会覆盖 IsRemote/IsDesktop 标志
		config.SetActive(cfg)
	}
	dsn := config.ResolveDSN(cfg.DB.DataSourceName)
	if cfg.DB.DriverName == "sqlite" {
		dsn = WithSQLitePragmas(dsn, true)
	}
	log.Printf("管理库连接 - driver=%s, dsn=%s", cfg.DB.DriverName, mask.MaskDSN(dsn))
	sqlxDb, err := sqlx.Connect(cfg.DB.DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("管理库连接失败: %w", err)
	}
	if cfg.DB.DriverName == "sqlite" {
		sqlxDb.SetMaxOpenConns(10)
		sqlxDb.SetMaxIdleConns(3)
		sqlxDb.SetConnMaxLifetime(0)
		sqlxDb.SetConnMaxIdleTime(5 * time.Minute)
		initSQLitePragma(sqlxDb)
	} else {
		mngtMaxOpen := cfg.DB.MaxOpenConns
		if mngtMaxOpen <= 0 {
			mngtMaxOpen = 20
		}
		mngtMaxIdle := cfg.DB.MaxIdleConns
		if mngtMaxIdle <= 0 {
			mngtMaxIdle = 10
		}
		sqlxDb.SetMaxOpenConns(mngtMaxOpen)
		sqlxDb.SetMaxIdleConns(mngtMaxIdle)
		sqlxDb.SetConnMaxLifetime(30 * time.Minute)
	}
	return sqlxDb, nil
}

// WithSQLitePragmas 通过 DSN 的 _pragma 参数为连接池中的【每个】连接设置 PRAGMA。
// 关键点：PRAGMA 是连接级配置，用 db.Exec 执行只对当时取到的那一个物理连接生效，
// 连接池后续新建的连接 busy_timeout 仍为 0，并发写入时会立即报 SQLITE_BUSY。
// modernc.org/sqlite 驱动支持 _pragma=name(args) 形式的 DSN 参数，对每个新建连接自动执行。
// immediateTx：true 时额外加 _txlock=immediate，适用于管理库这类短事务场景，
// 可避免读事务升级写事务时的互锁；用户数据库不建议开启（会串行化事务内读）。
func WithSQLitePragmas(dsn string, immediateTx bool) string {
	pragmas := []string{
		"_pragma=busy_timeout(30000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=temp_store(MEMORY)",
	}
	if immediateTx {
		pragmas = append(pragmas, "_txlock=immediate")
	}
	for _, p := range pragmas {
		if strings.Contains(dsn, p) {
			continue
		}
		if strings.Contains(dsn, "?") {
			dsn += "&" + p
		} else {
			dsn += "?" + p
		}
	}
	return dsn
}

// initSQLitePragma 对池中首个连接补设 DSN 未覆盖的 PRAGMA（如 cache_size）。
// 注意：仅影响执行时取到的单个连接，通用配置必须走 WithSQLitePragmas 的 DSN 方式。
func initSQLitePragma(db *sqlx.DB) {
	pragmas := []string{
		"PRAGMA cache_size=-64000",
	}
	for _, p := range pragmas {
		db.Exec(p)
	}
}

func GetConn(param *DBParam) *sqlx.DB {
	if param == nil {
		panic(fmt.Errorf("连接参数为空"))
	}
	// 复制一份再补默认超时，避免修改调用方持有的结构体
	p := *param
	if p.ConnectTimeoutSec <= 0 {
		p.ConnectTimeoutSec = config.Get().DB.ConnectTimeoutSec
	}
	if p.QueryTimeoutSec <= 0 {
		p.QueryTimeoutSec = config.Get().DB.QueryTimeoutSec
	}
	param = &p

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

	db, err := sqlx.Open(param.DbType, makeDsn(param))
	if err != nil {
		log.Printf("创建数据库连接失败 - name=%s, dbType=%s, err=%v\n", param.Name, param.DbType, err)
		panic(err)
	}
	// 连接超时必须可控：sqlx.Connect 内部的 Ping 没有超时，数据库不可达时会长时间挂住。
	connectTimeout := time.Duration(param.ConnectTimeoutSec) * time.Second
	if connectTimeout <= 0 {
		connectTimeout = defaultConnectTimeout
	}
	pingCtx, pingCancel := context.WithTimeout(context.Background(), connectTimeout)
	pingErr := db.PingContext(pingCtx)
	pingCancel()
	if pingErr != nil {
		_ = db.Close()
		// 不使用 %+v 打印 DBParam：它包含明文口令。
		log.Printf("连接数据库失败 - name=%s, dbType=%s, user=%s, url=%s, err=%v\n",
			param.Name, param.DbType, param.User, mask.MaskDSN(param.Url), mask.MaskSecret(pingErr.Error()))
		// 抛出异常由 middleware.CustomRecovery 捕获，将驱动返回的错误消息透传给用户
		panic(pingErr)
	}
	dbCfg := config.Get().DB
	maxOpen := 50
	if dbCfg.MaxOpenConns > 0 {
		maxOpen = dbCfg.MaxOpenConns
	}
	maxIdle := 25
	if dbCfg.MaxIdleConns > 0 {
		maxIdle = dbCfg.MaxIdleConns
	}
	connMaxLife := 10 * time.Minute
	if dbCfg.ConnMaxLifeMin > 0 {
		connMaxLife = time.Duration(dbCfg.ConnMaxLifeMin) * time.Minute
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
	queryTimeout := time.Duration(param.QueryTimeoutSec) * time.Second
	if queryTimeout > 0 {
		dbTimeouts[key] = queryTimeout
	}
	dbMapMu.Unlock()

	log.Printf("[DBPool] 连接成功 - name=%s, dbType=%s, user=%s", param.Name, param.DbType, param.User)

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
	delete(dbTimeouts, key)
	delete(DBMap, key)
	delete(dbLastUsed, key)
}

// ConnQueryTimeout 返回指定连接的语句超时。
// 未配置时回退到默认 5 分钟（保持原有行为）。
func ConnQueryTimeout(connID string) time.Duration {
	dbMapMu.RLock()
	t, ok := dbTimeouts[connID]
	dbMapMu.RUnlock()
	if ok && t > 0 {
		return t
	}
	return defaultQueryTimeout
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
		// 用户 SQLite 库同样通过 DSN 为每个连接设置 busy_timeout/WAL，规避并发写锁冲突
		return WithSQLitePragmas(param.Url, false)
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

// WithTempConn 用一次性 key 创建连接，执行完 fn 后必定关闭并从池中移除。
// 用于「测试连接」「保存前校验」等不应污染共享连接池的场景：
// 既不会复用到别人的连接，也不会把临时连接留在池里占着健康检查 goroutine。
// GetConn 在连接失败时会 panic（由 HTTP 恢复中间件兜底），这里就地转成 error 返回。
func WithTempConn(param *DBParam, fn func(db *sqlx.DB) error) (err error) {
	if param == nil {
		return fmt.Errorf("连接参数为空")
	}
	if fn == nil {
		return fmt.Errorf("连接校验函数为空")
	}
	tmp := *param
	tmp.Id = "tmp:" + idgen.SecureRandomToken()[:16]

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	db := GetConn(&tmp)
	defer ReleaseConn(&tmp)
	return fn(db)
}

// CloseConnLater 延迟关闭指定连接的池，给在途查询留出完成窗口。
// 连接配置变更后若立即 Close，正在执行的查询会报 "database is closed"；
// 延迟期间新请求仍可复用旧连接（最多 delay 时长），代价可接受。
// 仅当该 key 仍指向同一个池时才关闭，避免误关变更后新建的池。
func CloseConnLater(id string, delay time.Duration) {
	if id == "" {
		return
	}
	dbMapMu.RLock()
	db, ok := DBMap[id]
	dbMapMu.RUnlock()
	if !ok {
		return
	}

	log.Printf("[DBPool] 连接配置变更，%s 后关闭旧连接池: id=%s", delay, id)
	time.AfterFunc(delay, func() {
		defer safego.Recover("db-pool-close-later")
		dbMapMu.Lock()
		if current, exists := DBMap[id]; exists && current == db {
			closeConnLocked(id)
		}
		dbMapMu.Unlock()
	})
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
	if param.Id != "" {
		return param.Id
	}
	// 临时连接（新建连接测试 / 保存前校验）没有 Id，必须按目标指纹分池。
	// 若统一用空串做 key，所有未保存的连接会共用一个池，
	// 出现「测完 A 库再测 B 库」时复用 A 的连接，把 B 的版本 / Schema 写错的问题。
	return tempConnKey(param)
}

// tempConnKey 生成临时连接池 key：DbType/Url/User/口令/默认 schema 的指纹，不含明文。
func tempConnKey(param *DBParam) string {
	h := sha256.New()
	for _, part := range []string{param.DbType, param.Url, param.User, param.Pwd, param.DbSchema} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "tmp:" + hex.EncodeToString(h.Sum(nil))[:16]
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
	// 连接超时（秒），<=0 使用默认 10s；语句超时（秒），<=0 使用默认 300s
	ConnectTimeoutSec int `json:"connectTimeoutSec"`
	QueryTimeoutSec   int `json:"queryTimeoutSec"`
}

func LoadConfigFromDB() {
	if Mngtdb == nil {
		return
	}

	loadSystemConfigValue("system.outterUser", &config.Get().OutterUser)
	loadSystemConfigValue("system.allowedIP", &config.Get().AllowedIP)
	loadSystemConfigValue("ai.provider", &config.Get().AI.Provider)
	loadSystemConfigValue("ai.baseUrl", &config.Get().AI.BaseURL)
	loadSystemConfigValue("ai.model", &config.Get().AI.Model)
	loadSystemConfigValue("ai.apiKey", &config.Get().AI.ApiKey)

	loadSystemConfigValue("system.redisAddr", &config.Get().Redis.Addr)
	loadSystemConfigValue("system.redisPassword", &config.Get().Redis.Password)
	var redisDBStr string
	loadSystemConfigValue("system.redisDB", &redisDBStr)
	if redisDBStr != "" {
		if dbVal, err := strconv.Atoi(redisDBStr); err == nil {
			config.Get().Redis.DB = dbVal
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
