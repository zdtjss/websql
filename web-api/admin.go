package webapi

import (
	"bytes"
	"errors"
	"go-web/utils"
	"net/http"
	"strings"
)

func SaveRole(w http.ResponseWriter, r *http.Request) {
	role := &RoleSave{}
	utils.UnmarshalJson(r.Body, role)
	tx, _ := db.Beginx()
	defer tx.Rollback()
	if role.Id == 0 {
		stmt, _ := tx.Prepare("insert into t_role (name) values (?)")
		stmt.Exec(&role.Name)
	} else {
		stmt, _ := tx.Prepare("update t_role set name = ? where id = ?")
		stmt.Exec(&role.Name, &role.Id)
		tx.Exec("delete from t_power where role_id = ?", &role.Id)
		tx.Exec("update t_user set role_id = '' where id = ?", &role.Id)
	}
	if len(role.PowerIdList) > 0 {
		stmt, _ := tx.Prepare("insert into t_power (role_id, conn_id) values (?, ?)")
		for _, connId := range role.PowerIdList {
			stmt.Exec(&role.Id, connId)
		}
	}
	if len(role.UserIdList) > 0 {
		stmt, _ := tx.Prepare("update t_user set role_id = ? where id = ?")
		for _, userId := range role.UserIdList {
			stmt.Exec(&role.Id, userId)
		}
	}
	tx.Commit()
	utils.WriteJson(w, "")
}

func DelRole(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()
	id := r.Form.Get("id")

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
	err := db.Select(&userList, "select * from t_user where role_id = ?", r.Form.Get("roleId"))
	utils.Panicln(err)
	utils.WriteJson(w, userList)
}

func SaveUser(w http.ResponseWriter, r *http.Request) {
	user := &User{}
	utils.UnmarshalJson(r.Body, user)
	if user.Id == 0 {
		stmt, _ := db.Prepare("insert into t_user (role_id, name, login_name, pwd) values (?, ?, ?, ?)")
		stmt.Exec(&user.RoleId, &user.Name, &user.LoginName, &user.Pwd)
	} else {
		stmt, _ := db.Prepare("update t_user set role_id = ?, name = ?, login_name = ?, pwd = ? where id = ?")
		stmt.Exec(&user.RoleId, &user.Name, &user.LoginName, &user.Pwd, &user.Id)
	}
	utils.WriteJson(w, "")
}

func DelUser(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id := r.Form.Get("id")
	db.Exec("delete from t_user where id = ?", id)
	utils.WriteJson(w, "")
}

func FindUser(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("name")
	loginName := r.FormValue("loginName")
	userIdList, _ := r.Form["userIdList[]"]
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_user where 1 = 1")
	if name != "" {
		sql.WriteString(" and like('%" + name + "', name)")
	} else if loginName != "" {
		param = append(param, loginName)
		sql.WriteString(" and login_name = ?")
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

func findConnByRole(roleIdList []any) map[int][]*PowerDto {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[int][]*PowerDto{}
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select p.id, p.role_id, p.conn_id, c.name conn_name from t_config_dbconn c left join t_power p on c.id = p.conn_id where ")
	sqlBuf.WriteString("role_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", roleCount)[0 : roleCount*2-1])
	sqlBuf.WriteString(") ")
	powerList := []*PowerDto{}
	err := db.Select(&powerList, sqlBuf.String(), roleIdList...)
	utils.Panicln(err)
	rolePowerMap := make(map[int][]*PowerDto, len(powerList))
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

func findUserByRole(roleIdList []any) map[int][]*User {
	roleCount := len(roleIdList)
	if roleCount == 0 {
		return map[int][]*User{}
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
	roleUserMap := make(map[int][]*User, len(userList))
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
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			role_id INTEGER,
			login_name VARCHAR(64),
			name VARCHAR(64),
			pwd VARCHAR(64)
		);
		`
	db.Exec(sql_table)

	sql_table = `
		CREATE TABLE IF NOT EXISTS t_role (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name VARCHAR(64)
		);
		`
	db.Exec(sql_table)

	sql_table = `
		CREATE TABLE IF NOT EXISTS t_power (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			role_id INTEGER,
			conn_id INTEGER
		);
		`
	db.Exec(sql_table)
}

type User struct {
	Id        int    `json:"id"`
	RoleId    int    `json:"roleId" db:"role_id"`
	LoginName string `json:"loginName" db:"login_name"`
	Name      string `json:"name"`
	Pwd       string `json:"pwd"`
}

type Role struct {
	Id        int         `json:"id"`
	Name      string      `json:"name"`
	PowerList []*PowerDto `json:"powerList"`
	UserList  []*User     `json:"userList"`
}

type RoleSave struct {
	Id          int       `json:"id"`
	Name        string    `json:"name"`
	PowerIdList []*string `json:"powerIdList"`
	UserIdList  []*int    `json:"userIdList"`
}

type Power struct {
	Id     int `json:"id"`
	RoleId int `json:"roleId" db:"role_id"`
	ConnId int `json:"connId" db:"conn_id"`
}

type PowerDto struct {
	Id       int    `json:"id"`
	RoleId   int    `json:"roleId" db:"role_id"`
	ConnId   int    `json:"connId" db:"conn_id"`
	ConnName string `json:"connName" db:"conn_name"`
}
