package webapi

import (
	"bytes"
	"errors"
	"go-web/utils"
	"net/http"
	"strings"
)

func SaveRole(w http.ResponseWriter, r *http.Request) {
	user := &Role{}
	utils.UnmarshalJson(r.Body, user)
	if user.Id == 0 {
		stmt, _ := db.Prepare("insert into t_role (name) values (?)")
		stmt.Exec(&user.Name)
	} else {
		stmt, _ := db.Prepare("update t_role set name = ? where id = ?")
		stmt.Exec(&user.Name, &user.Id)
	}
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
		role.User = roleUserMap[role.Id]
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
		stmt, _ := db.Prepare("insert into t_user (role_id, name, login_name, pwd) values (?, ?, ?, ?, ?, ?)")
		stmt.Exec(&user.RoleId, &user.Name, &user.LoginName, &user.Pwd)
	} else {
		stmt, _ := db.Prepare("update t_user set role_id = ?, name = ?, login_name = ?, pwd = ? where id = ?")
		stmt.Exec(&user.Name, &user.RoleId, &user.Name, &user.LoginName, &user.Pwd, &user.Id)
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
	name := r.Form.Get("name")
	loginName := r.Form.Get("loginName")
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_user where 1 = 1")
	if name != "" {
		sql.WriteString(" and like('%" + name + "', name)")
	} else if loginName != "" {
		param = append(param, loginName)
		sql.WriteString(" and login_name = ?")
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
	sqlBuf.WriteString("select p.*,c.name conn_name from t_config_dbconn c left join t_power p on c.id = p.conn_id where ")
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
			rolePowerMap[power.Id] = v
		}
		*&v = append(v, power)
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
	sqlBuf.WriteString("select id,name,login_name t_user where ")
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
			roleUserMap[user.Id] = v
		}
		*&v = append(v, user)
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
	User      []*User     `json:"user"`
}

type Power struct {
	Id     int `json:"id"`
	RoleId int `json:"roleId" db:"role_id"`
	ConnId int `json:"connId" db:"conn_id"`
}

type PowerDto struct {
	Id       int `json:"id"`
	RoleId   int `json:"roleId" db:"role_id"`
	ConnId   int `json:"connId" db:"conn_id"`
	ConnName int `json:"connName" db:"conn_name"`
}
