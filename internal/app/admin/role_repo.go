package admin

import (
	"bytes"
	"fmt"
	"log"
	"strings"

	"websql/internal/config"
	"websql/internal/pkg/dberr"
	"websql/internal/pkg/idgen"

	"github.com/jmoiron/sqlx"
)

// RoleRepo 定义角色数据访问接口，所有 SQL 查询均在此实现
type RoleRepo interface {
	FindRoleList() ([]*Role, error)
	FindRoleBaseList() ([]*Role, error)
	FindUsersByRoleId(roleId string) ([]*User, error)
	FindConnByRole(roleIdList []any) (map[string][]*PowerDto, error)
	FindPowerDetails(roleIdList []any) (map[string][]*PowerDetail, error)
	SaveRole(role *RoleSave) error
	SaveRolePermission(role *RoleSave) error
	RolePermissionDigest(roleID string) (string, error)
	DeleteRole(id string) error
}

type roleRepo struct {
	db *sqlx.DB
}

// NewRoleRepo 创建 RoleRepo 实例，接受 *sqlx.DB 以便未来依赖注入
func NewRoleRepo(db *sqlx.DB) RoleRepo {
	return &roleRepo{db: db}
}

func (r *roleRepo) FindRoleList() ([]*Role, error) {
	roleList := []*Role{}
	err := r.db.Select(&roleList, "select id, name, view_classic, allow_modify, ai_allow_modify from t_role")
	return roleList, err
}

func (r *roleRepo) FindRoleBaseList() ([]*Role, error) {
	roleList := []*Role{}
	err := r.db.Select(&roleList, "select id, name, view_classic, allow_modify, ai_allow_modify from t_role")
	return roleList, err
}

func (r *roleRepo) FindUsersByRoleId(roleId string) ([]*User, error) {
	userList := []*User{}
	err := r.db.Select(&userList, "select * from t_user where role_id = ?", roleId)
	return userList, err
}

func (r *roleRepo) FindConnByRole(roleIdList []any) (map[string][]*PowerDto, error) {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[string][]*PowerDto{}, nil
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select p.id, p.role_id, p.conn_id, c.name conn_name from t_power p left join t_conn c on p.conn_id = c.id where p.role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") ")
	powerList := []*PowerDto{}
	err := r.db.Select(&powerList, sqlBuf.String(), roleIdList...)
	if err != nil {
		return nil, err
	}
	rolePowerMap := make(map[string][]*PowerDto, len(powerList))
	for _, power := range powerList {
		v, ok := rolePowerMap[power.RoleId]
		if !ok {
			v = []*PowerDto{}
		}
		v = append(v, power)
		rolePowerMap[power.RoleId] = v
	}
	return rolePowerMap, nil
}

func (r *roleRepo) FindPowerDetails(roleIdList []any) (map[string][]*PowerDetail, error) {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[string][]*PowerDetail{}, nil
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select p.id, p.role_id, p.conn_id, p.schema_name, p.table_name, p.column_name, p.power_level, c.name conn_name from t_power p left join t_conn c on p.conn_id = c.id where p.role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") order by p.power_level, p.schema_name, p.table_name, p.column_name")
	powerList := []*PowerDetail{}
	err := r.db.Select(&powerList, sqlBuf.String(), roleIdList...)
	if err != nil {
		return nil, err
	}
	rolePowerMap := make(map[string][]*PowerDetail, len(powerList))
	for _, power := range powerList {
		v, ok := rolePowerMap[power.RoleId]
		if !ok {
			v = []*PowerDetail{}
		}
		v = append(v, power)
		rolePowerMap[power.RoleId] = v
	}
	return rolePowerMap, nil
}

func (r *roleRepo) SaveRole(role *RoleSave) error {
	_, err := r.saveRoleInternal(role)
	return err
}

// saveRoleInternal 在单一事务内完成「冲突检测 + 角色写入 + 权限增删 + 修订号递增」。
// 返回提交后的权限摘要。
func (r *roleRepo) saveRoleInternal(role *RoleSave) (string, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// 冲突检测必须与写入同事务：先锁定角色行（MySQL 行锁），再基于库内当前快照计算摘要，
	// 防止两个管理员同时通过校验后互相覆盖。
	existing, err := loadRoleForUpdate(tx, role.Id)
	if err != nil {
		return "", err
	}
	if existing != nil && role.ExpectedDigest != "" {
		existingPowers, err := loadPowersTx(tx, role.Id)
		if err != nil {
			return "", err
		}
		digest := computePermissionDigest(existing.Id, existingPowers, existing.ViewClassic, existing.AllowModify, existing.AIAllowModify)
		if digest != role.ExpectedDigest {
			return "", ErrRolePermissionConflict
		}
	}

	if role.Id == "" {
		role.Id = idgen.RandomStr()
		// 新建角色的 AI 写能力：显式配置优先，缺省时与全局写能力保持一致
		// （兼容期行为；AIPolicyEnforce 打开后由服务层显式写入 0）。
		aiAllow := role.AllowModify
		if role.AIAllowModify != nil {
			aiAllow = *role.AIAllowModify
		}
		if _, err := tx.Exec("insert into t_role (id, name, view_classic, allow_modify, ai_allow_modify) values (?, ?, ?, ?, ?)",
			role.Id, role.Name, role.ViewClassic, role.AllowModify, aiAllow); err != nil {
			return "", err
		}
	} else {
		if role.AIAllowModify != nil {
			_, err = tx.Exec("update t_role set name = ?, view_classic = ?, allow_modify = ?, ai_allow_modify = ? where id = ?",
				role.Name, role.ViewClassic, role.AllowModify, *role.AIAllowModify, role.Id)
		} else {
			// 旧客户端未提交新字段：保持数据库原值，避免把 AI 开关意外重置
			_, err = tx.Exec("update t_role set name = ?, view_classic = ?, allow_modify = ? where id = ?",
				role.Name, role.ViewClassic, role.AllowModify, role.Id)
		}
		if err != nil {
			return "", err
		}
	}

	if len(role.AddPowers) > 0 {
		if err := insertPowers(tx, role.Id, role.AddPowers); err != nil {
			return "", err
		}
	}
	if len(role.DelPowers) > 0 {
		if err := deletePowers(tx, role.Id, role.DelPowers); err != nil {
			return "", err
		}
	}
	// 权限数据与修订号必须原子提交：崩溃后其他实例仍能看到权限回收
	if err := BumpPermissionRevisionTx(tx); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	syncRevisionAfterCommit()

	// 返回提交后的新摘要，供前端刷新 baseline；读取失败不影响保存结果。
	digest, digErr := r.RolePermissionDigest(role.Id)
	if digErr != nil {
		log.Printf("[Role] 保存后计算权限摘要失败 - role=%s, err=%v", role.Id, digErr)
	}
	return digest, nil
}

func (r *roleRepo) SaveRolePermission(role *RoleSave) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	existing, err := loadRoleForUpdate(tx, role.Id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrRolePermissionConflict
	}
	if role.ExpectedDigest != "" {
		existingPowers, err := loadPowersTx(tx, role.Id)
		if err != nil {
			return err
		}
		digest := computePermissionDigest(existing.Id, existingPowers, existing.ViewClassic, existing.AllowModify, existing.AIAllowModify)
		if digest != role.ExpectedDigest {
			return ErrRolePermissionConflict
		}
	}

	if len(role.AddPowers) > 0 {
		if err := insertPowers(tx, role.Id, role.AddPowers); err != nil {
			return err
		}
	}
	if len(role.DelPowers) > 0 {
		if err := deletePowers(tx, role.Id, role.DelPowers); err != nil {
			return err
		}
	}
	if err := BumpPermissionRevisionTx(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	syncRevisionAfterCommit()
	return nil
}

// RolePermissionDigest 返回角色当前权限摘要。
func (r *roleRepo) RolePermissionDigest(roleID string) (string, error) {
	role := &Role{}
	err := r.db.Get(role,
		"select id, name, view_classic, allow_modify, ai_allow_modify from t_role where id = ?", roleID)
	if err != nil {
		return "", err
	}
	powers := []*PowerDetail{}
	if err := r.db.Select(&powers,
		"select id, role_id, conn_id, schema_name, table_name, column_name, power_level from t_power where role_id = ?", roleID); err != nil {
		return "", err
	}
	return computePermissionDigest(role.Id, powers, role.ViewClassic, role.AllowModify, role.AIAllowModify), nil
}

// loadRoleForUpdate 在事务内读取角色当前配置。
// MySQL 使用 SELECT ... FOR UPDATE 拿行锁；SQLite 不支持该语法，由写事务序列化保证。
func loadRoleForUpdate(tx *sqlx.Tx, roleID string) (*Role, error) {
	if roleID == "" {
		return nil, nil
	}
	query := "select id, name, view_classic, allow_modify, ai_allow_modify from t_role where id = ?"
	if isMySQLDriver() {
		query += " for update"
	}
	role := &Role{}
	if err := tx.Get(role, query, roleID); err != nil {
		if dberr.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return role, nil
}

func loadPowersTx(tx *sqlx.Tx, roleID string) ([]*PowerDetail, error) {
	powers := []*PowerDetail{}
	err := tx.Select(&powers, `
		select id, role_id, conn_id, schema_name, table_name, column_name, power_level
		from t_power where role_id = ?
		order by power_level, schema_name, table_name, column_name`, roleID)
	return powers, err
}

func isMySQLDriver() bool {
	cfg := config.Get()
	if cfg == nil {
		return false
	}
	driver := strings.ToLower(cfg.DB.DriverName)
	return strings.Contains(driver, "mysql") || strings.Contains(driver, "mariadb")
}

func (r *roleRepo) DeleteRole(id string) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("delete from t_power where role_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("delete from t_user_role where role_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("delete from t_role where id = ?", id); err != nil {
		return err
	}
	if err := BumpPermissionRevisionTx(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	syncRevisionAfterCommit()
	return nil
}

// syncRevisionAfterCommit 在权限事务提交后立即刷新本实例修订号。
//
// 失败只告警：提交事实已生效，其他实例由 500ms 轮询对账；
// 本实例在租约过期后会因快照不可信而 fail-closed，而不是继续使用旧 Scope。
func syncRevisionAfterCommit() {
	if err := SyncPermissionRevision(); err != nil {
		log.Printf("[PermRevision] 提交后刷新本地修订号失败 - err=%v", err)
	}
}

func insertPowers(tx *sqlx.Tx, roleId string, powers []*PowerDetail) error {
	if len(powers) == 0 {
		return nil
	}

	values := make([]string, 0, len(powers))
	args := make([]any, 0, len(powers)*7)

	for _, power := range powers {
		switch power.Level {
		case "conn":
			power.SchemaName = nil
			power.TableName = nil
			power.ColumnName = nil
		case "schema":
			power.TableName = nil
			power.ColumnName = nil
		case "table":
			power.ColumnName = nil
		}

		id := idgen.RandomStr()
		values = append(values, "(?, ?, ?, ?, ?, ?, ?)")
		args = append(args,
			id,
			roleId,
			power.ConnId,
			ptrToString(power.SchemaName),
			ptrToString(power.TableName),
			ptrToString(power.ColumnName),
			power.Level,
		)
	}

	sql := "insert into t_power (id, role_id, conn_id, schema_name, table_name, column_name, power_level) values " + strings.Join(values, ", ")
	_, err := tx.Exec(sql, args...)
	return err
}

func deletePowers(tx *sqlx.Tx, roleId string, powers []*PowerDetail) error {
	if len(powers) == 0 {
		return nil
	}

	type powerKey struct {
		level  string
		connId string
		schema string
		table  string
	}

	connIds := make([]any, 0)
	for _, p := range powers {
		if p.Level == "conn" {
			connIds = append(connIds, p.ConnId)
		}
	}
	if len(connIds) > 0 {
		placeholders := make([]string, len(connIds))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		sql := fmt.Sprintf("delete from t_power where role_id = ? and conn_id in (%s) and power_level = 'conn'", strings.Join(placeholders, ","))
		args := append([]any{roleId}, connIds...)
		_, err := tx.Exec(sql, args...)
		if err != nil {
			return err
		}
	}

	schemaPowers := make(map[string][]string)
	for _, p := range powers {
		if p.Level == "schema" && p.SchemaName != nil {
			key := p.ConnId
			schemaPowers[key] = append(schemaPowers[key], *p.SchemaName)
		}
	}
	for connId, schemas := range schemaPowers {
		if len(schemas) == 1 {
			if _, err := tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and power_level = 'schema'",
				roleId, connId, schemas[0]); err != nil {
				return err
			}
		} else {
			placeholders := make([]string, len(schemas))
			for i := range placeholders {
				placeholders[i] = "?"
			}
			sql := fmt.Sprintf("delete from t_power where role_id = ? and conn_id = ? and schema_name in (%s) and power_level = 'schema'", strings.Join(placeholders, ","))
			args := append([]any{roleId, connId}, schemasToInterfaces(schemas)...)
			_, err := tx.Exec(sql, args...)
			if err != nil {
				return err
			}
		}
	}

	// 表/列删除必须检查错误：权限删除静默失败会导致「以为已回收、实际仍可访问」，
	// 而修订号已递增，其他实例不会再次重试。
	for _, p := range powers {
		if p.Level == "table" && p.TableName != nil {
			if _, err := tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and table_name = ? and power_level = 'table'",
				roleId, p.ConnId, ptrToString(p.SchemaName), *p.TableName); err != nil {
				return err
			}
		}
	}

	for _, p := range powers {
		if p.Level == "column" && p.ColumnName != nil {
			if _, err := tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and table_name = ? and column_name = ? and power_level = 'column'",
				roleId, p.ConnId, ptrToString(p.SchemaName), ptrToString(p.TableName), *p.ColumnName); err != nil {
				return err
			}
		}
	}
	return nil
}

func schemasToInterfaces(schemas []string) []any {
	result := make([]any, len(schemas))
	for i, v := range schemas {
		result[i] = v
	}
	return result
}

func ptrToString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
