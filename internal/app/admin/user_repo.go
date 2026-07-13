package admin

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"time"

	"websql/internal/logger"
	"websql/internal/pkg/idgen"

	"github.com/jmoiron/sqlx"
)

// UserRepo 定义用户数据访问接口，所有 SQL 查询均在此实现
type UserRepo interface {
	FindByLoginName(loginName string) (*User, error)
	FindByBio(hashedBioKey string) (*User, error)
	FindByLoginNameForToken(loginName any) (*User, error)
	FindUserPower(userId string) []string
	FindUserPowerDetails(userId string) []*PowerDetail
	FindUserRoles(userId string) []*Role
	FindUserRole(userIdList []any) (map[string][]*UserRole, error)
	FindUserBaseList(loginName, key string) ([]*SharedUser, error)
	FindUserList(roleId, name, loginName, key string, userIdList []string) ([]*User, error)
	GetPassword(userId string) (string, error)
	CheckUserExist(user *User) error
	Save(user *User) error
	SaveUserBio(userId, hashedBioKey string) error
	ChangePassword(userId, hashedPwd string) error
	InitUser(userId, name, loginName, hashedPwd string) error
	Delete(id string) error
}

type userRepo struct {
	db *sqlx.DB
}

// NewUserRepo 创建 UserRepo 实例，接受 *sqlx.DB 以便未来依赖注入
func NewUserRepo(db *sqlx.DB) UserRepo {
	return &userRepo{db: db}
}

func (r *userRepo) FindByLoginName(loginName string) (*User, error) {
	var users []User
	err := r.db.Select(&users, "select id,name,pwd from t_user where login_name = ?", loginName)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return &users[0], nil
}

func (r *userRepo) FindByBio(hashedBioKey string) (*User, error) {
	var users []User
	err := r.db.Select(&users, "select id,login_name,name from t_user where bio = ?", hashedBioKey)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return &users[0], nil
}

func (r *userRepo) FindByLoginNameForToken(loginName any) (*User, error) {
	var users []User
	err := r.db.Select(&users, "select id,login_name,name from t_user where login_name = ?", loginName)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return &users[0], nil
}

func (r *userRepo) FindUserPower(userId string) []string {
	resIds := []string{}
	rows, err := r.db.Query("select distinct p.conn_id from t_power p left join t_user_role ur on ur.role_id = p.role_id where ur.user_id = ?", userId)
	logger.PrintErr(err)
	var resId string
	for rows.Next() {
		if err := rows.Scan(&resId); err != nil {
			log.Printf("扫描行失败: %v", err)
			continue
		}
		resIds = append(resIds, resId)
	}
	return resIds
}

func (r *userRepo) FindUserPowerDetails(userId string) []*PowerDetail {
	powerList := []*PowerDetail{}
	sql := `
		select p.id, p.role_id, p.conn_id, p.schema_name, p.table_name, p.column_name, p.power_level, c.name conn_name
		from t_power p
		left join t_user_role ur on ur.role_id = p.role_id
		left join t_conn c on p.conn_id = c.id
		where ur.user_id = ?
		order by p.power_level, p.schema_name, p.table_name, p.column_name
	`
	err := r.db.Select(&powerList, sql, userId)
	logger.PrintErr(err)
	return powerList
}

func (r *userRepo) FindUserRoles(userId string) []*Role {
	roles := []*Role{}
	err := r.db.Select(&roles,
		"select r.id, r.name, r.allow_modify from t_role r inner join t_user_role ur on r.id = ur.role_id where ur.user_id = ?", userId)
	if err != nil {
		logger.PrintErr(err)
		return nil
	}
	return roles
}

func (r *userRepo) FindUserRole(userIdList []any) (map[string][]*UserRole, error) {
	userCount := len(userIdList)
	if userCount == 0 {
		return map[string][]*UserRole{}, nil
	}
	var (
		sqlBuf = bytes.Buffer{}
	)
	sqlBuf.WriteString("select ur.*, r.name role_name from t_user_role ur left join t_role r on ur.role_id = r.id where ")
	sqlBuf.WriteString("user_id in ( ")
	sqlBuf.WriteString(strings.Repeat("?,", userCount)[0 : userCount*2-1])
	sqlBuf.WriteString(") ")
	userRoleList := []*UserRole{}
	err := r.db.Select(&userRoleList, sqlBuf.String(), userIdList...)
	if err != nil {
		return nil, err
	}
	roleUserMap := make(map[string][]*UserRole, len(userRoleList))
	for _, userRole := range userRoleList {
		v, ok := roleUserMap[userRole.UserId]
		if !ok {
			v = []*UserRole{}
		}
		v = append(v, userRole)
		roleUserMap[userRole.UserId] = v
	}
	return roleUserMap, nil
}

func (r *userRepo) FindUserBaseList(loginName, key string) ([]*SharedUser, error) {
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
	err := r.db.Select(&userList, sql.String(), param...)
	return userList, err
}

func (r *userRepo) FindUserList(roleId, name, loginName, key string, userIdList []string) ([]*User, error) {
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select u.* from t_user u where 1 = 1")
	if roleId != "" {
		sql.WriteString(" and u.id in (select ur.user_id from t_user_role ur where ur.role_id = ?)")
		param = append(param, roleId)
	}
	if name != "" {
		sql.WriteString(" and u.name like ?")
		param = append(param, "%"+name+"%")
	} else if loginName != "" {
		sql.WriteString(" and u.login_name like ?")
		param = append(param, "%"+loginName+"%")
	} else if key != "" {
		sql.WriteString(" and (u.login_name like ? or u.name like ?)")
		param = append(param, "%"+key+"%", "%"+key+"%")
	} else if len(userIdList) > 0 {
		for _, userId := range userIdList {
			param = append(param, userId)
		}
		sql.WriteString(" and u.id in ( ")
		sql.WriteString(strings.Repeat("?,", len(userIdList))[0 : len(userIdList)*2-1])
		sql.WriteString(") ")
	}
	if len(param) == 0 {
		sql.WriteString(" and 1 = 2")
	}
	userList := []*User{}
	err := r.db.Select(&userList, sql.String(), param...)
	return userList, err
}

func (r *userRepo) GetPassword(userId string) (string, error) {
	var pwd string
	err := r.db.Get(&pwd, "select pwd from t_user where id = ?", userId)
	return pwd, err
}

func (r *userRepo) CheckUserExist(user *User) error {
	checkSqlParam := make([]any, 2)
	checkSqlParam[0] = &user.LoginName
	sql := bytes.NewBufferString("select id from t_user where login_name = ?")
	if user.Id != "" {
		checkSqlParam[1] = user.Id
		sql.WriteString(" and id <> ?")
	}
	sql.WriteString("limit 1")
	row := r.db.QueryRow(sql.String(), checkSqlParam...)
	var checkUserId string
	if err := row.Scan(&checkUserId); err != nil {
		log.Printf("扫描行失败: %v", err)
	}
	if checkUserId != "" {
		return errors.New("此登录名已存在")
	}
	return nil
}

func (r *userRepo) Save(user *User) error {
	tx, _ := r.db.Beginx()
	defer tx.Rollback()
	if user.Id == "" {
		user.Id = idgen.RandomStr()
		stmt, _ := tx.Prepare("insert into t_user (id, name, login_name, pwd, bio) values (?, ?, ?, ?, '')")
		tx.Stmt(stmt).Exec(user.Id, user.Name, user.LoginName, user.Pwd)
	} else {
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
	return tx.Commit()
}

func (r *userRepo) SaveUserBio(userId, hashedBioKey string) error {
	stmt, _ := r.db.Prepare("update t_user set bio = ? where id = ?")
	_, err := stmt.Exec(hashedBioKey, userId)
	return err
}

func (r *userRepo) ChangePassword(userId, hashedPwd string) error {
	_, err := r.db.Exec("update t_user set pwd = ? where id = ?", hashedPwd, userId)
	return err
}

func (r *userRepo) InitUser(userId, name, loginName, hashedPwd string) error {
	_, err := r.db.Exec("insert into t_user (id, name, login_name, pwd, bio) values (?, ?, ?, ?, '')",
		userId, name, loginName, hashedPwd)
	return err
}

func (r *userRepo) Delete(id string) error {
	_, err := r.db.Exec("delete from t_user where id = ?", id)
	return err
}
