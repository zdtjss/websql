// Package dbaccess 提供管理库 *sqlx.DB 的依赖注入容器。
// 各业务包通过 Holder 管理注入的 DB，避免在各包内重复编写相同的 DI 样板代码。
package dbaccess

import (
	"websql/internal/database"

	"github.com/jmoiron/sqlx"
)

// Holder 管理注入的 *sqlx.DB，未注入时回退到全局 database.Mngtdb。
type Holder struct {
	db *sqlx.DB
}

// Init 由 DI 容器在启动阶段调用，将管理库 *sqlx.DB 注入。
func (h *Holder) Init(db *sqlx.DB) {
	h.db = db
}

// Get 返回注入的 DB，未注入时回退到全局 database.Mngtdb。
func (h *Holder) Get() *sqlx.DB {
	if h.db != nil {
		return h.db
	}
	return database.Mngtdb
}
