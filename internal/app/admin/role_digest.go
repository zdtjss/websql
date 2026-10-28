package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrRolePermissionConflict 表示角色权限已被其他管理员修改，保存请求基于过期快照。
//
// 处理约定：HTTP 409 + 前端重新加载并提示合并，禁止自动覆盖重试。
var ErrRolePermissionConflict = errors.New("角色权限已被其他管理员修改，请刷新后重试")

// ErrInvalidRoleSave 表示角色保存参数未通过集中校验。
var ErrInvalidRoleSave = errors.New("角色保存参数无效")

// validPowerLevels 允许的权限层级（与 PowerDetail.Level 一致）。
var validPowerLevels = map[string]bool{
	"conn": true, "schema": true, "table": true, "column": true,
}

// maxPowersPerSave 单次保存允许的权限变更数量上限，防止超大请求拖垮事务。
const maxPowersPerSave = 10000

// normalizeAndValidateRoleSave 集中校验并规范化角色保存请求。
//
// 规范化规则必须与 insertPowers 完全一致，不新增层级推断：
//   - conn   ：清空 schema/table/column
//   - schema ：必须有 schema，清空 table/column
//   - table  ：必须有 schema+table，清空 column
//   - column ：必须有完整路径
//
// 同一权限项不允许同时出现在 add 与 del 中（否则结果取决于执行顺序）。
func normalizeAndValidateRoleSave(role *RoleSave) error {
	if role == nil || strings.TrimSpace(role.Name) == "" {
		return fmt.Errorf("%w: 角色名称不能为空", ErrInvalidRoleSave)
	}
	return normalizeAndValidatePowers(role)
}

// normalizeAndValidatePowers 校验权限列表与能力开关（不含角色名称）。
func normalizeAndValidatePowers(role *RoleSave) error {
	if role == nil {
		return fmt.Errorf("%w: 请求为空", ErrInvalidRoleSave)
	}
	if len(role.AddPowers)+len(role.DelPowers) > maxPowersPerSave {
		return fmt.Errorf("%w: 单次权限变更数量超过限制", ErrInvalidRoleSave)
	}
	if role.AIAllowModify != nil && *role.AIAllowModify != 0 && *role.AIAllowModify != 1 {
		return fmt.Errorf("%w: AI 修改开关取值无效", ErrInvalidRoleSave)
	}
	if role.AllowModify != 0 && role.AllowModify != 1 {
		return fmt.Errorf("%w: 修改数据开关取值无效", ErrInvalidRoleSave)
	}
	if role.ViewClassic != 0 && role.ViewClassic != 1 {
		return fmt.Errorf("%w: 经典视图开关取值无效", ErrInvalidRoleSave)
	}

	seen := make(map[string]string)
	for _, group := range []struct {
		kind   string
		powers []*PowerDetail
	}{{"add", role.AddPowers}, {"del", role.DelPowers}} {
		for _, p := range group.powers {
			if p == nil || strings.TrimSpace(p.ConnId) == "" || !validPowerLevels[p.Level] {
				return fmt.Errorf("%w: 权限项缺少连接或层级无效", ErrInvalidRoleSave)
			}
			if p.RoleId != "" && role.Id != "" && p.RoleId != role.Id {
				return fmt.Errorf("%w: 权限项角色与保存角色不一致", ErrInvalidRoleSave)
			}
			switch p.Level {
			case "conn":
				p.SchemaName, p.TableName, p.ColumnName = nil, nil, nil
			case "schema":
				if p.SchemaName == nil || strings.TrimSpace(*p.SchemaName) == "" {
					return fmt.Errorf("%w: Schema 权限缺少名称", ErrInvalidRoleSave)
				}
				p.TableName, p.ColumnName = nil, nil
			case "table":
				if p.SchemaName == nil || p.TableName == nil || strings.TrimSpace(*p.TableName) == "" {
					return fmt.Errorf("%w: 表权限缺少 Schema 或表名", ErrInvalidRoleSave)
				}
				p.ColumnName = nil
			case "column":
				if p.SchemaName == nil || p.TableName == nil || p.ColumnName == nil ||
					strings.TrimSpace(*p.TableName) == "" || strings.TrimSpace(*p.ColumnName) == "" {
					return fmt.Errorf("%w: 列权限缺少完整资源路径", ErrInvalidRoleSave)
				}
			}

			key := permissionCanonicalKey(p)
			if previous, exists := seen[key]; exists && previous != group.kind {
				return fmt.Errorf("%w: 同一权限项不能同时新增和删除", ErrInvalidRoleSave)
			}
			seen[key] = group.kind
		}
	}
	return nil
}

// permissionCanonicalKey 生成权限项的唯一键（用于 add/del 冲突检测与摘要排序）。
func permissionCanonicalKey(p *PowerDetail) string {
	if p == nil {
		return ""
	}
	return strings.Join([]string{
		strings.ToLower(p.Level),
		strings.TrimSpace(p.ConnId),
		derefTrim(p.SchemaName),
		derefTrim(p.TableName),
		strings.ToLower(derefTrim(p.ColumnName)),
	}, "|")
}

func derefTrim(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// computePermissionDigest 计算角色权限摘要。
//
// 摘要覆盖「权限项集合 + 角色能力开关」，用于并发编辑冲突检测：
// 两个管理员同时打开同一角色时，后提交者会因摘要不一致得到 409。
func computePermissionDigest(roleID string, powers []*PowerDetail, viewClassic, allowModify, aiAllowModify int) string {
	keys := make([]string, 0, len(powers))
	for _, p := range powers {
		keys = append(keys, permissionCanonicalKey(p))
	}
	sort.Strings(keys)

	h := sha256.New()
	fmt.Fprintf(h, "role=%s\nvc=%d\nam=%d\nai=%d\n", roleID, viewClassic, allowModify, aiAllowModify)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
