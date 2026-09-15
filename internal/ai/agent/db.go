package agent

import (
	"log"

	"websql/internal/pkg/dbaccess"

	"github.com/jmoiron/sqlx"
)

var holder = &dbaccess.Holder{}

func Init(db *sqlx.DB) { holder.Init(db) }

func getDB() *sqlx.DB { return holder.Get() }

// getDBOrNil 安全获取管理库连接：未初始化时返回 nil 而非 panic。
// dbaccess.Holder.Get 在两者皆为空时会 panic（用于在启动阶段暴露初始化错误），
// 但后台异步任务（会话 debounce 保存等）不应因此崩溃进程，故在此做兜底。
func getDBOrNil() (db *sqlx.DB) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Agent] 管理库未初始化，跳过本次 DB 操作 - %v\n", r)
			db = nil
		}
	}()
	return holder.Get()
}
