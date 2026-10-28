package app

import (
	"log"
	"sync"

	"websql/internal/ai/agent"
	admin "websql/internal/app/admin"
	"websql/internal/app/backup"
	"websql/internal/app/configexport"
	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/monitor"
	"websql/internal/app/permission"
	"websql/internal/app/snippet"
	sqlapp "websql/internal/app/sql"
	"websql/internal/app/storage"
	"websql/internal/app/system"
	tree "websql/internal/app/treehandler"
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
	if database.Mngtdb == nil {
		database.InitMngtDbConn()
	}
	return NewContainerWithDB(database.Mngtdb)
}

// NewContainerWithDB 使用显式传入的 *sqlx.DB 构建容器。
// 这是推荐的构建方式：调用方通过 database.OpenMngtDB 获取 DB 后传入，
// 避免依赖全局变量。
func NewContainerWithDB(db *sqlx.DB) *Container {
	cfg := config.Get()
	if cfg == nil {
		cfg = config.ReadConfig()
		config.SetActive(cfg)
	}

	// 同步设置 Mngtdb（过渡期兼容：部分代码仍通过 Holder fallback 依赖它）
	if database.Mngtdb == nil {
		database.Mngtdb = db
	}

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
	configexport.Init(db)

	c := &Container{
		Config:       config.Get(),
		Mngtdb:       db,
		AuditService: audit.GetAuditService(),
	}
	appContainer = c

	// 权限修订号对账：远程模式启动 500ms 轮询（桌面/本地模式内部自动跳过）。
	// 必须在管理库与 config 就绪后启动，否则无法判断运行时 Profile。
	admin.StartPermissionRevisionWatcher()
	registerAIConfigChangeHooks()
	return c
}

// aiConfigHooksOnce 避免多次构建容器时重复注册配置变更钩子。
var aiConfigHooksOnce sync.Once

// registerAIConfigChangeHooks 在系统配置保存后失效 AI 安全相关的进程内缓存。
//
// 灰度配置与脱敏密钥均存放于 t_system_config：保存后必须立即失效，
// 否则最长会有一个刷新周期（5s）的旧配置窗口。
func registerAIConfigChangeHooks() {
	aiConfigHooksOnce.Do(func() {
		system.OnSystemConfigChanged(func(key string) {
			switch key {
			case agent.PolicyRolloutConfigKey, admin.AIPolicyEnforceKey, admin.RoleCapabilityVersionKey, "ai.deploymentSalt":
				agent.InvalidateRolloutCache()
			case "ai.maskKey", "ai.maskKeyVersion":
				if err := agent.GetMaskerProvider().Refresh(); err != nil {
					log.Printf("[AIConfigHook] 脱敏密钥/规则刷新失败（保留上一已验证快照） - err=%v", err)
				}
			}
		})
	})
}

// Close 释放容器持有的资源。
func (c *Container) Close() {
	admin.StopPermissionRevisionWatcher()
	if c.Mngtdb != nil {
		c.Mngtdb.Close()
	}
	database.CloseAllConns()
}
