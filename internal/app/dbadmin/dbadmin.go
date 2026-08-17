// Package dbadmin 提供连接内数据库用户与库(schema)管理能力。
// 仅支持 MySQL/MariaDB 与 Oracle；本地/桌面模式无权限限制，
// 非本地模式（远程部署）仅管理员可操作（admin.CheckAdminPower）。
package dbadmin

import (
	"log"
	"regexp"
	"runtime/debug"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/sanitize"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// ===== 请求参数 =====

type DbUserParam struct {
	ConnId   string `json:"connId"`
	Username string `json:"username"`
	Host     string `json:"host"`
	Password string `json:"password"`
}

type SchemaParam struct {
	ConnId    string `json:"connId"`
	Schema    string `json:"schema"`
	Password  string `json:"password"`  // Oracle 创建 schema 即创建用户，需指定密码
	Charset   string `json:"charset"`   // MySQL 字符集，可空
	Collation string `json:"collation"` // MySQL 排序规则，可空
	// TableSpace Oracle 默认表空间，可空（默认 users）
	TableSpace string `json:"tableSpace"`
}

// ===== 安全校验 =====
// DDL（CREATE/DROP USER、CREATE/DROP DATABASE）不支持绑定参数，必须字符串拼接。
// 防注入策略：白名单正则校验 + 引号转义，双保险。

var (
	// hostPattern MySQL 主机模式：IP、主机名、通配符（%、_），禁止引号等特殊字符
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9._%-]{1,60}$`)
	// passwordPattern 密码：ASCII 可打印字符，长度 6-128；引号与反斜杠另行禁止（避免各数据库转义差异）
	passwordPattern = regexp.MustCompile(`^[!-~]{6,128}$`)
	// nameOptionPattern 字符集/排序规则/表空间名选项
	nameOptionPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,60}$`)
)

// quoteMySQLString 单引号字符串转义（' → ''，两种 sql_mode 下均安全）
func quoteMySQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// quoteOraclePassword 双引号密码转义（" → ""）
func quoteOraclePassword(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
}

func validatePassword(password string) error {
	if !passwordPattern.MatchString(password) || strings.ContainsAny(password, "\"'\\") {
		return errSafe("密码需 6-128 位，仅支持字母、数字与常见英文符号，不能包含引号和反斜杠")
	}
	return nil
}

func validateHost(host string) error {
	if !hostPattern.MatchString(host) {
		return errSafe("非法的主机名")
	}
	return nil
}

func validateNameOption(val, label string) error {
	if val == "" {
		return nil
	}
	if !nameOptionPattern.MatchString(val) {
		return errSafe("非法的" + label)
	}
	return nil
}

// errSafe 返回不携带原始 SQL/标识符的错误信息，避免泄露内部细节
type safeErr struct{ msg string }

func (e *safeErr) Error() string { return e.msg }

func errSafe(msg string) error { return &safeErr{msg: msg} }

// getDBOrErr 获取数据库连接。conn.GetConn 连接失败时会 panic（供 HTTP 中间件透传原始错误），
// 此处 recover 转为友好错误：管理界面直接展示驱动原因，且避免 gin recovery 打印长堆栈。
// recover 时将异常堆栈写入日志文件，便于定位连接失败的调用来源。
func getDBOrErr(connId, authorization string) (*sqlx.DB, error) {
	var panicVal any
	dc := func() (dc *sqlx.DB) {
		defer func() {
			if r := recover(); r != nil {
				panicVal = r
				dc = nil
				stack := debug.Stack()
				log.Printf("[dbadmin] 获取连接 panic 已恢复 - connId=%s, panic=%v\n%s", connId, r, string(stack))
			}
		}()
		return conn.GetConn(connId, authorization)
	}()
	if dc != nil {
		return dc, nil
	}
	if err, ok := panicVal.(error); ok {
		return nil, errSafe("数据库连接失败：" + err.Error())
	}
	// 非 panic 路径（无权访问/配置不存在等）conn.GetConn 内部已记录日志，此处补充关联信息
	if panicVal == nil {
		log.Printf("[dbadmin] 获取连接失败（无 panic，详见前述日志） - connId=%s", connId)
	}
	return nil, errSafe("数据库连接失败，请检查连接配置与网络")
}

// checkSupported 校验连接类型是否支持管理操作，返回数据库类型
func checkSupported(dc *sqlx.DB) (string, error) {
	dbType := dc.DriverName()
	switch dbType {
	case "mysql", "mariadb", "oracle":
		return dbType, nil
	}
	return "", errSafe("仅支持 MySQL/MariaDB 与 Oracle 连接")
}

// ===== HTTP Handlers =====

// ListDbUsers 列出连接内数据库用户。GET /api/db/admin/users?connId=xxx
// 返回 {users: [...], restricted: bool}：
// MySQL 普通账号无 mysql.user 系统表查询权限（Error 1142）时降级为仅显示当前连接账号，restricted=true。
func ListDbUsers(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	authorization := appctx.Ctx.GetAuthorization(c)
	connId := appctx.Ctx.GetConnID(c)
	dc, err := getDBOrErr(connId, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	dbType, err := checkSupported(dc)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	users := make([]map[string]any, 0)
	restricted := false
	switch dbType {
	case "mysql", "mariadb":
		rows, qErr := dc.Queryx("SELECT user AS username, host FROM mysql.user ORDER BY user, host")
		if qErr != nil {
			// 普通业务账号通常无 mysql.user 查询权限，降级为仅显示当前连接账号
			currentUser, cErr := queryCurrentUser(dc)
			if cErr != nil {
				response.WriteErr(c, 200, 500, "查询数据库用户失败，当前连接账号无 mysql.user 系统表权限")
				return
			}
			users = append(users, currentUser)
			restricted = true
		} else {
			defer rows.Close()
			users = scanUserRows(rows)
		}
	case "oracle":
		rows, qErr := dc.Queryx("SELECT username, NULL AS host FROM all_users ORDER BY username")
		if qErr != nil {
			response.WriteErr(c, 200, 500, "查询数据库用户失败，请确认当前连接账号具有相应权限")
			return
		}
		defer rows.Close()
		users = scanUserRows(rows)
	}
	response.WriteOK(c, gin.H{"users": users, "restricted": restricted})
}

// scanUserRows 统一扫描用户列表行
func scanUserRows(rows *sqlx.Rows) []map[string]any {
	users := make([]map[string]any, 0)
	for rows.Next() {
		var username, host *string
		if err := rows.Scan(&username, &host); err != nil {
			continue
		}
		item := map[string]any{"username": ""}
		if username != nil {
			item["username"] = *username
		}
		if host != nil {
			item["host"] = *host
		}
		users = append(users, item)
	}
	return users
}

// queryCurrentUser 查询 MySQL 当前连接账号（user@host），用于降级展示
func queryCurrentUser(dc *sqlx.DB) (map[string]any, error) {
	var currentUser string
	if err := dc.Get(&currentUser, "SELECT CURRENT_USER()"); err != nil {
		return nil, err
	}
	item := map[string]any{"username": currentUser}
	// CURRENT_USER() 返回 'user'@'host'，按最后一个 @ 拆分（host 部分不含 @）
	if idx := strings.LastIndex(currentUser, "@"); idx > 0 {
		item["username"] = currentUser[:idx]
		item["host"] = currentUser[idx+1:]
	}
	return item, nil
}

// SaveDbUser 创建数据库用户或重置密码。POST /api/db/admin/user/save
// mode=create 创建用户；mode=resetpwd 仅重置密码。
func SaveDbUser(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	authorization := appctx.Ctx.GetAuthorization(c)
	param := DbUserParam{}
	if err := c.ShouldBindJSON(&param); err != nil {
		response.WriteErr(c, 200, 500, "参数错误")
		return
	}
	mode := c.DefaultQuery("mode", "create")

	dc, err := getDBOrErr(param.ConnId, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	dbType, err := checkSupported(dc)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	if err := sanitize.ValidateIdentifier(param.Username, "用户名"); err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	if err := validatePassword(param.Password); err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	var sqlStr string
	switch dbType {
	case "mysql", "mariadb":
		if err := validateHost(param.Host); err != nil {
			response.WriteErr(c, 200, 500, err.Error())
			return
		}
		if mode == "resetpwd" {
			sqlStr = "ALTER USER " + quoteMySQLString(param.Username) + "@" + quoteMySQLString(param.Host) + " IDENTIFIED BY " + quoteMySQLString(param.Password)
		} else {
			sqlStr = "CREATE USER " + quoteMySQLString(param.Username) + "@" + quoteMySQLString(param.Host) + " IDENTIFIED BY " + quoteMySQLString(param.Password)
		}
	case "oracle":
		if mode == "resetpwd" {
			sqlStr = "ALTER USER " + sanitize.QuoteIdentifier(param.Username, "oracle") + " IDENTIFIED BY " + quoteOraclePassword(param.Password)
		} else {
			sqlStr = "CREATE USER " + sanitize.QuoteIdentifier(param.Username, "oracle") + " IDENTIFIED BY " + quoteOraclePassword(param.Password)
		}
	}
	if _, err := dc.Exec(sqlStr); err != nil {
		response.WriteErr(c, 200, 500, friendlyExecErr(mode, err))
		return
	}
	response.WriteOK(c, nil)
}

// DropDbUser 删除数据库用户。POST /api/db/admin/user/drop
// Oracle 使用 CASCADE 连同用户对象一并删除（前端已有明确二次确认）。
func DropDbUser(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	authorization := appctx.Ctx.GetAuthorization(c)
	param := DbUserParam{}
	if err := c.ShouldBindJSON(&param); err != nil {
		response.WriteErr(c, 200, 500, "参数错误")
		return
	}
	dc, err := getDBOrErr(param.ConnId, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	dbType, err := checkSupported(dc)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	if err := sanitize.ValidateIdentifier(param.Username, "用户名"); err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	var sqlStr string
	switch dbType {
	case "mysql", "mariadb":
		if err := validateHost(param.Host); err != nil {
			response.WriteErr(c, 200, 500, err.Error())
			return
		}
		sqlStr = "DROP USER " + quoteMySQLString(param.Username) + "@" + quoteMySQLString(param.Host)
	case "oracle":
		sqlStr = "DROP USER " + sanitize.QuoteIdentifier(param.Username, "oracle") + " CASCADE"
	}
	if _, err := dc.Exec(sqlStr); err != nil {
		response.WriteErr(c, 200, 500, friendlyExecErr("drop-user", err))
		return
	}
	response.WriteOK(c, nil)
}

// ListAdminSchemas 列出连接内所有库/schema（管理员视角，不做权限过滤）。
// GET /api/db/admin/schemas?connId=xxx
func ListAdminSchemas(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	authorization := appctx.Ctx.GetAuthorization(c)
	connId := appctx.Ctx.GetConnID(c)
	dc, err := getDBOrErr(connId, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	dbType, err := checkSupported(dc)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	var sqlStr string
	switch dbType {
	case "mysql", "mariadb":
		sqlStr = "SELECT schema_name FROM information_schema.schemata ORDER BY schema_name"
	case "oracle":
		sqlStr = "SELECT username FROM all_users ORDER BY username"
	}
	rows, err := dc.Queryx(sqlStr)
	if err != nil {
		response.WriteErr(c, 200, 500, "查询库列表失败")
		return
	}
	defer rows.Close()

	schemas := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		schemas = append(schemas, name)
	}
	response.WriteOK(c, schemas)
}

// CreateSchema 创建库（MySQL/MariaDB）或 schema（Oracle，等价于创建用户）。
// POST /api/db/admin/schema/create
func CreateSchema(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	authorization := appctx.Ctx.GetAuthorization(c)
	param := SchemaParam{}
	if err := c.ShouldBindJSON(&param); err != nil {
		response.WriteErr(c, 200, 500, "参数错误")
		return
	}
	dc, err := getDBOrErr(param.ConnId, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	dbType, err := checkSupported(dc)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	if err := sanitize.ValidateIdentifier(param.Schema, "库名"); err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	var sqlStr string
	switch dbType {
	case "mysql", "mariadb":
		if err := validateNameOption(param.Charset, "字符集"); err != nil {
			response.WriteErr(c, 200, 500, err.Error())
			return
		}
		if err := validateNameOption(param.Collation, "排序规则"); err != nil {
			response.WriteErr(c, 200, 500, err.Error())
			return
		}
		sqlStr = "CREATE DATABASE " + sanitize.QuoteIdentifier(param.Schema, "mysql")
		if param.Charset != "" {
			sqlStr += " DEFAULT CHARACTER SET " + param.Charset
		}
		if param.Collation != "" {
			sqlStr += " COLLATE " + param.Collation
		}
	case "oracle":
		if err := validatePassword(param.Password); err != nil {
			response.WriteErr(c, 200, 500, err.Error())
			return
		}
		tableSpace := param.TableSpace
		if tableSpace == "" {
			tableSpace = "users"
		}
		if err := validateNameOption(tableSpace, "表空间名"); err != nil {
			response.WriteErr(c, 200, 500, err.Error())
			return
		}
		sqlStr = "CREATE USER " + sanitize.QuoteIdentifier(param.Schema, "oracle") +
			" IDENTIFIED BY " + quoteOraclePassword(param.Password) +
			" DEFAULT TABLESPACE " + tableSpace
	}
	if _, err := dc.Exec(sqlStr); err != nil {
		response.WriteErr(c, 200, 500, friendlyExecErr("create-schema", err))
		return
	}
	response.WriteOK(c, nil)
}

// DropSchema 删除库（MySQL DROP DATABASE）或 schema（Oracle DROP USER CASCADE）。
// POST /api/db/admin/schema/drop
func DropSchema(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	authorization := appctx.Ctx.GetAuthorization(c)
	param := SchemaParam{}
	if err := c.ShouldBindJSON(&param); err != nil {
		response.WriteErr(c, 200, 500, "参数错误")
		return
	}
	dc, err := getDBOrErr(param.ConnId, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	dbType, err := checkSupported(dc)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	if err := sanitize.ValidateIdentifier(param.Schema, "库名"); err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	// 防止删除连接当前正在使用的库
	var sqlStr string
	switch dbType {
	case "mysql", "mariadb":
		var current string
		if err := dc.Get(&current, "SELECT DATABASE()"); err == nil && strings.EqualFold(current, param.Schema) {
			response.WriteErr(c, 200, 500, "不能删除连接当前正在使用的库")
			return
		}
		sqlStr = "DROP DATABASE " + sanitize.QuoteIdentifier(param.Schema, "mysql")
	case "oracle":
		var current string
		if err := dc.Get(&current, "SELECT SYS_CONTEXT('USERENV','CURRENT_SCHEMA') FROM DUAL"); err == nil && strings.EqualFold(current, param.Schema) {
			response.WriteErr(c, 200, 500, "不能删除连接当前正在使用的 schema")
			return
		}
		sqlStr = "DROP USER " + sanitize.QuoteIdentifier(param.Schema, "oracle") + " CASCADE"
	}
	if _, err := dc.Exec(sqlStr); err != nil {
		response.WriteErr(c, 200, 500, friendlyExecErr("drop-schema", err))
		return
	}
	response.WriteOK(c, nil)
}

// friendlyExecErr 将执行错误转换为用户友好的提示（不透出原始 SQL 细节）
func friendlyExecErr(op string, err error) string {
	msg := "操作失败，请检查名称是否已存在或当前账号是否有权限"
	switch op {
	case "create":
		return msg
	case "resetpwd":
		return "重置密码失败，请检查用户是否存在或当前账号是否有权限"
	case "drop-user":
		return "删除用户失败，请检查用户是否存在或当前账号是否有权限"
	case "create-schema":
		return "创建失败，请检查名称是否已存在或当前账号是否有权限"
	case "drop-schema":
		return "删除失败，请检查名称是否存在或当前账号是否有权限"
	}
	return msg
}
