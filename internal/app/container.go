package app

import (
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"

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

// NewContainer 构建应用依赖容器。
// 不接管已有全局变量的初始化（保持兼容），仅聚合引用。
func NewContainer() *Container {
	if config.Cfg == nil {
		config.Cfg = config.ReadConfig()
	}
	if database.Mngtdb == nil {
		database.InitMngtDbConn()
	}

	return &Container{
		Config:       config.Cfg,
		Mngtdb:       database.Mngtdb,
		AuditService: audit.GetAuditService(),
	}
}

// Close 释放容器持有的资源。
func (c *Container) Close() {
	if c.Mngtdb != nil {
		c.Mngtdb.Close()
	}
	database.CloseAllConns()
}
