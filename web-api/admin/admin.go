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
	"strings"
	"time"
)

func SaveRole(w http.ResponseWriter, r *http.Request) {
	CheckPower(r)
	role := &RoleSave{}
	utils.UnmarshalJson(r.Body, role)
	tx, _ := config.Mngtdb.Beginx()
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
	}
	if len(role.ConnIdList) > 0 {
		stmt, _ := tx.Prepare("insert into t_power (id, role_id, conn_id) values (?, ?, ?)")
		for _, connId := range role.ConnIdList {
			time.Sleep(10 * time.Millisecond)
			tx.Stmt(stmt).Exec(utils.RandomInt64(), role.Id, connId)
		}
	}
	err := tx.Commit()
	logutils.PanicErrf("保存角色失败", err)
	utils.WriteJson(w, "")
}

func DelRole(w http.ResponseWriter, r *http.Request) {
	CheckPower(r)
	r.ParseForm()
	id := utils.AtoUint64(r.FormValue("id"))

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
	err := config.Mngtdb.Select(&userList, "select * from t_user where role_id = ?", utils.AtoUint64(r.FormValue("roleId")))
	logutils.PanicErr(err)
	utils.WriteJson(w, userList)
}

func SaveUser(w http.ResponseWriter, r *http.Request) {
	CheckPower(r)
	user := &User{}
	utils.UnmarshalJson(r.Body, user)
	tx, _ := config.Mngtdb.Beginx()
	defer tx.Rollback()
	if user.Id == 0 {
		stmt, _ := config.Mngtdb.Prepare("insert into t_user (id, name, login_name, pwd) values (?, ?, ?, ?)")
		rs, _ := tx.Stmt(stmt).Exec(utils.RandomInt64(), &user.Name, &user.LoginName, Md5sum(user.Pwd))
		id, _ := rs.LastInsertId()
		*&user.Id = uint64(id)
	} else {
		var userDb []User
		config.Mngtdb.Select(&userDb, "select pwd from t_user where id = ?", user.Id)
		md5pwd := Md5sum(user.Pwd)
		if userDb[0].Pwd != md5pwd {
			user.Pwd = md5pwd
		}
		stmt, _ := config.Mngtdb.Prepare("update t_user set name = ?, login_name = ?, pwd = ? where id = ?")
		tx.Stmt(stmt).Exec(&user.Name, &user.LoginName, &user.Pwd, &user.Id)
		tx.Exec("delete from t_user_role where user_id = ?", user.Id)
	}
	if len(user.RoleId) > 0 {
		stmt, _ := tx.Prepare("insert into t_user_role (id, role_id, user_id) values (?, ?, ?)")
		for _, rid := range user.RoleId {
			time.Sleep(10 * time.Millisecond)
			tx.Stmt(stmt).Exec(utils.RandomInt64(), rid, user.Id)
		}
	}
	err := tx.Commit()
	logutils.PanicErrf("保存用户失败", err)
	utils.WriteJson(w, "")
}

func Md5sum(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	h.Write([]byte("dd5ac9a6fa2da9aaacc3cccca15b9707"))
	return hex.EncodeToString(h.Sum(nil))
}

func DelUser(w http.ResponseWriter, r *http.Request) {
	CheckPower(r)
	r.ParseForm()
	config.Mngtdb.Exec("delete from t_user where id = ?", utils.AtoUint64(r.FormValue("id")))
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
	err := config.Mngtdb.Select(&userList, sql.String(), param...)
	logutils.PanicErr(err)

	userIds := []any{}
	for _, user := range userList {
		userIds = append(userIds, user.Id)
	}
	userRoleMap := findUserRole(userIds)
	for _, user := range userList {
		user.Pwd = ""
		roleIds := []*uint64{}
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

func findByLoginName(loginName string) User {
	var user []User
	err := config.Mngtdb.Select(&user, "select id,name,pwd from t_user where login_name = ?", loginName)
	logutils.PanicErr(err)
	return user[0]
}

func findUserPower(userId uint64) []uint64 {
	resIds := []uint64{}
	rows, err := config.Mngtdb.Query("select p.conn_id from t_power p left join t_user_role ur on ur.role_id = p.role_id where ur.user_id = ?", userId)
	logutils.PrintErr(err)
	var resId uint64
	for rows.Next() {
		rows.Scan(&resId)
		resIds = append(resIds, resId)
	}
	return resIds
}

func appendPmsn(sql *bytes.Buffer, col string, param *[]any, userPower UserPower) {
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
		(*param) = append((*param), userPower.Power[i])
	}
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
	err := config.Mngtdb.Select(&powerList, sqlBuf.String(), roleIdList...)
	logutils.PanicErr(err)
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

func findUserRole(userIdList []any) map[uint64][]*UserRole {
	userCount := len(userIdList)
	if userCount == 0 {
		return map[uint64][]*UserRole{}
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
	roleUserMap := make(map[uint64][]*UserRole, len(userRoleList))
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
	Id        uint64    `json:"id"`
	RoleId    []*uint64 `json:"roleId"`
	RoleName  []*string `json:"roleName"`
	LoginName string    `json:"loginName" db:"login_name"`
	Name      string    `json:"name"`
	Pwd       string    `json:"pwd"`
}

type UserPower struct {
	UserId uint64
	Power  []uint64
}

type Role struct {
	Id        uint64      `json:"id"`
	Name      string      `json:"name"`
	PowerList []*PowerDto `json:"powerList"`
}

type UserRole struct {
	Id       uint64 `json:"id"`
	UserId   uint64 `json:"userId" db:"user_id"`
	RoleId   uint64 `json:"roleId" db:"role_id"`
	RoleName string `json:"roleName" db:"role_name"`
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
