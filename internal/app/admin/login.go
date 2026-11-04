package admin

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"websql/internal/config"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"
	"websql/internal/store"

	"github.com/gin-gonic/gin"
)

type authCacheEntry struct {
	user      *User
	userPower *UserPower
	expiresAt time.Time
}

type authLocalCache struct {
	mu      sync.RWMutex
	entries map[string]*authCacheEntry
}

var authCache = &authLocalCache{
	entries: make(map[string]*authCacheEntry, 64),
}

const authCacheTTL = 10 * time.Second

func init() {
	safego.GoWithName("authcache-cleanup", func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			authCache.mu.Lock()
			now := time.Now()
			for k, v := range authCache.entries {
				if now.After(v.expiresAt) {
					delete(authCache.entries, k)
				}
			}
			authCache.mu.Unlock()
		}
	})
}

func (c *authLocalCache) get(token string) (*User, *UserPower, bool) {
	c.mu.RLock()
	entry, ok := c.entries[token]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, nil, false
	}
	return entry.user, entry.userPower, true
}

func (c *authLocalCache) set(token string, user *User, userPower *UserPower) {
	c.mu.Lock()
	c.entries[token] = &authCacheEntry{
		user:      user,
		userPower: userPower,
		expiresAt: time.Now().Add(authCacheTTL),
	}
	c.mu.Unlock()
}

func (c *authLocalCache) remove(token string) {
	c.mu.Lock()
	delete(c.entries, token)
	c.mu.Unlock()
}

// InvalidateByUserId 清除指定用户的所有缓存条目（该用户权限/角色变更时调用）
func (c *authLocalCache) InvalidateByUserId(userId string) {
	c.mu.Lock()
	for k, v := range c.entries {
		if v.userPower != nil && v.userPower.UserId == userId {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
	// 同步清除该用户的权限详情缓存
	powerCache.invalidateUser(userId)
	// 通知外部缓存失效（Permission Agent / SQLAgent 缓存内含权限快照）
	notifyPermissionChanged()
}

// InvalidateAll 清除所有缓存条目（角色权限变更时调用，因为无法确定影响哪些用户）
func InvalidateAllAuthCache() {
	authCache.mu.Lock()
	authCache.entries = make(map[string]*authCacheEntry, 64)
	authCache.mu.Unlock()
	// 同步清空权限详情缓存（powerCache），确保角色权限变更后立即生效
	invalidatePowerCacheAll()
	// 通知外部缓存失效（如 Permission Agent Cache、permission 包的决策缓存）
	notifyPermissionChanged()
}

// permissionChangedCallbacks 权限变更时的回调列表
var permissionChangedCallbacks []func()
var permCallbackMu sync.Mutex

// OnPermissionChanged 注册权限变更回调（供外部包如 agent 注册缓存失效逻辑）
func OnPermissionChanged(fn func()) {
	permCallbackMu.Lock()
	permissionChangedCallbacks = append(permissionChangedCallbacks, fn)
	permCallbackMu.Unlock()
}

func notifyPermissionChanged() {
	permCallbackMu.Lock()
	cbs := make([]func(), len(permissionChangedCallbacks))
	copy(cbs, permissionChangedCallbacks)
	permCallbackMu.Unlock()
	for _, fn := range cbs {
		fn()
	}
	// 单进程立即生效：权限写入提交后同步刷新本地修订号。
	// 多实例仍由 500ms 轮询对账；刷新失败只告警，不阻断已提交的权限变更。
	if err := SyncPermissionRevision(); err != nil {
		log.Printf("[PermRevision] 权限变更后刷新修订号失败（将由轮询补偿） - err=%v", err)
	}
}

func GetCachedUserAndPower(authorization string) (*User, *UserPower) {
	if user, power, ok := authCache.get(authorization); ok {
		return user, power
	}

	user := new(User)
	store.Get(formatStoreKey(authorization+"_user"), user)
	userPower := new(UserPower)
	store.Get(formatStoreKey(authorization), userPower)

	if user.Id != "" {
		authCache.set(authorization, user, userPower)
	}

	return user, userPower
}

func Login(c *gin.Context) {
	loginName := c.PostForm("name")
	pwd := c.PostForm("password")
	key := c.PostForm("key")
	loginType := c.PostForm("loginType")

	var user *User
	switch loginType {
	case "pwd":
		loginName = strings.TrimSpace(loginName)
		// 空白凭证必须直接拒绝：否则会命中 login_name='' 的空白账号，
		// 且空明文可能匹配 bcrypt('')，形成未认证登录。
		if loginName == "" || strings.TrimSpace(pwd) == "" {
			response.WriteErr(c, 200, 400, "用户名或密码不正确")
			return
		}
		u, err := findByLoginName(loginName)
		if err != nil {
			log.Printf("查询用户失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
			return
		}
		user = u
		if user == nil || !CheckPassword(pwd, user.Pwd) {
			response.WriteErr(c, 200, 400, "用户名或密码不正确")
			return
		}
		user.LoginName = loginName
	case "bio":
		u, err := findByBio(key)
		if err != nil {
			log.Printf("查询用户失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
			return
		}
		user = u
		if user == nil {
			response.WriteErr(c, 200, 400, "无效的指纹/面容信息")
			return
		}
	case "token":
		u, err := findByToken(key)
		if err != nil {
			log.Printf("token登录失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
			return
		}
		user = u
		if user == nil {
			response.WriteErr(c, 200, 400, "传入的登录信息无效")
			return
		}
	default:
		response.WriteErr(c, 200, 400, "不支持的登录方式")
		return
	}
	power := findUserPower(user.Id)
	token := idgen.SecureRandomToken()
	c.Header("Authentication", token)
	userPowerVal := UserPower{UserId: user.Id, Power: power}
	store.Add(formatStoreKey(token), userPowerVal)
	user.Pwd = ""
	store.Add(formatStoreKey(token+"_user"), user)
	authCache.set(token, user, &userPowerVal)
	response.WriteOK(c, map[string]any{"id": user.Id, "name": user.Name, "isAdmin": isUserAdmin(user.Id), "authentication": token})
}

func Logout(c *gin.Context) {
	key := c.GetHeader("Authentication")
	store.Remove(formatStoreKey(key))
	authCache.remove(key)
	response.WriteOK(c, "退出成功")
}

func GetUserPower(authorization string) *UserPower {
	user, power := GetCachedUserAndPower(authorization)
	// 内置管理员的连接权限由当前 t_conn 动态生成：登录时写入的 UserPower 快照
	// 不包含之后新增的连接，这里每次按最新连接清单重算，避免管理员看不到新连接。
	if user != nil && user.Id != "" && isUserAdmin(user.Id) {
		return &UserPower{UserId: user.Id, Power: findUserPower(user.Id)}
	}
	return power
}

func GetUser(authorization string) *User {
	user, _ := GetCachedUserAndPower(authorization)
	return user
}

func CheckAdminPower(c *gin.Context) bool {
	if config.IsLocalMode() {
		return true
	}
	authorization := appctx.Ctx.GetAuthorization(c)
	var userPower = GetUserPower(authorization)
	// 支持多管理员：检查用户角色是否有管理员标记（role name 为 "admin" 或 "管理员"）
	if isUserAdmin(userPower.UserId) {
		return true
	}
	// 已登录但权限不足使用 403：401 会让前端误判为会话过期并清空登录态。
	response.WriteErr(c, http.StatusForbidden, http.StatusForbidden, "无权访问")
	// 必须中断后续 handler：仅写响应不会阻止调用方继续执行，
	// 曾导致忽略返回值的接口（如审计配置保存）在非管理员请求下继续落库
	c.Abort()
	return false
}

// isUserAdmin 检查用户是否拥有管理员角色。
//
// 管理员身份以 t_role.is_admin 显式标志为准；名称匹配仅作为尚未执行
// 角色标志迁移的存量库兜底，避免升级瞬间丢失管理入口。
func isUserAdmin(userId string) bool {
	roles := FindUserRoles(userId)
	for _, role := range roles {
		if role == nil {
			continue
		}
		if role.IsAdmin > 0 {
			return true
		}
		name := strings.ToLower(role.Name)
		if name == "admin" || name == "管理员" || name == "administrator" {
			return true
		}
	}
	return false
}

// IsUserAdmin 导出版本：供权限合并逻辑识别内置管理员的全量数据授权。
func IsUserAdmin(userId string) bool {
	return isUserAdmin(userId)
}

func formatStoreKey(key string) string {
	return fmt.Sprintf("USER:POWER:%s", key)
}

// FormatStoreKey 导出版本，供 app 包本地模式自动登录使用
func FormatStoreKey(key string) string {
	return formatStoreKey(key)
}

// LocalAutoToken 本地/桌面模式自动登录使用的固定 token。
// 定义于 admin 包（底层），供 app 包与 middleware 复用，避免魔法字符串。
const LocalAutoToken = "LOCAL_AUTO_SESSION_TOKEN"

// EnsureLocalSession 重新查询 local 用户并把会话注入缓存（store）。
// 用于：
//   - 本地/桌面模式启动初始化（app.EnsureLocalUser 委托调用）
//   - 本地会话过期后的自愈（middleware 在 401 前调用）
//
// 返回 true 表示会话已成功注入。仅应在 !IsRemote 模式下调用。
func EnsureLocalSession() bool {
	db := getDB()
	if db == nil {
		return false
	}

	var user User
	err := db.Get(&user, "SELECT id, login_name, name, pwd, bio FROM t_user WHERE login_name = 'local'")
	if err != nil || user.Id == "" {
		return false
	}

	power := findUserPower(user.Id)
	userPower := UserPower{UserId: user.Id, Power: power}
	store.Add(formatStoreKey(LocalAutoToken), userPower)
	user.Pwd = ""
	store.Add(formatStoreKey(LocalAutoToken+"_user"), user)
	return true
}
