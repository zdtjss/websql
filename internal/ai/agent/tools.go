package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
	"websql/internal/ai/agent/sqlutil"
	admin "websql/internal/app/admin"
	conn "websql/internal/app/conn"
	"websql/internal/audit"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"

	"github.com/jmoiron/sqlx"
)

var validTableNameRegex = regexp.MustCompile("^[a-zA-Z0-9_`\".]+$")

var dangerousTableNamePatterns = []string{
	";", "--", "/*", "*/", "'", "\\",
	"xp_", "sp_", "0x", "char(", "exec(",
	"union", "select", "insert", "update", "delete",
	"drop", "alter", "create", "truncate", "exec",
}

func isValidTableName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || !validTableNameRegex.MatchString(name) {
		return false
	}
	lower := strings.ToLower(name)
	for _, pattern := range dangerousTableNamePatterns {
		if strings.Contains(lower, pattern) {
			return false
		}
	}
	return true
}

func safeQuoteTableName(name string) string {
	if !isValidTableName(name) {
		return ""
	}
	cleaned := strings.ReplaceAll(name, "`", "")
	cleaned = strings.ReplaceAll(cleaned, `"`, "")
	cleaned = strings.ReplaceAll(cleaned, ".", "")
	return "'" + strings.ReplaceAll(cleaned, "'", "''") + "'"
}

type QueryInput struct {
	SQL    string `json:"sql" jsonschema:"required"`
	ConnID string `json:"connId,omitempty"`
}
type QueryOutput struct {
	Columns []string         `json:"columns"`
	Data    []map[string]any `json:"data"`
	Count   int              `json:"count"`
}
type ExecInput struct {
	SQL    string `json:"sql" jsonschema:"required"`
	ConnID string `json:"connId,omitempty"`
}
type ExecOutput struct {
	AffectedRows int64  `json:"affectedRows"`
	Message      string `json:"message"`
}
type ListTablesInput struct {
	ConnID string `json:"connId,omitempty"`
}
type ListTablesOutput struct {
	Tables []TableInfo `json:"tables"`
	Count  int         `json:"count"`
}
type TableInfo struct {
	TableName    string `json:"tableName"`
	TableComment string `json:"tableComment,omitempty"`
}

type SchemaInput struct {
	Tables []string `json:"tables" jsonschema:"required"`
}
type SchemaOutput struct {
	Schema string `json:"schema"`
}
type ExportInput struct {
	SQL      string `json:"sql" jsonschema:"required"`
	FileName string `json:"fileName"`
}
type ExportOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
}
type CurrentDateInfoInput struct{}
type CurrentDateInfoOutput struct {
	Date     string `json:"date"`
	Weekday  string `json:"weekday"`
	DateTime string `json:"dateTime"`
}

func GetConn(connId, userId string) (*sqlx.DB, string) {
	if connId == "" {
		return nil, ""
	}
	if !admin.CheckConnAccessByUserId(userId, connId) {
		return nil, ""
	}
	cfgList := []conn.ConnCfg{}
	err := database.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connId)
	if err != nil || len(cfgList) == 0 {
		return nil, ""
	}
	cfg := &cfgList[0]
	deref := func(p *string) string {
		if p != nil {
			return *p
		}
		return ""
	}
	pwd := ""
	if cfg.Pwd != nil && cfg.DbType != "sqlite" {
		pwd = crypto.AESDecode(*cfg.Pwd)
	}
	conn := database.GetConn(&database.DBParam{
		Id: cfg.Id, Name: deref(cfg.Name), DbType: cfg.DbType,
		User: deref(cfg.User), Pwd: pwd, Url: deref(cfg.Url),
	})
	return conn, cfg.DbType
}

func GetCurrentDateInfo() func(ctx context.Context, input *CurrentDateInfoInput) (*CurrentDateInfoOutput, error) {
	return func(ctx context.Context, input *CurrentDateInfoInput) (*CurrentDateInfoOutput, error) {
		now := time.Now()
		weekdayNames := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
		weekday := weekdayNames[now.Weekday()]
		return &CurrentDateInfoOutput{
			Date:     now.Format("2006-01-02"),
			Weekday:  weekday,
			DateTime: now.Format("2006-01-02 15:04:05"),
		}, nil
	}
}

func buildConnLookup(schemas []SchemaRef) map[string]string {
	connLookup := make(map[string]string)
	for _, s := range schemas {
		if s.ConnID != "" && s.Schema != "" {
			key := strings.ToUpper(s.Schema)
			if existing, ok := connLookup[key]; ok && existing != s.ConnID {
				logger.PrintErr(fmt.Errorf("schema name collision: %q 同时存在于连接 %s 和 %s，将使用后者",
					s.Schema, existing, s.ConnID))
			}
			connLookup[key] = s.ConnID
		}
	}
	return connLookup
}

// buildSchemaNames 返回所有 schema 名的切片，用于错误提示
func buildSchemaNames(schemas []SchemaRef) []string {
	seen := make(map[string]bool)
	var names []string
	for _, s := range schemas {
		if s.Schema != "" && !seen[s.Schema] {
			seen[s.Schema] = true
			names = append(names, s.Schema)
		}
	}
	return names
}

func resolveConnID(defaultConnID, connID string, connLookup map[string]string) string {
	if connID == "" {
		return defaultConnID
	}
	if schemaConnID, ok := connLookup[strings.ToUpper(connID)]; ok {
		return schemaConnID
	}
	return connID
}

func NewQueryFunc(connId string, schemas []SchemaRef, auditCtx *ExecAuditCtx, userId string) func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
	connLookup := buildConnLookup(schemas)
	schemaNames := buildSchemaNames(schemas)
	return func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
		startTime := time.Now()
		targetConnID := resolveConnID(connId, input.ConnID, connLookup)
		log.Printf("[Tool:query_data] connId=%s sql=%s \n", targetConnID, input.SQL)
		conn, dbType := GetConn(targetConnID, userId)
		if conn == nil {
			msg := fmt.Sprintf("db conn not found: %s", targetConnID)
			if len(schemaNames) > 0 {
				msg += fmt.Sprintf("。可用 schema 名：%v（作为 connId 传入），默认连接ID：%s", schemaNames, connId)
			}
			err := fmt.Errorf("%s", msg)
			recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), audit.FormatErrorWithStack(err))
			return nil, err
		}
		sql := strings.TrimSpace(input.SQL)
		stripped := sqlutil.StripSQLComments(sql)
		upper := strings.ToUpper(stripped)
		if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "SHOW") &&
			!strings.HasPrefix(upper, "DESCRIBE") && !strings.HasPrefix(upper, "EXPLAIN") &&
			!strings.HasPrefix(upper, "WITH") {
			err := errors.New("query_data only supports SELECT/SHOW/DESCRIBE/EXPLAIN/WITH")
			recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), audit.FormatErrorWithStack(err))
			return nil, err
		}
		// 方言兼容性预检：在执行前检测不兼容语法（如 MySQL 上的 PERCENTILE_CONT），
		// 避免无谓的数据库往返，同时给 LLM 提供精确的替代写法
		if dialectErrs := sqlutil.CheckDialectCompatibility(sql, dbType); len(dialectErrs) > 0 {
			errMsg := sqlutil.FormatDialectErrors(dialectErrs)
			log.Printf("[Tool:query_data] 方言预检失败 - dbType=%s, errors=%d\n", dbType, len(dialectErrs))
			recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), errMsg)
			return nil, errors.New(errMsg)
		}
		if strings.HasPrefix(upper, "WITH") {
			writeKW := []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "TRUNCATE ", "ALTER ", "CREATE ", "REPLACE ", "MERGE "}
			for _, kw := range writeKW {
				if strings.Contains(strings.ToUpper(stripped), kw) {
					err := fmt.Errorf("query_data does not allow write operations (%s) in WITH, use exec_sql", strings.TrimSpace(kw))
					recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), audit.FormatErrorWithStack(err))
					return nil, err
				}
			}
		}
		if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") {
			sql = applyRowLimit(sql, conn.DriverName(), 500)
		}
		queryCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		rows, err := conn.QueryxContext(queryCtx, sql)
		if err != nil {
			if input.ConnID == "" && targetConnID == connId {
				if altConnID, altConn, altSchema := tryAlternativeConn(input.SQL, connLookup, connId, userId); altConn != nil {
					log.Printf("[Tool:query_data] 默认连接查询失败，自动路由到连接 %s（schema=%s）\n", altConnID, altSchema)
					// 安全审计：自动路由事件必须可追溯（EINO_DEEP_ANALYSIS §13）
					if auditCtx != nil {
						audit.GetAuditService().Record(&audit.AuditEntry{
							Source:    "agent",
							ToolName:  "query_data.auto_route",
							SQLText:   input.SQL,
							SQLType:   "SELECT",
							RiskLevel: "low",
							Status:    "auto_routed",
							ConnID:    altConnID,
							SessionID: auditCtx.SessionID,
							UserID:    auditCtx.UserID,
							UserName:  auditCtx.UserName,
							ErrorMsg:  fmt.Sprintf("from=%s to=%s schema=%s reason=%v", connId, altConnID, altSchema, err),
						})
					}
					altRawSQL := strings.TrimSpace(input.SQL)
					altSQL := qualifyBareTableNames(altRawSQL, altConn.DriverName(), altSchema)
					altQueryCtx, altCancel := context.WithTimeout(ctx, 60*time.Second)
					defer altCancel()
					if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") {
						altSQL = applyRowLimit(altSQL, altConn.DriverName(), 500)
					}
					altRows, altErr := altConn.QueryxContext(altQueryCtx, altSQL)
					if altErr == nil {
						defer altRows.Close()
						cols, _ := altRows.Columns()
						data, err := database.GetResultRows(altConn.DriverName(), altRows)
						if err != nil {
							logger.PrintErrf("查询数据失败", err)
							return nil, fmt.Errorf("query failed: %w", err)
						}
						recordQueryAudit(auditCtx, input.SQL, altConnID, "success", len(data), int(time.Since(startTime).Milliseconds()), "")
						return &QueryOutput{Columns: cols, Data: data, Count: len(data)}, nil
					}
					log.Printf("[Tool:query_data] 自动路由查询也失败: %v，返回原始错误\n", altErr)
				}
			}
			recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), audit.FormatErrorWithStack(err))
			return nil, fmt.Errorf("query failed: %w", err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		data, err := database.GetResultRows(conn.DriverName(), rows)
		if err != nil {
			logger.PrintErrf("查询数据失败", err)
			return nil, fmt.Errorf("query failed: %w", err)
		}
		recordQueryAudit(auditCtx, input.SQL, targetConnID, "success", len(data), int(time.Since(startTime).Milliseconds()), "")
		return &QueryOutput{Columns: cols, Data: data, Count: len(data)}, nil
	}
}

func recordQueryAudit(auditCtx *ExecAuditCtx, sql, connID, status string, affectedRows, execTimeMs int, errorMsg string) {
	if auditCtx == nil {
		return
	}
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:       "agent",
		ToolName:     "query_data",
		SQLText:      sql,
		SQLType:      "SELECT",
		RiskLevel:    "low",
		Status:       status,
		ConnID:       connID,
		SessionID:    auditCtx.SessionID,
		UserID:       auditCtx.UserID,
		UserName:     auditCtx.UserName,
		AffectedRows: affectedRows,
		ExecTimeMs:   execTimeMs,
		ErrorMsg:     errorMsg,
	})
}

// tryAlternativeConn 在用户没有显式传 connId 时，尝试把 SQL 自动路由到其他连接。
//
// EINO_DEEP_ANALYSIS §13 安全加固：
//  1. **必须**是用户 SQL 显式带了 schema（`select * from x.users` 中的 x），
//     才允许跨连接自动路由。**否则**直接放弃路由——让 LLM 知道"当前连接没这个表"。
//     这避免了"用户在 connA 想查 users，系统静默切到 connB 的 users"这种隐式
//     横向移动（违反 least-privilege）。
//  2. 自动路由**只能**发生在 SELECT/SHOW/DESCRIBE/EXPLAIN/WITH 这类只读
//     操作上（由调用方在调用本函数前保证）。
//  3. 任何一次自动路由都会**强制**记一条 audit，便于事后追溯。
//
// 返回：目标 connID、连接的 *sqlx.DB、匹配的 schema；都不满足时返回 "", nil, ""。
func tryAlternativeConn(sql string, connLookup map[string]string, defaultConnID string, userId string) (string, *sqlx.DB, string) {
	// 安全：只承认"显式 schema.表名"形式。模糊 FROM users 不会触发跨连接路由。
	schema := extractExplicitSchemaFromSQL(sql)
	if schema == "" {
		log.Printf("[Tool:query_data] tryAlternativeConn 拒绝：用户 SQL 未指定显式 schema，按 least-privilege 不跨连接路由 - connId=%s\n", defaultConnID)
		return "", nil, ""
	}
	altConnID, ok := connLookup[strings.ToUpper(schema)]
	if !ok || altConnID == defaultConnID {
		return "", nil, ""
	}
	altConn, _ := GetConn(altConnID, userId)
	if altConn != nil {
		log.Printf("[Tool:query_data] 命中显式 schema=%s 跨连接路由 - from=%s to=%s\n", schema, defaultConnID, altConnID)
		return altConnID, altConn, schema
	}
	return "", nil, ""
}

// extractExplicitSchemaFromSQL 只识别"schema 显式出现"的情况：
//   - SELECT * FROM mySchema.myTable
//   - SELECT * FROM mySchema.myTable t1 JOIN mySchema.myTable t2 ON ...
//
// **不**识别：
//   - SELECT * FROM myTable（无 schema 前缀）
//
// 区别于 extractSchemaFromSQL（后者可能从配置/会话中推断 schema），
// 本函数是**纯静态**文本分析，避免误判。
func extractExplicitSchemaFromSQL(sql string) string {
	cleaned := sqlutil.StripSQLComments(sql)
	// 三段式：schema.table
	re := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|UPDATE|INSERT\s+INTO|DELETE\s+FROM|MERGE\s+INTO)\s+(?:` +
		"`" + `([A-Za-z_][A-Za-z0-9_]*)` + "`" + `\s*\.\s*` + "`" + `[A-Za-z_][A-Za-z0-9_]*` + "`" + `|` +
		`"([A-Za-z_][A-Za-z0-9_]*)"\s*\.\s*"([A-Za-z_][A-Za-z0-9_]*)"` + `|` +
		`([A-Za-z_][A-Za-z0-9_]*)\s*\.\s*([A-Za-z_][A-Za-z0-9_]*))`)
	matches := re.FindAllStringSubmatch(cleaned, -1)
	for _, m := range matches {
		// 第一个非空 capture group 就是 schema（capture 1/2/4 都是 schema 部分）
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				// 双引号场景的 schema 在 group 2（双引号左半边）；反引号在 group 1
				// 无引号场景的 schema 在 group 4（左半边）
				return strings.ToUpper(m[i])
			}
		}
	}
	return ""
}

func extractTableNamesFromSQL(sql string) []string {
	// 先剥离注释，避免注释里的 from/join/select 等被误识别为表名或关键字
	cleaned := sqlutil.StripSQLComments(sql)
	re := regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+(?:` +
		"`" + `([^` + "`" + `]+)` + "`" + `|` +
		`"([^"]+)"|` +
		`\[([^\]]+)\]|` +
		`([A-Za-z_][A-Za-z0-9_]*(?:\s*\.\s*[A-Za-z_][A-Za-z0-9_]*)*))`)
	matches := re.FindAllStringSubmatch(cleaned, -1)
	var tables []string
	seen := make(map[string]bool)
	for _, m := range matches {
		var raw string
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				raw = m[i]
				break
			}
		}
		if raw == "" {
			continue
		}
		dotIdx := strings.LastIndex(raw, ".")
		var tableName string
		if dotIdx >= 0 && dotIdx < len(raw)-1 {
			tableName = raw[dotIdx+1:]
		} else {
			tableName = raw
		}
		tableName = strings.TrimSpace(tableName)
		if tableName != "" && !seen[strings.ToUpper(tableName)] {
			seen[strings.ToUpper(tableName)] = true
			tables = append(tables, strings.ToUpper(tableName))
		}
	}
	return tables
}

func qualifyBareTableNames(sql, dbType, schema string) string {
	if schema == "" {
		return sql
	}
	// 先剥离注释，避免注释中的"FROM xxx"被误加前缀
	cleaned := sqlutil.StripSQLComments(sql)
	re := regexp.MustCompile(`(?i)\b(FROM|JOIN)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	var result strings.Builder
	lastEnd := 0
	for _, loc := range re.FindAllStringSubmatchIndex(cleaned, -1) {
		// 注意：以下索引均指向 cleaned，但写回到 result，意味着返回值
		// 不包含原始注释（这通常是期望的：避免 SQL 含 "/* schema-1 */ SELECT 1"
		// 然后被错误地加上 schema-2 前缀）。
		// 重新拼装 cleaned[loc[2]:loc[5]] 部分以保留原 case。
		keyword := cleaned[loc[2]:loc[3]]
		tableName := cleaned[loc[4]:loc[5]]
		afterMatch := cleaned[loc[1]:]
		trimmed := strings.TrimLeft(afterMatch, " \t")
		result.WriteString(cleaned[lastEnd:loc[0]])
		if len(trimmed) > 0 && trimmed[0] == '.' {
			// 已有 schema 前缀，原样保留
			result.WriteString(cleaned[loc[0]:loc[1]])
		} else {
			result.WriteString(keyword + " " + quoteTableRef(dbType, schema, tableName))
		}
		lastEnd = loc[1]
	}
	result.WriteString(cleaned[lastEnd:])
	return result.String()
}

// ExecAuditCtx holds context for audit logging within exec_sql tool
type ExecAuditCtx struct {
	ConnID    string
	UserID    string
	UserName  string
	SessionID string
}

func NewExecFunc(connId string, schemas []SchemaRef, auditCtx *ExecAuditCtx, userId string) func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
	connLookup := buildConnLookup(schemas)
	schemaNames := buildSchemaNames(schemas)
	return func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
		startTime := time.Now()
		targetConnID := resolveConnID(connId, input.ConnID, connLookup)
		log.Printf("[Tool:exec_sql] sql=%s connId=%s\n", input.SQL, targetConnID)
		sql := strings.TrimSpace(input.SQL)
		if sql == "" {
			return nil, errors.New("SQL is empty")
		}

		isDefaultConn := targetConnID == connId
		if !isDefaultConn && input.ConnID != "" {
			log.Printf("[Tool:exec_sql] 非默认连接操作：原始connId=%s → 目标连接=%s（默认=%s）", input.ConnID, targetConnID, connId)
		}
		conn, dbType := GetConn(targetConnID, userId)
		if conn == nil {
			msg := fmt.Sprintf("db conn not found: %s（原始输入 connId=%s，解析后=%s）", targetConnID, input.ConnID, targetConnID)
			if len(schemaNames) > 0 {
				msg += fmt.Sprintf("。可用 schema 名：%v（作为 connId 传入），默认连接ID：%s", schemaNames, connId)
			}
			return nil, fmt.Errorf("%s", msg)
		}

		// 方言兼容性预检
		if dialectErrs := sqlutil.CheckDialectCompatibility(sql, dbType); len(dialectErrs) > 0 {
			errMsg := sqlutil.FormatDialectErrors(dialectErrs)
			log.Printf("[Tool:exec_sql] 方言预检失败 - dbType=%s, errors=%d\n", dbType, len(dialectErrs))
			return nil, errors.New(errMsg)
		}

		sqlType := string(sqlutil.DetectSQLType(sql))
		riskLevel := string(sqlutil.DetectRiskLevel(sql))

		result, err := conn.ExecContext(ctx, sql)
		if err != nil {
			recordExecAudit(auditCtx, sql, targetConnID, sqlType, riskLevel, "failed", 0, int(time.Since(startTime).Milliseconds()), audit.FormatErrorWithStack(err))
			return nil, fmt.Errorf("exec failed on conn=%s: %w", targetConnID, err)
		}
		affected, _ := result.RowsAffected()
		recordExecAudit(auditCtx, sql, targetConnID, sqlType, riskLevel, "success", int(affected), int(time.Since(startTime).Milliseconds()), "")
		msg := fmt.Sprintf("ok, %d rows affected（连接：%s）", affected, targetConnID)
		log.Printf("[Tool:exec_sql] ok - %s\n", msg)
		return &ExecOutput{AffectedRows: affected, Message: msg}, nil
	}
}

func recordExecAudit(auditCtx *ExecAuditCtx, sql, connID, sqlType, riskLevel, status string, affectedRows, execTimeMs int, errorMsg string) {
	if auditCtx == nil {
		return
	}
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:       "agent",
		ToolName:     "exec_sql",
		SQLText:      sql,
		SQLType:      sqlType,
		RiskLevel:    riskLevel,
		Status:       status,
		ConnID:       connID,
		SessionID:    auditCtx.SessionID,
		UserID:       auditCtx.UserID,
		UserName:     auditCtx.UserName,
		AffectedRows: affectedRows,
		ExecTimeMs:   execTimeMs,
		ErrorMsg:     errorMsg,
	})
}

func NewSchemaFunc(connId, dbType, dbSchema string, schemas []SchemaRef, userId string) func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
	schemaConnMap := buildConnLookup(schemas)
	schemaNames := buildSchemaNames(schemas)

	return func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
		log.Printf("[Tool:get_table_schema] tables=%v\n", input.Tables)

		getTableConn := func(table string) (*sqlx.DB, string, string) {
			schemaName, _ := splitSchemaTable(table, dbSchema)
			if schemaName != "" && schemaName != dbSchema {
				if targetConnID, ok := schemaConnMap[strings.ToUpper(schemaName)]; ok {
					if c, t := GetConn(targetConnID, userId); c != nil {
						return c, t, targetConnID
					}
				}
			}
			c, t := GetConn(connId, userId)
			return c, t, connId
		}

		trySchemaOnConn := func(conn *sqlx.DB, actualDBType, schemaName, tableName string) (string, bool) {
			if conn == nil {
				return "", false
			}
			var schemaSQL string
			switch actualDBType {
			case "mysql", "mariadb":
				if schemaName != "" {
					schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", schemaName, tableName)
				} else {
					schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`", tableName)
				}
			case "sqlite":
				schemaSQL = "SELECT sql FROM sqlite_master WHERE type='table' AND name=?"
			case "oracle":
				if schemaName != "" {
					schemaSQL = fmt.Sprintf("SELECT DBMS_METADATA.GET_DDL('TABLE', '%s', '%s') FROM DUAL",
						strings.ToUpper(tableName), strings.ToUpper(schemaName))
				} else {
					schemaSQL = fmt.Sprintf("SELECT DBMS_METADATA.GET_DDL('TABLE', '%s') FROM DUAL",
						strings.ToUpper(tableName))
				}
			default:
				if schemaName != "" {
					schemaSQL = fmt.Sprintf("SHOW CREATE TABLE \"%s\".\"%s\"", schemaName, tableName)
				} else {
					schemaSQL = fmt.Sprintf("SHOW CREATE TABLE \"%s\"", tableName)
				}
			}
			schemaCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			var rows *sqlx.Rows
			var err error
			switch actualDBType {
			case "sqlite":
				rows, err = conn.QueryxContext(schemaCtx, schemaSQL, tableName)
			default:
				rows, err = conn.QueryxContext(schemaCtx, schemaSQL)
			}
			if err != nil {
				return "", false
			}
			defer rows.Close()
			var sb strings.Builder
			for rows.Next() {
				switch actualDBType {
				case "sqlite":
					var createSQL string
					if err := rows.Scan(&createSQL); err == nil && createSQL != "" {
						sb.WriteString(createSQL)
						sb.WriteString(";\n\n")
					}
				case "oracle":
					var ddl string
					if err := rows.Scan(&ddl); err == nil && ddl != "" {
						sb.WriteString(ddl)
						sb.WriteString(";\n\n")
					}
				default:
					var tn, createTable string
					if err := rows.Scan(&tn, &createTable); err == nil {
						sb.WriteString(createTable)
						sb.WriteString(";\n\n")
					}
				}
			}
			result := sb.String()
			return result, result != ""
		}

		var sb strings.Builder
		for _, table := range input.Tables {
			if !isValidTableName(table) {
				logger.PrintErr(fmt.Errorf("invalid table name: %s", table))
				continue
			}
			schemaName, tableName := splitSchemaTable(table, dbSchema)
			tableConn, actualDBType, _ := getTableConn(table)

			if tableConn == nil {
				msg := fmt.Sprintf("db conn not found for table: %s", table)
				if len(schemaNames) > 0 {
					msg += fmt.Sprintf("。可用 schema 名：%v", schemaNames)
				}
				logger.PrintErr(fmt.Errorf("%s", msg))
				continue
			}

			if result, ok := trySchemaOnConn(tableConn, actualDBType, schemaName, tableName); ok {
				sb.WriteString(result)
				continue
			}

			if schemaName == dbSchema || schemaName == "" {
				found := false
				for altSchema, altConnID := range schemaConnMap {
					if altConnID == connId {
						continue
					}
					altConn, altDBType := GetConn(altConnID, userId)
					if altConn == nil {
						continue
					}
					if result, ok := trySchemaOnConn(altConn, altDBType, altSchema, tableName); ok {
						log.Printf("[Tool:get_table_schema] 表 %s 在默认连接未找到，自动路由到 schema=%s (conn=%s)\n", table, altSchema, altConnID)
						sb.WriteString(result)
						found = true
						break
					}
				}
				if found {
					continue
				}
			}

			logger.PrintErr(fmt.Errorf("get schema failed %s: not found on any connection", table))
			sb.WriteString(fallbackColumnInfo(tableConn, actualDBType, schemaName, tableName))
		}
		return &SchemaOutput{Schema: sb.String()}, nil
	}
}

func NewListTablesFunc(connId, dbType, dbSchema string, schemas []SchemaRef, userId string) func(ctx context.Context, input *ListTablesInput) (*ListTablesOutput, error) {
	connLookup := buildConnLookup(schemas)
	schemaNames := buildSchemaNames(schemas)

	return func(ctx context.Context, input *ListTablesInput) (*ListTablesOutput, error) {
		targetConnID := resolveConnID(connId, input.ConnID, connLookup)
		conn, actualDBType := GetConn(targetConnID, userId)
		if conn == nil {
			msg := fmt.Sprintf("db conn not found: %s", targetConnID)
			if len(schemaNames) > 0 {
				msg += fmt.Sprintf("。可用 schema 名：%v（作为 connId 传入），默认连接ID：%s", schemaNames, connId)
			}
			return nil, fmt.Errorf("%s", msg)
		}

		_, actualDbSchema, _ := GetDBInfo(targetConnID)
		if actualDbSchema == "" {
			actualDbSchema = dbSchema
		}

		log.Printf("[Tool:list_tables] connId=%s, targetConn=%s, dbType=%s, dbSchema=%s\n", input.ConnID, targetConnID, actualDBType, actualDbSchema)

		listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		var query string
		var args []any

		switch actualDBType {
		case "mysql", "mariadb":
			if actualDbSchema != "" {
				query = "SELECT TABLE_NAME, TABLE_COMMENT FROM information_schema.TABLES WHERE table_schema = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME"
				args = []any{actualDbSchema}
			} else {
				query = "SELECT TABLE_NAME, TABLE_COMMENT FROM information_schema.TABLES WHERE table_schema = DATABASE() AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME"
			}
		case "oracle":
			if actualDbSchema != "" {
				query = "SELECT TABLE_NAME, COMMENTS FROM ALL_TAB_COMMENTS WHERE OWNER = :1 AND TABLE_TYPE = 'TABLE' ORDER BY TABLE_NAME"
				args = []any{strings.ToUpper(actualDbSchema)}
			} else {
				query = "SELECT TABLE_NAME, COMMENTS FROM USER_TAB_COMMENTS WHERE TABLE_TYPE = 'TABLE' ORDER BY TABLE_NAME"
			}
		case "sqlite":
			query = "SELECT name, '' FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
		default:
			if actualDbSchema != "" {
				query = "SELECT TABLE_NAME, TABLE_COMMENT FROM information_schema.TABLES WHERE table_schema = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME"
				args = []any{actualDbSchema}
			} else {
				query = "SELECT TABLE_NAME, TABLE_COMMENT FROM information_schema.TABLES WHERE table_schema = DATABASE() AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME"
			}
		}

		rows, err := conn.QueryxContext(listCtx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("list tables failed: %w", err)
		}
		defer rows.Close()

		var tables []TableInfo
		for rows.Next() {
			var ti TableInfo
			if err := rows.Scan(&ti.TableName, &ti.TableComment); err != nil {
				continue
			}
			tables = append(tables, ti)
		}

		return &ListTablesOutput{Tables: tables, Count: len(tables)}, nil
	}
}

func fallbackColumnInfo(conn *sqlx.DB, dbType, dbSchema, table string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "-- Table: %s", table)
	if dbSchema != "" && !strings.EqualFold(dbSchema, table) {
		fmt.Fprintf(&sb, " (schema: %s)", dbSchema)
	}
	sb.WriteString("\n")
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		query = "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
		args = []any{dbSchema, table}
	case "sqlite":
		quoted := safeQuoteTableName(table)
		if quoted == "" {
			return sb.String()
		}
		query = "PRAGMA table_info(" + quoted + ")"
	case "oracle":
		if !isValidTableName(table) {
			return sb.String()
		}
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME, DATA_TYPE, DATA_LENGTH, NULLABLE FROM ALL_TAB_COLUMNS WHERE OWNER = :1 AND TABLE_NAME = :2 ORDER BY COLUMN_ID"
			args = []any{strings.ToUpper(dbSchema), strings.ToUpper(table)}
		} else {
			query = "SELECT COLUMN_NAME, DATA_TYPE, DATA_LENGTH, NULLABLE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = :1 ORDER BY COLUMN_ID"
			args = []any{strings.ToUpper(table)}
		}
	default:
		return sb.String()
	}
	rows, err := conn.Queryx(query, args...)
	if err != nil {
		return sb.String()
	}
	defer rows.Close()
	data, err := database.GetResultRows(conn.DriverName(), rows)
	if err != nil {
		logger.PrintErrf("获取列信息失败", err)
		return sb.String()
	}
	for _, row := range data {
		fmt.Fprintf(&sb, "  %v\n", row)
	}
	sb.WriteString("\n")
	return sb.String()
}

// ReadFileDataInput 读取已上传文件数据（只读，用于数据分析，不会导入数据库）
type ReadFileDataInput struct {
	FileID string `json:"fileId" jsonschema:"required"`
	Limit  int    `json:"limit"`  // 返回行数，默认 100，最大 500
	Offset int    `json:"offset"` // 起始行偏移，默认 0
}
type ReadFileDataOutput struct {
	Type         string     `json:"type"` // "table"（excel/csv）| "text"（markdown）
	Columns      []string   `json:"columns,omitempty"`
	Rows         [][]string `json:"rows,omitempty"`
	TotalRows    int        `json:"totalRows,omitempty"`
	ReturnedRows int        `json:"returnedRows,omitempty"`
	Text         string     `json:"text,omitempty"`      // markdown 全文
	CharCount    int        `json:"charCount,omitempty"` // markdown 字符数
}

// NewReadFileDataFunc 返回读取已上传文件数据的工具函数（只读，供 LLM 分析文件内容）。
// 与 import_data 不同：它不会写入数据库，也不会删除暂存文件，可多次分页调用。
// 表格类（excel/csv）按 limit/offset 分页返回行；文本类（markdown）不做数据量限制，返回全文。
func NewReadFileDataFunc() func(ctx context.Context, input *ReadFileDataInput) (*ReadFileDataOutput, error) {
	return func(ctx context.Context, input *ReadFileDataInput) (*ReadFileDataOutput, error) {
		log.Printf("[Tool:read_file_data] fileId=%s, limit=%d, offset=%d\n", input.FileID, input.Limit, input.Offset)
		if input.FileID == "" {
			return nil, errors.New("fileId is required")
		}
		upload, err := GetUploadedFile(input.FileID)
		if err != nil {
			return nil, err
		}

		// 文本类（Markdown）：不做数据量限制，返回全文
		if upload.Type == "text" {
			return &ReadFileDataOutput{
				Type:      "text",
				Text:      upload.Text,
				CharCount: utf8.RuneCountInString(upload.Text),
			}, nil
		}

		// 表格类：分页读取
		limit := input.Limit
		if limit <= 0 {
			limit = 100
		}
		if limit > 500 {
			limit = 500
		}
		offset := input.Offset
		if offset < 0 {
			offset = 0
		}
		total := len(upload.Data)
		start := offset
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		src := upload.Data[start:end]
		rows := make([][]string, len(src))
		for i, r := range src {
			cp := make([]string, len(r))
			copy(cp, r)
			rows[i] = cp
		}
		return &ReadFileDataOutput{
			Type:         "table",
			Columns:      upload.Columns,
			Rows:         rows,
			TotalRows:    total,
			ReturnedRows: len(rows),
		}, nil
	}
}

type ImportDataInput struct {
	FileID    string            `json:"fileId" jsonschema:"required"`
	TableName string            `json:"tableName" jsonschema:"required"`
	Mapping   map[string]string `json:"mapping"`
	Mode      string            `json:"mode"`
}
type ImportDataOutput struct {
	Message      string `json:"message"`
	InsertedRows int    `json:"insertedRows"`
	UpdatedRows  int    `json:"updatedRows"`
}

func NewImportDataFunc(connID, dbType, dbSchema string, auditCtx *ExecAuditCtx, userId string) func(ctx context.Context, input *ImportDataInput) (*ImportDataOutput, error) {
	return func(ctx context.Context, input *ImportDataInput) (*ImportDataOutput, error) {
		log.Printf("[Tool:import_data] fileId=%s, table=%s, mode=%s\n", input.FileID, input.TableName, input.Mode)
		conn, _ := GetConn(connID, userId)
		if conn == nil {
			return nil, fmt.Errorf("db conn not found: %s", connID)
		}
		if input.FileID == "" {
			return nil, errors.New("fileId is required")
		}
		if !isValidTableName(input.TableName) {
			return nil, fmt.Errorf("invalid table name: %s", input.TableName)
		}
		mode := strings.ToLower(input.Mode)
		if mode == "" {
			mode = "insert"
		}
		upload, err := GetUploadedFile(input.FileID)
		if err != nil {
			return nil, err
		}
		// Markdown/文本文件非表格结构，不支持导入，仅支持分析
		if upload.Type == "text" {
			return nil, errors.New("Markdown/文本文件不支持导入数据库，仅支持内容分析")
		}
		tableColumns, err := getTableColumns(conn, dbType, dbSchema, input.TableName)
		if err != nil {
			return nil, fmt.Errorf("get columns failed for %s: %w", input.TableName, err)
		}
		if len(tableColumns) == 0 {
			return nil, fmt.Errorf("table %s not found or has no columns", input.TableName)
		}
		finalMapping, err := buildFinalMapping(upload.Columns, tableColumns, input.Mapping)
		if err != nil {
			return nil, err
		}
		if len(finalMapping) == 0 {
			return nil, fmt.Errorf("no columns matched for table %s", input.TableName)
		}
		var dbColumns []string
		var excelIndices []int
		for excelIdx, dbCol := range finalMapping {
			dbColumns = append(dbColumns, dbCol)
			excelIndices = append(excelIndices, excelIdx)
		}
		primaryKeys, _ := getPrimaryKeys(conn, dbType, dbSchema, input.TableName)
		tx, err := conn.Beginx()
		if err != nil {
			return nil, fmt.Errorf("begin tx failed: %w", err)
		}
		defer tx.Rollback()

		insertedRows, updatedRows := 0, 0

		if mode == "insert" {
			// 批量插入：每 200 行一批，使用 prepared statement
			batchSize := 200
			tableRef := quoteTableRef(dbType, dbSchema, input.TableName)
			quotedCols := make([]string, len(dbColumns))
			for i, col := range dbColumns {
				quotedCols[i] = quoteIdent(dbType, col)
			}
			colList := strings.Join(quotedCols, ", ")

			for batchStart := 0; batchStart < len(upload.Data); batchStart += batchSize {
				batchEnd := batchStart + batchSize
				if batchEnd > len(upload.Data) {
					batchEnd = len(upload.Data)
				}
				batch := upload.Data[batchStart:batchEnd]

				// 构建多行 VALUES
				var valueParts []string
				var allArgs []any
				for _, excelRow := range batch {
					row := make([]string, len(dbColumns))
					for i, idx := range excelIndices {
						if idx < len(excelRow) {
							row[i] = excelRow[idx]
						}
					}
					placeholders := make([]string, len(dbColumns))
					for i := range dbColumns {
						if dbType == "oracle" {
							placeholders[i] = fmt.Sprintf(":%d", len(allArgs)+i+1)
						} else {
							placeholders[i] = "?"
						}
						allArgs = append(allArgs, row[i])
					}
					valueParts = append(valueParts, "("+strings.Join(placeholders, ", ")+")")
				}

				if dbType == "oracle" {
					// Oracle 不支持多行 VALUES，逐行插入
					for _, excelRow := range batch {
						row := make([]string, len(dbColumns))
						for i, idx := range excelIndices {
							if idx < len(excelRow) {
								row[i] = excelRow[idx]
							}
						}
						if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
							return nil, fmt.Errorf("row %d insert failed: %w", batchStart+insertedRows+2, err)
						}
						insertedRows++
					}
				} else {
					query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", tableRef, colList, strings.Join(valueParts, ", "))
					if _, err := tx.Exec(query, allArgs...); err != nil {
						return nil, fmt.Errorf("batch insert failed at row %d: %w", batchStart+2, err)
					}
					insertedRows += len(batch)
				}
			}
		} else {
			// upsert 模式：逐行处理
			for rowNum, excelRow := range upload.Data {
				row := make([]string, len(dbColumns))
				for i, idx := range excelIndices {
					if idx < len(excelRow) {
						row[i] = excelRow[idx]
					}
				}
				if len(primaryKeys) > 0 {
					exists, err := checkRowExists(tx, dbType, dbSchema, input.TableName, dbColumns, row, primaryKeys)
					if err != nil {
						return nil, fmt.Errorf("row %d check failed: %w", rowNum+2, err)
					}
					if exists {
						if err := updateRow(tx, dbType, dbSchema, input.TableName, dbColumns, row, primaryKeys); err != nil {
							return nil, fmt.Errorf("row %d update failed: %w", rowNum+2, err)
						}
						updatedRows++
					} else {
						if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
							return nil, fmt.Errorf("row %d insert failed: %w", rowNum+2, err)
						}
						insertedRows++
					}
				} else {
					if err := insertRow(tx, dbType, dbSchema, input.TableName, dbColumns, row); err != nil {
						return nil, fmt.Errorf("row %d insert failed: %w", rowNum+2, err)
					}
					insertedRows++
				}
			}
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit failed: %w", err)
		}
		RemoveUploadedFile(input.FileID)

		if auditCtx != nil {
			auditSQL := fmt.Sprintf("IMPORT INTO %s (%d rows, mode=%s)", input.TableName, insertedRows+updatedRows, mode)
			audit.GetAuditService().Record(&audit.AuditEntry{
				Source:       "agent",
				ToolName:     "import_data",
				SQLText:      auditSQL,
				SQLType:      "IMPORT",
				RiskLevel:    "medium",
				Status:       "success",
				ConnID:       connID,
				SessionID:    auditCtx.SessionID,
				UserID:       auditCtx.UserID,
				UserName:     auditCtx.UserName,
				AffectedRows: insertedRows + updatedRows,
			})
		}

		var mappingDesc strings.Builder
		for i, idx := range excelIndices {
			if i > 0 {
				mappingDesc.WriteString(", ")
			}
			fmt.Fprintf(&mappingDesc, "%s->%s", upload.Columns[idx], dbColumns[i])
		}
		msg := fmt.Sprintf("imported %d rows", insertedRows)
		if updatedRows > 0 {
			msg += fmt.Sprintf(", updated %d rows", updatedRows)
		}
		msg += fmt.Sprintf(". mapping: %s", mappingDesc.String())
		log.Printf("[Tool:import_data] done - %s\n", msg)
		return &ImportDataOutput{Message: msg, InsertedRows: insertedRows, UpdatedRows: updatedRows}, nil
	}
}

func normalizeColName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "")
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}

func buildFinalMapping(excelColumns, tableColumns []string, aiMapping map[string]string) (map[int]string, error) {
	result := make(map[int]string)
	usedDBCols := make(map[string]bool)
	excelByExact := make(map[string]int)
	excelByNorm := make(map[string]int)
	for i, c := range excelColumns {
		excelByExact[c] = i
		excelByNorm[normalizeColName(c)] = i
	}
	dbByUpper := make(map[string]string)
	dbByNorm := make(map[string]string)
	for _, c := range tableColumns {
		dbByUpper[strings.ToUpper(c)] = c
		dbByNorm[normalizeColName(c)] = c
	}
	if len(aiMapping) > 0 {
		for aiExcelCol, aiDBCol := range aiMapping {
			excelIdx := -1
			if idx, ok := excelByExact[aiExcelCol]; ok {
				excelIdx = idx
			} else if idx, ok := excelByNorm[normalizeColName(aiExcelCol)]; ok {
				excelIdx = idx
			}
			if excelIdx == -1 {
				continue
			}
			dbCol := ""
			if orig, ok := dbByUpper[strings.ToUpper(aiDBCol)]; ok {
				dbCol = orig
			} else if orig, ok := dbByNorm[normalizeColName(aiDBCol)]; ok {
				dbCol = orig
			}
			if dbCol == "" || usedDBCols[strings.ToUpper(dbCol)] {
				continue
			}
			if _, exists := result[excelIdx]; exists {
				continue
			}
			result[excelIdx] = dbCol
			usedDBCols[strings.ToUpper(dbCol)] = true
		}
	}
	for i, excelCol := range excelColumns {
		if _, exists := result[i]; exists {
			continue
		}
		if orig, ok := dbByUpper[strings.ToUpper(excelCol)]; ok && !usedDBCols[strings.ToUpper(orig)] {
			result[i] = orig
			usedDBCols[strings.ToUpper(orig)] = true
			continue
		}
		norm := normalizeColName(excelCol)
		if orig, ok := dbByNorm[norm]; ok && !usedDBCols[strings.ToUpper(orig)] {
			result[i] = orig
			usedDBCols[strings.ToUpper(orig)] = true
		}
	}
	return result, nil
}

func getTableColumns(conn *sqlx.DB, dbType, dbSchema, tableName string) ([]string, error) {
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{tableName}
		}
	case "oracle":
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM ALL_TAB_COLUMNS WHERE OWNER = :1 AND TABLE_NAME = :2 ORDER BY COLUMN_ID"
			args = []any{strings.ToUpper(dbSchema), strings.ToUpper(tableName)}
		} else {
			query = "SELECT COLUMN_NAME FROM USER_TAB_COLUMNS WHERE TABLE_NAME = :1 ORDER BY COLUMN_ID"
			args = []any{strings.ToUpper(tableName)}
		}
	case "sqlite":
		quoted := safeQuoteTableName(tableName)
		if quoted == "" {
			return nil, fmt.Errorf("invalid table name: %s", tableName)
		}
		query = "PRAGMA table_info(" + quoted + ")"
	default:
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			query = "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE table_schema = DATABASE() AND table_name = ? ORDER BY ORDINAL_POSITION"
			args = []any{tableName}
		}
	}
	if dbType == "sqlite" {
		rows, err := conn.Queryx(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var cols []string
		for rows.Next() {
			var cid int
			var name, colType string
			var notNull int
			var dfltValue *string
			var pk int
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
				continue
			}
			cols = append(cols, name)
		}
		return cols, nil
	}
	rows, err := conn.Queryx(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			continue
		}
		cols = append(cols, colName)
	}
	return cols, nil
}

func getPrimaryKeys(conn *sqlx.DB, dbType, dbSchema, tableName string) ([]string, error) {
	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = ? AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = DATABASE() AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
			args = []any{tableName}
		}
	case "oracle":
		if dbSchema != "" {
			query = `SELECT cols.COLUMN_NAME FROM ALL_CONSTRAINTS cons JOIN ALL_CONS_COLUMNS cols ON cons.CONSTRAINT_NAME = cols.CONSTRAINT_NAME AND cons.OWNER = cols.OWNER WHERE cons.CONSTRAINT_TYPE = 'P' AND cons.OWNER = :1 AND cons.TABLE_NAME = :2 ORDER BY cols.POSITION`
			args = []any{strings.ToUpper(dbSchema), strings.ToUpper(tableName)}
		} else {
			query = `SELECT cols.COLUMN_NAME FROM USER_CONSTRAINTS cons JOIN USER_CONS_COLUMNS cols ON cons.CONSTRAINT_NAME = cols.CONSTRAINT_NAME WHERE cons.CONSTRAINT_TYPE = 'P' AND cons.TABLE_NAME = :1 ORDER BY cols.POSITION`
			args = []any{strings.ToUpper(tableName)}
		}
	case "sqlite":
		quoted := safeQuoteTableName(tableName)
		if quoted == "" {
			return nil, fmt.Errorf("invalid table name: %s", tableName)
		}
		query = "PRAGMA table_info(" + quoted + ")"
		rows, err := conn.Queryx(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var keys []string
		for rows.Next() {
			var cid int
			var name, colType string
			var notNull int
			var dfltValue *string
			var pk int
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
				continue
			}
			if pk > 0 {
				keys = append(keys, name)
			}
		}
		return keys, nil
	default:
		if dbSchema != "" {
			query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = ? AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
			args = []any{dbSchema, tableName}
		} else {
			query = "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE table_schema = DATABASE() AND table_name = ? AND CONSTRAINT_NAME = 'PRIMARY' ORDER BY ORDINAL_POSITION"
			args = []any{tableName}
		}
	}
	rows, err := conn.Queryx(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			continue
		}
		keys = append(keys, colName)
	}
	return keys, nil
}

func checkRowExists(tx *sqlx.Tx, dbType, dbSchema, tableName string, columns []string, row []string, primaryKeys []string) (bool, error) {
	var whereParts []string
	var args []any
	argIdx := 0
	for _, pk := range primaryKeys {
		for i, col := range columns {
			if strings.EqualFold(col, pk) {
				if dbType == "oracle" {
					argIdx++
					whereParts = append(whereParts, fmt.Sprintf("%s = :%d", quoteIdent(dbType, pk), argIdx))
				} else {
					whereParts = append(whereParts, fmt.Sprintf("%s = ?", quoteIdent(dbType, pk)))
				}
				args = append(args, row[i])
				break
			}
		}
	}
	if len(whereParts) == 0 {
		return false, nil
	}
	tableRef := quoteTableRef(dbType, dbSchema, tableName)
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", tableRef, strings.Join(whereParts, " AND "))
	var count int
	err := tx.Get(&count, query, args...)
	return count > 0, err
}

func insertRow(tx *sqlx.Tx, dbType, dbSchema, tableName string, columns []string, row []string) error {
	placeholders := make([]string, len(columns))
	quotedCols := make([]string, len(columns))
	args := make([]any, len(columns))
	for i := range columns {
		quotedCols[i] = quoteIdent(dbType, columns[i])
		if dbType == "oracle" {
			placeholders[i] = fmt.Sprintf(":%d", i+1)
		} else {
			placeholders[i] = "?"
		}
		args[i] = row[i]
	}
	tableRef := quoteTableRef(dbType, dbSchema, tableName)
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableRef, strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))
	_, err := tx.Exec(query, args...)
	return err
}

func updateRow(tx *sqlx.Tx, dbType, dbSchema, tableName string, columns []string, row []string, primaryKeys []string) error {
	pkSet := make(map[string]bool)
	for _, pk := range primaryKeys {
		pkSet[strings.ToUpper(pk)] = true
	}
	var setParts, whereParts []string
	var setArgs, whereArgs []any
	for i, col := range columns {
		qCol := quoteIdent(dbType, col)
		if pkSet[strings.ToUpper(col)] {
			// PK 列：进 WHERE 子句
			if dbType == "oracle" {
				// Oracle 的 :N 占位符编号是相对于最终合并 args 切片的位置
				// 最终 args = setArgs + whereArgs，所以 PK 位置 = len(setArgs) + 1（1-based）
				whereParts = append(whereParts, fmt.Sprintf("%s = :%d", qCol, len(setArgs)+len(whereArgs)+1))
			} else {
				whereParts = append(whereParts, fmt.Sprintf("%s = ?", qCol))
			}
			whereArgs = append(whereArgs, row[i])
		} else {
			// 普通列：进 SET 子句
			if dbType == "oracle" {
				setParts = append(setParts, fmt.Sprintf("%s = :%d", qCol, len(setArgs)+1))
			} else {
				setParts = append(setParts, fmt.Sprintf("%s = ?", qCol))
			}
			setArgs = append(setArgs, row[i])
		}
	}
	if len(setParts) == 0 {
		return errors.New("cannot build update: no SET columns (all columns are PKs?)")
	}
	if len(whereParts) == 0 {
		return errors.New("cannot build update: missing WHERE (PK column missing in data row?)")
	}
	args := append(setArgs, whereArgs...)
	tableRef := quoteTableRef(dbType, dbSchema, tableName)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s", tableRef, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))
	_, err := tx.Exec(query, args...)
	return err
}

// quoteIdent 根据数据库类型对标识符加引号，防止保留字冲突
func quoteIdent(dbType, name string) string {
	switch dbType {
	case "oracle":
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	default: // mysql, mariadb, sqlite
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
}

// quoteTableRef 构建带 schema 的表引用
func splitSchemaTable(table, dbSchema string) (schema, tableName string) {
	schema = dbSchema
	tableName = table
	dotIdx := strings.IndexByte(table, '.')
	if dotIdx > 0 && dotIdx < len(table)-1 {
		schema = table[:dotIdx]
		tableName = table[dotIdx+1:]
	}
	return
}

func quoteTableRef(dbType, dbSchema, tableName string) string {
	if dbSchema != "" {
		return quoteIdent(dbType, dbSchema) + "." + quoteIdent(dbType, tableName)
	}
	return quoteIdent(dbType, tableName)
}

// applyRowLimit 给 SELECT 加 LIMIT，未带 LIMIT 时附加。
//
// 支持检测：
//   - LIMIT n
//   - LIMIT n, m  (MySQL offset, count)
//   - LIMIT n OFFSET m
//   - FETCH NEXT/FIRST n ROWS ONLY (Oracle 12c+)
//   - ROWNUM 谓词 (Oracle 老版本)
//
// Oracle 12c+ 优先使用 OFFSET 0 ROWS FETCH NEXT N ROWS ONLY（保留 ORDER BY 语义）；
// 老版本回退到 SELECT * FROM (...) WHERE ROWNUM <= N（注意：此方法会破坏 ORDER BY）。
func applyRowLimit(sql, driverName string, maxRows int) string {
	// **必须**先剥注释——否则 SELECT * FROM users -- LIMIT 999 中的 LIMIT 999
	// 会被正则误识别为"已有 LIMIT"，导致永远不追加真实 LIMIT。
	// （EINO_DEEP_ANALYSIS §5.3 双重 LIMIT 反向 case）
	stripped := sqlutil.StripSQLComments(sql)
	trimmed := strings.TrimRight(stripped, "; \t\r\n")
	upper := strings.ToUpper(trimmed)
	switch driverName {
	case "oracle":
		if reLimitOracle.MatchString(upper) {
			return sql
		}
		// Oracle 12c+ 使用标准 OFFSET/FETCH 语法，避免 ROWNUM 破坏 ORDER BY
		return fmt.Sprintf("%s OFFSET 0 ROWS FETCH NEXT %d ROWS ONLY", trimmed, maxRows)
	default:
		if reLimit.MatchString(upper) {
			return sql
		}
		return fmt.Sprintf("%s LIMIT %d", trimmed, maxRows)
	}
}

// reLimit 匹配 MySQL/PostgreSQL/SQLite 的 LIMIT 形式
var reLimit = regexp.MustCompile(`(?i)\bLIMIT\s+(?:\d+\s+OFFSET\s+\d+|\d+(?:\s*,\s*\d+)?)\b`)

// reLimitOracle 匹配 Oracle 的 LIMIT 等价形式
var reLimitOracle = regexp.MustCompile(`(?i)\b(?:ROWNUM\b|FETCH\s+(?:NEXT|FIRST)\s+\d+\s+ROWS?\s+ONLY)`)
