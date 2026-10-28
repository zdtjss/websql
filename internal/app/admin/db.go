package admin

import (
	"websql/internal/pkg/dbaccess"

	"github.com/jmoiron/sqlx"
)

var holder = &dbaccess.Holder{}

func Init(db *sqlx.DB) { holder.Init(db) }

func getDB() *sqlx.DB { return holder.Get() }

// getDBOrNil 返回管理库连接；未初始化时返回 nil（供修订号对账、审计等旁路使用，
// 避免启动期或测试环境下因管理库未就绪而 panic）。
func getDBOrNil() *sqlx.DB { return holder.GetOrNil() }
