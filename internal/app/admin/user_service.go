package admin

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"websql/internal/config"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/lazyinit"
	"websql/internal/pkg/safego"

	"golang.org/x/crypto/bcrypt"
)

// UserService 封装用户相关的业务逻辑：密码哈希、校验、审计记录等
type UserService interface {
	FindByLoginName(loginName string) (*User, error)
	FindByBio(bioKey string) (*User, error)
	FindByToken(token string) (*User, error)
	FindUserBase(loginName, key string) ([]*SharedUser, error)
	FindUser(roleId, name, loginName, key string, userIdList []string) ([]*User, error)
	Save(user *User, currentUserId, currentUserName string) error
	SaveUserBio(userId, bioKey string) error
	ChangePassword(userId, oldPwd, newPwd string) error
	InitUser() error
	Delete(id string) error
}

type userService struct {
	repo UserRepo
}

// NewUserService 创建 UserService 实例
func NewUserService(repo UserRepo) UserService {
	return &userService{repo: repo}
}

// 默认实例：lazyinit.Holder 替代散落的 sync.Once + 包级变量模式。
// user 包需两个 Holder：部分包级函数直接访问 repo（FindUserPower 等），
// 另一些通过 service 委托。
var (
	defaultUserRepoHolder = &lazyinit.Holder[UserRepo]{}
	defaultUserSvcHolder  = &lazyinit.Holder[UserService]{}
)

func getDefaultUserRepo() UserRepo {
	return defaultUserRepoHolder.Get(func() UserRepo {
		return NewUserRepo(getDB())
	})
}

func getDefaultUserService() UserService {
	return defaultUserSvcHolder.Get(func() UserService {
		return NewUserService(getDefaultUserRepo())
	})
}

// FindByLoginName 按登录名查询用户
func (s *userService) FindByLoginName(loginName string) (*User, error) {
	return s.repo.FindByLoginName(loginName)
}

// FindByBio 按指纹/面容信息查询用户
func (s *userService) FindByBio(bioKey string) (*User, error) {
	return s.repo.FindByBio(Md5sum(bioKey))
}

// FindByToken 通过外部 token 服务校验并返回本地用户
func (s *userService) FindByToken(token string) (*User, error) {
	if user, ok := tokenCache.get(token); ok {
		return user, nil
	}

	cfg := config.Get()
	req, err := http.NewRequest("GET", cfg.OutterUser, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var outterUser struct {
		Code uint16         `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	err = json.Unmarshal(body, &outterUser)
	if err != nil {
		return nil, err
	}

	log.Println(string(jsonutil.ToJsonString(outterUser)))

	user, err := s.repo.FindByLoginNameForToken(outterUser.Data["employeeId"])
	if err != nil {
		return nil, err
	}

	if user == nil {
		return nil, nil
	}

	tokenCache.set(token, user)

	return user, nil
}

// FindUserBase 查询用户基础信息列表
func (s *userService) FindUserBase(loginName, key string) ([]*SharedUser, error) {
	return s.repo.FindUserBaseList(loginName, key)
}

// FindUser 查询用户列表并填充角色信息
func (s *userService) FindUser(roleId, name, loginName, key string, userIdList []string) ([]*User, error) {
	userList, err := s.repo.FindUserList(roleId, name, loginName, key, userIdList)
	if err != nil {
		return nil, err
	}

	userIds := []any{}
	for _, user := range userList {
		userIds = append(userIds, user.Id)
	}
	userRoleMap, err := s.repo.FindUserRole(userIds)
	if err != nil {
		return nil, err
	}
	for _, user := range userList {
		user.Pwd = ""
		roleIds := []*string{}
		roleNames := []*string{}
		for _, userRole := range userRoleMap[user.Id] {
			roleIds = append(roleIds, &userRole.RoleId)
			roleNames = append(roleNames, &userRole.RoleName)
		}
		user.RoleId = roleIds
		user.RoleName = roleNames
	}

	return userList, nil
}

// Save 保存用户，包含密码哈希、校验与审计记录
func (s *userService) Save(user *User, currentUserId, currentUserName string) error {
	if err := s.repo.CheckUserExist(user); err != nil {
		return err
	}
	if user.Id == "" {
		hashedPwd, err := HashPassword(user.Pwd)
		if err != nil {
			return err
		}
		user.Pwd = hashedPwd
	} else {
		pwdDb, err := s.repo.GetPassword(user.Id)
		if err != nil {
			return err
		}
		if user.Pwd == "" || CheckPassword(user.Pwd, pwdDb) {
			user.Pwd = pwdDb
		} else {
			hashedPwd, err := HashPassword(user.Pwd)
			if err != nil {
				return err
			}
			user.Pwd = hashedPwd
		}
	}
	if err := s.repo.Save(user); err != nil {
		return err
	}
	recordPermissionAudit("save_user", fmt.Sprintf("用户 %s (id=%s, loginName=%s) 保存", user.Name, user.Id, user.LoginName), currentUserId, currentUserName)
	return nil
}

// SaveUserBio 保存用户指纹/面容信息
func (s *userService) SaveUserBio(userId, bioKey string) error {
	return s.repo.SaveUserBio(userId, Md5sum(bioKey))
}

// ChangePassword 修改密码，包含旧密码校验
func (s *userService) ChangePassword(userId, oldPwd, newPwd string) error {
	currentPwd, err := s.repo.GetPassword(userId)
	if err != nil {
		return errors.New("用户信息异常")
	}
	if !CheckPassword(oldPwd, currentPwd) {
		return errors.New("旧密码不正确")
	}
	hashedPwd, err := HashPassword(newPwd)
	if err != nil {
		return err
	}
	return s.repo.ChangePassword(userId, hashedPwd)
}

// InitUser 初始化默认管理员账户
func (s *userService) InitUser() error {
	userId := idgen.RandomStr()
	hashedPwd, err := HashPassword("Nway@12345")
	if err != nil {
		return err
	}
	return s.repo.InitUser(userId, "admin", "admin", hashedPwd)
}

// Delete 删除用户
func (s *userService) Delete(id string) error {
	return s.repo.Delete(id)
}

// HashPassword 使用 bcrypt 加密密码
func HashPassword(s string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword 校验明文密码与哈希值是否匹配，兼容旧版 md5
func CheckPassword(plainPassword, hashedPassword string) bool {
	if strings.HasPrefix(hashedPassword, "$2a$") || strings.HasPrefix(hashedPassword, "$2b$") {
		return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword)) == nil
	}
	return Md5sum(plainPassword) == hashedPassword
}

// Md5sum 使用内置 salt 计算 md5
func Md5sum(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	h.Write([]byte("dd5ac9a6fa2da9aaacc3cccca15b9707"))
	return hex.EncodeToString(h.Sum(nil))
}

// ===== 向后兼容的包级别委托函数 =====
// 这些函数被 admin 包内其他文件（login.go / exports.go / misc.go）或外部包调用，
// 保持原有签名不变，委托到 getDefaultUserService() / getDefaultUserRepo()。

func findByLoginName(loginName string) (*User, error) {
	return getDefaultUserService().FindByLoginName(loginName)
}

func findByBio(bioKey string) (*User, error) {
	return getDefaultUserService().FindByBio(bioKey)
}

func findByToken(token string) (*User, error) {
	return getDefaultUserService().FindByToken(token)
}

func findUserPower(userId string) []string {
	return getDefaultUserRepo().FindUserPower(userId)
}

// FindUserPower 导出版本，供 app 包本地模式自动登录使用
func FindUserPower(userId string) []string {
	return findUserPower(userId)
}

func findUserPowerDetails(userId string) []*PowerDetail {
	// 优先走缓存，避免每次权限检查都查询 t_power 表
	if details, ok := powerCache.getDetails(userId); ok {
		return details
	}
	details := getDefaultUserRepo().FindUserPowerDetails(userId)
	powerCache.setDetails(userId, details)
	return details
}

func FindUserPowerDetails(userId string) []*PowerDetail {
	// 优先走缓存，避免每次权限检查都查询 t_power 表
	if details, ok := powerCache.getDetails(userId); ok {
		return details
	}
	details := getDefaultUserRepo().FindUserPowerDetails(userId)
	powerCache.setDetails(userId, details)
	return details
}

func FindUserRoles(userId string) []*Role {
	// 优先走缓存，与权限详情缓存共用同一 TTL 和失效机制
	if roles, ok := powerCache.getRoles(userId); ok {
		return roles
	}
	roles := getDefaultUserRepo().FindUserRoles(userId)
	powerCache.setRoles(userId, roles)
	return roles
}

func findUserRole(userIdList []any) (map[string][]*UserRole, error) {
	return getDefaultUserRepo().FindUserRole(userIdList)
}

// ===== token 缓存（findByToken 使用） =====

type tokenLocalCache struct {
	mu      sync.RWMutex
	entries map[string]*tokenCacheEntry
}

type tokenCacheEntry struct {
	user      *User
	expiresAt time.Time
}

var tokenCache = &tokenLocalCache{
	entries: make(map[string]*tokenCacheEntry, 16),
}

const tokenCacheTTL = 30 * time.Minute

func init() {
	safego.GoWithName("tokencache-cleanup", func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			tokenCache.mu.Lock()
			now := time.Now()
			for k, v := range tokenCache.entries {
				if now.After(v.expiresAt) {
					delete(tokenCache.entries, k)
				}
			}
			tokenCache.mu.Unlock()
		}
	})
}

func (c *tokenLocalCache) get(token string) (*User, bool) {
	c.mu.RLock()
	entry, ok := c.entries[token]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, token)
		c.mu.Unlock()
		return nil, false
	}
	return entry.user, true
}

func (c *tokenLocalCache) set(token string, user *User) {
	c.mu.Lock()
	c.entries[token] = &tokenCacheEntry{
		user:      user,
		expiresAt: time.Now().Add(tokenCacheTTL),
	}
	c.mu.Unlock()
}
