package version

// Version 由 -ldflags="-X internal/version.Version=x.y.z" 注入，默认 dev。
var Version = "dev"

// RequiredMigrationVersion 本程序要求的最小 DB schema 迁移版本号。
// 每次新增迁移脚本时同步更新此值。启动时校验若低于此值则拒绝启动（MySQL）或告警（SQLite）。
var RequiredMigrationVersion = "0002"
