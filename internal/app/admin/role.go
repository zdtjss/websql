package admin

import (
	"bytes"
	"fmt"
	"strings"

	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type Role struct {
	Id          string         `json:"id"`
	Name        string         `json:"name"`
	PowerList   []*PowerDetail `json:"powerList"`
	ViewClassic int            `json:"viewClassic" db:"view_classic"`
	AllowModify int            `json:"allowModify" db:"allow_modify"`
}

type UserRole struct {
	Id       string `json:"id"`
	UserId   string `json:"userId" db:"user_id"`
	RoleId   string `json:"roleId" db:"role_id"`
	RoleName string `json:"roleName" db:"role_name"`
}

type RoleSave struct {
	Id          string         `json:"id"`
	Name        string         `json:"name"`
	AddPowers   []*PowerDetail `json:"addPowers"`
	DelPowers   []*PowerDetail `json:"delPowers"`
	ViewClassic int            `json:"viewClassic"`
	AllowModify int            `json:"allowModify"`
}

type Power struct {
	Id     string `json:"id"`
	RoleId string `json:"roleId" db:"role_id"`
	ConnId string `json:"connId" db:"conn_id"`
}

type PowerDto struct {
	Id       string  `json:"id"`
	RoleId   string  `json:"roleId" db:"role_id"`
	ConnId   string  `json:"connId" db:"conn_id"`
	ConnName *string `json:"connName" db:"conn_name"`
}

type PowerDetail struct {
	Id         string  `json:"id"`
	RoleId     string  `json:"roleId" db:"role_id"`
	ConnId     string  `json:"connId" db:"conn_id"`
	ConnName   *string `json:"connName" db:"conn_name"`
	SchemaName *string `json:"schemaName,omitempty" db:"schema_name"`
	TableName  *string `json:"tableName,omitempty" db:"table_name"`
	ColumnName *string `json:"columnName,omitempty" db:"column_name"`
	Level      string  `json:"level" db:"power_level"`
}

func SaveRole(c *gin.Context) {
	CheckAdminPower(c)
	role := &RoleSave{}
	jsonutil.UnmarshalJson(c.Request.Body, role)

	tx, _ := database.Mngtdb.Beginx()
	defer tx.Rollback()

	if role.Id == "" {
		role.Id = idgen.RandomStr()
		_, err := tx.Exec("insert into t_role (id, name, view_classic, allow_modify) values (?, ?, ?, ?)", role.Id, role.Name, role.ViewClassic, role.AllowModify)
		logger.PanicErrf("保存角色失败", err)
	} else {
		_, err := tx.Exec("update t_role set name = ?, view_classic = ?, allow_modify = ? where id = ?", role.Name, role.ViewClassic, role.AllowModify, role.Id)
		logger.PanicErrf("更新角色失败", err)
	}

	if len(role.AddPowers) > 0 {
		insertPowers(tx, role.Id, role.AddPowers)
	}
	if len(role.DelPowers) > 0 {
		deletePowers(tx, role.Id, role.DelPowers)
	}

	err := tx.Commit()
	logger.PanicErrf("保存角色失败", err)

	currentUser := GetUser(c.GetHeader("Authorization"))
	recordPermissionAudit("save_role", fmt.Sprintf("角色 %s (id=%s) 保存，新增权限%d条，删除权限%d条", role.Name, role.Id, len(role.AddPowers), len(role.DelPowers)), currentUser.Id, currentUser.Name)

	jsonutil.WriteJson(c.Writer, "保存成功")
}

func insertPowers(tx *sqlx.Tx, roleId string, powers []*PowerDetail) {
	if len(powers) == 0 {
		return
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
	logger.PanicErr(err)
}

func deletePowers(tx *sqlx.Tx, roleId string, powers []*PowerDetail) {
	if len(powers) == 0 {
		return
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
		logger.PanicErr(err)
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
			logger.PanicErr(err)
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

func DelRole(c *gin.Context) {
	CheckAdminPower(c)
	id := c.Query("id")

	tx, _ := database.Mngtdb.Beginx()
	defer tx.Rollback()

	tx.Exec("delete from t_power where role_id = ?", id)
	tx.Exec("delete from t_user_role where role_id = ?", id)
	tx.Exec("delete from t_role where id = ?", id)

	err := tx.Commit()
	logger.PanicErrf("删除角色失败", err)

	currentUser := GetUser(c.GetHeader("Authorization"))
	recordPermissionAudit("del_role", fmt.Sprintf("删除角色 id=%s", id), currentUser.Id, currentUser.Name)

	jsonutil.WriteJson(c.Writer, "")
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func RoleList(c *gin.Context) {
	roleList := []*Role{}
	err := database.Mngtdb.Select(&roleList, "select id, name, view_classic, allow_modify from t_role")
	logger.PanicErr(err)

	roleIdList := make([]any, len(roleList))
	for idx, role := range roleList {
		roleIdList[idx] = role.Id
	}
	rolePowerMap := findPowerDetails(roleIdList)
	for _, role := range roleList {
		role.PowerList = rolePowerMap[role.Id]
	}
	jsonutil.WriteJson(c.Writer, roleList)
}

func RoleBaseList(c *gin.Context) {
	roleList := []*Role{}
	err := database.Mngtdb.Select(&roleList, "select * from t_role")
	logger.PanicErr(err)
	jsonutil.WriteJson(c.Writer, roleList)
}

func FindUserByRole(c *gin.Context) {
	userList := []*User{}
	err := database.Mngtdb.Select(&userList, "select * from t_user where role_id = ?", c.PostForm("roleId"))
	logger.PanicErr(err)
	jsonutil.WriteJson(c.Writer, userList)
}

func SaveRolePermission(c *gin.Context) {
	CheckAdminPower(c)
	role := &RoleSave{}
	jsonutil.UnmarshalJson(c.Request.Body, role)

	tx, _ := database.Mngtdb.Beginx()
	defer tx.Rollback()

	if len(role.AddPowers) > 0 {
		insertPowers(tx, role.Id, role.AddPowers)
	}
	if len(role.DelPowers) > 0 {
		deletePowers(tx, role.Id, role.DelPowers)
	}

	err := tx.Commit()
	logger.PanicErrf("保存权限失败", err)

	currentUser := GetUser(c.GetHeader("Authorization"))
	recordPermissionAudit("save_role_permission", fmt.Sprintf("角色 id=%s 权限变更，新增权限%d条，删除权限%d条", role.Id, len(role.AddPowers), len(role.DelPowers)), currentUser.Id, currentUser.Name)

	jsonutil.WriteJson(c.Writer, "保存成功")
}

func findConnByRole(roleIdList []any) map[string][]*PowerDto {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[string][]*PowerDto{}
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select p.id, p.role_id, p.conn_id, c.name conn_name from t_power p left join t_conn c on p.conn_id = c.id where p.role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") ")
	powerList := []*PowerDto{}
	err := database.Mngtdb.Select(&powerList, sqlBuf.String(), roleIdList...)
	logger.PanicErr(err)
	rolePowerMap := make(map[string][]*PowerDto, len(powerList))
	for _, power := range powerList {
		v, ok := rolePowerMap[power.RoleId]
		if !ok {
			v = []*PowerDto{}
		}
		v = append(v, power)
		rolePowerMap[power.RoleId] = v
	}
	return rolePowerMap
}

func findPowerDetails(roleIdList []any) map[string][]*PowerDetail {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[string][]*PowerDetail{}
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select p.id, p.role_id, p.conn_id, p.schema_name, p.table_name, p.column_name, p.power_level, c.name conn_name from t_power p left join t_conn c on p.conn_id = c.id where p.role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") order by p.power_level, p.schema_name, p.table_name, p.column_name")
	powerList := []*PowerDetail{}
	err := database.Mngtdb.Select(&powerList, sqlBuf.String(), roleIdList...)
	logger.PanicErr(err)
	rolePowerMap := make(map[string][]*PowerDetail, len(powerList))
	for _, power := range powerList {
		v, ok := rolePowerMap[power.RoleId]
		if !ok {
			v = []*PowerDetail{}
		}
		v = append(v, power)
		rolePowerMap[power.RoleId] = v
	}
	return rolePowerMap
}