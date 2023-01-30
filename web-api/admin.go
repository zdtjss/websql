package webapi

import (
	"go-web/utils"
	"net/http"
)

func RoleList(w http.ResponseWriter, r *http.Request) {
	roleList := []*Role{}
	err := db.Select(&roleList, "select * from t_role")
	utils.Panicln(err)
	utils.WriteJson(w, roleList)
}

func FindUserByRole(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	userList := []*User{}
	err := db.Select(&userList, "select * from t_user where role_id = ?", r.Form.Get("roleId"))
	utils.Panicln(err)
	utils.WriteJson(w, userList)
}

func FindUserByName(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	userList := []*User{}
	err := db.Select(&userList, "select * from t_user where name like concat('%', ?, '%')", r.Form.Get("name"))
	utils.Panicln(err)
	utils.WriteJson(w, userList)
}

func FindConnByRole(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	powerList := []*PowerDto{}
	err := db.Select(&powerList, "select p.*,c.name conn_name from t_config_dbconn c left join t_power p on c.id = p.conn_id where role_id = ?", r.Form.Get("roleId"))
	utils.Panicln(err)
	utils.WriteJson(w, powerList)
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
	Id   int    `json:"id"`
	Name string `json:"name"`
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
