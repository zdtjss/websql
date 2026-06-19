package admin

import (
	"bytes"
	"fmt"
	"strings"

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
	err := r.db.Select(&roleList, "select id, name, view_classic, allow_modify from t_role")
	return roleList, err
}

func (r *roleRepo) FindRoleBaseList() ([]*Role, error) {
	roleList := []*Role{}
	err := r.db.Select(&roleList, "select * from t_role")
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
	tx, _ := r.db.Beginx()
	defer tx.Rollback()

	if role.Id == "" {
		role.Id = idgen.RandomStr()
		_, err := tx.Exec("insert into t_role (id, name, view_classic, allow_modify) values (?, ?, ?, ?)", role.Id, role.Name, role.ViewClassic, role.AllowModify)
		if err != nil {
			return err
		}
	} else {
		_, err := tx.Exec("update t_role set name = ?, view_classic = ?, allow_modify = ? where id = ?", role.Name, role.ViewClassic, role.AllowModify, role.Id)
		if err != nil {
			return err
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

	return tx.Commit()
}

func (r *roleRepo) SaveRolePermission(role *RoleSave) error {
	tx, _ := r.db.Beginx()
	defer tx.Rollback()

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

	return tx.Commit()
}

func (r *roleRepo) DeleteRole(id string) error {
	tx, _ := r.db.Beginx()
	defer tx.Rollback()

	tx.Exec("delete from t_power where role_id = ?", id)
	tx.Exec("delete from t_user_role where role_id = ?", id)
	tx.Exec("delete from t_role where id = ?", id)

	return tx.Commit()
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
			tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and power_level = 'schema'",
				roleId, connId, schemas[0])
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

	for _, p := range powers {
		if p.Level == "table" && p.TableName != nil {
			tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and table_name = ? and power_level = 'table'",
				roleId, p.ConnId, ptrToString(p.SchemaName), *p.TableName)
		}
	}

	for _, p := range powers {
		if p.Level == "column" && p.ColumnName != nil {
			tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and table_name = ? and column_name = ? and power_level = 'column'",
				roleId, p.ConnId, ptrToString(p.SchemaName), ptrToString(p.TableName), *p.ColumnName)
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
