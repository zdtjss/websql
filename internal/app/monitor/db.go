package monitor

import (
	"time"

	"websql/internal/database"

	"github.com/jmoiron/sqlx"
)

// injectedDB 由 DI 容器通过 Init 注入；为 nil 时回退到全局 database.Mngtdb（向后兼容）。
var injectedDB *sqlx.DB

// Init 由 app 容器在启动阶段调用，将管理库 *sqlx.DB 注入到 monitor 包。
// 不调用也能工作——getDB 会回退到全局 database.Mngtdb。
func Init(db *sqlx.DB) {
	injectedDB = db
}

// getDB 返回注入的 DB，未注入时回退到全局 database.Mngtdb。
// Deprecated: 仅为兼容未调用 Init 的场景，后续应移除回退
func getDB() *sqlx.DB {
	if injectedDB != nil {
		return injectedDB
	}
	return database.Mngtdb // Deprecated: 回退兼容
}

// execWithRetry 在管理库上执行写操作，对 SQLITE_BUSY/死锁等可重试错误自动退避重试。
// 替代 database.MngtdbExec（基于已废弃全局变量的辅助函数），使 monitor 包不再间接依赖 database.Mngtdb。
func execWithRetry(query string, args ...any) error {
	return database.RetryOnBusy(func() error {
		_, err := getDB().Exec(query, args...)
		return err
	}, 3, 50*time.Millisecond)
}

