package admin

import (
	"fmt"
	"strings"

	"websql/internal/pkg/lazyinit"
)

// RoleService 封装角色相关的业务逻辑：审计记录、缓存失效等
type RoleService interface {
	SaveRole(role *RoleSave, currentUserId, currentUserName string) error
	DeleteRole(id, currentUserId, currentUserName string) error
	RoleList() ([]*Role, error)
	RoleBaseList() ([]*Role, error)
	FindUserByRole(roleId string) ([]*User, error)
	SaveRolePermission(role *RoleSave, currentUserId, currentUserName string) error
}

type roleService struct {
	repo RoleRepo
}

// NewRoleService 创建 RoleService 实例
func NewRoleService(repo RoleRepo) RoleService {
	return &roleService{repo: repo}
}

// 默认实例：lazyinit.Holder 替代散落的 sync.Once + 包级变量模式。
var defaultRole = &lazyinit.Holder[RoleService]{}

func getDefaultRole() RoleService {
	return defaultRole.Get(func() RoleService {
		return NewRoleService(NewRoleRepo(getDB()))
	})
}

// SaveRole 保存角色及权限，并记录审计日志、清除认证缓存
func (s *roleService) SaveRole(role *RoleSave, currentUserId, currentUserName string) error {
	if err := s.repo.SaveRole(role); err != nil {
		return err
	}
	recordPermissionAudit("save_role", fmt.Sprintf("角色 %s (id=%s) 保存，新增权限%d条，删除权限%d条，详情: add=%s, del=%s",
		role.Name, role.Id, len(role.AddPowers), len(role.DelPowers),
		summarizePowers(role.AddPowers), summarizePowers(role.DelPowers)), currentUserId, currentUserName)

	// 权限变更后清除认证缓存，确保新权限立即生效
	InvalidateAllAuthCache()
	return nil
}

// DeleteRole 删除角色及关联权限，并记录审计日志、清除认证缓存
func (s *roleService) DeleteRole(id string, currentUserId, currentUserName string) error {
	if err := s.repo.DeleteRole(id); err != nil {
		return err
	}
	recordPermissionAudit("del_role", fmt.Sprintf("删除角色 id=%s", id), currentUserId, currentUserName)

	// 权限变更后清除认证缓存
	InvalidateAllAuthCache()
	return nil
}

// RoleList 查询角色列表并填充权限详情
func (s *roleService) RoleList() ([]*Role, error) {
	roleList, err := s.repo.FindRoleList()
	if err != nil {
		return nil, err
	}

	roleIdList := make([]any, len(roleList))
	for idx, role := range roleList {
		roleIdList[idx] = role.Id
	}
	rolePowerMap, err := s.repo.FindPowerDetails(roleIdList)
	if err != nil {
		return nil, err
	}
	for _, role := range roleList {
		role.PowerList = rolePowerMap[role.Id]
	}
	return roleList, nil
}

// RoleBaseList 查询角色基础列表
func (s *roleService) RoleBaseList() ([]*Role, error) {
	return s.repo.FindRoleBaseList()
}

// FindUserByRole 按角色 ID 查询关联用户
func (s *roleService) FindUserByRole(roleId string) ([]*User, error) {
	return s.repo.FindUsersByRoleId(roleId)
}

// SaveRolePermission 保存角色权限变更，并记录审计日志、清除认证缓存
func (s *roleService) SaveRolePermission(role *RoleSave, currentUserId, currentUserName string) error {
	if err := s.repo.SaveRolePermission(role); err != nil {
		return err
	}
	recordPermissionAudit("save_role_permission", fmt.Sprintf("角色 id=%s 权限变更，新增权限%d条，删除权限%d条，详情: add=%s, del=%s",
		role.Id, len(role.AddPowers), len(role.DelPowers),
		summarizePowers(role.AddPowers), summarizePowers(role.DelPowers)), currentUserId, currentUserName)

	// 权限变更后清除认证缓存
	InvalidateAllAuthCache()
	return nil
}

// summarizePowers 将权限列表序列化为可读的审计摘要
func summarizePowers(powers []*PowerDetail) string {
	if len(powers) == 0 {
		return "[]"
	}
	const maxItems = 10
	var parts []string
	for i, p := range powers {
		if i >= maxItems {
			parts = append(parts, fmt.Sprintf("...(%d more)", len(powers)-maxItems))
			break
		}
		desc := p.Level + ":" + p.ConnId
		if p.SchemaName != nil {
			desc += "/" + *p.SchemaName
		}
		if p.TableName != nil {
			desc += "/" + *p.TableName
		}
		if p.ColumnName != nil {
			desc += "/" + *p.ColumnName
		}
		parts = append(parts, desc)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
