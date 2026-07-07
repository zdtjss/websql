package admin

// 本文件集中存放供 Wails binding 直接调用的包级委托函数。
// 命名采用 <Method>ByService 后缀，与 snippet/conn/dbops/backup 包保持一致。
// 这些函数委托到 defaultUserService / defaultRoleService，与对应 HTTP handler 共用同一份业务逻辑。
//
// admin 模块业务复杂，部分 handler (GetPermissionTree, ListBackupData, ShowBackupData,
// CanUseClassicView, CanModifyData, UserPermissions, Prompt*) 是内联实现，尚未提取 service。
// 这些方法在 binding 中暂时直接复用包内函数（如 findUserPowerDetails），
// 后续按需要再提取 service。

import (
	"websql/internal/logger"
	"websql/internal/pkg/idgen"
	"websql/internal/store"
)

// ===== UserService 委托 =====

func FindUserBaseByService(loginName, key string) ([]*SharedUser, error) {
	ensureDefaultUser()
	return defaultUserService.FindUserBase(loginName, key)
}

func FindUserByService(roleId, name, loginName, key string, userIdList []string) ([]*User, error) {
	ensureDefaultUser()
	return defaultUserService.FindUser(roleId, name, loginName, key, userIdList)
}

func SaveUserByService(user *User, currentUserId, currentUserName string) error {
	ensureDefaultUser()
	return defaultUserService.Save(user, currentUserId, currentUserName)
}

func SaveUserBioByService(userId, bioKey string) error {
	ensureDefaultUser()
	return defaultUserService.SaveUserBio(userId, bioKey)
}

func ChangePasswordByService(userId, oldPwd, newPwd string) error {
	ensureDefaultUser()
	return defaultUserService.ChangePassword(userId, oldPwd, newPwd)
}

func InitUserByService() error {
	ensureDefaultUser()
	return defaultUserService.InitUser()
}

func DeleteUserByService(id string) error {
	ensureDefaultUser()
	return defaultUserService.Delete(id)
}

// ===== RoleService 委托 =====

func SaveRoleByService(role *RoleSave, currentUserId, currentUserName string) error {
	ensureDefaultRole()
	return defaultRoleService.SaveRole(role, currentUserId, currentUserName)
}

func DeleteRoleByService(id, currentUserId, currentUserName string) error {
	ensureDefaultRole()
	return defaultRoleService.DeleteRole(id, currentUserId, currentUserName)
}

func RoleListByService() ([]*Role, error) {
	ensureDefaultRole()
	return defaultRoleService.RoleList()
}

func RoleBaseListByService() ([]*Role, error) {
	ensureDefaultRole()
	return defaultRoleService.RoleBaseList()
}

func FindUserByRoleByService(roleId string) ([]*User, error) {
	ensureDefaultRole()
	return defaultRoleService.FindUserByRole(roleId)
}

func SaveRolePermissionByService(role *RoleSave, currentUserId, currentUserName string) error {
	ensureDefaultRole()
	return defaultRoleService.SaveRolePermission(role, currentUserId, currentUserName)
}

// ===== Login 委托 =====

// LoginResult 是登录返回结构，与 HTTP handler 响应一致。
type LoginResult struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	IsAdmin        bool   `json:"isAdmin"`
	Authentication string `json:"authentication"`
}

// LoginByService 执行登录校验并生成 token，返回登录结果。
// 业务逻辑来自原 login.go 的 Login handler。
// 桌面模式默认 IsRemote=false，登录主要用于多用户切换场景。
func LoginByService(loginName, pwd, key, loginType string) (*LoginResult, error) {
	var user *User
	switch loginType {
	case "pwd":
		u, err := findByLoginName(loginName)
		if err != nil {
			return nil, err
		}
		user = u
		if user == nil || !CheckPassword(pwd, user.Pwd) {
			return nil, errLoginInvalidCredentials
		}
		user.LoginName = loginName
	case "bio":
		u, err := findByBio(key)
		if err != nil {
			return nil, err
		}
		user = u
		if user == nil {
			return nil, errLoginInvalidBio
		}
	case "token":
		u, err := findByToken(key)
		if err != nil {
			return nil, err
		}
		user = u
		if user == nil {
			return nil, errLoginInvalidToken
		}
	default:
		return nil, errLoginUnsupportedType
	}

	power := findUserPower(user.Id)
	token := idgen.SecureRandomToken()
	userPowerVal := UserPower{UserId: user.Id, Power: power}
	store.Add(formatStoreKey(token), userPowerVal)
	user.Pwd = ""
	store.Add(formatStoreKey(token+"_user"), user)
	authCache.set(token, user, &userPowerVal)

	return &LoginResult{
		ID:             user.Id,
		Name:           user.Name,
		IsAdmin:        isUserAdmin(user.Id),
		Authentication: token,
	}, nil
}

// LogoutByService 注销 token。
func LogoutByService(token string) {
	store.Remove(formatStoreKey(token))
	authCache.remove(token)
}

// ===== 权限/视图标记 委托 =====

// UserPermissionsByService 返回当前用户的权限键列表。
// 键格式: connId[::schemaName[::tableName[::columnName]]]，与 HTTP handler 完全一致。
// 桌面模式 IsRemote=false 时由 binding 层直接返回 ["__all__"]，无需调用本函数。
func UserPermissionsByService(authorization string) []string {
	user := GetUser(authorization)
	if user == nil {
		return []string{}
	}
	powerList := FindUserPowerDetails(user.Id)

	permissionKeys := make([]string, 0, len(powerList))
	for _, power := range powerList {
		key := power.ConnId
		if power.SchemaName != nil && *power.SchemaName != "" {
			key += "::" + *power.SchemaName
		}
		if power.TableName != nil && *power.TableName != "" {
			key += "::" + *power.TableName
		}
		if power.ColumnName != nil && *power.ColumnName != "" {
			key += "::" + *power.ColumnName
		}
		permissionKeys = append(permissionKeys, key)
	}
	return permissionKeys
}

// CanModifyDataByService 检查当前用户的角色是否允许修改数据。
// 业务逻辑来自 misc.go 的 CanModifyData handler。
func CanModifyDataByService(authorization string) bool {
	user := GetUser(authorization)
	if user == nil {
		return false
	}
	roles := []*Role{}
	err := getDB().Select(&roles,
		"select r.id, r.name, r.allow_modify from t_role r inner join t_user_role ur on r.id = ur.role_id where ur.user_id = ?", user.Id)
	if err != nil {
		logger.PrintErr(err)
		return false
	}
	for _, role := range roles {
		if role.AllowModify > 0 {
			return true
		}
	}
	return false
}

// CanUseClassicViewByService 检查当前用户的角色是否允许使用经典视图。
// 业务逻辑来自 misc.go 的 CanUseClassicView handler。
func CanUseClassicViewByService(authorization string) bool {
	user := GetUser(authorization)
	if user == nil {
		return false
	}
	roles := []*Role{}
	err := getDB().Select(&roles,
		"select r.id, r.view_classic from t_role r inner join t_user_role ur on r.id = ur.role_id where ur.user_id = ?", user.Id)
	if err != nil {
		logger.PrintErr(err)
		return false
	}
	for _, role := range roles {
		if role.ViewClassic > 0 {
			return true
		}
	}
	return false
}

// ===== 错误定义 (供 binding 用 errors.Is 区分) =====

var (
	errLoginInvalidCredentials = &loginError{msg: "用户名或密码不正确"}
	errLoginInvalidBio          = &loginError{msg: "无效的指纹/面容信息"}
	errLoginInvalidToken        = &loginError{msg: "传入的登录信息无效"}
	errLoginUnsupportedType     = &loginError{msg: "不支持的登录方式"}
)

type loginError struct{ msg string }

func (e *loginError) Error() string { return e.msg }
