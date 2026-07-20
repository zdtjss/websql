package conn

import (
	"errors"
	"log"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/lazyinit"

	"github.com/jmoiron/sqlx"
)

// ConnService 封装连接管理的业务逻辑：连接测试、权限校验、配置转换等
type ConnService interface {
	SaveConn(cfg *ConnCfg) (*ConnCfg, error)
	TestDbConn(cfg *ConnCfg) (string, string, string, error)
	DeleteConn(id string)
	ListConn(parentId string, userPower *admin.UserPower) []*Tree
	ListConn2(name, parentId string, page, pageSize int) ([]ConnCfg, int, error)
	ListConnBase() ([]*ConnCfgBase, error)
	ListUserConn(userPower *admin.UserPower) ([]UserConnDTO, error)
	FilterTablesByPermission(tables []*Table, connId, schema string, userPower *admin.UserPower) []*Table
	GetConn(id, authorization string) *sqlx.DB
	GetConnNoCheck(connId string) *sqlx.DB
}

type connService struct {
	repo ConnRepo
}

// NewConnService 创建 ConnService 实例
func NewConnService(repo ConnRepo) ConnService {
	return &connService{repo: repo}
}

// 默认实例：lazyinit.Holder 替代散落的 sync.Once + 包级变量模式。
var defaultConn = &lazyinit.Holder[ConnService]{}

func getDefaultConn() ConnService {
	return defaultConn.Get(func() ConnService {
		return NewConnService(NewConnRepo(getDB()))
	})
}

// 连接相关错误，HTTP 处理函数通过 errors.Is 判断
var (
	ErrConnOpenFailed = errors.New("连接失败，无法打开数据库")
	ErrConnPingFailed = errors.New("连接失败，请检查数据库配置")
)

// SaveConn 保存连接配置，返回保存后的配置（不含密码）
func (s *connService) SaveConn(cfg *ConnCfg) (*ConnCfg, error) {
	dbParam := ConvertToDBParam(cfg)
	db := database.GetConn(dbParam)
	if db == nil {
		return nil, ErrConnOpenFailed
	}
	defer database.ReleaseConn(dbParam)

	dbSchema, dbVersion, actualDbType := getDbVersionAndSchema(db, cfg.DbType)
	// 自动修正 dbType：用户在前端可能误选 MySQL/MariaDB，这里根据 VERSION() 实际值修正
	cfg.DbType = actualDbType

	var savedId string
	if cfg.Id == "" {
		var insertErr error
		savedId, insertErr = s.repo.InsertConn(cfg, dbSchema, dbVersion)
		if insertErr != nil {
			log.Printf("[SaveConn] 新增连接失败 - err=%v\n", insertErr)
			return nil, insertErr
		}
	} else {
		savedId = cfg.Id
		var updateErr error
		if cfg.Pwd == nil || *cfg.Pwd == "" {
			updateErr = s.repo.UpdateConn(cfg, dbSchema, dbVersion)
		} else {
			updateErr = s.repo.UpdateConnWithPwd(cfg, dbSchema, dbVersion)
		}
		if updateErr != nil {
			log.Printf("[SaveConn] 更新连接失败 - id=%s, err=%v\n", savedId, updateErr)
			return nil, updateErr
		}
	}

	saved, err := s.repo.FindConnByIdWithParent(savedId)
	if err != nil {
		return nil, err
	}
	if len(saved) > 0 {
		saved[0].Pwd = nil
		return &saved[0], nil
	}
	return nil, nil
}

// TestDbConn 测试数据库连接，返回版本、schema 和实际数据库类型信息
func (s *connService) TestDbConn(cfg *ConnCfg) (string, string, string, error) {
	dbParam := ConvertToDBParam(cfg)
	db := database.GetConn(dbParam)
	if db == nil {
		return "", "", "", ErrConnOpenFailed
	}

	err := db.Ping()
	if err != nil {
		log.Printf("[TestDbConn] 数据库连接失败 - err=%v\n", err)
		return "", "", "", ErrConnPingFailed
	}

	dbSchema, dbVersion, actualDbType := getDbVersionAndSchema(db, cfg.DbType)
	return dbSchema, dbVersion, actualDbType, nil
}

// DeleteConn 删除连接配置
// 原实现忽略删除错误，保持一致
func (s *connService) DeleteConn(id string) {
	_ = s.repo.DeleteConn(id)
}

// ListConn 按父节点查询连接列表并构建树节点
func (s *connService) ListConn(parentId string, userPower *admin.UserPower) []*Tree {
	cfgList, err := s.repo.FindConnList(parentId, userPower)
	if err != nil || cfgList == nil {
		return nil
	}
	tree := make([]*Tree, len(cfgList))
	for i, cfg := range cfgList {
		label := ""
		if cfg.Name != nil {
			label = *cfg.Name
		}
		tree[i] = &Tree{Label: label, Id: cfg.Id, Type: TREE_NODE_TYPE_CONN}
	}
	return tree
}

// ListConn2 分页查询连接列表
func (s *connService) ListConn2(name, parentId string, page, pageSize int) ([]ConnCfg, int, error) {
	offset := (page - 1) * pageSize
	return s.repo.FindConnList2(name, parentId, pageSize, offset)
}

// ListConnBase 查询连接基础列表
func (s *connService) ListConnBase() ([]*ConnCfgBase, error) {
	return s.repo.FindConnBaseList()
}

// ListUserConn 查询用户有权限的连接列表
func (s *connService) ListUserConn(userPower *admin.UserPower) ([]UserConnDTO, error) {
	return s.repo.FindUserConnList(userPower)
}

// FilterTablesByPermission 按权限过滤表列表
func (s *connService) FilterTablesByPermission(tables []*Table, connId, schema string, userPower *admin.UserPower) []*Table {
	if config.IsLocalMode() {
		return tables
	}
	if userPower == nil || len(userPower.Power) == 0 {
		return []*Table{}
	}

	powerDetails := admin.FindUserPowerDetails(userPower.UserId)
	if len(powerDetails) == 0 {
		return []*Table{}
	}

	filtered := make([]*Table, 0)
	for _, table := range tables {
		param := &admin.PowerCheckParam{
			ConnId:     connId,
			SchemaName: schema,
			TableName:  table.Name,
		}

		hasAccess := checkPowerByParam(powerDetails, param)

		if hasAccess {
			filtered = append(filtered, table)
		}
	}

	return filtered
}

// GetConn 获取数据库连接（带权限校验）
func (s *connService) GetConn(id string, authorization string) *sqlx.DB {
	userPower := admin.GetUserPower(authorization)
	if !config.IsLocalMode() {
		if !admin.CheckConnAccess(userPower, id) {
			logger.PrintErrf("无权访问连接: %s", nil, id)
			return nil
		}
	}
	cfgList, err := s.repo.FindConnById(id)
	if err != nil {
		logger.PrintErrf("查询连接配置失败: %s", err, id)
		return nil
	}
	if len(cfgList) == 0 {
		logger.PrintErrf("连接配置不存在: %s", nil, id)
		return nil
	}

	pwd := ""
	if cfgList[0].Pwd != nil && cfgList[0].DbType != "sqlite" {
		decoded, decErr := crypto.AESDecode(*cfgList[0].Pwd)
		if decErr != nil {
			logger.PrintErrf("连接密码解密失败: %s", decErr, id)
		} else {
			pwd = decoded
		}
	}
	cfgList[0].Pwd = &pwd

	db := database.GetConn(ConvertToDBParam(&cfgList[0]))
	if db == nil {
		logger.PrintErrf("数据库连接创建失败: %s", nil, id)
	}
	return db
}

// GetConnNoCheck 获取数据库连接（不带权限校验）
func (s *connService) GetConnNoCheck(connId string) *sqlx.DB {
	if connId == "" {
		return nil
	}

	cfgList, err := s.repo.FindConnById(connId)
	if err != nil || len(cfgList) == 0 {
		logger.PrintErr(err)
		return nil
	}

	pwd := ""
	if cfgList[0].Pwd != nil {
		decoded, decErr := crypto.AESDecode(*cfgList[0].Pwd)
		if decErr != nil {
			logger.PrintErrf("连接密码解密失败: %s", decErr, connId)
		} else {
			pwd = decoded
		}
	}

	name := ""
	if cfgList[0].Name != nil {
		name = *cfgList[0].Name
	}
	user := ""
	if cfgList[0].User != nil {
		user = *cfgList[0].User
	}
	url := ""
	if cfgList[0].Url != nil {
		url = *cfgList[0].Url
	}

	return database.GetConn(&database.DBParam{
		Id: cfgList[0].Id, Name: name, DbType: cfgList[0].DbType,
		User: user, Pwd: pwd, Url: url,
	})
}

// ConvertToDBParam 将 ConnCfg 转换为 database.DBParam
func ConvertToDBParam(cfg *ConnCfg) *database.DBParam {
	dbSchema := ""
	if cfg.DbSchema != nil {
		dbSchema = *cfg.DbSchema
	}
	dbVersion := ""
	if cfg.DbVersion != nil {
		dbVersion = *cfg.DbVersion
	}
	name := ""
	if cfg.Name != nil {
		name = *cfg.Name
	}
	user := ""
	if cfg.User != nil {
		user = *cfg.User
	}
	pwd := ""
	if cfg.Pwd != nil {
		pwd = *cfg.Pwd
	}
	url := ""
	if cfg.Url != nil {
		url = *cfg.Url
	}
	return &database.DBParam{Id: cfg.Id, Name: name, DbType: cfg.DbType, User: user, Pwd: pwd, Url: url, DbSchema: dbSchema, DbVersion: dbVersion}
}

// getDbVersionAndSchema 获取数据库版本和 schema 信息，并自动修正 dbType。
// 对于 MySQL/MariaDB，通过 SELECT VERSION() 返回值判断真实数据库类型：
//   - 返回值包含 "MariaDB" 则为 MariaDB
//   - 否则视为 MySQL
//
// 这样即使用户在前端误选了 MySQL/MariaDB，保存时也会自动修正为正确的类型。
func getDbVersionAndSchema(db *sqlx.DB, dbType string) (string, string, string) {
	var versionSQL string
	var schemaSQL string

	switch dbType {
	case "mysql", "mariadb":
		versionSQL = "SELECT VERSION()"
		schemaSQL = "SELECT DATABASE()"
	case "oracle":
		versionSQL = "SELECT BANNER FROM V$VERSION WHERE ROWNUM = 1"
		schemaSQL = "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL"
	case "sqlite":
		versionSQL = "SELECT SQLITE_VERSION()"
	default:
		versionSQL = "SELECT VERSION()"
		schemaSQL = "SELECT DATABASE()"
	}

	version := ""
	schema := ""
	actualDbType := dbType

	if err := db.Get(&version, versionSQL); err != nil {
		log.Printf("[getDbVersionAndSchema] 获取版本失败 - dbType=%s, err=%v\n", dbType, err)
		version = ""
	}

	// 通过 VERSION() 返回值区分 MySQL 与 MariaDB
	if dbType == "mysql" || dbType == "mariadb" {
		if strings.Contains(strings.ToUpper(version), "MARIADB") {
			actualDbType = "mariadb"
		} else {
			actualDbType = "mysql"
		}
	}

	if dbType == "sqlite" {
		schema = "main"
	} else if err := db.Get(&schema, schemaSQL); err != nil {
		log.Printf("[getDbVersionAndSchema] 获取schema失败 - dbType=%s, err=%v\n", dbType, err)
		schema = ""
	}

	return schema, version, actualDbType
}

func checkPowerByParam(powerDetails []*admin.PowerDetail, param *admin.PowerCheckParam) bool {
	byRole := admin.GroupPowerDetailsByRole(powerDetails, param.ConnId)

	for _, roleDetails := range byRole {
		if checkPowerByParamForRole(roleDetails, param) {
			return true
		}
	}

	return false
}

func checkPowerByParamForRole(roleDetails []*admin.PowerDetail, param *admin.PowerCheckParam) bool {
	r := admin.ResolveRolePermissions(roleDetails)
	if param.ColumnName != "" {
		return r.CanAccessColumn(param.SchemaName, param.TableName, param.ColumnName)
	}
	if param.TableName != "" {
		return r.CanAccessTable(param.SchemaName, param.TableName)
	}
	return r.CanAccessSchema(param.SchemaName)
}

// ===== 向后兼容的包级别委托函数 =====
// 这些函数被其他包调用，保持原有签名不变，委托到 getDefaultConn()。

func ListConn(parentId string, userPower *admin.UserPower) []*Tree {
	return getDefaultConn().ListConn(parentId, userPower)
}

func FilterTablesByPermission(tables []*Table, connId, schema string, userPower *admin.UserPower) []*Table {
	return getDefaultConn().FilterTablesByPermission(tables, connId, schema, userPower)
}

func GetConn(id string, authorization string) *sqlx.DB {
	return getDefaultConn().GetConn(id, authorization)
}

// GetUserConnIds 获取用户有权限的所有连接 ID 列表
func GetUserConnIds(authorization string) []string {
	userPower := admin.GetUserPower(authorization)
	conns, err := getDefaultConn().ListUserConn(userPower)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(conns))
	for _, c := range conns {
		if c.ConnId != "" {
			ids = append(ids, c.ConnId)
		}
	}
	return ids
}

func GetConnNoCheck(connId string) *sqlx.DB {
	return getDefaultConn().GetConnNoCheck(connId)
}

// GetConnDefaultSchema 获取连接配置中保存的默认 schema（db_schema 字段）
func GetConnDefaultSchema(connId string) string {
	if connId == "" {
		return ""
	}
	repo := NewConnRepo(getDB())
	cfgList, err := repo.FindConnById(connId)
	if err != nil || len(cfgList) == 0 {
		return ""
	}
	if cfgList[0].DbSchema != nil {
		return *cfgList[0].DbSchema
	}
	return ""
}

// GetConnInfo 返回连接名和默认 schema（db_schema 字段），用于审计日志等场景。
// 连接不存在或字段为空时对应返回空字符串。
func GetConnInfo(connId string) (name, schema string) {
	if connId == "" {
		return "", ""
	}
	repo := NewConnRepo(getDB())
	cfgList, err := repo.FindConnById(connId)
	if err != nil || len(cfgList) == 0 {
		return "", ""
	}
	if cfgList[0].Name != nil {
		name = *cfgList[0].Name
	}
	if cfgList[0].DbSchema != nil {
		schema = *cfgList[0].DbSchema
	}
	return name, schema
}
