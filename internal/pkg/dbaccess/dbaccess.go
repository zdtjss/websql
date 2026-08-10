// Package dbaccess 提供管理库 *sqlx.DB 的依赖注入容器。
// 各业务包通过 Holder 管理注入的 DB，避免在各包内重复编写相同的 DI 样板代码。
package dbaccess

import (
	"log"

	"websql/internal/database"

	"github.com/jmoiron/sqlx"
)

// Holder 管理注入的 *sqlx.DB。
// Init 必须在容器启动阶段被调用，否则 Get() 将回退到 database.Mngtdb 并打印警告。
// 当两者均为 nil 时 panic，确保初始化错误在启动阶段立即暴露。
type Holder struct {
	db *sqlx.DB
}

// Init 由 DI 容器在启动阶段调用，将管理库 *sqlx.DB 注入。
func (h *Holder) Init(db *sqlx.DB) {
	h.db = db
}

// Get 返回注入的 DB。
// 优先返回 Init 注入的实例；未注入时回退到 database.Mngtdb（过渡期兼容，会打印警告）。
// 两者均为 nil 时 panic，表明应用初始化流程有错误。
func (h *Holder) Get() *sqlx.DB {
	if h.db != nil {
		return h.db
	}
	// 过渡期回退：通过 database.Mngtdb 兼容未完成迁移的场景
	if database.Mngtdb != nil {
		log.Println("[dbaccess] WARNING: Holder.Get() fallback to database.Mngtdb — ensure Init(db) is called during startup")
		return database.Mngtdb
	}
	panic("dbaccess: DB not initialized — Init(db) was never called and database.Mngtdb is nil. Check application startup sequence.")
}
