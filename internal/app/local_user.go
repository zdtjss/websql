package app

import (
	"log"

	"websql/internal/app/admin"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/pkg/idgen"
)

// LocalAutoToken 本地/桌面模式自动登录使用的固定 token（复用 admin 包常量）
const LocalAutoToken = admin.LocalAutoToken

// EnsureLocalUser 在本地/桌面模式下确保 local 用户存在，并注入会话。
// 会话注入部分委托给 admin.EnsureLocalSession（同时供 middleware 自愈复用）。
func EnsureLocalUser() {
	cfg := config.Get()
	if cfg.IsRemote {
		return
	}

	db := database.Mngtdb
	if ctr := GetContainer(); ctr != nil && ctr.Mngtdb != nil {
		db = ctr.Mngtdb
	}
	if db == nil {
		log.Println("[LocalUser] 数据库未初始化，跳过")
		return
	}

	// 确保 local 用户存在（兼容旧数据库没有此用户的情况）
	var count int
	err := db.Get(&count, "SELECT COUNT(1) FROM t_user WHERE login_name = 'local'")
	if err != nil || count == 0 {
		// 用与 admin 相同的密码哈希(密码为 1)
		_, err = db.Exec(
			`INSERT OR IGNORE INTO t_user (id, login_name, name, pwd, bio) VALUES (?, 'local', 'local', '7e2e1f2e1eb71a6f7915a96201237ff0', '')`,
			idgen.RandomStr(),
		)
		if err != nil {
			log.Printf("[LocalUser] 创建 local 用户失败: %v", err)
			return
		}
		// 绑定 admin 角色
		var adminRoleId string
		err = db.Get(&adminRoleId, "SELECT id FROM t_role WHERE name = 'admin' LIMIT 1")
		if err == nil && adminRoleId != "" {
			var userId string
			_ = db.Get(&userId, "SELECT id FROM t_user WHERE login_name = 'local'")
			if userId != "" {
				_, _ = db.Exec("INSERT OR IGNORE INTO t_user_role (id, user_id, role_id) VALUES (?, ?, ?)",
					idgen.RandomStr(), userId, adminRoleId)
			}
		}
		log.Println("[LocalUser] 已创建 local 用户并绑定 admin 角色")
	}

	// 注入会话到缓存（委托给 admin 包，与 middleware 自愈共用同一逻辑）
	if admin.EnsureLocalSession() {
		log.Printf("[LocalUser] 本地模式自动登录成功 (user=local)")
	} else {
		log.Printf("[LocalUser] 注入本地会话失败")
	}
}
