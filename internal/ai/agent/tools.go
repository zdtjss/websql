package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"websql/internal/ai/agent/dataset"
	"websql/internal/ai/agent/fileparse"
	"websql/internal/ai/agent/sqlutil"
	admin "websql/internal/app/admin"
	connpkg "websql/internal/app/conn"
	appperm "websql/internal/app/permission"
	"websql/internal/audit"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/sqlguard"

	"github.com/jmoiron/sqlx"
)

var validTableNameRegex = regexp.MustCompile("^[a-zA-Z0-9_`\".]+$")

// 工具返回量的服务端上限（此前完全依赖 reduction/summarization 事后兜底）
const (
	// maxListTables 单次 list_tables 默认返回的表数量上限
	maxListTables = 500
	// maxListTablesHard list_tables 单次可请求的硬上限（服务端安全边界）
	maxListTablesHard = 20_000
	// maxSchemaOutputChars 单次 get_table_schema 返回的 DDL 字符上限
	maxSchemaOutputChars = 60_000
	// maxMarkdownFileChars 上传文档（md/txt/docx/pdf）单次读取的默认字符窗口
	maxMarkdownFileChars = 30_000
	// maxTextWindowChars 文本/文档类单次读取的字符窗口硬上限
	maxTextWindowChars = 60_000
	// querySafetyRowLimit query_data 默认读取上限（防止把超大结果集读进内存）
	querySafetyRowLimit = 500
	// querySafetyRowLimitHard query_data 可通过 maxRows 请求的硬上限（服务端安全边界）。
	// 10k 行是在“可分析”与“内存/序列化成本”之间的折中；更大分析应先聚合或物化数据集。
	querySafetyRowLimitHard = 10_000
	// fileDatasetMinRows 上传表格物化为数据集的最小行数（代码层成本阈值）
	fileDatasetMinRows = 1_000
)

// dangerousTableTokens 表名中禁止出现的 SQL 关键字。
// 按「标识符段」精确匹配（而非子串匹配），避免把正常表名误判为非法，
// 例如 user_selection（含 select 子串）、product_updates（含 update 子串）都应合法。
var dangerousTableTokens = map[string]bool{
	"union": true, "select": true, "insert": true, "update": true,
	"delete": true, "drop": true, "alter": true, "create": true,
	"truncate": true, "exec": true, "merge": true, "grant": true,
}

// dangerousTableSubstrings 与标识符段无关的危险片段（十六进制字面量等）
var dangerousTableSubstrings = []string{"0x"}

func isValidTableName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || !validTableNameRegex.MatchString(name) {
		return false
	}
	lower := strings.ToLower(name)
	for _, pattern := range dangerousTableSubstrings {
		if strings.Contains(lower, pattern) {
			return false
		}
	}
	// 系统过程前缀（SQL Server 遗留约定），仅前缀匹配，避免误伤 tsp_log 这类正常表名
	if strings.HasPrefix(lower, "xp_") || strings.HasPrefix(lower, "sp_") {
		return false
	}
	// 逐段比对（支持 schema.table / `schema`.`table`），段内精确匹配关键字
	normalized := strings.NewReplacer("`", " ", `"`, " ", ".", " ").Replace(lower)
	for _, seg := range strings.Fields(normalized) {
		if dangerousTableTokens[seg] {
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
	// MaxRows 模型根据分析场景主动请求的读取行数；服务端仍有硬上限。
	MaxRows int `json:"maxRows,omitempty"`
}
type QueryOutput struct {
	Columns []string         `json:"columns"`
	Data    []map[string]any `json:"data,omitempty"`
	Count   int              `json:"count"`
	// ReturnedRows 本次返回给模型的行数
	ReturnedRows int `json:"returnedRows,omitempty"`
	// RowCount 本次结果行数；truncated=true 时为 0（总数未知，需另行 COUNT）；
	// 物化为数据集后为数据集总行数。
	RowCount int `json:"rowCount,omitempty"`
	// TotalKnown 为 true 时 RowCount/TotalRows 才是可用的总行数。
	TotalKnown bool `json:"totalKnown,omitempty"`
	// TotalRows 仅数据集物化时填充，表示数据集总行数。
	TotalRows int `json:"totalRows,omitempty"`
	// EstimatedBytes 结果集序列化后的估算字节数
	EstimatedBytes int `json:"estimatedBytes,omitempty"`
	// Truncated 是否因服务端安全上限而截断
	Truncated bool `json:"truncated,omitempty"`
	// HasMore 是否还有未返回的数据
	HasMore bool `json:"hasMore,omitempty"`
	// DatasetID 大结果物化后的数据集句柄（P2）
	DatasetID string `json:"datasetId,omitempty"`
	// Sample 数据集物化时返回的少量样本
	Sample []map[string]any `json:"sample,omitempty"`
	// Notice 截断/降级/继续取数提示
	Notice string `json:"notice,omitempty"`
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
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
type ListTablesOutput struct {
	Tables     []TableInfo `json:"tables"`
	Count      int         `json:"count"`
	TotalCount int         `json:"totalCount,omitempty"`
	Offset     int         `json:"offset,omitempty"`
	Limit      int         `json:"limit,omitempty"`
	NextOffset int         `json:"nextOffset,omitempty"`
	HasMore    bool        `json:"hasMore,omitempty"`
	Truncated  bool        `json:"truncated,omitempty"`
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
	cfg, err := loadConnCfgCached(connId)
	if err != nil || cfg == nil {
		return nil, ""
	}
	deref := func(p *string) string {
		if p != nil {
			return *p
		}
		return ""
	}
	pwd := ""
	if cfg.Pwd != nil && cfg.DbType != "sqlite" {
		decoded, decErr := crypto.AESDecode(*cfg.Pwd)
		if decErr != nil {
			logger.PrintErrf("连接密码解密失败: %s", decErr, cfg.Id)
		} else {
			pwd = decoded
		}
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

// connCfgCacheTTL 连接配置短缓存：工具调用频繁，避免每次查询 t_conn + AES 解密。
// 30 秒的窗口足以覆盖单轮工具调用，又不会让后台修改长时间不生效。
const connCfgCacheTTL = 30 * time.Second

type connCfgCacheEntry struct {
	cfg      connpkg.ConnCfg
	loadedAt time.Time
}

var connCfgCache sync.Map // connID -> *connCfgCacheEntry

func loadConnCfgCached(connID string) (*connpkg.ConnCfg, error) {
	if v, ok := connCfgCache.Load(connID); ok {
		if e, ok := v.(*connCfgCacheEntry); ok && time.Since(e.loadedAt) < connCfgCacheTTL {
			cp := e.cfg
			return &cp, nil
		}
	}
	var cfg connpkg.ConnCfg
	if err := getDB().Get(&cfg, "select * from t_conn where id = ?", connID); err != nil {
		return nil, err
	}
	connCfgCache.Store(connID, &connCfgCacheEntry{cfg: cfg, loadedAt: time.Now()})
	cp := cfg
	return &cp, nil
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
		auditCtx := auditCtx.ForRun(ctx)
		targetConnID := resolveConnID(connId, input.ConnID, connLookup)
		log.Printf("[Tool:query_data] connId=%s sql{%s}\n", targetConnID, safeSQLSummary(input.SQL))
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
		// 语句类型白名单：用词级首关键字判断（而非前缀匹配），
		// 避免 "SELECTX ..." 之类被误判为合法 SELECT
		switch sqlutil.FirstKeyword(sql) {
		case "SELECT", "SHOW", "DESCRIBE", "EXPLAIN", "WITH":
		default:
			err := errors.New("query_data only supports SELECT/SHOW/DESCRIBE/EXPLAIN/WITH")
			recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), audit.FormatErrorWithStack(err))
			return nil, err
		}
		// 词法级只读校验：拦截可执行注释、SELECT ... INTO OUTFILE、LOAD_FILE()、
		// FOR UPDATE 行锁、EXPLAIN ANALYZE（会真实执行）以及 CTE 包裹的写操作等
		// 首关键字白名单无法识别的副作用写法
		if err := sqlguard.ValidateReadOnlyQuery(sql); err != nil {
			err = fmt.Errorf("query_data 仅允许无副作用的只读查询：%w", err)
			recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), err.Error())
			return nil, err
		}
		// 多语句注入防护：检测 SQL 中是否包含分号分隔的多条语句。
		// 防止 "SELECT 1; DROP TABLE x" 绕过语句类型白名单。
		// 必须在类型白名单之后、执行之前检查。
		if sqlutil.ContainsMultipleStatements(sql) {
			err := errors.New("query_data 不支持多语句执行（检测到分号分隔的多条SQL），请拆分为单条语句分别执行")
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
		if sqlutil.FirstKeyword(sql) == "WITH" {
			// 词级扫描写关键字：可识别 CTE 包裹的写操作，且不受空白字符变体（制表符/换行）影响
			if sqlutil.HasWriteKeyword(stripped) {
				err := errors.New("query_data does not allow write operations in WITH, use exec_sql")
				recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), audit.FormatErrorWithStack(err))
				return nil, err
			}
		}
		readLimit := input.MaxRows
		if readLimit <= 0 {
			readLimit = querySafetyRowLimit
		}
		if readLimit > querySafetyRowLimitHard {
			readLimit = querySafetyRowLimitHard
		}
		if sqlutil.FirstKeyword(sql) == "SELECT" || sqlutil.FirstKeyword(sql) == "WITH" {
			sql = applyRowLimit(sql, conn.DriverName(), readLimit)
		}
		queryCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		rows, err := conn.QueryxContext(queryCtx, sql)
		if err != nil {
			if input.ConnID == "" && targetConnID == connId {
				if altConnID, altConn, altSchema := tryAlternativeConn(input.SQL, connLookup, connId, userId, mustRunSecurity(ctx)); altConn != nil {
					log.Printf("[Tool:query_data] 默认连接查询失败，自动路由到连接 %s（schema=%s）\n", altConnID, altSchema)
					// 安全审计：自动路由事件必须可追溯（EINO_DEEP_ANALYSIS §13）
					if auditCtx != nil {
						altConnName, _ := connpkg.GetConnInfo(altConnID)
						audit.GetAuditService().Record(&audit.AuditEntry{
							Source:     "agent",
							ToolName:   "query_data.auto_route",
							SQLText:    input.SQL,
							SQLType:    "SELECT",
							RiskLevel:  "low",
							Status:     "auto_routed",
							ConnID:     altConnID,
							ConnName:   altConnName,
							SchemaName: altSchema,
							SessionID:  auditCtx.SessionID,
							UserID:     auditCtx.UserID,
							UserName:   auditCtx.UserName,
							ErrorMsg:   fmt.Sprintf("from=%s to=%s schema=%s reason=%v", connId, altConnID, altSchema, err),
						})
					}
					altRawSQL := strings.TrimSpace(input.SQL)
					altSQL := qualifyBareTableNames(altRawSQL, altConn.DriverName(), altSchema)
					altQueryCtx, altCancel := context.WithTimeout(ctx, 60*time.Second)
					defer altCancel()
					if kw := sqlutil.FirstKeyword(sql); kw == "SELECT" || kw == "WITH" {
						altSQL = applyRowLimit(altSQL, altConn.DriverName(), readLimit)
					}
					altRows, altErr := altConn.QueryxContext(altQueryCtx, altSQL)
					if altErr == nil {
						defer altRows.Close()
						cols, _ := altRows.Columns()
						data, truncated, err := database.GetResultRowsLimited(altConn.DriverName(), altRows, readLimit)
						if err != nil {
							logger.PrintErrf("查询数据失败", err)
							return nil, fmt.Errorf("query failed: %w", err)
						}
						recordQueryAudit(auditCtx, input.SQL, altConnID, "success", len(data), int(time.Since(startTime).Milliseconds()), "")
						out := buildQueryOutput(cols, data, truncated)
						// 自动路由后必须用目标连接的 Scope 做列过滤/脱敏，
						// 不能沿用默认连接的 Scope（否则 A 全权 + B 受限时越权）。
						if sec := mustRunSecurity(ctx); sec != nil {
							if altScope, scopeErr := sec.ScopeFor(altConnID); scopeErr == nil {
								if encoded, marshalErr := json.Marshal(out); marshalErr == nil {
									mw := &PermissionMiddleware{Scope: altScope}
									if filtered, filterErr := mw.applyQueryResultFilterMasked(ctx, input.SQL, string(encoded), appperm.ExtractTablesFromSQL(input.SQL)); filterErr == nil {
										var filteredOut QueryOutput
										if json.Unmarshal([]byte(filtered), &filteredOut) == nil {
											out = &filteredOut
										}
									} else {
										return nil, filterErr
									}
								}
							}
						}
						return out, nil
					}
					log.Printf("[Tool:query_data] 自动路由查询也失败: %v，返回原始错误\n", altErr)
				}
			}
			recordQueryAudit(auditCtx, input.SQL, targetConnID, "failed", 0, int(time.Since(startTime).Milliseconds()), audit.FormatErrorWithStack(err))
			return nil, fmt.Errorf("query failed: %w", err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		data, truncated, err := database.GetResultRowsLimited(conn.DriverName(), rows, readLimit)
		if err != nil {
			logger.PrintErrf("查询数据失败", err)
			return nil, fmt.Errorf("query failed: %w", err)
		}
		recordQueryAudit(auditCtx, input.SQL, targetConnID, "success", len(data), int(time.Since(startTime).Milliseconds()), "")
		return buildQueryOutput(cols, data, truncated), nil
	}
}

// recordToolAudit 通用 Agent 工具审计出口：ToolName / SQLType 由调用方指定，
// 风险等级统一为 low（只读类工具）。
func recordToolAudit(auditCtx *ExecAuditCtx, toolName, sqlType, sql, connID, status string, affectedRows, execTimeMs int, errorMsg string) {
	if auditCtx == nil {
		return
	}
	connName, schemaName := connpkg.GetConnInfo(connID)
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:       "agent",
		ToolName:     toolName,
		SQLText:      audit.MaskSQL(sql),
		SQLType:      sqlType,
		RiskLevel:    "low",
		Status:       status,
		ConnID:       connID,
		ConnName:     connName,
		SchemaName:   schemaName,
		SessionID:    auditCtx.SessionID,
		UserID:       auditCtx.UserID,
		UserName:     auditCtx.UserName,
		AffectedRows: affectedRows,
		ExecTimeMs:   execTimeMs,
		ErrorMsg:     errorMsg,
	})
}

// recordQueryAudit 保持 query_data 既有审计行为不变（ToolName=query_data，SQLType=SELECT）。
func recordQueryAudit(auditCtx *ExecAuditCtx, sql, connID, status string, affectedRows, execTimeMs int, errorMsg string) {
	recordToolAudit(auditCtx, "query_data", "SELECT", sql, connID, status, affectedRows, execTimeMs, errorMsg)
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
func tryAlternativeConn(sql string, connLookup map[string]string, defaultConnID string, userId string, sec *RunSecurityContext) (string, *sqlx.DB, string) {
	// 安全：只承认"显式 schema.表名"形式。模糊 FROM users 不会触发跨连接路由。
	schema := extractExplicitSchemaFromSQL(sql)
	if schema == "" {
		log.Printf("[Tool:query_data] tryAlternativeConn 拒绝：用户 SQL 未指定显式 schema，按 least-privilege 不跨连接路由 - connId=%s\n", defaultConnID)
		return "", nil, ""
	}

	// 优化：检查 SQL 中是否引用了多个不同 schema。
	// 如果只有一个 schema，且该 schema 属于默认连接，不触发跨连接路由。
	// 这避免了"用户写 SELECT * FROM current_schema.table" 被误判为跨库操作的情况。
	allSchemas := extractAllExplicitSchemasFromSQL(sql)
	if len(allSchemas) == 1 {
		singleSchema := allSchemas[0]
		if singleConnID, ok := connLookup[singleSchema]; ok && singleConnID == defaultConnID {
			log.Printf("[Tool:query_data] tryAlternativeConn 跳过：SQL 仅引用当前连接的 schema=%s，无需路由\n", singleSchema)
			return "", nil, ""
		}
	}

	altConnID, ok := connLookup[strings.ToUpper(schema)]
	if !ok || altConnID == defaultConnID {
		return "", nil, ""
	}
	altConn, _ := GetConn(altConnID, userId)
	if altConn != nil {
		// 自动路由到另一连接前，必须用目标连接的 Scope 重新校验表/列权限。
		// 不能沿用默认连接的 Scope（默认连接全权 + 目标连接受限时会越权）。
		if err := authorizeAltConnQuery(sec, altConnID, sql); err != nil {
			log.Printf("[Tool:query_data] 拒绝自动路由：目标连接权限校验失败 - conn=%s, err=%v\n", altConnID, err)
			return "", nil, ""
		}
		log.Printf("[Tool:query_data] 命中显式 schema=%s 跨连接路由 - from=%s to=%s\n", schema, defaultConnID, altConnID)
		return altConnID, altConn, schema
	}
	return "", nil, ""
}

// mustRunSecurity 返回运行安全上下文；缺失时返回 nil（调用方按无上下文处理）。
func mustRunSecurity(ctx context.Context) *RunSecurityContext {
	sec, err := RunSecurityFrom(ctx)
	if err != nil {
		return nil
	}
	return sec
}

// authorizeAltConnQuery 用目标连接的 Scope 校验自动路由 SQL 的表/列权限。
func authorizeAltConnQuery(sec *RunSecurityContext, connID, sql string) error {
	if sec == nil {
		return &PermissionError{Message: "缺少运行安全上下文，已拒绝跨连接自动路由"}
	}
	scope, err := sec.ScopeFor(connID)
	if err != nil {
		return err
	}
	tables := appperm.ExtractTablesFromSQL(sql)
	for _, table := range tables {
		if !scope.IsTableRefAllowed(table) {
			return &PermissionError{Message: "自动路由目标连接无表权限", Objects: []string{table}}
		}
	}
	if len(tables) == 0 && !scope.SkipChecks() && !appperm.IsVerifiedNoResourceQuery(sql) {
		return &PermissionError{Message: "无法确认 SQL 的数据访问范围，已拒绝自动路由", Objects: []string{connID}}
	}
	if err := authorizeColumnsInSQL(scope, sql, tables, columnCheckOptions{RequireKnownOrigin: true}); err != nil {
		return err
	}
	return nil
}

// extractExplicitSchemaFromSQL 只识别"schema 显式出现"的情况：
//   - SELECT * FROM mySchema.myTable
//   - SELECT * FROM mySchema.myTable t1 JOIN mySchema.myTable t2 ON ...
//
// **不**识别：
//   - SELECT * FROM myTable（无 schema 前缀）
//   - 别名引用 t1.column（不是 FROM/JOIN 后面紧跟的）
//   - CTE 内部的引用（只路由最外层 schema）
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
				candidate := strings.ToUpper(m[i])
				// 排除 SQL 内置函数和常见关键字被误识别为 schema 名
				if isSQLFunctionOrKeyword(candidate) {
					break // 跳过此 match，继续下一个
				}
				return candidate
			}
		}
	}
	return ""
}

// extractAllExplicitSchemasFromSQL 提取 SQL 中所有显式出现的 schema 前缀。
// 与 extractExplicitSchemaFromSQL（只返回第一个）不同，本函数返回所有唯一的 schema 名。
// 用于判断一条 SQL 是否引用了多个不同 schema，从而精确判断是否为跨库操作。
func extractAllExplicitSchemasFromSQL(sql string) []string {
	cleaned := sqlutil.StripSQLComments(sql)
	re := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|UPDATE|INSERT\s+INTO|DELETE\s+FROM|MERGE\s+INTO)\s+(?:` +
		"`" + `([A-Za-z_][A-Za-z0-9_]*)` + "`" + `\s*\.\s*` + "`" + `[A-Za-z_][A-Za-z0-9_]*` + "`" + `|` +
		`"([A-Za-z_][A-Za-z0-9_]*)"\s*\.\s*"([A-Za-z_][A-Za-z0-9_]*)"` + `|` +
		`([A-Za-z_][A-Za-z0-9_]*)\s*\.\s*([A-Za-z_][A-Za-z0-9_]*))`)
	matches := re.FindAllStringSubmatch(cleaned, -1)
	seen := make(map[string]bool)
	var schemas []string
	for _, m := range matches {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				candidate := strings.ToUpper(m[i])
				if isSQLFunctionOrKeyword(candidate) {
					break
				}
				if !seen[candidate] {
					seen[candidate] = true
					schemas = append(schemas, candidate)
				}
				break
			}
		}
	}
	return schemas
}

// isSQLFunctionOrKeyword 检查一个标识符是否是 SQL 内置函数名或关键字。
// 这些如果出现在 "FROM func_name.something" 的位置，不应被当做 schema 前缀。
// 典型误判场景：复杂 SQL 中的 CTE 名、系统函数等。
func isSQLFunctionOrKeyword(name string) bool {
	sqlFunctionsAndKeywords := map[string]bool{
		// 聚合函数
		"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true,
		"GROUP_CONCAT": true, "LISTAGG": true, "STRING_AGG": true,
		// 窗口函数
		"ROW_NUMBER": true, "RANK": true, "DENSE_RANK": true, "NTILE": true,
		"LAG": true, "LEAD": true, "FIRST_VALUE": true, "LAST_VALUE": true,
		"PERCENTILE_CONT": true, "PERCENTILE_DISC": true, "CUME_DIST": true,
		"PERCENT_RANK": true,
		// 字符串函数
		"CONCAT": true, "SUBSTR": true, "SUBSTRING": true, "TRIM": true,
		"UPPER": true, "LOWER": true, "REPLACE": true, "LENGTH": true,
		"CHAR_LENGTH": true, "INSTR": true, "LOCATE": true, "LPAD": true, "RPAD": true,
		// 日期函数
		"DATE": true, "DATETIME": true, "TIMESTAMP": true, "NOW": true,
		"SYSDATE": true, "SYSTIMESTAMP": true, "DATE_FORMAT": true,
		"DATE_ADD": true, "DATE_SUB": true, "DATEDIFF": true, "DATEADD": true,
		"TO_DATE": true, "TO_CHAR": true, "TO_TIMESTAMP": true,
		"YEAR": true, "MONTH": true, "DAY": true, "HOUR": true, "MINUTE": true, "SECOND": true,
		// 数值函数
		"ABS": true, "CEIL": true, "CEILING": true, "FLOOR": true, "ROUND": true,
		"MOD": true, "POWER": true, "SQRT": true, "SIGN": true, "TRUNC": true,
		// 转换函数
		"CAST": true, "CONVERT": true, "COALESCE": true, "NVL": true, "NVL2": true,
		"IFNULL": true, "ISNULL": true, "NULLIF": true, "DECODE": true,
		// 条件函数
		"IF": true, "IIF": true, "CASE": true, "WHEN": true,
		// 系统内置 schema（不应被当做用户 schema）
		"DUAL": true, "INFORMATION_SCHEMA": true, "SYS": true, "SYSTEM": true,
		"MYSQL": true, "PERFORMANCE_SCHEMA": true, "PG_CATALOG": true,
		// SQL 关键字（可能被解析器误匹配）
		"SELECT": true, "FROM": true, "WHERE": true, "GROUP": true, "ORDER": true,
		"HAVING": true, "LIMIT": true, "OFFSET": true, "UNION": true, "ALL": true,
		"EXISTS": true, "NOT": true, "AND": true, "OR": true, "AS": true,
		"SET": true, "VALUES": true, "INTO": true, "TABLE": true, "INDEX": true,
		"LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true, "CROSS": true,
		"NATURAL": true, "ON": true, "USING": true, "LATERAL": true,
	}
	return sqlFunctionsAndKeywords[name]
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
		auditCtx := auditCtx.ForRun(ctx)
		targetConnID := resolveConnID(connId, input.ConnID, connLookup)
		log.Printf("[Tool:exec_sql] sql{%s} connId=%s\n", safeSQLSummary(input.SQL), targetConnID)
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

		// 可执行注释防护：MySQL 会执行 /*! ... */ 中的内容，而审批弹窗与权限解析
		// 都会把它当作普通注释，用户无法从确认框看出真正执行的语句，直接拒绝
		if sqlguard.HasExecutableComment(sql) {
			err := errors.New("exec_sql 不允许包含 MySQL 可执行注释（/*! ... */），请去掉注释后以明文 SQL 提交")
			recordExecAudit(auditCtx, sql, targetConnID, sqlType, riskLevel, "failed", 0, int(time.Since(startTime).Milliseconds()), err.Error())
			return nil, err
		}

		// 多语句防护：写操作一次只允许一条语句，
		// 防止模型在一次调用里夹带未经过审批确认的额外写操作
		if sqlutil.ContainsMultipleStatements(sql) {
			err := errors.New("exec_sql 不支持多语句执行（检测到分号分隔的多条SQL），请拆分为单条语句分别执行")
			recordExecAudit(auditCtx, sql, targetConnID, sqlType, riskLevel, "failed", 0, int(time.Since(startTime).Milliseconds()), err.Error())
			return nil, err
		}

		// 统一接入项目级 SQL 安全校验（与结构同步/备份恢复/建模同一份规则），
		// 避免 AI 路径自成一套校验栈导致规则漂移
		if err := sqlguard.ValidateNoFileSideEffects(sql); err != nil {
			recordExecAudit(auditCtx, sql, targetConnID, sqlType, riskLevel, "failed", 0, int(time.Since(startTime).Milliseconds()), err.Error())
			return nil, err
		}
		switch sqlutil.DetectSQLType(sql) {
		case sqlutil.SQLTypeCreate, sqlutil.SQLTypeAlter, sqlutil.SQLTypeDrop:
			if err := sqlguard.ValidateDDL(sql); err != nil {
				recordExecAudit(auditCtx, sql, targetConnID, sqlType, riskLevel, "failed", 0, int(time.Since(startTime).Milliseconds()), err.Error())
				return nil, fmt.Errorf("DDL 未通过安全校验：%w", err)
			}
		case sqlutil.SQLTypeInsert, sqlutil.SQLTypeUpdate, sqlutil.SQLTypeDelete:
			if err := sqlguard.ValidateDML(sql); err != nil {
				recordExecAudit(auditCtx, sql, targetConnID, sqlType, riskLevel, "failed", 0, int(time.Since(startTime).Milliseconds()), err.Error())
				return nil, fmt.Errorf("DML 未通过安全校验：%w", err)
			}
		}

		// 执行超时：与 query_data 对齐（60s），避免长事务/锁等待把 SSE 连接拖满 30 分钟
		execCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		result, err := conn.ExecContext(execCtx, sql)
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
	connName, schemaName := connpkg.GetConnInfo(connID)
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:       "agent",
		ToolName:     "exec_sql",
		SQLText:      audit.MaskSQL(sql),
		SQLType:      sqlType,
		RiskLevel:    riskLevel,
		Status:       status,
		ConnID:       connID,
		ConnName:     connName,
		SchemaName:   schemaName,
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
				// 显式反馈给模型，避免「无输出」导致模型误判为表不存在而反复尝试
				fmt.Fprintf(&sb, "-- 表名 %q 非法，已跳过（仅允许字母/数字/下划线/点，且不得为 SQL 关键字，需用 `` 或 \"\" 包裹特殊名称）。\n\n", table)
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
				fmt.Fprintf(&sb, "-- 表 %q 所属连接不可用，已跳过。%s\n\n", table, msg)
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
		result := sb.String()
		if utf8.RuneCountInString(result) > maxSchemaOutputChars {
			runes := []rune(result)
			log.Printf("[Tool:get_table_schema] 输出超过上限 %d 字符，已截断\n", maxSchemaOutputChars)
			result = string(runes[:maxSchemaOutputChars]) +
				fmt.Sprintf("\n\n...(结果过长，已截断至 %d 字符。请分批传入表名以获取完整结构)", maxSchemaOutputChars)
		}
		return &SchemaOutput{Schema: result}, nil
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

		page, totalTables, offset, limit, nextOffset, hasMore := paginateTables(tables, input.Offset, input.Limit)
		if hasMore {
			log.Printf("[Tool:list_tables] 分页返回 - total=%d, offset=%d, limit=%d, next=%d\n",
				totalTables, offset, limit, nextOffset)
		}
		return &ListTablesOutput{
			Tables:     page,
			Count:      len(page),
			TotalCount: totalTables,
			Offset:     offset,
			Limit:      limit,
			NextOffset: nextOffset,
			HasMore:    hasMore,
			Truncated:  hasMore,
		}, nil
	}
}

// paginateTables 对表名列表分页；limit<=0 使用默认值，超过硬上限时收敛。
func paginateTables(tables []TableInfo, offset, limit int) (page []TableInfo, total, normalizedOffset, normalizedLimit, nextOffset int, hasMore bool) {
	total = len(tables)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = maxListTables
	}
	if limit > maxListTablesHard {
		limit = maxListTablesHard
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page = tables[offset:end]
	hasMore = end < total
	if hasMore {
		nextOffset = end
	}
	return page, total, offset, limit, nextOffset, hasMore
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
	FileID string `json:"fileId" jsonschema:"required" jsonschema_description:"上传文件 ID（fileId）"`
	// Mode read | outline | search | profile；默认 read
	Mode string `json:"mode,omitempty" jsonschema_description:"读取模式：read|outline|search|profile，默认 read"`
	// Unit rows | chars | lines | page | section；默认按文件类别推断
	Unit string `json:"unit,omitempty" jsonschema_description:"分页单位：rows|chars|lines|page|section，默认按文件类别推断"`
	// Sheet 表格类：指定工作表名称；默认第一个工作表
	Sheet string `json:"sheet,omitempty" jsonschema_description:"表格类文件的工作表名称，默认第一个工作表"`
	// Query search 模式：关键词或正则
	Query string `json:"query,omitempty" jsonschema_description:"search 模式的关键词或正则"`
	// Regex search 模式：是否按正则解释 Query
	Regex bool `json:"regex,omitempty" jsonschema_description:"search 模式：是否按正则解释 query"`
	// CaseSensitive search 模式：是否区分大小写
	CaseSensitive bool `json:"caseSensitive,omitempty" jsonschema_description:"search 模式：是否区分大小写"`
	// MaxMatches search 模式：最多返回的命中数
	MaxMatches int `json:"maxMatches,omitempty" jsonschema_description:"search 模式：最多返回的命中数"`
	Limit      int `json:"limit,omitempty" jsonschema_description:"返回数量上限：表格=行数；文本=字符数/行数（取决于 unit）"`
	Offset     int `json:"offset,omitempty" jsonschema_description:"起始位置：表格=行号；文本=字符/行/页/章节"`
}

// FileSectionInfo 文档章节。
type FileSectionInfo struct {
	Title     string `json:"title"`
	Level     int    `json:"level,omitempty"`
	StartChar int    `json:"startChar"`
	EndChar   int    `json:"endChar,omitempty"`
}

// FilePageInfo 文档页码区间。
type FilePageInfo struct {
	Number    int `json:"number"`
	StartChar int `json:"startChar"`
	EndChar   int `json:"endChar,omitempty"`
}

// SheetInfo 工作表摘要。
type SheetInfo struct {
	Name      string   `json:"name"`
	Columns   []string `json:"columns,omitempty"`
	TotalRows int      `json:"totalRows,omitempty"`
	Notice    string   `json:"notice,omitempty"`
}

// FileMatch 搜索命中。
type FileMatch struct {
	Location string `json:"location,omitempty"` // page/section/line 描述
	Sheet    string `json:"sheet,omitempty"`
	Line     int    `json:"line,omitempty"`
	Row      int    `json:"row,omitempty"`
	Column   string `json:"column,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Snippet  string `json:"snippet"`
}

// ColumnProfile 单列统计。
type ColumnProfile struct {
	Name     string   `json:"name"`
	Type     string   `json:"type,omitempty"`
	Nulls    int      `json:"nulls,omitempty"`
	Distinct int      `json:"distinct,omitempty"`
	Min      string   `json:"min,omitempty"`
	Max      string   `json:"max,omitempty"`
	Top      []string `json:"top,omitempty"`
}

// FileProfile 文件/表格画像。
type FileProfile struct {
	Columns []ColumnProfile `json:"columns,omitempty"`
	Sampled bool            `json:"sampled,omitempty"`
	Notice  string          `json:"notice,omitempty"`
}

// ReadFileDataOutput 文件内容读取结果。
//
// 字段保持向后兼容（table/text 两类的既有字段不变），并追加：
//   - Meta：图片/视频元信息；
//   - Notice：截断/降级说明（面向模型，如「仅返回前 N 字符」「docx 为降级解析」）；
//   - Degraded：是否降级解析（如无 Python 环境时的内置 docx 解析）。
type ReadFileDataOutput struct {
	Type         string         `json:"type"` // "table"（xlsx/xls/csv）| "text"（txt/md/docx/pdf）| "image" | "video"
	Columns      []string       `json:"columns,omitempty"`
	Rows         [][]string     `json:"rows,omitempty"`
	Sample       [][]string     `json:"sample,omitempty"`
	TotalRows    int            `json:"totalRows,omitempty"`
	ReturnedRows int            `json:"returnedRows,omitempty"`
	Text         string         `json:"text,omitempty"`
	CharCount    int            `json:"charCount,omitempty"`
	Meta         map[string]any `json:"meta,omitempty"`
	Notice       string         `json:"notice,omitempty"`
	Degraded     bool           `json:"degraded,omitempty"`

	// 结构化读取结果（P1）
	Sections []FileSectionInfo `json:"sections,omitempty"`
	Pages    []FilePageInfo    `json:"pages,omitempty"`
	Sheets   []SheetInfo       `json:"sheets,omitempty"`
	Matches  []FileMatch       `json:"matches,omitempty"`
	Profile  *FileProfile      `json:"profile,omitempty"`
	// DatasetID 表格类物化后的数据集句柄（P2）
	DatasetID string `json:"datasetId,omitempty"`
	// LineStart/LineEnd 文本/文档按行读取时的行号范围
	LineStart int `json:"lineStart,omitempty"`
	LineEnd   int `json:"lineEnd,omitempty"`
	// PageStart/PageEnd 文档按页读取时的页码范围
	PageStart int `json:"pageStart,omitempty"`
	PageEnd   int `json:"pageEnd,omitempty"`
	// SectionTitle 按章节读取时的章节标题
	SectionTitle string `json:"sectionTitle,omitempty"`
}

// NewReadFileDataFunc 返回读取已上传文件数据的工具函数（只读，供 LLM 分析文件内容）。
//
// 延迟解析：内容在首次调用本工具时才解析（fileparse），结果按 fileId 缓存复用；
// 不会写入数据库，也不会删除暂存文件，可多次分页调用。
//   - 表格类（xlsx/xls/csv）：按 limit/offset 分页返回行；
//   - 文本/文档类（txt/md/docx/pdf）：按 limit/offset 分页返回字符窗口；
//   - 图片/视频：返回元信息（内容由多模态注入链路直接随消息提供）。
//
// userID 为调用者身份，用于上传文件归属校验（仅能读取本人上传的文件）。
func NewReadFileDataFunc(userID string) func(ctx context.Context, input *ReadFileDataInput) (*ReadFileDataOutput, error) {
	return func(ctx context.Context, input *ReadFileDataInput) (*ReadFileDataOutput, error) {
		log.Printf("[Tool:read_file_data] fileId=%s, limit=%d, offset=%d\n", input.FileID, input.Limit, input.Offset)
		if input.FileID == "" {
			return nil, errors.New("fileId is required")
		}

		res, err := GetUploadedFile(ctx, input.FileID, userID)
		if err != nil {
			return nil, err
		}

		mode := strings.ToLower(strings.TrimSpace(input.Mode))
		if mode == "" {
			mode = "read"
		}

		switch res.Kind {
		case fileparse.KindTable:
			switch mode {
			case "outline":
				return buildTableOutline(res, input), nil
			case "search":
				return searchTableFile(res, input), nil
			case "profile":
				return profileTableFile(res, input), nil
			case "read":
				out := readTableWindow(res, input)
				maybeMaterializeReadFile(ctx, res, input, out)
				return out, nil
			default:
				return nil, fmt.Errorf("不支持的 mode：%s（可选 read/outline/search/profile）", input.Mode)
			}
		case fileparse.KindText, fileparse.KindDoc:
			switch mode {
			case "outline":
				return buildTextOutline(res), nil
			case "search":
				return searchTextFile(res, input), nil
			case "profile":
				return profileTextFile(res), nil
			case "read":
				return readTextWindow(res, input), nil
			default:
				return nil, fmt.Errorf("不支持的 mode：%s（可选 read/outline/search/profile）", input.Mode)
			}
		case fileparse.KindImage:
			return &ReadFileDataOutput{
				Type: "image",
				Meta: res.Meta,
				Notice: "图片内容已由多模态链路直接提供给模型，此处仅返回元信息。" +
					"请基于已看到的图像内容回答用户问题，不要重复请求图片内容。",
			}, nil
		case fileparse.KindVideo:
			return &ReadFileDataOutput{
				Type: "video",
				Meta: res.Meta,
				Notice: "视频仅提供元信息（时长/分辨率/编码），不支持逐帧内容分析；" +
					"若当前模型支持视频输入，视频内容已随本次消息提供给模型。",
			}, nil
		default:
			return nil, fmt.Errorf("不支持的文件类型：%s", res.Kind)
		}
	}
}

// maybeMaterializeReadFile 把大表格（或大分页结果）物化为数据集。
// 小结果保持原样；失败时保留分页结果并追加 notice。
func maybeMaterializeReadFile(ctx context.Context, res *fileparse.Result, input *ReadFileDataInput, out *ReadFileDataOutput) {
	if os.Getenv("AGENT_DATASET") == "0" {
		return
	}
	if out == nil || out.Type != "table" {
		return
	}
	if sheet := strings.TrimSpace(input.Sheet); sheet != "" && len(res.Sheets) > 0 {
		found := false
		for _, s := range res.Sheets {
			if strings.EqualFold(s.Name, sheet) {
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	columns, rows, total, _, _ := selectTableData(res, input.Sheet)
	if len(rows) == 0 || len(columns) == 0 {
		return
	}
	if len(rows) < fileDatasetMinRows && estimateRowsBytes(rows) < datasetMaterializeMinBytes {
		return
	}
	id, ok := runIdentityFrom(ctx)
	if !ok || id.UserID == "" || id.SessionID == "" {
		return
	}

	mapped := make([]map[string]any, len(rows))
	for i, row := range rows {
		m := make(map[string]any, len(columns))
		for j, col := range columns {
			if j < len(row) {
				m[col] = row[j]
			}
		}
		mapped[i] = m
	}
	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	entry, err := dataset.Default().Create(createCtx, id.UserID, id.SessionID, columns, mapped)
	if err != nil {
		recordAgentMetric("ai_dataset_created_total", "failed")
		out.Notice = joinNotice(out.Notice, "表格物化为数据集失败，已保留分页结果；请继续用 offset 分页或 profile 分析")
		return
	}
	recordAgentMetric("ai_dataset_created_total", "ok")
	sample := out.Rows
	if len(sample) > datasetSampleRows {
		sample = sample[:datasetSampleRows]
	}
	out.Columns = entry.Columns
	out.Rows = nil
	out.Sample = sample
	out.DatasetID = entry.ID
	out.TotalRows = total
	out.ReturnedRows = len(sample)
	out.Notice = joinNotice(out.Notice, "表格已物化为数据集，可用 dataset_query 在数据集上执行只读 SQL（聚合/过滤/采样）；样本仅用于预览")
	if total > entry.RowCount {
		out.Notice = joinNotice(out.Notice, fmt.Sprintf("数据集仅包含可解析的前 %d 行（原始共 %d 行），超出解析上限的部分需另行处理", entry.RowCount, total))
	}
}

func estimateRowsBytes(rows [][]string) int {
	total := 0
	for _, row := range rows {
		for _, cell := range row {
			total += len(cell)
			if total >= datasetMaterializeMinBytes {
				return total
			}
		}
	}
	return total
}

// readTableWindow 表格类分页读取；支持按 sheet 选择工作表。
func readTableWindow(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	columns := res.Columns
	dataRows := res.Rows
	total := res.TotalRows
	baseNotice := res.Notice

	sheetsInfo := make([]SheetInfo, 0, len(res.Sheets))
	for _, s := range res.Sheets {
		sheetsInfo = append(sheetsInfo, SheetInfo{
			Name:      s.Name,
			Columns:   s.Columns,
			TotalRows: s.TotalRows,
			Notice:    s.Notice,
		})
	}

	if sheetName := strings.TrimSpace(input.Sheet); sheetName != "" {
		if len(res.Sheets) == 0 {
			return &ReadFileDataOutput{
				Type:         "table",
				Columns:      columns,
				TotalRows:    total,
				ReturnedRows: 0,
				Sheets:       sheetsInfo,
				Notice:       "当前文件不是多工作表 Excel，sheet 参数不适用",
				Degraded:     res.Degraded,
			}
		}
		found := false
		for _, s := range res.Sheets {
			if strings.EqualFold(s.Name, sheetName) {
				columns = s.Columns
				dataRows = s.Rows
				total = s.TotalRows
				baseNotice = joinNotice(baseNotice, s.Notice)
				found = true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(res.Sheets))
			for _, s := range res.Sheets {
				names = append(names, s.Name)
			}
			return &ReadFileDataOutput{
				Type:         "table",
				Columns:      res.Columns,
				TotalRows:    res.TotalRows,
				ReturnedRows: 0,
				Sheets:       sheetsInfo,
				Notice:       fmt.Sprintf("sheet %q 不存在；可用工作表：%s", sheetName, strings.Join(names, ", ")),
				Degraded:     res.Degraded,
			}
		}
	}

	limit := input.Limit
	if limit <= 0 {
		limit = fileparse.DefaultRowLimit
	}
	if limit > fileparse.MaxRowLimit {
		limit = fileparse.MaxRowLimit
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	if total < len(dataRows) {
		total = len(dataRows)
	}
	start := offset
	if start > len(dataRows) {
		start = len(dataRows)
	}
	end := start + limit
	if end > len(dataRows) {
		end = len(dataRows)
	}

	src := dataRows[start:end]
	rows := make([][]string, len(src))
	for i, r := range src {
		cp := make([]string, len(r))
		copy(cp, r)
		rows[i] = cp
	}

	notice := baseNotice
	if end < total {
		if end < len(dataRows) {
			notice = joinNotice(notice, fmt.Sprintf("本次返回第 %d-%d 行（共 %d 行），如需后续数据可再次调用并传入 offset=%d",
				start+1, end, total, end))
		} else {
			notice = joinNotice(notice, "解析结果已在服务端上限处截断，无法继续分页；请基于已有数据聚合/抽样，或改用其他数据源")
		}
	}
	return &ReadFileDataOutput{
		Type:         "table",
		Columns:      columns,
		Rows:         rows,
		TotalRows:    total,
		ReturnedRows: len(rows),
		Sheets:       sheetsInfo,
		Notice:       notice,
		Degraded:     res.Degraded,
	}
}

// readTextWindow 文本/文档类分页读取。
//
// 默认按字符（rune）窗口，保持既有 Markdown 行为；unit=lines 时按行读取。
// 单元语义写入返回值与 Notice，避免模型把“行”误当“字符”。
func readTextWindow(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	unit := strings.ToLower(strings.TrimSpace(input.Unit))
	switch unit {
	case "lines":
		return readTextLines(res, input)
	case "page":
		return readTextPages(res, input)
	case "section":
		return readTextSections(res, input)
	}

	runes := []rune(res.Text)
	total := len(runes)

	limit := input.Limit
	if limit <= 0 {
		limit = maxMarkdownFileChars
	}
	if limit > maxTextWindowChars {
		limit = maxTextWindowChars
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	notice := res.Notice
	if end < total {
		notice = joinNotice(notice, fmt.Sprintf("文档较长，本次返回第 %d-%d 字符（共 %d 字符）。如需后续内容可再次调用并传入 offset=%d（unit=chars）",
			offset+1, end, total, end))
	}

	lineStart := 1 + strings.Count(string(runes[:offset]), "\n")
	lineEnd := lineStart + strings.Count(string(runes[offset:end]), "\n")
	return &ReadFileDataOutput{
		Type:      "text",
		Text:      string(runes[offset:end]),
		CharCount: total,
		LineStart: lineStart,
		LineEnd:   lineEnd,
		Notice:    notice,
		Degraded:  res.Degraded,
	}
}

// readTextPages 按页读取：offset 为起始页号（1-based），limit 为页数。
func readTextPages(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	pages := res.PageList
	if len(pages) == 0 {
		pages = extractPageMarkers(res.Text)
	}
	if len(pages) == 0 {
		return &ReadFileDataOutput{Type: "text", CharCount: res.TotalChars, Notice: "未提取到页码结构；可用 unit=chars 或 unit=lines 读取"}
	}
	offset := input.Offset
	if offset <= 0 {
		offset = 1
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 1
	}
	if offset > len(pages) {
		return &ReadFileDataOutput{Type: "text", CharCount: res.TotalChars, Notice: fmt.Sprintf("页号 %d 超出范围（共 %d 页）", offset, len(pages))}
	}
	start := offset - 1
	end := start + limit
	if end > len(pages) {
		end = len(pages)
	}
	runes := []rune(res.Text)
	charStart := pages[start].StartChar
	charEnd := pages[end-1].EndChar
	if charStart > len(runes) {
		charStart = len(runes)
	}
	if charEnd > len(runes) {
		charEnd = len(runes)
	}
	notice := res.Notice
	if end < len(pages) {
		notice = joinNotice(notice, fmt.Sprintf("本次返回第 %d-%d 页（共 %d 页），可继续传入 offset=%d（unit=page）", pages[start].Number, pages[end-1].Number, len(pages), end+1))
	}
	return &ReadFileDataOutput{
		Type:      "text",
		Text:      string(runes[charStart:charEnd]),
		CharCount: res.TotalChars,
		PageStart: pages[start].Number,
		PageEnd:   pages[end-1].Number,
		Notice:    notice,
		Degraded:  res.Degraded,
	}
}

// readTextSections 按章节读取：offset 为章节序号（1-based），limit 为章节数。
func readTextSections(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	if len(res.Sections) == 0 {
		return &ReadFileDataOutput{Type: "text", CharCount: res.TotalChars, Notice: "未提取到章节结构；可用 unit=chars 或 unit=lines 读取"}
	}
	offset := input.Offset
	if offset <= 0 {
		offset = 1
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 1
	}
	if offset > len(res.Sections) {
		return &ReadFileDataOutput{Type: "text", CharCount: res.TotalChars, Notice: fmt.Sprintf("章节序号 %d 超出范围（共 %d 个章节）", offset, len(res.Sections))}
	}
	start := offset - 1
	end := start + limit
	if end > len(res.Sections) {
		end = len(res.Sections)
	}
	runes := []rune(res.Text)
	charStart := res.Sections[start].StartChar
	charEnd := res.Sections[end-1].EndChar
	if charStart > len(runes) {
		charStart = len(runes)
	}
	if charEnd > len(runes) {
		charEnd = len(runes)
	}
	notice := res.Notice
	if end < len(res.Sections) {
		notice = joinNotice(notice, fmt.Sprintf("本次返回第 %d-%d 个章节（共 %d 个），可继续传入 offset=%d（unit=section）", start+1, end, len(res.Sections), end+1))
	}
	return &ReadFileDataOutput{
		Type:         "text",
		Text:         string(runes[charStart:charEnd]),
		CharCount:    res.TotalChars,
		SectionTitle: res.Sections[start].Title,
		Notice:       notice,
		Degraded:     res.Degraded,
	}
}

// readTextLines 文本/文档类按行分页。
func readTextLines(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	lines := strings.Split(res.Text, "\n")
	totalLines := len(lines)
	offset := input.Offset
	if offset <= 0 {
		offset = 1
	}
	if offset > totalLines {
		offset = totalLines
	}
	limit := input.Limit
	if limit <= 0 {
		limit = maxTextWindowChars
	}
	start := offset - 1
	end := start + limit
	if end > totalLines {
		end = totalLines
	}
	notice := res.Notice
	if end < totalLines {
		notice = joinNotice(notice, fmt.Sprintf("本次返回第 %d-%d 行（共 %d 行）。如需后续内容可再次调用并传入 offset=%d（unit=lines）",
			start+1, end, totalLines, end+1))
	}
	return &ReadFileDataOutput{
		Type:      "text",
		Text:      strings.Join(lines[start:end], "\n"),
		CharCount: utf8.RuneCountInString(res.Text),
		LineStart: start + 1,
		LineEnd:   end,
		Notice:    notice,
		Degraded:  res.Degraded,
	}
}

// buildTableOutline 返回工作表/列结构，供模型决定后续读取范围。
func buildTableOutline(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	sheetsInfo := make([]SheetInfo, 0, len(res.Sheets))
	for _, s := range res.Sheets {
		sheetsInfo = append(sheetsInfo, SheetInfo{
			Name:      s.Name,
			Columns:   s.Columns,
			TotalRows: s.TotalRows,
			Notice:    s.Notice,
		})
	}
	notice := res.Notice
	if len(sheetsInfo) == 0 {
		sheetsInfo = append(sheetsInfo, SheetInfo{Columns: res.Columns, TotalRows: res.TotalRows})
		notice = joinNotice(notice, "单工作表/CSV，无多 sheet 结构")
	}
	return &ReadFileDataOutput{
		Type:      "table",
		Columns:   res.Columns,
		TotalRows: res.TotalRows,
		Sheets:    sheetsInfo,
		Notice:    notice,
		Degraded:  res.Degraded,
	}
}

// buildTextOutline 返回章节/页码结构，供模型定位后再精读。
func buildTextOutline(res *fileparse.Result) *ReadFileDataOutput {
	sections := make([]FileSectionInfo, 0, len(res.Sections))
	for _, s := range res.Sections {
		sections = append(sections, FileSectionInfo{Title: s.Title, Level: s.Level, StartChar: s.StartChar, EndChar: s.EndChar})
	}
	pages := make([]fileparse.Page, len(res.PageList))
	copy(pages, res.PageList)
	if len(pages) == 0 {
		pages = extractPageMarkers(res.Text)
	}
	pageInfos := make([]FilePageInfo, 0, len(pages))
	for _, p := range pages {
		pageInfos = append(pageInfos, FilePageInfo{Number: p.Number, StartChar: p.StartChar, EndChar: p.EndChar})
	}
	notice := res.Notice
	if len(sections) == 0 && len(pageInfos) == 0 {
		notice = joinNotice(notice, "未提取到章节/页码结构；可用 search 定位或 read 按字符/行读取")
	}
	return &ReadFileDataOutput{
		Type:      "text",
		CharCount: res.TotalChars,
		Sections:  sections,
		Pages:     pageInfos,
		Notice:    notice,
		Degraded:  res.Degraded,
	}
}

// searchTextFile 在文本/文档中搜索关键词或正则，返回行号、页码/章节与片段。
func searchTextFile(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return &ReadFileDataOutput{Type: "text", CharCount: res.TotalChars, Notice: "search 模式需要提供 query"}
	}
	matcher, err := buildSearchMatcher(query, input.Regex, input.CaseSensitive)
	if err != nil {
		return &ReadFileDataOutput{Type: "text", CharCount: res.TotalChars, Notice: err.Error()}
	}
	maxMatches := input.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 20
	}
	if maxMatches > 200 {
		maxMatches = 200
	}

	pages := res.PageList
	if len(pages) == 0 {
		pages = extractPageMarkers(res.Text)
	}
	lines := strings.Split(res.Text, "\n")
	offset := 0
	matches := make([]FileMatch, 0, maxMatches)
	truncated := false
	for i, line := range lines {
		if matcher(line) {
			if len(matches) >= maxMatches {
				truncated = true
				break
			}
			start := i - 1
			if start < 0 {
				start = 0
			}
			end := i + 2
			if end > len(lines) {
				end = len(lines)
			}
			snippet := clampUTF8Prefix(strings.Join(lines[start:end], "\n"), 500)
			matches = append(matches, FileMatch{
				Location: locationAt(res.Sections, pages, offset),
				Line:     i + 1,
				Offset:   offset,
				Snippet:  snippet,
			})
		}
		offset += utf8.RuneCountInString(line) + 1
	}
	notice := res.Notice
	if truncated {
		notice = joinNotice(notice, fmt.Sprintf("命中超过 %d 条，仅返回前 %d 条；可缩小 query 或增大 maxMatches", maxMatches, len(matches)))
	}
	if len(matches) == 0 {
		notice = joinNotice(notice, "没有匹配项")
	}
	return &ReadFileDataOutput{
		Type:      "text",
		CharCount: res.TotalChars,
		Matches:   matches,
		Notice:    notice,
		Degraded:  res.Degraded,
	}
}

// searchTableFile 在表格中搜索单元格内容，返回 sheet/行/列与片段。
func searchTableFile(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return &ReadFileDataOutput{Type: "table", Notice: "search 模式需要提供 query"}
	}
	matcher, err := buildSearchMatcher(query, input.Regex, input.CaseSensitive)
	if err != nil {
		return &ReadFileDataOutput{Type: "table", Notice: err.Error()}
	}
	maxMatches := input.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 20
	}
	if maxMatches > 200 {
		maxMatches = 200
	}

	columns, rows, total, baseNotice, sheetsInfo := selectTableData(res, input.Sheet)
	sheetLabel := strings.TrimSpace(input.Sheet)
	if sheetLabel == "" && len(res.Sheets) > 0 {
		sheetLabel = res.Sheets[0].Name
	}
	matches := make([]FileMatch, 0, maxMatches)
	truncated := false
	for rowIdx, row := range rows {
		for colIdx, cell := range row {
			if cell == "" || !matcher(cell) {
				continue
			}
			if len(matches) >= maxMatches {
				truncated = true
				break
			}
			colName := ""
			if colIdx < len(columns) {
				colName = columns[colIdx]
			}
			matches = append(matches, FileMatch{
				Location: fmt.Sprintf("第 %d 行", rowIdx+1),
				Sheet:    sheetLabel,
				Row:      rowIdx + 1,
				Column:   colName,
				Snippet:  clampUTF8Prefix(cell, 500),
			})
		}
		if truncated {
			break
		}
	}
	notice := baseNotice
	if truncated {
		notice = joinNotice(notice, fmt.Sprintf("命中超过 %d 条，仅返回前 %d 条；可缩小 query 或增大 maxMatches", maxMatches, len(matches)))
	}
	if len(matches) == 0 {
		notice = joinNotice(notice, "没有匹配项")
	}
	return &ReadFileDataOutput{
		Type:         "table",
		Columns:      columns,
		TotalRows:    total,
		ReturnedRows: len(matches),
		Sheets:       sheetsInfo,
		Matches:      matches,
		Notice:       notice,
		Degraded:     res.Degraded,
	}
}

// profileTextFile 返回文本/文档的基本画像。
func profileTextFile(res *fileparse.Result) *ReadFileDataOutput {
	pages := res.PageList
	if len(pages) == 0 {
		pages = extractPageMarkers(res.Text)
	}
	return &ReadFileDataOutput{
		Type:      "text",
		CharCount: res.TotalChars,
		Sections:  fileSections(res.Sections),
		Pages:     filePages(pages),
		Profile:   &FileProfile{Notice: fmt.Sprintf("字符数=%d，章节数=%d，页数=%d", res.TotalChars, len(res.Sections), len(pages))},
		Notice:    res.Notice,
		Degraded:  res.Degraded,
	}
}

// profileTableFile 返回表格列统计；大表基于样本并标注。
func profileTableFile(res *fileparse.Result, input *ReadFileDataInput) *ReadFileDataOutput {
	columns, rows, total, baseNotice, sheetsInfo := selectTableData(res, input.Sheet)
	const profileSampleLimit = 50_000
	sampled := false
	if len(rows) > profileSampleLimit {
		rows = rows[:profileSampleLimit]
		sampled = true
	}
	profiles := make([]ColumnProfile, 0, len(columns))
	for colIdx, name := range columns {
		profile := ColumnProfile{Name: name}
		numeric := true
		hasValue := false
		hasNumeric := false
		var min, max float64
		distinct := make(map[string]int, 64)
		for _, row := range rows {
			if colIdx >= len(row) {
				profile.Nulls++
				continue
			}
			value := strings.TrimSpace(row[colIdx])
			if value == "" {
				profile.Nulls++
				continue
			}
			hasValue = true
			if f, parseErr := strconv.ParseFloat(value, 64); parseErr == nil {
				if !hasNumeric {
					min, max, hasNumeric = f, f, true
				} else {
					if f < min {
						min = f
					}
					if f > max {
						max = f
					}
				}
			} else {
				numeric = false
			}
			if len(distinct) < 100 {
				distinct[value]++
			}
		}
		if numeric && hasValue && hasNumeric {
			profile.Type = "number"
			profile.Min = fmt.Sprintf("%g", min)
			profile.Max = fmt.Sprintf("%g", max)
		} else {
			profile.Type = "string"
		}
		profile.Distinct = len(distinct)
		profile.Top = topValues(distinct, 5)
		profiles = append(profiles, profile)
	}
	notice := baseNotice
	if sampled {
		notice = joinNotice(notice, fmt.Sprintf("列统计基于前 %d 行样本（共 %d 行）", profileSampleLimit, total))
	}
	return &ReadFileDataOutput{
		Type:         "table",
		Columns:      columns,
		TotalRows:    total,
		ReturnedRows: len(rows),
		Sheets:       sheetsInfo,
		Profile:      &FileProfile{Columns: profiles, Sampled: sampled},
		Notice:       notice,
		Degraded:     res.Degraded,
	}
}

func topValues(counts map[string]int, limit int) []string {
	type pair struct {
		value string
		count int
	}
	pairs := make([]pair, 0, len(counts))
	for value, count := range counts {
		pairs = append(pairs, pair{value: value, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].value < pairs[j].value
		}
		return pairs[i].count > pairs[j].count
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, fmt.Sprintf("%s (%d)", clampUTF8Prefix(p.value, 200), p.count))
	}
	return out
}

// selectTableData 选择工作表并返回列/行/总数/说明。
func selectTableData(res *fileparse.Result, sheet string) (columns []string, rows [][]string, total int, notice string, sheetsInfo []SheetInfo) {
	columns, rows, total, notice = res.Columns, res.Rows, res.TotalRows, res.Notice
	sheetsInfo = make([]SheetInfo, 0, len(res.Sheets))
	for _, s := range res.Sheets {
		sheetsInfo = append(sheetsInfo, SheetInfo{Name: s.Name, Columns: s.Columns, TotalRows: s.TotalRows, Notice: s.Notice})
	}
	if strings.TrimSpace(sheet) == "" || len(res.Sheets) == 0 {
		return
	}
	for _, s := range res.Sheets {
		if strings.EqualFold(s.Name, sheet) {
			return s.Columns, s.Rows, s.TotalRows, joinNotice(notice, s.Notice), sheetsInfo
		}
	}
	return columns, rows, total, joinNotice(notice, fmt.Sprintf("sheet %q 不存在，已回退到默认工作表", sheet)), sheetsInfo
}

func buildSearchMatcher(query string, regex, caseSensitive bool) (func(string) bool, error) {
	if regex {
		expr := query
		if !caseSensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("query 不是合法正则：%w", err)
		}
		return re.MatchString, nil
	}
	if caseSensitive {
		return func(s string) bool { return strings.Contains(s, query) }, nil
	}
	lower := strings.ToLower(query)
	return func(s string) bool { return strings.Contains(strings.ToLower(s), lower) }, nil
}

func extractPageMarkers(text string) []fileparse.Page {
	re := regexp.MustCompile(`----- 第 (\d+) 页 -----`)
	locs := re.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return nil
	}
	pages := make([]fileparse.Page, 0, len(locs))
	for i, loc := range locs {
		number, _ := strconv.Atoi(text[loc[2]:loc[3]])
		start := utf8.RuneCountInString(text[:loc[0]])
		end := utf8.RuneCountInString(text)
		if i+1 < len(locs) {
			end = utf8.RuneCountInString(text[:locs[i+1][0]])
		}
		pages = append(pages, fileparse.Page{Number: number, StartChar: start, EndChar: end})
	}
	return pages
}

func locationAt(sections []fileparse.Section, pages []fileparse.Page, offset int) string {
	parts := make([]string, 0, 2)
	for _, p := range pages {
		if offset >= p.StartChar && offset < p.EndChar {
			parts = append(parts, fmt.Sprintf("第 %d 页", p.Number))
			break
		}
	}
	var title string
	for _, s := range sections {
		if offset >= s.StartChar {
			title = s.Title
		}
	}
	if title != "" {
		parts = append(parts, "章节："+title)
	}
	return strings.Join(parts, " / ")
}

func fileSections(in []fileparse.Section) []FileSectionInfo {
	out := make([]FileSectionInfo, 0, len(in))
	for _, s := range in {
		out = append(out, FileSectionInfo{Title: s.Title, Level: s.Level, StartChar: s.StartChar, EndChar: s.EndChar})
	}
	return out
}

func filePages(in []fileparse.Page) []FilePageInfo {
	out := make([]FilePageInfo, 0, len(in))
	for _, p := range in {
		out = append(out, FilePageInfo{Number: p.Number, StartChar: p.StartChar, EndChar: p.EndChar})
	}
	return out
}

// joinNotice 拼接说明文案（分号分隔）
func joinNotice(existing, add string) string {
	if add == "" {
		return existing
	}
	if existing == "" {
		return add
	}
	return existing + "；" + add
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

// buildQueryOutput 统一构造查询结果，并显式标注截断状态。
// 不把“服务端安全上限”写成分析建议，只告诉模型实际返回了多少、是否还有更多。
func buildQueryOutput(columns []string, data []map[string]any, truncated bool) *QueryOutput {
	out := &QueryOutput{
		Columns:      columns,
		Data:         data,
		Count:        len(data),
		ReturnedRows: len(data),
		RowCount:     len(data),
		TotalKnown:   !truncated,
		Truncated:    truncated,
		HasMore:      truncated,
	}
	if truncated {
		// 总数未知：不要把“返回了多少行”伪装成总行数。
		out.RowCount = 0
	}
	if encoded, err := json.Marshal(out); err == nil {
		out.EstimatedBytes = len(encoded)
	}
	if truncated {
		out.Notice = fmt.Sprintf(
			"结果超过服务端安全上限，仅返回前 %d 行。请先聚合/过滤，或基于已有结果继续分析；不要把它当作全量数据。",
			len(data),
		)
	}
	return out
}

// reOracleRowNum 识别 Oracle 老版本的 ROWNUM 行数限制谓词。
var reOracleRowNum = regexp.MustCompile(`(?i)\bROWNUM\b`)

// applyRowLimit 给 SELECT 加 LIMIT，未带顶层 LIMIT 时附加。
//
// 支持检测 LIMIT / FETCH FIRST|NEXT / SELECT TOP；子查询内的 LIMIT 不算。
// Oracle 12c+ 使用 OFFSET 0 ROWS FETCH NEXT N ROWS ONLY（保留 ORDER BY 语义）。
func applyRowLimit(sql, driverName string, maxRows int) string {
	// 先剥注释，避免注释中的 LIMIT 干扰判断；再用词法级 HasTopLevelLimit
	// 只识别最外层 LIMIT/FETCH/TOP，子查询内的 LIMIT 不算。
	stripped := sqlutil.StripSQLComments(sql)
	trimmed := strings.TrimRight(stripped, "; \t\r\n")
	if sqlguard.HasTopLevelLimit(trimmed) {
		return sql
	}
	// Oracle 老版本常用 ROWNUM 谓词限制行数；已有该谓词时不再追加 FETCH。
	if driverName == "oracle" && reOracleRowNum.MatchString(trimmed) {
		return sql
	}
	switch driverName {
	case "oracle":
		// Oracle 12c+ 使用标准 OFFSET/FETCH 语法，避免 ROWNUM 破坏 ORDER BY
		return fmt.Sprintf("%s OFFSET 0 ROWS FETCH NEXT %d ROWS ONLY", trimmed, maxRows)
	default:
		return fmt.Sprintf("%s LIMIT %d", trimmed, maxRows)
	}
}
