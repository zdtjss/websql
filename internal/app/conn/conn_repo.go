package conn

import (
	"bytes"
	"log"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/idgen"

	"github.com/jmoiron/sqlx"
)

// ConnRepo 定义连接配置数据访问接口，所有针对 t_conn 的 SQL 查询均在此实现
type ConnRepo interface {
	InsertConn(cfg *ConnCfg, dbSchema, dbVersion string) (string, error)
	UpdateConn(cfg *ConnCfg, dbSchema, dbVersion string) error
	UpdateConnWithPwd(cfg *ConnCfg, dbSchema, dbVersion string) error
	FindConnByIdWithParent(id string) ([]ConnCfg, error)
	DeleteConn(id string) error
	FindConnList(parentId string, userPower *admin.UserPower) ([]ConnCfg, error)
	FindConnList2(name, parentId string, pageSize, offset int) ([]ConnCfg, int, error)
	FindConnBaseList() ([]*ConnCfgBase, error)
	FindUserConnList(userPower *admin.UserPower) ([]UserConnDTO, error)
	FindConnById(id string) ([]ConnCfg, error)
}

type connRepo struct {
	db *sqlx.DB
}

// NewConnRepo 创建 ConnRepo 实例，接受 *sqlx.DB 以便未来依赖注入
func NewConnRepo(db *sqlx.DB) ConnRepo {
	return &connRepo{db: db}
}

func (r *connRepo) InsertConn(cfg *ConnCfg, dbSchema, dbVersion string) (string, error) {
	savedId := idgen.RandomStr()
	stmt, _ := r.db.Prepare("insert into t_conn (id, name, db_type, parent_id, user, pwd, url, db_schema, db_version) values (?, ?, ?, ?, ?, ?, ?, ?, ?)")
	pwdEncoded := ""
	if cfg.Pwd != nil && *cfg.Pwd != "" {
		encoded, encErr := crypto.AESEncode(*cfg.Pwd)
		if encErr != nil {
			return "", encErr
		}
		pwdEncoded = encoded
	}
	_, err := stmt.Exec(savedId, cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, pwdEncoded, cfg.Url, dbSchema, dbVersion)
	if err != nil {
		return "", err
	}
	return savedId, nil
}

func (r *connRepo) UpdateConn(cfg *ConnCfg, dbSchema, dbVersion string) error {
	stmt, _ := r.db.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, url = ?, db_schema = ?, db_version = ? where id = ?")
	_, err := stmt.Exec(cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, cfg.Url, dbSchema, dbVersion, cfg.Id)
	return err
}

func (r *connRepo) UpdateConnWithPwd(cfg *ConnCfg, dbSchema, dbVersion string) error {
	stmt, _ := r.db.Prepare("update t_conn set name = ?, db_type = ?,parent_id = ?, user = ?, pwd = ?, url = ?, db_schema = ?, db_version = ? where id = ?")
	pwdEncoded, err := crypto.AESEncode(*cfg.Pwd)
	if err != nil {
		return err
	}
	_, err = stmt.Exec(cfg.Name, cfg.DbType, cfg.ParentId, cfg.User, pwdEncoded, cfg.Url, dbSchema, dbVersion, cfg.Id)
	return err
}

func (r *connRepo) FindConnByIdWithParent(id string) ([]ConnCfg, error) {
	saved := []ConnCfg{}
	err := r.db.Select(&saved, "select c.*, t.label parent_name from t_conn c left join t_tree t on c.parent_id = t.id where c.id = ?", id)
	if err != nil {
		log.Printf("[SaveConn] 查询保存结果失败: %v", err)
		return nil, err
	}
	return saved, nil
}

func (r *connRepo) DeleteConn(id string) error {
	_, err := r.db.Exec("delete from t_conn where id = ?", id)
	return err
}

func (r *connRepo) FindConnList(parentId string, userPower *admin.UserPower) ([]ConnCfg, error) {
	if parentId == "" {
		return nil, nil
	}
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_conn")
	if strings.EqualFold(parentId, "noneParent") {
		sql.WriteString(" where (parent_id = '' or parent_id is null)")
	} else if parentId != "" {
		param = append(param, parentId)
		sql.WriteString(" where parent_id = ?")
	}
	admin.AppendPmsn(&sql, "id", &param, userPower)
	cfgList := []ConnCfg{}
	err := r.db.Select(&cfgList, sql.String(), param...)
	if err != nil {
		log.Printf("[ListConn] 查询连接列表失败: %v", err)
		return nil, err
	}
	return cfgList, nil
}

func (r *connRepo) FindConnList2(name, parentId string, pageSize, offset int) ([]ConnCfg, int, error) {
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select c.*,t.label parent_name from t_conn c left join t_tree t on c.parent_id = t.id where 1 = 1 ")
	if name != "" {
		sql.WriteString(" and c.name like ?")
		param = append(param, "%"+name+"%")
	}
	if parentId != "" {
		if parentId == "none" {
			sql.WriteString(" and (c.parent_id = '' or c.parent_id is null)")
		} else {
			sql.WriteString(" and c.parent_id = ?")
			param = append(param, parentId)
		}
	}

	countSQL := "select count(*) from (" + sql.String() + ") as total_count"
	var total int
	err := r.db.Get(&total, countSQL, param...)
	if err != nil {
		log.Printf("[ListConn2] 查询总数失败: %v", err)
		return nil, 0, err
	}

	sql.WriteString(" order by c.id limit ? offset ?")
	param = append(param, pageSize, offset)

	cfgList := []ConnCfg{}
	err = r.db.Select(&cfgList, sql.String(), param...)
	if err != nil {
		log.Printf("[ListConn2] 查询列表失败: %v", err)
		return nil, 0, err
	}
	return cfgList, total, nil
}

func (r *connRepo) FindConnBaseList() ([]*ConnCfgBase, error) {
	cfgList := []*ConnCfgBase{}
	err := r.db.Select(&cfgList, "select id,name,parent_id from t_conn")
	if err != nil {
		log.Printf("[ListConnBase] 查询连接基础列表失败: %v", err)
		return nil, err
	}
	return cfgList, nil
}

// UserConnDTO 用户连接列表数据传输对象
type UserConnDTO struct {
	ConnId   string  `json:"connId" db:"id"`
	Name     string  `json:"name" db:"name"`
	DbSchema *string `json:"dbSchema" db:"db_schema"`
	DirName  *string `json:"dirName" db:"dir_name"`
}

func (r *connRepo) FindUserConnList(userPower *admin.UserPower) ([]UserConnDTO, error) {
	dtoList := []UserConnDTO{}
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select c.id, c.name, c.db_schema, t.label as dir_name from t_conn c left join t_tree t on c.parent_id = t.id where 1 = 1 ")
	admin.AppendPmsn(&sql, "c.id", &param, userPower)

	sql.WriteString(" order by t.label,c.name ")
	err := r.db.Select(&dtoList, sql.String(), param...)
	if err != nil {
		log.Printf("[ListUserConn] 查询用户连接列表失败: %v", err)
		return nil, err
	}
	return dtoList, nil
}

func (r *connRepo) FindConnById(id string) ([]ConnCfg, error) {
	cfgList := []ConnCfg{}
	err := r.db.Select(&cfgList, "select * from t_conn where id = ?", id)
	return cfgList, err
}
