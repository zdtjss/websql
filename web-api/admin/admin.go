package admin

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	utils.WriteJson(c.Writer, "")
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

func RoleList(c *gin.Context) {
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
	bioKey := c.Query("bioKey")
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

type AIConfig struct {
	Provider       string  `json:"provider"`
	BaseURL        string  `json:"baseUrl"`
	Model          string  `json:"model"`
	ApiKey         string  `json:"apiKey"`
	Temperature    float32 `json:"temperature"`    // 生成随机性 0.0-2.0，默认 0.7
	MaxTokens      int     `json:"maxTokens"`      // 最大生成 token 数，0 表示不限
	EnableThinking bool    `json:"enableThinking"` // 是否启用思考模式（Ollama thinking）
}
