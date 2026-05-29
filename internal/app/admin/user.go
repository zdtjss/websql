package admin

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

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

type SharedUser struct {
	Id        string `json:"id" db:"id"`
	Name      string `json:"name" db:"name"`
	LoginName string `json:"loginName" db:"login_name"`
}

func FindUserBase(c *gin.Context) {
	loginName := c.Query("loginName")
	key := c.Query("key")
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select id, name, login_name from t_user where 1 = 1")
	if loginName != "" {
		sql.WriteString(" and login_name = ?")
		param = append(param, loginName)
	} else if key != "" {
		sql.WriteString(" and (login_name like ? or name like ?)")
		param = append(param, "%"+key+"%", "%"+key+"%")
	}
	userList := []*SharedUser{}
	err := database.Mngtdb.Select(&userList, sql.String(), param...)
	logger.PanicErr(err)
	jsonutil.WriteJson(c.Writer, userList)
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
		sql.WriteString(" and name like ?")
		param = append(param, "%"+name+"%")
	} else if loginName != "" {
		sql.WriteString(" and login_name like ?")
		param = append(param, "%"+loginName+"%")
	} else if key != "" {
		sql.WriteString(" and (login_name like ? or name like ?)")
		param = append(param, "%"+key+"%", "%"+key+"%")
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
	err := database.Mngtdb.Select(&userList, sql.String(), param...)
	logger.PanicErr(err)

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

	jsonutil.WriteJson(c.Writer, userList)
}

func SaveUser(c *gin.Context) {
	CheckAdminPower(c)
	user := &User{}
	jsonutil.UnmarshalJson(c.Request.Body, user)
	tx, _ := database.Mngtdb.Beginx()
	defer tx.Rollback()
	checkUserExist(user, tx)
	if user.Id == "" {
		user.Id = idgen.RandomStr()
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
			tx.Stmt(stmt).Exec(idgen.RandomStr(), rid, user.Id)
		}
	}
	err := tx.Commit()
	logger.PanicErrf("保存用户失败", err)
	jsonutil.WriteJson(c.Writer, "")
}

func SaveUserBio(c *gin.Context) {
	bioKey := c.PostForm("bioKey")
	authorization := c.GetHeader("Authorization")
	user := GetUser(authorization)
	stmt, _ := database.Mngtdb.Prepare("update t_user set bio = ? where id = ?")
	_, err := stmt.Exec(Md5sum(bioKey), user.Id)
	logger.PanicErrf("保存用户失败", err)
	jsonutil.WriteJson(c.Writer, "设置成功")
}

func ChangePassword(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	user := GetUser(authorization)
	if user == nil || user.Id == "" {
		logger.PanicErr(errors.New("未登录"))
	}

	oldPwd := c.PostForm("oldPassword")
	newPwd := c.PostForm("newPassword")

	if oldPwd == "" || newPwd == "" {
		logger.PanicErr(errors.New("旧密码和新密码不能为空"))
	}
	if len(newPwd) < 6 {
		logger.PanicErr(errors.New("新密码长度不能少于6位"))
	}

	var currentPwd string
	err := database.Mngtdb.Get(&currentPwd, "select pwd from t_user where id = ?", user.Id)
	if err != nil {
		logger.PanicErr(errors.New("用户信息异常"))
	}
	if currentPwd != Md5sum(oldPwd) {
		logger.PanicErr(errors.New("旧密码不正确"))
	}

	_, err = database.Mngtdb.Exec("update t_user set pwd = ? where id = ?", Md5sum(newPwd), user.Id)
	if err != nil {
		logger.PanicErr(errors.New("修改密码失败"))
	}

	jsonutil.WriteJson(c.Writer, "密码修改成功")
}

func GetUserToken(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	user := GetUser(authorization)
	jsonutil.WriteJson(c.Writer, map[string]any{
		"id":    user.Id,
		"name":  user.Name,
		"token": authorization,
	})
}

func InitUser(c *gin.Context) {
	CheckAdminPower(c)
	userId := idgen.RandomStr()
	_, err := database.Mngtdb.Exec("insert into t_user (id, name, login_name, pwd, bio) values (?, ?, ?, ?, '')",
		userId, "admin", "admin", Md5sum("admin123"))
	logger.PanicErrf("初始化用户失败", err)
	jsonutil.WriteJson(c.Writer, "初始化成功")
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
		logger.PanicErr(errors.New("此登录名已存在"))
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
	database.Mngtdb.Exec("delete from t_user where id = ?", c.PostForm("id"))
	jsonutil.WriteJson(c.Writer, "")
}

func findByLoginName(loginName string) *User {
	var users []User
	err := database.Mngtdb.Select(&users, "select id,name,pwd from t_user where login_name = ?", loginName)
	logger.PanicErr(err)
	if len(users) == 0 {
		return nil
	}
	return &users[0]
}

func findByBio(bioKey string) *User {
	var users []User
	err := database.Mngtdb.Select(&users, "select id,login_name,name from t_user where bio = ?", Md5sum(bioKey))
	logger.PanicErr(err)
	if len(users) == 0 {
		return nil
	}
	return &users[0]
}

func findByToken(token string) *User {
	var users []User

	cfg := config.Cfg
	req, err := http.NewRequest("GET", cfg.OutterUser, nil)
	logger.PanicErr(err)
	req.Header.Add("Authorization", token)
	resp, err := http.DefaultClient.Do(req)
	logger.PanicErr(err)
	body, err := io.ReadAll(resp.Body)
	logger.PanicErr(err)
	defer resp.Body.Close()

	var outterUser struct {
		Code uint16         `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	err = json.Unmarshal(body, &outterUser)
	logger.PanicErr(err)

	log.Println(string(jsonutil.ToJsonString(outterUser)))

	err = database.Mngtdb.Select(&users, "select id,login_name,name from t_user where login_name = ?", outterUser.Data["employeeId"])
	logger.PanicErr(err)

	if len(users) == 0 {
		return nil
	}
	return &users[0]
}

func findUserPower(userId string) []string {
	resIds := []string{}
	rows, err := database.Mngtdb.Query("select distinct p.conn_id from t_power p left join t_user_role ur on ur.role_id = p.role_id where ur.user_id = ?", userId)
	logger.PrintErr(err)
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
	err := database.Mngtdb.Select(&powerList, sql, userId)
	logger.PrintErr(err)
	return powerList
}

func FindUserPowerDetails(userId string) []*PowerDetail {
	return findUserPowerDetails(userId)
}

func FindUserRoles(userId string) []*Role {
	roles := []*Role{}
	err := database.Mngtdb.Select(&roles,
		"select r.id, r.name, r.allow_modify from t_role r inner join t_user_role ur on r.id = ur.role_id where ur.user_id = ?", userId)
	if err != nil {
		logger.PrintErr(err)
		return nil
	}
	return roles
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
	err := database.Mngtdb.Select(&userRoleList, sqlBuf.String(), userIdList...)
	logger.PanicErr(err)
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