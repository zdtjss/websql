package conn

import (
	"websql/internal/pkg/dbaccess"

	"github.com/jmoiron/sqlx"
)

var holder = &dbaccess.Holder{}

func Init(db *sqlx.DB) { holder.Init(db) }

func getDB() *sqlx.DB { return holder.Get() }

// getDBOrNil 返回管理库连接；未初始化时返回 nil（供审计/日志等旁路调用，不得 panic）
func getDBOrNil() *sqlx.DB { return holder.GetOrNil() }
