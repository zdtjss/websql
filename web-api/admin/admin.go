package admin

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"net/http"
	"strconv"
	"strings"
	"time"

	dbutils "go-web/utils/db"

	"github.com/jmoiron/sqlx"
)

func SaveRole(w http.ResponseWriter, r *http.Request) {
	CheckAdminPower(r)
	role := &RoleSave{}
	utils.UnmarshalJson(r.Body, role)
	tx, _ := config.Mngtdb.Beginx()
	defer tx.Rollback()
	if role.Id == "" {
		role.Id = utils.RandomStr()
		stmt, _ := tx.Prepare("insert into t_role (id, name) values (?, ?)")
		tx.Stmt(stmt).Exec(role.Id, role.Name)
	} else {
		stmt, _ := tx.Prepare("update t_role set name = ? where id = ?")
		tx.Stmt(stmt).Exec(role.Name, role.Id)
		tx.Exec("delete from t_power where role_id = ?", role.Id)
	}
	if len(role.ConnIdList) > 0 {
		stmt, _ := tx.Prepare("insert into t_power (id, role_id, conn_id) values (?, ?, ?)")
		for _, connId := range role.ConnIdList {
			time.Sleep(10 * time.Millisecond)
			tx.Stmt(stmt).Exec(utils.RandomStr(), role.Id, connId)
		}
	}
	err := tx.Commit()
	logutils.PanicErrf("保存角色失败", err)
	utils.WriteJson(w, "")
}

func DelRole(w http.ResponseWriter, r *http.Request) {
	CheckAdminPower(r)
	r.ParseForm()
	id := r.FormValue("id")

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
	utils.WriteJson(w, "")
}

func RoleList(w http.ResponseWriter, r *http.Request) {
	roleList := []*Role{}
	err := config.Mngtdb.Select(&roleList, "select * from t_role")
	logutils.PanicErr(err)

	roleIdList := make([]any, len(roleList))
	for idx, role := range roleList {
		roleIdList[idx] = role.Id
	}
	rolePowerMap := findConnByRole(roleIdList)
	for _, role := range roleList {
		role.PowerList = rolePowerMap[role.Id]
	}
	utils.WriteJson(w, roleList)
}

func RoleBaseList(w http.ResponseWriter, r *http.Request) {
	roleList := []*Role{}
	err := config.Mngtdb.Select(&roleList, "select * from t_role")
	logutils.PanicErr(err)
	utils.WriteJson(w, roleList)
}

func FindUserByRole(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	userList := []*User{}
	err := config.Mngtdb.Select(&userList, "select * from t_user where role_id = ?", r.FormValue("roleId"))
	logutils.PanicErr(err)
	utils.WriteJson(w, userList)
}

func SaveUser(w http.ResponseWriter, r *http.Request) {
	CheckAdminPower(r)
	user := &User{}
	utils.UnmarshalJson(r.Body, user)
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
	utils.WriteJson(w, "")
}

func SaveUserBio(w http.ResponseWriter, r *http.Request) {
	CheckAdminPower(r)
	r.ParseForm()
	bioKey := r.PostForm.Get("bioKey")
	authorization := r.Header.Get("Authorization")
	user := GetUser(authorization)
	stmt, _ := config.Mngtdb.Prepare("update t_user set bio = ? where id = ?")
	_, err := stmt.Exec(Md5sum(bioKey), user.Id)
	logutils.PanicErrf("保存用户失败", err)
	utils.WriteJson(w, "")
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

func DelUser(w http.ResponseWriter, r *http.Request) {
	CheckAdminPower(r)
	r.ParseForm()
	config.Mngtdb.Exec("delete from t_user where id = ?", r.FormValue("id"))
	utils.WriteJson(w, "")
}

func ShowBackupData(w http.ResponseWriter, r *http.Request) {
	CheckAdminPower(r)
	r.ParseForm()
	backupId := r.Form.Get("backupId")
	stmt, err := config.Mngtdb.Preparex("select data from t_backup where id = ?")
	logutils.PanicErr(err)
	rowsx, err2 := stmt.Query(backupId)
	logutils.PanicErr(err2)
	var backupData any
	if rowsx.Next() {
		rowsx.Scan(&backupData)
	}
	utils.WriteJson(w, backupData)
}

func ListBackupData(w http.ResponseWriter, r *http.Request) {
	CheckAdminPower(r)
	r.ParseForm()
	user := GetUser(r.Header.Get("Authorization"))
	connId := r.Form.Get("connId")

	total := 0
	var data []map[string]any
	stmt, err := config.Mngtdb.Preparex("select count(*) from t_backup where user = ? and conn_id = ?")
	logutils.PanicErr(err)
	defer stmt.Close()
	stmt.QueryRow(user.LoginName, connId).Scan(&total)

	if total != 0 {
		current, _ := strconv.Atoi((r.FormValue("current")))
		pageSize, _ := strconv.Atoi((r.FormValue("pageSize")))
		stmt2, err2 := config.Mngtdb.Preparex("select a.id,a.exec_sql,exec_time from t_backup a where a.user = ? and conn_id = ? order by exec_time desc limit ?,?")
		logutils.PanicErr(err2)
		defer stmt2.Close()
		rows, err := stmt2.Queryx(user.LoginName, connId, (current-1)*pageSize, pageSize)
		logutils.PanicErr(err)
		defer rows.Close()
		data = dbutils.GetResultRows(config.Mngtdb.DriverName(), rows)
	}
	utils.WriteJson(w, map[string]any{"data": data, "total": total})
}

func FindUser(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	key := r.FormValue("key")
	name := r.FormValue("name")
	loginName := r.FormValue("loginName")
	userIdList := r.Form["userIdList[]"]
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

	utils.WriteJson(w, userList)
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

func appendPmsn(sql *bytes.Buffer, col string, param *[]any, userPower *UserPower) {
	// 非远程模式下不做权限管理
	if !config.IsRemote {
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

type Role struct {
	Id        string      `json:"id"`
	Name      string      `json:"name"`
	PowerList []*PowerDto `json:"powerList"`
}

type UserRole struct {
	Id       string `json:"id"`
	UserId   string `json:"userId" db:"user_id"`
	RoleId   string `json:"roleId" db:"role_id"`
	RoleName string `json:"roleName" db:"role_name"`
}

type RoleSave struct {
	Id         string    `json:"id"`
	Name       string    `json:"name"`
	ConnIdList []*string `json:"connIdList"`
	UserIdList []*string `json:"userIdList"`
}

type Power struct {
	Id     string `json:"id"`
	RoleId string `json:"roleId" db:"role_id"`
	ConnId string `json:"connId" db:"conn_id"`
}

type PowerDto struct {
	Id       string `json:"id"`
	RoleId   string `json:"roleId" db:"role_id"`
	ConnId   string `json:"connId" db:"conn_id"`
	ConnName string `json:"connName" db:"conn_name"`
}
