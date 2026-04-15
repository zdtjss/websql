package admin

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

func SaveRole(c *gin.Context) {
	CheckAdminPower(c)
	role := &RoleSave{}
	utils.UnmarshalJson(c.Request.Body, role)

	tx, _ := config.Mngtdb.Beginx()
	defer tx.Rollback()

	// 1. 保存或更新角色基本信息
	if role.Id == "" {
		role.Id = utils.RandomStr()
		_, err := tx.Exec("insert into t_role (id, name) values (?, ?)", role.Id, role.Name)
		logutils.PanicErrf("保存角色失败", err)
	} else {
		_, err := tx.Exec("update t_role set name = ? where id = ?", role.Name, role.Id)
		logutils.PanicErrf("更新角色失败", err)
	}

	// 2. 增量更新权限
	if len(role.AddPowers) > 0 {
		insertPowers(tx, role.Id, role.AddPowers)
	}
	if len(role.DelPowers) > 0 {
		deletePowers(tx, role.Id, role.DelPowers)
	}

	err := tx.Commit()
	logutils.PanicErrf("保存角色失败", err)
	utils.WriteJson(c.Writer, "保存成功")
}

// insertPowers 批量插入权限（使用批量 SQL）
func insertPowers(tx *sqlx.Tx, roleId string, powers []*PowerDetail) {
	if len(powers) == 0 {
		return
	}

	// 构建批量插入 SQL：VALUES (...), (...), (...)
	values := make([]string, 0, len(powers))
	args := make([]interface{}, 0, len(powers)*7)

	for _, power := range powers {
		id := utils.RandomStr()
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
	logutils.PanicErr(err)
}

// deletePowers 批量删除权限（按层级优化）
func deletePowers(tx *sqlx.Tx, roleId string, powers []*PowerDetail) {
	if len(powers) == 0 {
		return
	}

	// 按层级和连接分组，最大化批量效果
	type powerKey struct {
		level  string
		connId string
		schema string
		table  string
	}

	// 连接级权限批量删除
	connIds := make([]interface{}, 0)
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
		args := append([]interface{}{roleId}, connIds...)
		_, err := tx.Exec(sql, args...)
		logutils.PanicErr(err)
	}

	// Schema 级权限批量删除
	schemaPowers := make(map[string][]string) // connId -> [schema1, schema2, ...]
	for _, p := range powers {
		if p.Level == "schema" && p.SchemaName != nil {
			key := p.ConnId
			schemaPowers[key] = append(schemaPowers[key], *p.SchemaName)
		}
	}
	for connId, schemas := range schemaPowers {
		if len(schemas) == 1 {
			tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and power_level = 'schema'",
				roleId, connId, schemas[0], "schema")
		} else {
			placeholders := make([]string, len(schemas))
			for i := range placeholders {
				placeholders[i] = "?"
			}
			sql := fmt.Sprintf("delete from t_power where role_id = ? and conn_id = ? and schema_name in (%s) and power_level = 'schema'", strings.Join(placeholders, ","))
			args := append([]interface{}{roleId, connId}, schemasToInterfaces(schemas)...)
			args = append(args, "schema")
			_, err := tx.Exec(sql, args...)
			logutils.PanicErr(err)
		}
	}

	// 表级权限批量删除（逐条处理，因为需要精确匹配）
	for _, p := range powers {
		if p.Level == "table" && p.TableName != nil {
			tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and table_name = ? and power_level = 'table'",
				roleId, p.ConnId, ptrToString(p.SchemaName), *p.TableName, "table")
		}
	}

	// 字段级权限批量删除（逐条处理，因为需要精确匹配）
	for _, p := range powers {
		if p.Level == "column" && p.ColumnName != nil {
			tx.Exec("delete from t_power where role_id = ? and conn_id = ? and schema_name = ? and table_name = ? and column_name = ? and power_level = 'column'",
				roleId, p.ConnId, ptrToString(p.SchemaName), ptrToString(p.TableName), *p.ColumnName, "column")
		}
	}
}

// schemasToInterfaces 将字符串切片转为 interface 切片
func schemasToInterfaces(schemas []string) []interface{} {
	result := make([]interface{}, len(schemas))
	for i, v := range schemas {
		result[i] = v
	}
	return result
}

// ptrToString 将字符串指针转为字符串
func ptrToString(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func DelRole(c *gin.Context) {
	CheckAdminPower(c)
	id := c.Query("id")

	userCount := 0
	config.Mngtdb.Select(&userCount, "select count(*) from t_user_role where role_id = ?", id)
	if userCount > 0 {
		logutils.PanicErr(errors.New("有用户，不能删。"))
	}

	powerCount := 0
	config.Mngtdb.Select(&powerCount, "select count(*) from t_power where role_id = ?", id)
	if powerCount > 0 {
		logutils.PanicErr(errors.New("有权限，不能删。"))
	}

	config.Mngtdb.Exec("delete from t_role where id = ?", id)
	utils.WriteJson(c.Writer, "")
}

// nullIfEmpty 空字符串转 nil
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func RoleList(c *gin.Context) {
	roleList := []*Role{}
	err := config.Mngtdb.Select(&roleList, "select * from t_role")
	logutils.PanicErr(err)

	roleIdList := make([]any, len(roleList))
	for idx, role := range roleList {
		roleIdList[idx] = role.Id
	}
	rolePowerMap := findPowerDetails(roleIdList)
	for _, role := range roleList {
		role.PowerList = rolePowerMap[role.Id]
	}
	utils.WriteJson(c.Writer, roleList)
}

func RoleBaseList(c *gin.Context) {
	roleList := []*Role{}
	err := config.Mngtdb.Select(&roleList, "select * from t_role")
	logutils.PanicErr(err)
	utils.WriteJson(c.Writer, roleList)
}

func FindUserByRole(c *gin.Context) {
	userList := []*User{}
	err := config.Mngtdb.Select(&userList, "select * from t_user where role_id = ?", c.PostForm("roleId"))
	logutils.PanicErr(err)
	utils.WriteJson(c.Writer, userList)
}

func SaveUser(c *gin.Context) {
	CheckAdminPower(c)
	user := &User{}
	utils.UnmarshalJson(c.Request.Body, user)
	tx, _ := config.Mngtdb.Beginx()
	defer tx.Rollback()
	checkUserExist(user, tx)
	if user.Id == "" {
		user.Id = utils.RandomStr()
		stmt, _ := tx.Prepare("insert into t_user (id, name, login_name, pwd, bio) values (?, ?, ?, ?, '')")
		tx.Stmt(stmt).Exec(user.Id, user.Name, user.LoginName, Md5sum(user.Pwd))
	} else {
		var pwdDb string
		rowE := tx.QueryRow("select pwd from t_user where id = ?", user.Id)
		rowE.Scan(&pwdDb)
		newPwd := Md5sum(user.Pwd)
		if user.Pwd == "" || pwdDb == newPwd {
			user.Pwd = pwdDb
		} else {
			user.Pwd = newPwd
		}
		stmt, _ := tx.Prepare("update t_user set name = ?, login_name = ?, pwd = ? where id = ?")
		tx.Stmt(stmt).Exec(user.Name, user.LoginName, user.Pwd, user.Id)
		tx.Exec("delete from t_user_role where user_id = ?", user.Id)
	}
	if len(user.RoleId) > 0 {
		stmt, _ := tx.Prepare("insert into t_user_role (id, role_id, user_id) values (?, ?, ?)")
		for _, rid := range user.RoleId {
			time.Sleep(3 * time.Millisecond)
			tx.Stmt(stmt).Exec(utils.RandomStr(), rid, user.Id)
		}
	}
	err := tx.Commit()
	logutils.PanicErrf("保存用户失败", err)
	utils.WriteJson(c.Writer, "")
}

func SaveUserBio(c *gin.Context) {
	CheckAdminPower(c)
	bioKey := c.PostForm("bioKey")
	authorization := c.GetHeader("Authorization")
	user := GetUser(authorization)
	stmt, _ := config.Mngtdb.Prepare("update t_user set bio = ? where id = ?")
	_, err := stmt.Exec(Md5sum(bioKey), user.Id)
	logutils.PanicErrf("保存用户失败", err)
	utils.WriteJson(c.Writer, "设置成功")
}

func checkUserExist(user *User, tx *sqlx.Tx) {
	checkSqlParam := make([]any, 2)
	checkSqlParam[0] = &user.LoginName
	sql := bytes.NewBufferString("select id from t_user where login_name = ?")
	if user.Id != "" {
		checkSqlParam[1] = user.Id
		sql.WriteString(" and id <> ?")
	}
	sql.WriteString("limit 1")
	row := tx.QueryRow(sql.String(), checkSqlParam...)
	var checkUserId string
	row.Scan(&checkUserId)
	if checkUserId != "" {
		logutils.PanicErr(errors.New("此登录名已存在"))
	}
}

func Md5sum(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	h.Write([]byte("dd5ac9a6fa2da9aaacc3cccca15b9707"))
	return hex.EncodeToString(h.Sum(nil))
}

func DelUser(c *gin.Context) {
	CheckAdminPower(c)
	config.Mngtdb.Exec("delete from t_user where id = ?", c.PostForm("id"))
	utils.WriteJson(c.Writer, "")
}

func ShowBackupData(c *gin.Context) {
	CheckAdminPower(c)
	backupId := c.Query("backupId")
	stmt, err := config.Mngtdb.Preparex("select data from t_history where id = ?")
	logutils.PanicErr(err)
	rowsx, err2 := stmt.Query(backupId)
	logutils.PanicErr(err2)
	var backupData any
	if rowsx.Next() {
		rowsx.Scan(&backupData)
	}
	utils.WriteJson(c.Writer, backupData)
}
func ListBackupData(c *gin.Context) {
	CheckAdminPower(c)
	user := GetUser(c.GetHeader("Authorization"))
	connId := c.Query("connId")

	// 构建动态SQL查询
	var (
		countSQL  string
		querySQL  string
		countArgs []any
		queryArgs []any
	)

	// 基础SQL部分
	baseWhere := "WHERE conn_id = ? AND operation_type IN ('update','delete')"
	baseCountSQL := "SELECT COUNT(*) FROM t_history a " + baseWhere
	baseQuerySQL := "SELECT a.id, a.exec_sql, a.exec_time FROM t_history a " + baseWhere +
		" ORDER BY a.exec_time DESC LIMIT ?, ?"

	if user.Id != config.AdminId {
		// 非管理员：添加user条件
		countSQL = "SELECT COUNT(*) FROM t_history a WHERE a.user = ? AND conn_id = ? AND operation_type IN ('update','delete')"
		querySQL = "SELECT a.id, a.exec_sql, exec_time FROM t_history a WHERE a.user = ? AND conn_id = ? AND operation_type IN ('update','delete') ORDER BY exec_time DESC LIMIT ?,?"
		countArgs = []any{user.LoginName, connId}
	} else {
		// 管理员：忽略user条件
		countSQL = baseCountSQL
		querySQL = baseQuerySQL
		countArgs = []any{connId}
	}

	// 执行count查询
	stmt, err := config.Mngtdb.Preparex(countSQL)
	logutils.PanicErrf("准备count查询失败", err)
	defer stmt.Close()

	var total int
	err = stmt.QueryRow(countArgs...).Scan(&total)
	logutils.PanicErrf("执行count查询失败", err)

	if total == 0 {
		utils.WriteJson(c.Writer, map[string]any{"data": []map[string]any{}, "total": 0})
		return
	}

	// 获取分页参数
	current := c.GetInt("current")
	pageSize := c.GetInt("pageSize")
	offset := (current - 1) * pageSize

	// 构建查询参数
	if user.Id != config.AdminId {
		queryArgs = []any{user.LoginName, connId, offset, pageSize}
	} else {
		queryArgs = []any{connId, offset, pageSize}
	}

	// 执行数据查询
	stmt2, err := config.Mngtdb.Preparex(querySQL)
	logutils.PanicErrf("准备数据查询失败", err)
	defer stmt2.Close()

	rows, err := stmt2.Queryx(queryArgs...)
	logutils.PanicErrf("执行数据查询失败", err)
	defer rows.Close()

	data := dbutils.GetResultRows(config.Mngtdb.DriverName(), rows)
	utils.WriteJson(c.Writer, map[string]any{"data": data, "total": total})
}

func FindUser(c *gin.Context) {
	key := c.Query("key")
	name := c.Query("name")
	loginName := c.Query("loginName")
	userIdList, _ := c.GetPostFormArray("userIdList")
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_user where 1 = 1")
	if name != "" {
		sql.WriteString(" and name like '%" + name + "%'")
	} else if loginName != "" {
		sql.WriteString(" and login_name like '%" + loginName + "%'")
	} else if key != "" {
		sql.WriteString(" and (login_name like '%" + key + "%' or name like '%" + key + "%')")
	} else if len(userIdList) > 0 {
		for _, userId := range userIdList {
			param = append(param, userId)
		}
		sql.WriteString(" and id in ( ")
		sql.WriteString(strings.Repeat("?,", len(userIdList))[0 : len(userIdList)*2-1])
		sql.WriteString(") ")
	} else {
		sql.WriteString(" and 1 = 2")
	}
	userList := []*User{}
	err := config.Mngtdb.Select(&userList, sql.String(), param...)
	logutils.PanicErr(err)

	userIds := []any{}
	for _, user := range userList {
		userIds = append(userIds, user.Id)
	}
	userRoleMap := findUserRole(userIds)
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

	utils.WriteJson(c.Writer, userList)
}

func FindUserBase(c *gin.Context) {
	loginName := c.Query("loginName")
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_user where 1 = 1")
	if loginName != "" {
		sql.WriteString(" and login_name = ?")
		param = append(param, loginName)
	}
	userList := []*SharedUser{}
	err := config.Mngtdb.Select(&userList, sql.String(), param...)
	logutils.PanicErr(err)
	utils.WriteJson(c.Writer, userList)
}

func findByLoginName(loginName string) *User {
	var users []User
	err := config.Mngtdb.Select(&users, "select id,name,pwd from t_user where login_name = ?", loginName)
	logutils.PanicErr(err)
	if len(users) == 0 {
		return nil
	}
	return &users[0]
}

func findByBio(bioKey string) *User {
	var users []User
	err := config.Mngtdb.Select(&users, "select id,login_name,name from t_user where bio = ?", Md5sum(bioKey))
	logutils.PanicErr(err)
	if len(users) == 0 {
		return nil
	}
	return &users[0]
}

func findByToken(token string) *User {
	var users []User

	cfg := config.Cfg
	req, err := http.NewRequest("GET", cfg.OutterUser, nil)
	logutils.PanicErr(err)
	req.Header.Add("Authorization", token)
	resp, err := http.DefaultClient.Do(req)
	logutils.PanicErr(err)
	body, err := io.ReadAll(resp.Body)
	logutils.PanicErr(err)
	defer resp.Body.Close()

	var outterUser struct {
		Code uint16         `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	err = json.Unmarshal(body, &outterUser)
	logutils.PanicErr(err)

	log.Println(string(utils.ToJsonString(outterUser)))

	err = config.Mngtdb.Select(&users, "select id,login_name,name from t_user where login_name = ?", outterUser.Data["employeeId"])
	logutils.PanicErr(err)

	if len(users) == 0 {
		return nil
	}
	return &users[0]
}

func findUserPower(userId string) []string {
	resIds := []string{}
	rows, err := config.Mngtdb.Query("select p.conn_id from t_power p left join t_user_role ur on ur.role_id = p.role_id where ur.user_id = ?", userId)
	logutils.PrintErr(err)
	var resId string
	for rows.Next() {
		rows.Scan(&resId)
		resIds = append(resIds, resId)
	}
	return resIds
}

func findUserPowerDetails(userId string) []*PowerDetail {
	powerList := []*PowerDetail{}
	sql := `
		select p.id, p.role_id, p.conn_id, p.schema_name, p.table_name, p.column_name, p.power_level, c.name conn_name 
		from t_power p 
		left join t_user_role ur on ur.role_id = p.role_id 
		left join t_conn c on p.conn_id = c.id
		where ur.user_id = ?
		order by p.power_level, p.schema_name, p.table_name, p.column_name
	`
	err := config.Mngtdb.Select(&powerList, sql, userId)
	logutils.PrintErr(err)
	return powerList
}

// FindUserPowerDetails 导出权限查询接口（供 AI agent 使用）
func FindUserPowerDetails(userId string) []*PowerDetail {
	return findUserPowerDetails(userId)
}

func checkPower(userPower *UserPower, param *PowerCheckParam) bool {
	if !config.Cfg.IsRemote {
		return true
	}

	powerDetails := findUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return false
	}

	hasConnLevel := false
	hasSchemaLevel := false
	hasTableLevel := false

	// 第一遍：检查是否有上级权限
	for _, power := range powerDetails {
		if power.ConnId != param.ConnId {
			continue
		}

		switch power.Level {
		case "conn":
			hasConnLevel = true
		case "schema":
			if power.SchemaName != nil && *power.SchemaName == param.SchemaName {
				hasSchemaLevel = true
			}
		case "table":
			if power.SchemaName != nil && *power.SchemaName == param.SchemaName &&
				power.TableName != nil && *power.TableName == param.TableName {
				hasTableLevel = true
			}
		}
	}

	// 如果有 conn 级权限，直接通过
	if hasConnLevel {
		return true
	}

	// 如果有 schema 级权限，检查是否匹配
	if hasSchemaLevel {
		return true
	}

	// 如果有 table 级权限，默认包含所有字段
	if hasTableLevel {
		return true
	}

	// 第二遍：检查 column 级权限（只有明确授权了字段才通过）
	for _, power := range powerDetails {
		if power.ConnId != param.ConnId {
			continue
		}

		if power.Level == "column" {
			if power.SchemaName != nil && *power.SchemaName == param.SchemaName &&
				power.TableName != nil && *power.TableName == param.TableName &&
				power.ColumnName != nil && *power.ColumnName == param.ColumnName {
				return true
			}
		}
	}

	return false
}

func appendPmsn(sql *bytes.Buffer, col string, param *[]any, userPower *UserPower) {
	// 非远程模式下不做权限管理
	if !config.Cfg.IsRemote {
		return
	}
	powerCount := len(userPower.Power)
	sql.WriteString(" and ")
	if powerCount == 0 {
		sql.WriteString(" 1 = 2 ")
		return
	}
	sql.WriteString(col)
	sql.WriteString(" in ( ")
	sql.WriteString(strings.Repeat("?,", powerCount)[0 : powerCount*2-1])
	sql.WriteString(") ")

	for i := 0; i < powerCount; i++ {
		*param = append(*param, userPower.Power[i])
	}
}

func findConnByRole(roleIdList []any) map[string][]*PowerDto {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[string][]*PowerDto{}
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select p.id, p.role_id, p.conn_id, c.name conn_name from t_conn c left join t_power p on c.id = p.conn_id where ")
	sqlBuf.WriteString("role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") ")
	powerList := []*PowerDto{}
	err := config.Mngtdb.Select(&powerList, sqlBuf.String(), roleIdList...)
	logutils.PanicErr(err)
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
	sqlBuf.WriteString("select p.id, p.role_id, p.conn_id, p.schema_name, p.table_name, p.column_name, p.power_level, c.name conn_name from t_conn c left join t_power p on c.id = p.conn_id where ")
	sqlBuf.WriteString("role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") order by p.power_level, p.schema_name, p.table_name, p.column_name")
	powerList := []*PowerDetail{}
	err := config.Mngtdb.Select(&powerList, sqlBuf.String(), roleIdList...)
	logutils.PanicErr(err)
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

func findUserRole(userIdList []any) map[string][]*UserRole {
	userCount := len(userIdList)
	if userCount == 0 {
		return map[string][]*UserRole{}
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select ur.*, r.name role_name from t_user_role ur left join t_role r on ur.role_id = r.id where ")
	sqlBuf.WriteString("user_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", userCount)[0 : userCount*2-1])
	sqlBuf.WriteString(") ")
	userRoleList := []*UserRole{}
	err := config.Mngtdb.Select(&userRoleList, sqlBuf.String(), userIdList...)
	logutils.PanicErr(err)
	roleUserMap := make(map[string][]*UserRole, len(userRoleList))
	for _, userRole := range userRoleList {
		v, ok := roleUserMap[userRole.UserId]
		if !ok {
			v = []*UserRole{}
		}
		v = append(v, userRole)
		roleUserMap[userRole.UserId] = v
	}
	return roleUserMap
}

func GetPermissionTree(c *gin.Context) {
	// 非远程模式下不做权限检查，或者是管理员直接通过
	if config.Cfg.IsRemote {
		authorization := c.GetHeader("Authorization")
		user := GetUser(authorization)
		if user == nil || user.Id != config.AdminId {
			logutils.PanicErr(errors.New("无权访问"))
		}
	}

	connId := c.Query("connId")
	schemaName := c.Query("schema")
	tableName := c.Query("table")
	level := c.Query("level")
	roleId := c.Query("roleId")
	authorization := c.GetHeader("Authorization")

	if level == "" {
		level = "conn"
	}

	// 初始化 data 为空数组而不是 nil，避免返回 null
	data := []*PermissionNode{}

	switch level {
	case "conn":
		data = getConnTree(roleId)
	case "schema":
		if connId == "" {
			utils.WriteJson(c.Writer, data)
			return
		}
		data = getSchemaTree(connId, authorization, roleId)
	case "table":
		if connId == "" || schemaName == "" {
			utils.WriteJson(c.Writer, data)
			return
		}
		data = getTableTree(connId, schemaName, authorization, roleId)
	case "column":
		if connId == "" || schemaName == "" || tableName == "" {
			utils.WriteJson(c.Writer, data)
			return
		}
		data = getColumnTree(connId, schemaName, tableName, authorization, roleId)
	}

	// 确保不会返回 null
	if data == nil {
		data = []*PermissionNode{}
	}

	utils.WriteJson(c.Writer, data)
}

func getConnTree(roleId string) []*PermissionNode {
	// 通过左连接 t_tree 表获取目录名称
	type ConnWithDir struct {
		ConnCfg
		ParentName *string `db:"parent_name"`
	}

	connList := []*ConnWithDir{}
	err := config.Mngtdb.Select(&connList, "select c.*, t.label as parent_name from t_conn c left join t_tree t on c.parent_id = t.id order by t.label, c.name")
	logutils.PanicErr(err)

	// 如果提供了 roleId，获取该角色的权限
	var roleConnIds map[string]bool
	if roleId != "" {
		roleConnIds = getRoleConnPermissions(roleId)
	}

	// 按目录分组组织连接
	dirMap := make(map[string][]*ConnWithDir)
	var noParentConns []*ConnWithDir

	for _, conn := range connList {
		if conn.ParentName != nil && *conn.ParentName != "" {
			if dirMap[*conn.ParentName] == nil {
				dirMap[*conn.ParentName] = make([]*ConnWithDir, 0)
			}
			dirMap[*conn.ParentName] = append(dirMap[*conn.ParentName], conn)
		} else {
			noParentConns = append(noParentConns, conn)
		}
	}

	// 构建目录节点及其子连接
	nodes := make([]*PermissionNode, 0)

	// 先添加有目录的连接（目录作为父节点，连接作为子节点）
	for dirName, conns := range dirMap {
		dirNode := &PermissionNode{
			Id:       fmt.Sprintf("dir::%s", dirName),
			Label:    dirName,
			Type:     "dir",
			Level:    "dir",
			ParentId: "",
			Checked:  false,
			Data: map[string]any{
				"type": "dir",
			},
			Children: make([]*PermissionNode, 0),
		}

		for _, conn := range conns {
			checked := false
			if roleId != "" && roleConnIds[conn.Id] {
				checked = true
			}

			name := ""
			if conn.Name != nil {
				name = *conn.Name
			}

			dirNode.Children = append(dirNode.Children, &PermissionNode{
				Id:       conn.Id,
				Label:    name,
				Type:     "conn",
				Level:    "conn",
				ParentId: dirNode.Id,
				Checked:  checked,
				Data: map[string]any{
					"connId":     conn.Id,
					"parentName": conn.ParentName,
				},
				Children: nil, // 使用 nil 而不是空数组，避免前端误判为有子节点
			})
		}

		nodes = append(nodes, dirNode)
	}

	// 添加没有目录的连接
	for _, conn := range noParentConns {
		checked := false
		if roleId != "" && roleConnIds[conn.Id] {
			checked = true
		}

		name := ""
		if conn.Name != nil {
			name = *conn.Name
		}

		nodes = append(nodes, &PermissionNode{
			Id:       conn.Id,
			Label:    name,
			Type:     "conn",
			Level:    "conn",
			ParentId: conn.ParentId,
			Checked:  checked,
			Data: map[string]any{
				"connId":     conn.Id,
				"parentName": conn.ParentName,
			},
			Children: nil, // 使用 nil 而不是空数组，避免前端误判为有子节点
		})
	}

	return nodes
}

// getRoleConnPermissions 获取角色在连接级别的权限
func getRoleConnPermissions(roleId string) map[string]bool {
	connIds := make(map[string]bool)
	powerList := []*PowerDetail{}
	err := config.Mngtdb.Select(&powerList, "select conn_id from t_power where role_id = ? and power_level = 'conn'", roleId)
	logutils.PrintErr(err)
	for _, power := range powerList {
		connIds[power.ConnId] = true
	}
	return connIds
}

func getSchemaTree(connId, authorization string, roleId string) []*PermissionNode {
	dc := getConnNoCheck(connId)
	if dc == nil {
		return []*PermissionNode{}
	}

	// 获取角色的 schema 级权限
	var roleSchemaMap map[string]bool
	if roleId != "" {
		roleSchemaMap = getRoleSchemaPermissions(roleId, connId)
	}

	schemaName := ""
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listSchema"])
	if err != nil {
		logutils.PrintErr(err)
		return []*PermissionNode{}
	}
	defer row.Close()

	nodes := make([]*PermissionNode, 0)
	for row.Next() {
		row.Scan(&schemaName)
		checked := false
		if roleId != "" && roleSchemaMap[schemaName] {
			checked = true
		}

		nodes = append(nodes, &PermissionNode{
			Id:       connId + "::" + schemaName,
			Label:    schemaName,
			Type:     "schema",
			Level:    "schema",
			ParentId: connId,
			Checked:  checked,
			Data: map[string]any{
				"connId":     connId,
				"schema":     schemaName,
				"schemaName": schemaName,
			},
		})
	}

	return nodes
}

// getRoleSchemaPermissions 获取角色在 schema 级别的权限
func getRoleSchemaPermissions(roleId, connId string) map[string]bool {
	schemas := make(map[string]bool)
	powerList := []*PowerDetail{}
	err := config.Mngtdb.Select(&powerList, "select schema_name from t_power where role_id = ? and conn_id = ? and power_level = 'schema'", roleId, connId)
	logutils.PrintErr(err)
	for _, power := range powerList {
		if power.SchemaName != nil {
			schemas[*power.SchemaName] = true
		}
	}
	return schemas
}

func getTableTree(connId, schema, authorization string, roleId string) []*PermissionNode {
	dc := getConnNoCheck(connId)
	if dc == nil {
		return []*PermissionNode{}
	}

	// 如果 schema 包含 ::，说明是完整路径格式，需要提取纯 schema 名称
	schemaName := schema
	if strings.Contains(schema, "::") {
		parts := strings.Split(schema, "::")
		if len(parts) >= 2 {
			schemaName = parts[1]
		}
	}

	// 获取角色的 table 级权限
	var roleTableMap map[string]bool
	if roleId != "" {
		roleTableMap = getRoleTablePermissions(roleId, connId, schemaName)
	}

	tableName, tableType, tableComment := "", "", ""

	tableName, columnName, columnComment := "", "", ""
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listAllColumns"], schemaName)
	if err != nil {
		logutils.PrintErr(err)
		return []*PermissionNode{}
	}
	defer row.Close()

	tableColumns := make([]map[string]string, 0)
	for row.Next() {
		*&columnComment = ""
		row.Scan(&tableName, &columnName, &columnComment)
		tableColumns = append(tableColumns, map[string]string{"tableName": tableName, "columnName": columnName, "columnComment": columnComment})
	}

	grouped := make(map[string][]map[string]string)
	for _, col := range tableColumns {
		tableName := col["tableName"]
		if grouped[tableName] == nil {
			grouped[tableName] = make([]map[string]string, 0)
		}
		grouped[tableName] = append(grouped[tableName], col)
	}

	row, err = dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listTable"], schemaName)
	if err != nil {
		logutils.PrintErr(err)
		return []*PermissionNode{}
	}
	defer row.Close()

	nodes := make([]*PermissionNode, 0)
	for row.Next() {
		row.Scan(&tableName, &tableType, &tableComment)
		nodeType := "table"
		if dc.DriverName() == "mysql" || dc.DriverName() == "mariadb" {
			switch tableType {
			case "VIEW":
				nodeType = "view"
			case "BASE TABLE":
				nodeType = "table"
			}
		} else if dc.DriverName() == "oracle" {
			nodeType = strings.ToLower(tableType)
		}

		checked := false
		if roleId != "" && roleTableMap[tableName] {
			checked = true
		}

		nodes = append(nodes, &PermissionNode{
			Id:       connId + "::" + schemaName + "::" + tableName,
			Label:    tableName,
			Type:     nodeType,
			Level:    "table",
			ParentId: connId + "::" + schemaName,
			Checked:  checked,
			Data: map[string]any{
				"connId":     connId,
				"schema":     schemaName,
				"schemaName": schemaName,
				"table":      tableName,
				"tableName":  tableName,
				"comment":    tableComment,
			},
		})
	}

	return nodes
}

// getRoleTablePermissions 获取角色在 table 级别的权限
func getRoleTablePermissions(roleId, connId, schemaName string) map[string]bool {
	tables := make(map[string]bool)
	powerList := []*PowerDetail{}
	err := config.Mngtdb.Select(&powerList, "select table_name from t_power where role_id = ? and conn_id = ? and schema_name = ? and power_level = 'table'", roleId, connId, schemaName)
	logutils.PrintErr(err)
	for _, power := range powerList {
		if power.TableName != nil {
			tables[*power.TableName] = true
		}
	}
	return tables
}

func getColumnTree(connId, schema, table, authorization string, roleId string) []*PermissionNode {
	dc := getConnNoCheck(connId)
	if dc == nil {
		return []*PermissionNode{}
	}

	// 如果 schema 或 table 包含 ::，需要提取纯名称
	schemaName := schema
	if strings.Contains(schema, "::") {
		parts := strings.Split(schema, "::")
		if len(parts) >= 2 {
			schemaName = parts[1]
		}
	}

	tableName := table
	if strings.Contains(table, "::") {
		parts := strings.Split(table, "::")
		if len(parts) >= 3 {
			tableName = parts[2]
		}
	}

	// 获取角色的 column 级权限
	var roleColumnMap map[string]map[string]bool
	if roleId != "" {
		roleColumnMap = getRoleColumnPermissions(roleId, connId, schemaName, tableName)
	}

	columnName, columnComment := "", ""
	row, err := dc.Query(dbutils.SQL_DIALECT[dc.DriverName()]["listColumns"], tableName)
	if err != nil {
		logutils.PrintErr(err)
		return []*PermissionNode{}
	}
	defer row.Close()

	nodes := make([]*PermissionNode, 0)
	for row.Next() {
		row.Scan(&columnName, &columnComment)
		checked := false
		if roleId != "" && roleColumnMap != nil && roleColumnMap[tableName][columnName] {
			checked = true
		}

		nodes = append(nodes, &PermissionNode{
			Id:       connId + "::" + schemaName + "::" + tableName + "::" + columnName,
			Label:    columnName,
			Type:     "column",
			Level:    "column",
			ParentId: connId + "::" + schemaName + "::" + tableName,
			Checked:  checked,
			Data: map[string]any{
				"connId":     connId,
				"schema":     schemaName,
				"schemaName": schemaName,
				"table":      tableName,
				"tableName":  tableName,
				"column":     columnName,
				"columnName": columnName,
				"comment":    columnComment,
			},
		})
	}

	return nodes
}

// getRoleColumnPermissions 获取角色在 column 级别的权限
func getRoleColumnPermissions(roleId, connId, schemaName, tableName string) map[string]map[string]bool {
	columns := make(map[string]map[string]bool)
	columns[tableName] = make(map[string]bool)
	powerList := []*PowerDetail{}
	err := config.Mngtdb.Select(&powerList, "select column_name from t_power where role_id = ? and conn_id = ? and schema_name = ? and table_name = ? and power_level = 'column'", roleId, connId, schemaName, tableName)
	logutils.PrintErr(err)
	for _, power := range powerList {
		if power.ColumnName != nil {
			columns[tableName][*power.ColumnName] = true
		}
	}
	return columns
}

// getConnNoCheck 获取数据库连接（不进行权限检查，仅用于权限树加载）
func getConnNoCheck(connId string) *sqlx.DB {
	if connId == "" {
		return nil
	}

	cfgList := []ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connId)
	if err != nil || len(cfgList) == 0 {
		logutils.PrintErr(err)
		return nil
	}

	// 解码密码
	pwd := ""
	if cfgList[0].Pwd != nil {
		pwd = utils.AESDecode(*cfgList[0].Pwd)
	}

	// 处理可能为 nil 的字段
	name := ""
	if cfgList[0].Name != nil {
		name = *cfgList[0].Name
	}
	user := ""
	if cfgList[0].User != nil {
		user = *cfgList[0].User
	}
	url := ""
	if cfgList[0].Url != nil {
		url = *cfgList[0].Url
	}

	return config.GetConn(&config.DBParam{
		Id: cfgList[0].Id, Name: name, DbType: cfgList[0].DbType,
		User: user, Pwd: pwd, Url: url,
	})
}

type User struct {
	Id        string    `json:"id"`
	RoleId    []*string `json:"roleId"`
	RoleName  []*string `json:"roleName"`
	LoginName string    `json:"loginName" db:"login_name"`
	Name      string    `json:"name"`
	Pwd       string    `json:"pwd"`
	Bio       string    `json:"bio"`
}

type UserPower struct {
	UserId string
	Power  []string
}

type PowerCheckParam struct {
	ConnId     string
	SchemaName string
	TableName  string
	ColumnName string
}

type Role struct {
	Id        string         `json:"id"`
	Name      string         `json:"name"`
	PowerList []*PowerDetail `json:"powerList"`
}

type UserRole struct {
	Id       string `json:"id"`
	UserId   string `json:"userId" db:"user_id"`
	RoleId   string `json:"roleId" db:"role_id"`
	RoleName string `json:"roleName" db:"role_name"`
}

type RoleSave struct {
	Id        string         `json:"id"`
	Name      string         `json:"name"`
	AddPowers []*PowerDetail `json:"addPowers"` // 新增的权限
	DelPowers []*PowerDetail `json:"delPowers"` // 删除的权限
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

type PermissionNode struct {
	Id       string            `json:"id"`
	Label    string            `json:"label"`
	Type     string            `json:"type"`
	Level    string            `json:"level"`
	ParentId string            `json:"parentId,omitempty"`
	Checked  bool              `json:"checked,omitempty"`
	Data     map[string]any    `json:"data,omitempty"`
	Children []*PermissionNode `json:"children"`
}

type AIConfig struct {
	Provider       string  `json:"provider"`
	BaseURL        string  `json:"baseUrl"`
	Model          string  `json:"model"`
	ApiKey         string  `json:"apiKey"`
	Temperature    float32 `json:"temperature"`    // 生成随机性 0.0-2.0，默认 0.7
	MaxTokens      int     `json:"maxTokens"`      // 最大生成 token 数，0 表示不限
	EnableThinking bool    `json:"enableThinking"` // 是否启用思考模式（Ollama thinking）
}

// UserPermissions 获取用户权限列表接口
func UserPermissions(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	user := GetUser(authorization)
	if user == nil {
		utils.WriteJson(c.Writer, []string{})
		return
	}

	// 获取用户的所有权限详情
	powerList := findUserPowerDetails(user.Id)

	// 将权限转换为字符串格式：connId::schema::table::column
	permissionKeys := make([]string, 0)
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

	utils.WriteJson(c.Writer, permissionKeys)
}
