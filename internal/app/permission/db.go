package permission

import (
	"websql/internal/database"

	"github.com/jmoiron/sqlx"
)

// injectedDB 由 DI 容器通过 Init 注入；为 nil 时回退到全局 database.Mngtdb（向后兼容）。
var injectedDB *sqlx.DB

// Init 由 app 容器在启动阶段调用，将管理库 *sqlx.DB 注入到 permission 包。
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
