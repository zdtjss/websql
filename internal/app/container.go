package app

import (
	"websql/internal/ai/agent"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"
	admin "websql/internal/app/admin"
	"websql/internal/app/backup"
	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/monitor"
	"websql/internal/app/permission"
	"websql/internal/app/snippet"
	sqlapp "websql/internal/app/sql"
	"websql/internal/app/storage"
	"websql/internal/app/system"
	tree "websql/internal/app/treehandler"

	"github.com/jmoiron/sqlx"
)

// Container 是应用级依赖容器，在 main.go 启动时构建一次。
// 所有 Handler 通过 Container 获取 Service，Service 通过构造函数获取 Repo。
type Container struct {
	Config       *config.Config
	Mngtdb       *sqlx.DB
	AuditService *audit.AuditService
	// Redis 和其他依赖按需添加
}

// appContainer 由 NewContainer 设置，供同包内的 router.go 等基础设施代码引用，
// 避免在 app 包内直接使用 database.Mngtdb 全局变量。
var appContainer *Container

// GetContainer 返回当前应用容器；NewContainer 调用前为 nil。
func GetContainer() *Container {
	return appContainer
}

// NewContainer 构建应用依赖容器。
// 不接管已有全局变量的初始化（保持兼容），仅聚合引用，
// 并将管理库 *sqlx.DB 注入到尚未完成 repo 分层迁移的包，
// 使其 getDB() 返回注入实例而非已废弃的 database.Mngtdb 全局变量。
func NewContainer() *Container {
	cfg := config.Get()
	if cfg == nil {
		cfg = config.ReadConfig()
	}
	config.SetActive(cfg)
	if database.Mngtdb == nil {
		database.InitMngtDbConn()
	}

	db := database.Mngtdb
	// 注入到各业务包；未调用时各包 getDB() 回退到全局 database.Mngtdb（向后兼容）。
	// 顺序无依赖：各包 injectedDB 为独立包级变量。
	audit.Init(db)
	system.Init(db)
	admin.Init(db)
	tree.Init(db)
	permission.Init(db)
	monitor.Init(db)
	sqlapp.Init(db)
	agent.Init(db)
	conn.Init(db)
	backup.Init(db)
	dbops.Init(db)
	snippet.Init(db)
	storage.Init(db)

	c := &Container{
		Config:       config.Get(),
		Mngtdb:       db,
		AuditService: audit.GetAuditService(),
	}
	appContainer = c
	return c
}

// Close 释放容器持有的资源。
func (c *Container) Close() {
	if c.Mngtdb != nil {
		c.Mngtdb.Close()
	}
	database.CloseAllConns()
}
