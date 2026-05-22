package admin

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"websql/internal/config"
	"websql/internal/logger"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"
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
	go func() {
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
	}()
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
		user = findByLoginName(loginName)
		if user == nil || user.Pwd != Md5sum(pwd) {
			c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "用户名或密码不正确"})
			return
		}
		user.LoginName = loginName
	case "bio":
		user = findByBio(key)
		if user == nil {
			c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "无效的指纹/面容信息"})
			return
		}
	case "token":
		user = findByToken(key)
		if user == nil {
			c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "传入的登录信息无效"})
			return
		}
	default:
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "不支持的登录方式"})
		return
	}
	power := findUserPower(user.Id)
	token := idgen.SecureRandomToken()
	if loginType == "token" {
		token = key
	}
	c.Header("Authentication", token)
	userPowerVal := UserPower{UserId: user.Id, Power: power}
	store.Add(formatStoreKey(token), userPowerVal)
	user.Pwd = ""
	store.Add(formatStoreKey(token+"_user"), user)
	authCache.set(token, user, &userPowerVal)
	jsonutil.WriteJson(c.Writer, map[string]any{"id": user.Id, "name": user.Name, "isAdmin": user.Id == config.AdminId, "authentication": token})
}

func Logout(c *gin.Context) {
	key := c.GetHeader("Authentication")
	store.Remove(formatStoreKey(key))
	authCache.remove(key)
	jsonutil.WriteJson(c.Writer, "退出成功")
}

func GetUserPower(authorization string) *UserPower {
	_, power := GetCachedUserAndPower(authorization)
	return power
}

func GetUser(authorization string) *User {
	user, _ := GetCachedUserAndPower(authorization)
	return user
}

func CheckAdminPower(c *gin.Context) {
	if !config.Cfg.IsRemote {
		return
	}
	authorization := c.GetHeader("Authorization")
	var userPower = GetUserPower(authorization)
	if userPower.UserId != config.AdminId {
		logger.PanicErr(errors.New("无权访问"))
	}
}

func formatStoreKey(key string) string {
	return fmt.Sprintf("USER:POWER:%s", key)
}