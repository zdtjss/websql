package admin

import (
	"fmt"
	"log"
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

// InvalidateByUserId 清除指定用户的所有缓存条目（权限变更时调用）
func (c *authLocalCache) InvalidateByUserId(userId string) {
	c.mu.Lock()
	for k, v := range c.entries {
		if v.userPower != nil && v.userPower.UserId == userId {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

// InvalidateAll 清除所有缓存条目（角色权限变更时调用，因为无法确定影响哪些用户）
func InvalidateAllAuthCache() {
	authCache.mu.Lock()
	authCache.entries = make(map[string]*authCacheEntry, 64)
	authCache.mu.Unlock()
	// 通知外部缓存失效（如 Permission Agent Cache）
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
	_, power := GetCachedUserAndPower(authorization)
	return power
}

func GetUser(authorization string) *User {
	user, _ := GetCachedUserAndPower(authorization)
	return user
}

func CheckAdminPower(c *gin.Context) bool {
	if !config.Cfg.IsRemote {
		return true
	}
	authorization := appctx.Ctx.GetAuthorization(c)
	var userPower = GetUserPower(authorization)
	// 支持多管理员：检查用户角色是否有管理员标记（role name 为 "admin" 或 "管理员"）
	if isUserAdmin(userPower.UserId) {
		return true
	}
	response.WriteAuthErr(c, "无权访问")
	return false
}

// isUserAdmin 检查用户是否拥有管理员角色
func isUserAdmin(userId string) bool {
	roles := FindUserRoles(userId)
	for _, role := range roles {
		name := strings.ToLower(role.Name)
		if name == "admin" || name == "管理员" || name == "administrator" {
			return true
		}
	}
	return false
}

func formatStoreKey(key string) string {
	return fmt.Sprintf("USER:POWER:%s", key)
}
