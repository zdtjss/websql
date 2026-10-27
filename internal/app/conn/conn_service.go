package conn

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"websql/internal/app/admin"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/lazyinit"
	"websql/internal/pkg/mask"

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

// connOpenError 保留 errors.Is(err, ErrConnOpenFailed) 语义，
// 同时把面向用户的友好文案（不含驱动原文、主机名、账号）作为 Error()。
type connOpenError struct{ msg string }

func (e *connOpenError) Error() string { return e.msg }

func (e *connOpenError) Is(target error) bool { return target == ErrConnOpenFailed }

func newConnOpenError(err error) error {
	return &connOpenError{msg: connErrMessage(err)}
}

// connErrMessage 把连接错误转为面向用户的提示；驱动原文只进服务端日志。
func connErrMessage(err error) string {
	if err == nil {
		return ErrConnPingFailed.Error()
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "access denied"), strings.Contains(msg, "1045"), strings.Contains(msg, "ora-01017"):
		return "连接失败：用户名或口令错误"
	case strings.Contains(msg, "unknown database"), strings.Contains(msg, "1049"):
		return "连接失败：数据库不存在"
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "no such host"),
		strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "dial tcp"), strings.Contains(msg, "network"):
		return "连接失败：无法连接数据库主机，请检查地址与端口"
	case strings.Contains(msg, "1251"), strings.Contains(msg, "plugin"), strings.Contains(msg, "authentication"):
		return "连接失败：认证方式不兼容，请检查账号认证插件"
	default:
		return "连接失败，请检查连接配置（详情见服务端日志）"
	}
}

// metaCacheInvalidator 由 dbops 包注册（conn → dbops 直引会形成循环依赖）。
// 连接目标变更后必须失效表元数据缓存，否则 5 分钟内仍会返回旧库的主键/注释。
var metaCacheInvalidator func(connId string)

// SetMetaCacheInvalidator 注册表元数据缓存失效回调。
func SetMetaCacheInvalidator(fn func(connId string)) {
	metaCacheInvalidator = fn
}

func invalidateConnMetaCache(connID string) {
	if metaCacheInvalidator != nil && connID != "" {
		metaCacheInvalidator(connID)
	}
}

// strPtr 安全解引用可空字符串字段。
func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// connTargetChanged 判断是否需要重建连接池：只比较影响物理连接的字段。
// 名称/分组/默认 schema 变化不需要重连；本次提交带了口令则视为目标变更。
func connTargetChanged(old, cur *ConnCfg) bool {
	if old == nil || cur == nil {
		return true
	}
	if old.DbType != cur.DbType || strPtr(old.Url) != strPtr(cur.Url) ||
		strPtr(old.User) != strPtr(cur.User) || strPtr(old.Charset) != strPtr(cur.Charset) {
		return true
	}
	return cur.Pwd != nil && *cur.Pwd != ""
}

// SaveConn 保存连接配置，返回保存后的配置（不含密码）
func (s *connService) SaveConn(cfg *ConnCfg) (*ConnCfg, error) {
	// 目标变更检测需要在更新前读取旧配置（仅用于判断是否需要重建连接池）
	dsnChanged := true
	if cfg.Id != "" {
		if oldList, oldErr := s.repo.FindConnById(cfg.Id); oldErr == nil && len(oldList) > 0 {
			dsnChanged = connTargetChanged(&oldList[0], cfg)
		}
	}

	// 保存前用一次性连接校验：不进共享池，避免未保存的连接互相串池，
	// 也避免保存动作把别人正在使用的连接关掉。
	dbParam := ConvertToDBParam(cfg)
	var closeEphemeral func()
	if cfg.SSH != nil {
		// 新建/编辑时可能刚改过 SSH 配置，先用临时隧道验证（不依赖已保存的配置）
		forwarded, closeFn, tunnelErr := OpenEphemeralTunnel(cfg.SSH, cfg.DbType, strPtr(cfg.Url))
		if tunnelErr != nil {
			log.Printf("[SaveConn] SSH 隧道建立失败 - name=%s, err=%s\n", strPtr(cfg.Name), mask.MaskSecret(tunnelErr.Error()))
			return nil, newConnOpenError(tunnelErr)
		}
		dbParam.Url = forwarded
		closeEphemeral = closeFn
	} else if cfg.Id != "" {
		// 未改 SSH 配置但已有已保存的跳板机：用已存配置建链
		resolved, tunnelErr := ResolveConnURL(cfg.Id, cfg.DbType, strPtr(cfg.Url))
		if tunnelErr != nil {
			log.Printf("[SaveConn] SSH 隧道建立失败 - id=%s, err=%s\n", cfg.Id, mask.MaskSecret(tunnelErr.Error()))
			return nil, newConnOpenError(tunnelErr)
		}
		dbParam.Url = resolved
	}
	if closeEphemeral != nil {
		defer closeEphemeral()
	}
	var dbSchema, dbVersion, actualDbType string
	err := database.WithTempConn(dbParam, func(db *sqlx.DB) error {
		dbSchema, dbVersion, actualDbType = getDbVersionAndSchema(db, cfg.DbType)
		return nil
	})
	if err != nil {
		log.Printf("[SaveConn] 连接校验失败 - name=%s, dbType=%s, err=%s\n", strPtr(cfg.Name), cfg.DbType, mask.MaskSecret(err.Error()))
		return nil, newConnOpenError(err)
	}
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

	// 配置已变更：元数据缓存必须失效；仅当连接目标变化时才延迟替换连接池，
	// 避免「只改个名称」就把在途查询打断。
	invalidateConnMetaCache(savedId)
	if dsnChanged {
		database.CloseConnLater(savedId, 3*time.Second)
	}
	// 只读标记与 SSH 配置单独持久化（不落在 t_conn，无需迁移）
	if err := SetReadOnly(savedId, cfg.ReadOnly); err != nil {
		log.Printf("[SaveConn] 保存只读标记失败 - id=%s, err=%v\n", savedId, err)
	}
	if err := SaveSSHConfig(savedId, cfg.SSH); err != nil {
		log.Printf("[SaveConn] 保存 SSH 配置失败 - id=%s, err=%v\n", savedId, err)
		return nil, fmt.Errorf("保存 SSH 配置失败: %w", err)
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
	// 一次性连接：测试永远不会复用到其他（尤其是其他未保存连接的）连接池，
	// 也避免测试连接长期占用池与健康检查 goroutine。
	dbParam := ConvertToDBParam(cfg)
	var closeEphemeral func()
	if cfg.SSH != nil {
		forwarded, closeFn, tunnelErr := OpenEphemeralTunnel(cfg.SSH, cfg.DbType, strPtr(cfg.Url))
		if tunnelErr != nil {
			log.Printf("[TestDbConn] SSH 隧道建立失败 - dbType=%s, err=%s\n", cfg.DbType, mask.MaskSecret(tunnelErr.Error()))
			return "", "", "", newConnOpenError(tunnelErr)
		}
		dbParam.Url = forwarded
		closeEphemeral = closeFn
		defer closeEphemeral()
	} else if cfg.Id != "" {
		resolved, tunnelErr := ResolveConnURL(cfg.Id, cfg.DbType, strPtr(cfg.Url))
		if tunnelErr != nil {
			return "", "", "", newConnOpenError(tunnelErr)
		}
		dbParam.Url = resolved
	}
	var dbSchema, dbVersion, actualDbType string
	err := database.WithTempConn(dbParam, func(db *sqlx.DB) error {
		if pingErr := db.Ping(); pingErr != nil {
			return pingErr
		}
		dbSchema, dbVersion, actualDbType = getDbVersionAndSchema(db, cfg.DbType)
		return nil
	})
	if err != nil {
		log.Printf("[TestDbConn] 数据库连接失败 - dbType=%s, err=%s\n", cfg.DbType, mask.MaskSecret(err.Error()))
		return "", "", "", newConnOpenError(err)
	}
	return dbSchema, dbVersion, actualDbType, nil
}

// DeleteConn 删除连接配置
// 原实现忽略删除错误，保持一致
func (s *connService) DeleteConn(id string) {
	_ = s.repo.DeleteConn(id)
	// 同步清理只读标记与 SSH 配置（含隧道），避免 id 复用时继承旧设置
	DeleteReadOnlyFlag(id)
	DeleteSSHConfig(id)
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

// ListConn2 分页查询连接列表，并补充只读标记与 SSH 配置（不含口令）
func (s *connService) ListConn2(name, parentId string, page, pageSize int) ([]ConnCfg, int, error) {
	offset := (page - 1) * pageSize
	list, total, err := s.repo.FindConnList2(name, parentId, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	for i := range list {
		list[i].ReadOnly = IsReadOnly(list[i].Id)
		if sshCfg := LoadSSHConfig(list[i].Id); sshCfg != nil {
			safe := *sshCfg
			safe.Password = ""
			safe.KeyPassphrase = ""
			list[i].SSH = &safe
		}
	}
	return list, total, nil
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

	// SSH 隧道：配置了跳板机时把连接串换成本地转发地址；失败则拒绝连接（不回退直连）
	rawURL := ""
	if cfgList[0].Url != nil {
		rawURL = *cfgList[0].Url
	}
	resolvedURL, tunnelErr := ResolveConnURL(id, cfgList[0].DbType, rawURL)
	if tunnelErr != nil {
		logger.PrintErrf("SSH 隧道建立失败: %s", tunnelErr, id)
		return nil
	}
	cfgList[0].Url = &resolvedURL

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

	resolvedURL, tunnelErr := ResolveConnURL(connId, cfgList[0].DbType, url)
	if tunnelErr != nil {
		logger.PrintErrf("SSH 隧道建立失败: %s", tunnelErr, connId)
		return nil
	}

	return database.GetConn(&database.DBParam{
		Id: cfgList[0].Id, Name: name, DbType: cfgList[0].DbType,
		User: user, Pwd: pwd, Url: resolvedURL,
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
	// 管理库未初始化时返回空（审计/日志等旁路调用不得 panic 中断业务）
	db := getDBOrNil()
	if db == nil {
		return "", ""
	}
	repo := NewConnRepo(db)
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

// GetConnCfgWithPwd 返回连接的原始配置（含解密后的密码）。
//
// 仅用于「高级导出」沙箱：把连接信息注入本地 Python 子进程的环境变量，
// 使模型编写的临时代码能够以只读方式连接数据库查询。
// 【安全约定】返回值含明文密码，调用方不得写入日志、不得返回给前端或进入模型上下文。
func GetConnCfgWithPwd(connId string) (*ConnCfg, error) {
	if connId == "" {
		return nil, errors.New("connId 为空")
	}
	db := getDBOrNil()
	if db == nil {
		return nil, errors.New("管理库未初始化")
	}
	cfgList, err := NewConnRepo(db).FindConnById(connId)
	if err != nil {
		return nil, err
	}
	if len(cfgList) == 0 {
		return nil, fmt.Errorf("连接配置不存在: %s", connId)
	}
	cfg := cfgList[0]
	pwd := ""
	if cfg.Pwd != nil && cfg.DbType != "sqlite" {
		decoded, decErr := crypto.AESDecode(*cfg.Pwd)
		if decErr != nil {
			return nil, fmt.Errorf("连接密码解密失败: %w", decErr)
		}
		pwd = decoded
	}
	cfg.Pwd = &pwd
	return &cfg, nil
}
