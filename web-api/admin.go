package webapi

import (
	"bytes"
	"errors"
	"go-web/utils"
	"net/http"
	"strings"
	"time"
)

func SaveRole(w http.ResponseWriter, r *http.Request) {
	role := &RoleSave{}
	utils.UnmarshalJson(r.Body, role)
	tx, _ := db.Beginx()
	defer tx.Rollback()
	if role.Id == 0 {
		stmt, _ := tx.Prepare("insert into t_role (id, name) values (?, ?)")
		rs, _ := tx.Stmt(stmt).Exec(utils.RandomInt64(), &role.Name)
		nid, _ := rs.LastInsertId()
		*&role.Id = uint64(nid)
	} else {
		stmt, _ := tx.Prepare("update t_role set name = ? where id = ?")
		tx.Stmt(stmt).Exec(&role.Name, &role.Id)
		tx.Exec("delete from t_power where role_id = ?", &role.Id)
		tx.Exec("update t_user set role_id = 0 where id = ?", &role.Id)
	}
	if len(role.ConnIdList) > 0 {
		stmt, _ := tx.Prepare("insert into t_power (id, role_id, conn_id) values (?, ?, ?)")
		for _, connId := range role.ConnIdList {
			tx.Stmt(stmt).Exec(utils.RandomInt64(), role.Id, connId)
		}
	}
	if len(role.UserIdList) > 0 {
		stmt, _ := tx.Prepare("update t_user set role_id = ? where id = ?")
		for _, userId := range role.UserIdList {
			time.Sleep(10 * time.Millisecond)
			tx.Stmt(stmt).Exec(&role.Id, userId)
		}
	}
	err := tx.Commit()
	utils.Panicf("修改角色失败 %x", err)
	utils.WriteJson(w, "")
}

func DelRole(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()
	id := utils.AtoUint64(r.FormValue("id"))

	userCount := 0
	db.Select(userCount, "select count(*) from t_user where role_id = ?", id)
	if userCount > 0 {
		utils.Panicln(errors.New("有用户，不能删。"))
	}

	powerCount := 0
	db.Select(powerCount, "select count(*) from t_power where role_id = ?", id)
	if powerCount > 0 {
		utils.Panicln(errors.New("有权限，不能删。"))
	}

	db.Exec("delete from t_role where id = ?", id)
	utils.WriteJson(w, "")
}

func RoleList(w http.ResponseWriter, r *http.Request) {
	roleList := []*Role{}
	err := db.Select(&roleList, "select * from t_role")
	utils.Panicln(err)

	roleIdList := make([]any, len(roleList))
	for idx, role := range roleList {
		roleIdList[idx] = role.Id
	}
	roleUserMap := findUserByRole(roleIdList)
	rolePowerMap := findConnByRole(roleIdList)
	for _, role := range roleList {
		role.UserList = roleUserMap[role.Id]
		role.PowerList = rolePowerMap[role.Id]
	}
	utils.WriteJson(w, roleList)
}

func FindUserByRole(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	userList := []*User{}
	err := db.Select(&userList, "select * from t_user where role_id = ?", utils.AtoUint64(r.FormValue("roleId")))
	utils.Panicln(err)
	utils.WriteJson(w, userList)
}

func SaveUser(w http.ResponseWriter, r *http.Request) {
	user := &User{}
	utils.UnmarshalJson(r.Body, user)
	if user.Id == 0 {
		stmt, _ := db.Prepare("insert into t_user (id, role_id, name, login_name, pwd) values (?, ?, ?, ?, ?)")
		stmt.Exec(utils.RandomInt64(), &user.RoleId, &user.Name, &user.LoginName, &user.Pwd)
	} else {
		stmt, _ := db.Prepare("update t_user set role_id = ?, name = ?, login_name = ?, pwd = ? where id = ?")
		stmt.Exec(&user.RoleId, &user.Name, &user.LoginName, &user.Pwd, &user.Id)
	}
	utils.WriteJson(w, "")
}

func DelUser(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	db.Exec("delete from t_user where id = ?", utils.AtoUint64(r.FormValue("id")))
	utils.WriteJson(w, "")
}

func FindUser(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	key := r.FormValue("key")
	name := r.FormValue("name")
	loginName := r.FormValue("loginName")
	userIdList, _ := r.Form["userIdList[]"]
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_user where 1 = 1")
	if name != "" {
		sql.WriteString(" and like('%" + name + "%', name)")
	} else if loginName != "" {
		sql.WriteString(" and like('%" + loginName + "%', login_name)")
	} else if key != "" {
		sql.WriteString(" and (like('%" + key + "%', login_name) or like('%" + key + "%', name))")
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
	err := db.Select(&userList, sql.String(), param...)
	utils.Panicln(err)
	utils.WriteJson(w, userList)
}

func findConnByRole(roleIdList []any) map[uint64][]*PowerDto {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[uint64][]*PowerDto{}
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select p.id, p.role_id, p.conn_id, c.name conn_name from t_conn c left join t_power p on c.id = p.conn_id where ")
	sqlBuf.WriteString("role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") ")
	powerList := []*PowerDto{}
	err := db.Select(&powerList, sqlBuf.String(), roleIdList...)
	utils.Panicln(err)
	rolePowerMap := make(map[uint64][]*PowerDto, len(powerList))
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

func findUserByRole(roleIdList []any) map[uint64][]*User {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[uint64][]*User{}
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select id,role_id,name,login_name from t_user where ")
	sqlBuf.WriteString("role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") ")
	userList := []*User{}
	err := db.Select(&userList, sqlBuf.String(), roleIdList...)
	utils.Panicln(err)
	roleUserMap := make(map[uint64][]*User, len(userList))
	for _, user := range userList {
		v, ok := roleUserMap[user.RoleId]
		if !ok {
			v = []*User{}
		}
		v = append(v, user)
		roleUserMap[user.RoleId] = v
	}
	return roleUserMap
}

// 初始化表结构
func InitAdminTable() {

	sql_table := `
		CREATE TABLE IF NOT EXISTS t_user (
			id BIGINT PRIMARY KEY,
			role_id BIGINT,
			login_name VARCHAR(64),
			name VARCHAR(64),
			pwd VARCHAR(64)
		);
		`
	db.Exec(sql_table)

	sql_table = `
		CREATE TABLE IF NOT EXISTS t_role (
			id BIGINT PRIMARY KEY,
			name VARCHAR(64)
		);
		`
	db.Exec(sql_table)

	sql_table = `
		CREATE TABLE IF NOT EXISTS t_power (
			id BIGINT PRIMARY KEY,
			role_id BIGINT,
			conn_id BIGINT
		);
		`
	db.Exec(sql_table)
}

type User struct {
	Id        uint64 `json:"id"`
	RoleId    uint64 `json:"roleId" db:"role_id"`
	LoginName string `json:"loginName" db:"login_name"`
	Name      string `json:"name"`
	Pwd       string `json:"pwd"`
}

type Role struct {
	Id        uint64      `json:"id"`
	Name      string      `json:"name"`
	PowerList []*PowerDto `json:"powerList"`
	UserList  []*User     `json:"userList"`
}

type RoleSave struct {
	Id         uint64    `json:"id"`
	Name       string    `json:"name"`
	ConnIdList []*uint64 `json:"connIdList"`
	UserIdList []*uint64 `json:"userIdList"`
}

type Power struct {
	Id     uint64 `json:"id"`
	RoleId uint64 `json:"roleId" db:"role_id"`
	ConnId uint64 `json:"connId" db:"conn_id"`
}

type PowerDto struct {
	Id       uint64 `json:"id"`
	RoleId   uint64 `json:"roleId" db:"role_id"`
	ConnId   uint64 `json:"connId" db:"conn_id"`
	ConnName string `json:"connName" db:"conn_name"`
}
