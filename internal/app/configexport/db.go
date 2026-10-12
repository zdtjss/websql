package configexport

import (
	"websql/internal/pkg/dbaccess"

	"github.com/jmoiron/sqlx"
)

// holder 管理本包使用的管理库 *sqlx.DB，与其他业务包保持一致。
var holder = &dbaccess.Holder{}

// Init 由 DI 容器在启动阶段调用，注入管理库连接。
func Init(db *sqlx.DB) { holder.Init(db) }

func getDB() *sqlx.DB { return holder.Get() }