package sync

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// ===== 冲突处理策略常量 =====
//
// 数据同步在遇到目标已存在主键/唯一键时的处理方式。
// 不同数据库类型支持的策略不同（见 SupportedConflictStrategies）。
const (
	StrategyUpdate       = "update"        // 更新冲突记录（默认）：MySQL 用 INSERT...ON DUPLICATE KEY UPDATE
	StrategySkip         = "skip"          // 跳过冲突记录：目标已存在则不处理（INSERT 用 IGNORE，UPDATE 不生成）
	StrategyInsertIgnore = "insert_ignore" // MySQL 专用：INSERT IGNORE INTO
	StrategyReplace      = "replace"       // MySQL 专用：REPLACE INTO
	StrategyFail         = "fail"          // 普通 INSERT，遇到主键冲突立即停止
)

// SupportedConflictStrategies 返回指定数据库类型可用的冲突策略列表。
// Oracle 没有 INSERT IGNORE / REPLACE 语法，故仅暴露 skip/update/fail。
func SupportedConflictStrategies(dbType string) []string {
	switch dbType {
	case "mysql", "mariadb":
		return []string{StrategyUpdate, StrategySkip, StrategyInsertIgnore, StrategyReplace, StrategyFail}
	case "oracle":
		return []string{StrategyUpdate, StrategySkip, StrategyFail}
	default:
		return []string{StrategyUpdate, StrategySkip, StrategyFail}
	}
}

// buildInsertStmt 根据冲突策略生成单行 INSERT 语句。
// row 为源端行数据，keyCols 为主键列（用于排除 ON DUPLICATE KEY UPDATE 中的主键字段）。
func buildInsertStmt(strategy, dbType, schema, table string, row map[string]any, keyCols []string, qi *quoteInfo) string {
	keySet := make(map[string]bool, len(keyCols))
	for _, k := range keyCols {
		keySet[k] = true
	}

	cols := make([]string, 0, len(row))
	vals := make([]string, 0, len(row))
	// 排序以保证列顺序稳定，便于回滚解析
	for k := range row {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	for _, k := range cols {
		vals = append(vals, fmt.Sprintf("'%s'", escapeSQLValue(row[k])))
	}

	colStrs := make([]string, len(cols))
	for i, c := range cols {
		colStrs[i] = fmt.Sprintf("%s%s%s", qi.col, c, qi.colR)
	}
	targetTable := fmt.Sprintf("%s%s%s.%s%s%s", qi.col, schema, qi.colR, qi.col, table, qi.colR)
	colList := strings.Join(colStrs, ", ")
	valList := strings.Join(vals, ", ")

	switch strategy {
	case StrategyInsertIgnore:
		// MySQL 专用：INSERT IGNORE INTO
		if dbType != "mysql" && dbType != "mariadb" {
			return buildInsertStmt(StrategyFail, dbType, schema, table, row, keyCols, qi)
		}
		return fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES (%s);", targetTable, colList, valList)
	case StrategyReplace:
		// MySQL 专用：REPLACE INTO
		if dbType != "mysql" && dbType != "mariadb" {
			return buildInsertStmt(StrategyFail, dbType, schema, table, row, keyCols, qi)
		}
		return fmt.Sprintf("REPLACE INTO %s (%s) VALUES (%s);", targetTable, colList, valList)
	case StrategySkip:
		// skip：目标已存在则跳过。MySQL 用 INSERT IGNORE 实现；Oracle 退化为普通 INSERT
		if dbType == "mysql" || dbType == "mariadb" {
			return fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES (%s);", targetTable, colList, valList)
		}
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);", targetTable, colList, valList)
	case StrategyUpdate:
		// update：MySQL 用 INSERT...ON DUPLICATE KEY UPDATE 把冲突转为更新；Oracle 退化为普通 INSERT
		if dbType == "mysql" || dbType == "mariadb" {
			var setParts []string
			for _, c := range cols {
				if keySet[c] {
					continue
				}
				setParts = append(setParts, fmt.Sprintf("%s%s%s = VALUES(%s%s%s)", qi.col, c, qi.colR, qi.col, c, qi.colR))
			}
			if len(setParts) == 0 {
				return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);", targetTable, colList, valList)
			}
			return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s;", targetTable, colList, valList, strings.Join(setParts, ", "))
		}
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);", targetTable, colList, valList)
	default:
		// fail：普通 INSERT，主键冲突即报错停止
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);", targetTable, colList, valList)
	}
}

// buildUpdateStmt 生成单行 UPDATE 语句（用于修改行）。
func buildUpdateStmt(dbType, schema, table string, changes []FieldChange, keyCols []string, srcRow map[string]any, qi *quoteInfo) string {
	setParts := make([]string, 0, len(changes))
	for _, ch := range changes {
		setParts = append(setParts, fmt.Sprintf("%s%s%s = '%s'", qi.col, ch.ColumnName, qi.colR, escapeSQLValue(ch.NewValue)))
	}
	whereParts := make([]string, 0, len(keyCols))
	for _, kc := range keyCols {
		whereParts = append(whereParts, fmt.Sprintf("%s%s%s = '%s'", qi.col, kc, qi.colR, escapeSQLValue(srcRow[kc])))
	}
	targetTable := fmt.Sprintf("%s%s%s.%s%s%s", qi.col, schema, qi.colR, qi.col, table, qi.colR)
	return fmt.Sprintf("UPDATE %s SET %s WHERE %s;", targetTable, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))
}

// buildDeleteStmt 生成单行 DELETE 语句（用于目标多余行）。
func buildDeleteStmt(dbType, schema, table string, row map[string]any, keyCols []string, qi *quoteInfo) string {
	whereParts := make([]string, 0, len(keyCols))
	for _, kc := range keyCols {
		whereParts = append(whereParts, fmt.Sprintf("%s%s%s = '%s'", qi.col, kc, qi.colR, escapeSQLValue(row[kc])))
	}
	targetTable := fmt.Sprintf("%s%s%s.%s%s%s", qi.col, schema, qi.colR, qi.col, table, qi.colR)
	return fmt.Sprintf("DELETE FROM %s WHERE %s;", targetTable, strings.Join(whereParts, " AND "))
}

// generateChunkSQLWithStrategy 与 generateChunkSQL 类似，但根据冲突策略生成 SQL。
// skip 策略下不生成 UPDATE 语句（保留目标既有数据）。
func generateChunkSQLWithStrategy(addedRows []map[string]any, deletedRows []map[string]any, modifiedRows []ModifiedRow, keyColumns []string, tgtSchema, table string, qi *quoteInfo, strategy, dbType string) string {
	sqlBuf := new(bytes.Buffer)
	for _, row := range addedRows {
		sqlBuf.WriteString(buildInsertStmt(strategy, dbType, tgtSchema, table, row, keyColumns, qi))
		sqlBuf.WriteString("\n")
	}
	// skip 策略：跳过修改行，保留目标数据
	if strategy != StrategySkip {
		for _, mr := range modifiedRows {
			sqlBuf.WriteString(buildUpdateStmt(dbType, tgtSchema, table, mr.Changes, keyColumns, mr.SourceRow, qi))
			sqlBuf.WriteString("\n")
		}
	}
	for _, row := range deletedRows {
		sqlBuf.WriteString(buildDeleteStmt(dbType, tgtSchema, table, row, keyColumns, qi))
		sqlBuf.WriteString("\n")
	}
	return sqlBuf.String()
}

// ===== Dry-Run（试运行） =====
//
// DryRunSync 比对源/目标数据，但不执行任何写操作，仅返回预估影响行数与示例 SQL。
// 结果包含按操作类型（INSERT/UPDATE/DELETE）分组的统计与最多 5 条 SQL 预览，
// 以及潜在冲突列表（目标已存在且字段值不同的行）。

// DryRunTableReport 单张表的试运行报告。
type DryRunTableReport struct {
	TableName       string         `json:"tableName"`
	TotalSource     int            `json:"totalSource"`
	TotalTarget     int            `json:"totalTarget"`
	OperationCounts map[string]int `json:"operationCounts"` // INSERT/UPDATE/DELETE/CONFLICT
	Samples         []DryRunSample `json:"samples"`
	Conflicts       []ModifiedRow  `json:"conflicts"` // 前 10 条潜在冲突
}

// DryRunSample 单个操作类型的预览。
type DryRunSample struct {
	Operation  string `json:"operation"`  // INSERT / UPDATE / DELETE
	Estimate   int    `json:"estimate"`   // 预估行数
	SqlPreview string `json:"sqlPreview"` // 最多前 5 条 SQL
}

// DryRunSync 处理 Dry-Run 请求。参数与 CompareDataChunked 一致。
func DryRunSync(c *gin.Context) {
	connId1 := c.PostForm("sourceConnId")
	connId2 := c.PostForm("targetConnId")
	schema1 := c.PostForm("sourceSchema")
	schema2 := c.PostForm("targetSchema")
	table := c.PostForm("table")
	direction := c.DefaultPostForm("direction", "source_to_target")
	strategy := c.DefaultPostForm("conflictStrategy", StrategyUpdate)

	authorization := appctx.Ctx.GetAuthorization(c)
	srcConn := conn.GetConn(connId1, authorization)
	tgtConn := conn.GetConn(connId2, authorization)
	if srcConn == nil || tgtConn == nil {
		response.WriteOK(c, map[string]any{"error": "源或目标数据库连接不可用，请检查连接配置或权限"})
		return
	}
	dbType := srcConn.DriverName()

	if table == "" || !isValidIdentifier(table) {
		response.WriteOK(c, map[string]any{"error": "表名无效"})
		return
	}

	// 权限校验
	if !config.IsLocalMode() {
		permission.CheckTablePermission(connId1, schema1, table, authorization)
		permission.CheckTablePermission(connId2, schema2, table, authorization)
	}

	srcCount := getRowCount(srcConn, dbType, schema1, table)
	tgtCount := getRowCount(tgtConn, dbType, schema2, table)
	if srcCount > maxCompareRows || tgtCount > maxCompareRows {
		response.WriteOK(c, map[string]any{"error": fmt.Sprintf("数据量过大，源表%d行，目标%d行，上限%d行", srcCount, tgtCount, maxCompareRows)})
		return
	}

	// 根据方向确定实际写入的目标 schema（用于生成示例 SQL）
	tgtSchema := schema2
	if direction == "target_to_source" {
		tgtSchema = schema1
	}

	// 复用现有数据构建逻辑（已包含主键探测、行映射）
	sourceMap, targetMap, keyCols := buildSyncData(srcConn, tgtConn, dbType, schema1, schema2, table, direction)
	if len(keyCols) == 0 {
		response.WriteOK(c, map[string]any{"error": "无法确定比较键列，请确保表有主键"})
		return
	}

	qi := newQuoteInfo(dbType)

	addedRows := make([]map[string]any, 0)
	deletedRows := make([]map[string]any, 0)
	modifiedRows := make([]ModifiedRow, 0)

	for key, srcRow := range sourceMap {
		if tgtRow, ok := targetMap[key]; ok {
			changes := diffRows(srcRow, tgtRow, keyCols)
			if len(changes) > 0 {
				keyMap := make(map[string]any)
				for _, kc := range keyCols {
					keyMap[kc] = srcRow[kc]
				}
				modifiedRows = append(modifiedRows, ModifiedRow{Key: keyMap, Changes: changes, SourceRow: srcRow, TargetRow: tgtRow})
			}
		} else {
			addedRows = append(addedRows, srcRow)
		}
	}
	for key, tgtRow := range targetMap {
		if _, ok := sourceMap[key]; !ok {
			deletedRows = append(deletedRows, tgtRow)
		}
	}

	// skip 策略下修改行不生成 UPDATE，故 UPDATE 预估为 0
	updateCount := len(modifiedRows)
	if strategy == StrategySkip {
		updateCount = 0
	}

	report := DryRunTableReport{
		TableName:   table,
		TotalSource: srcCount,
		TotalTarget: tgtCount,
		OperationCounts: map[string]int{
			"INSERT":   len(addedRows),
			"UPDATE":   updateCount,
			"DELETE":   len(deletedRows),
			"CONFLICT": len(modifiedRows), // 潜在冲突 = 目标已存在且存在差异
		},
		Samples:   make([]DryRunSample, 0, 3),
		Conflicts: takeModified(modifiedRows, 10),
	}

	// 生成示例 SQL（每种操作最多 5 条）
	report.Samples = append(report.Samples, DryRunSample{
		Operation:  "INSERT",
		Estimate:   len(addedRows),
		SqlPreview: previewInserts(addedRows, strategy, dbType, tgtSchema, table, keyCols, qi, 5),
	})
	report.Samples = append(report.Samples, DryRunSample{
		Operation:  "UPDATE",
		Estimate:   updateCount,
		SqlPreview: previewUpdates(modifiedRows, dbType, tgtSchema, table, keyCols, qi, 5, strategy),
	})
	report.Samples = append(report.Samples, DryRunSample{
		Operation:  "DELETE",
		Estimate:   len(deletedRows),
		SqlPreview: previewDeletes(deletedRows, dbType, tgtSchema, table, keyCols, qi, 5),
	})

	response.WriteOK(c, report)
}

func takeModified(rows []ModifiedRow, n int) []ModifiedRow {
	if len(rows) <= n {
		return rows
	}
	return rows[:n]
}

func previewInserts(rows []map[string]any, strategy, dbType, schema, table string, keyCols []string, qi *quoteInfo, limit int) string {
	if len(rows) == 0 {
		return "-- 无新增行"
	}
	var buf bytes.Buffer
	for i, row := range rows {
		if i >= limit {
			buf.WriteString(fmt.Sprintf("-- ... 其余 %d 行省略\n", len(rows)-limit))
			break
		}
		buf.WriteString(buildInsertStmt(strategy, dbType, schema, table, row, keyCols, qi))
		buf.WriteString("\n")
	}
	return buf.String()
}

func previewUpdates(rows []ModifiedRow, dbType, schema, table string, keyCols []string, qi *quoteInfo, limit int, strategy string) string {
	if len(rows) == 0 || strategy == StrategySkip {
		if strategy == StrategySkip {
			return "-- skip 策略：跳过修改行，不生成 UPDATE"
		}
		return "-- 无修改行"
	}
	var buf bytes.Buffer
	for i, mr := range rows {
		if i >= limit {
			buf.WriteString(fmt.Sprintf("-- ... 其余 %d 行省略\n", len(rows)-limit))
			break
		}
		buf.WriteString(buildUpdateStmt(dbType, schema, table, mr.Changes, keyCols, mr.SourceRow, qi))
		buf.WriteString("\n")
	}
	return buf.String()
}

func previewDeletes(rows []map[string]any, dbType, schema, table string, keyCols []string, qi *quoteInfo, limit int) string {
	if len(rows) == 0 {
		return "-- 无删除行"
	}
	var buf bytes.Buffer
	for i, row := range rows {
		if i >= limit {
			buf.WriteString(fmt.Sprintf("-- ... 其余 %d 行省略\n", len(rows)-limit))
			break
		}
		buf.WriteString(buildDeleteStmt(dbType, schema, table, row, keyCols, qi))
		buf.WriteString("\n")
	}
	return buf.String()
}

// ===== 回滚日志存储 =====
//
// 同步执行时记录每条写操作的"撤销 SQL"，存储在内存中（sync.Map），
// key 为 syncSessionId。回滚时按逆序执行撤销 SQL。
// 日志在 30 分钟后自动清理。

const rollbackLogTTL = 30 * time.Minute

// RollbackLog 一次同步会话的回滚日志。
type RollbackLog struct {
	mu           sync.Mutex
	SessionId    string
	ConnId       string   // 目标连接 ID（回滚时使用）
	Schema       string   // 目标 schema
	DBType       string   // 目标数据库类型
	UndoSQLs     []string // 撤销 SQL（按执行顺序；回滚时逆序执行）
	OriginalSQLs []string // 原始 SQL（用于审计/展示）
	CreatedAt    time.Time
}

var rollbackStore sync.Map

// rollbackCleanerStarted 用 sync.Once 确保全局清理 goroutine 只启动一次。
// 改进点：原实现每新建一个会话就启动一个 sleep 30min 的 goroutine，
// 高并发下会有 goroutine 堆积风险。改为单 goroutine 定期扫描清理。
var rollbackCleanerStarted sync.Once

// startRollbackCleaner 启动全局清理 goroutine，定期删除超过 TTL 的回滚日志。
func startRollbackCleaner() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		now := time.Now()
		rollbackStore.Range(func(key, value any) bool {
			log := value.(*RollbackLog)
			if now.Sub(log.CreatedAt) > rollbackLogTTL {
				rollbackStore.Delete(key)
			}
			return true
		})
	}
}

// getOrCreateRollbackLog 获取或创建指定会话的回滚日志（并发安全）。
func getOrCreateRollbackLog(sessionId, connId, schema, dbType string) *RollbackLog {
	if sessionId == "" {
		return nil
	}
	// 首次调用时启动全局清理 goroutine
	rollbackCleanerStarted.Do(func() {
		safego.GoWithName("sync-rollback-cleaner", startRollbackCleaner)
	})
	v, ok := rollbackStore.Load(sessionId)
	if ok {
		return v.(*RollbackLog)
	}
	log := &RollbackLog{
		SessionId:    sessionId,
		ConnId:       connId,
		Schema:       schema,
		DBType:       dbType,
		CreatedAt:    time.Now(),
		UndoSQLs:     make([]string, 0),
		OriginalSQLs: make([]string, 0),
	}
	actual, loaded := rollbackStore.LoadOrStore(sessionId, log)
	if loaded {
		return actual.(*RollbackLog)
	}
	return log
}

// appendRollbackEntries 将本批次生成的撤销 SQL 追加到会话日志（加锁）。
func appendRollbackEntries(log *RollbackLog, originals, undos []string) {
	if log == nil {
		return
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	log.OriginalSQLs = append(log.OriginalSQLs, originals...)
	log.UndoSQLs = append(log.UndoSQLs, undos...)
}

// ===== 撤销 SQL 生成 =====
//
// 针对我们自己生成的 INSERT/UPDATE/DELETE 语句，解析出表名与条件，
// 在执行前先 SELECT 旧数据，生成对应的撤销 SQL。
// 解析失败时返回 "-- " 注释行，回滚时跳过。

var (
	// 通用标识符匹配：同时支持 MySQL 反引号 `name`、Oracle 双引号 "name"、普通标识符 name
	// 捕获组为标识符内容（不含引号）
	identRe = `(?:` + "`" + `([^` + "`" + `]+)` + "`" + `|"([^"]+)"|([a-zA-Z_][\w]*))`

	// INSERT 正则：匹配 INSERT/REPLACE INTO <table> (cols) VALUES (vals)
	// 同时匹配 MySQL `schema`.`table` 和 Oracle "schema"."table" 两种风格
	// 通过 alternation 捕获 schema/table，运行时取第一个非空组
	insertRe = regexp.MustCompile(
		`^(INSERT(?:\s+IGNORE)?|REPLACE)\s+INTO\s+` +
			`(?:` + identRe + `\s*\.\s*)?` + identRe +
			`\s*\(([^)]*)\)\s*VALUES\s*\((.*)\)\s*(?:ON\s+DUPLICATE.*)?$`)

	// UPDATE 正则：匹配 UPDATE <table> SET ... WHERE ...
	// MySQL 风格带 schema 前缀，Oracle 风格不带
	updateRe = regexp.MustCompile(
		`^UPDATE\s+` +
			`(?:` + identRe + `\s*\.\s*)?` + identRe +
			`\s+SET\s+(.*?)\s+WHERE\s+(.*)$`)

	// DELETE 正则：匹配 DELETE FROM <table> WHERE ...
	deleteRe = regexp.MustCompile(
		`^DELETE\s+FROM\s+` +
			`(?:` + identRe + `\s*\.\s*)?` + identRe +
			`\s+WHERE\s+(.*)$`)

	// whitespaceRe 用于把 SQL 中的连续空白压缩为单空格，让正则匹配更健壮
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// extractIdent 从正则匹配结果中提取第一个非空的标识符（处理 alternation 多捕获组）
func extractIdent(matches []string, startIdx int) string {
	// identRe 有 3 个捕获组（反引号/双引号/普通），schema 和 table 各占 3 组
	// schema 在 startIdx..startIdx+2，table 在 startIdx+3..startIdx+5
	for i := startIdx; i < startIdx+3 && i < len(matches); i++ {
		if matches[i] != "" {
			return matches[i]
		}
	}
	return ""
}

// normalizeSQL 把 SQL trim + 压缩空白，让正则匹配能容忍多空格/换行
func normalizeSQL(s string) string {
	s = strings.TrimSpace(s)
	s = whitespaceRe.ReplaceAllString(s, " ")
	return s
}

// generateUndoSQL 为单条 SQL 生成撤销 SQL。dbConn 用于 UPDATE/DELETE 前 SELECT 旧数据。
// 解析失败或无法生成时返回 "-- " 注释行（执行时跳过）。
//
// 设计说明：
//   - SQL 字符串来自前端用户确认，可能被修改，因此无法依赖 dry-run 阶段的上下文
//   - 对 INSERT/REPLACE：解析 cols/vals 生成等价 DELETE
//   - 对 UPDATE/DELETE：先用 WHERE 子句 SELECT 旧数据，再生成 INSERT
//   - 正则只解析我们自己生成的格式（buildInsertStmt 等），格式可控
//   - 用 normalizeSQL 压缩空白，容忍多空格/换行，提高健壮性
func generateUndoSQL(stmt string, dbConn *sqlx.DB, dbType string) string {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return ""
	}
	normalized := normalizeSQL(stmt)
	upper := strings.ToUpper(normalized)

	switch {
	case strings.HasPrefix(upper, "INSERT") || strings.HasPrefix(upper, "REPLACE"):
		return undoForInsert(normalized, dbType)
	case strings.HasPrefix(upper, "UPDATE"):
		return undoForUpdateOrDelete(normalized, dbType, dbConn, "UPDATE")
	case strings.HasPrefix(upper, "DELETE"):
		return undoForUpdateOrDelete(normalized, dbType, dbConn, "DELETE")
	}
	return fmt.Sprintf("-- 无法自动生成撤销SQL: %s", truncate(stmt, 80))
}

// undoForInsert 解析 INSERT 语句，生成等价 DELETE（按所有列值定位插入行）。
// 使用统一正则 insertRe，同时支持 MySQL 反引号和 Oracle 双引号风格。
func undoForInsert(stmt, dbType string) string {
	m := insertRe.FindStringSubmatch(stmt)
	if m == nil {
		return fmt.Sprintf("-- 无法解析INSERT，需人工回滚: %s", truncate(stmt, 80))
	}
	// insertRe 捕获组布局：
	// [0]=full [1]=op
	// [2..4]=schema（反引号/双引号/普通，三选一）
	// [5..7]=table（反引号/双引号/普通，三选一）
	// [8]=cols [9]=vals
	schema := extractIdent(m, 2)
	table := extractIdent(m, 5)
	colsRaw := m[8]
	valsRaw := m[9]
	cols := splitTopLevel(colsRaw, ',')
	vals := splitSQLValues(valsRaw)
	if len(cols) == 0 || len(cols) != len(vals) {
		return fmt.Sprintf("-- INSERT列数与值数不匹配，需人工回滚: %s", truncate(stmt, 80))
	}

	qi := newQuoteInfo(dbType)
	var whereParts []string
	for i, col := range cols {
		col = strings.TrimSpace(col)
		col = strings.Trim(col, "`\"")
		whereParts = append(whereParts, fmt.Sprintf("%s%s%s = %s", qi.col, col, qi.colR, vals[i]))
	}
	whereClause := strings.Join(whereParts, " AND ")

	// 生成 DELETE，表名引用风格与 dbType 一致（schema/table 用相同引号）
	if schema != "" {
		return fmt.Sprintf("DELETE FROM %s%s%s.%s%s%s WHERE %s;",
			qi.col, schema, qi.colR, qi.col, table, qi.colR, whereClause)
	}
	return fmt.Sprintf("DELETE FROM %s%s%s WHERE %s;", qi.col, table, qi.colR, whereClause)
}

// undoForUpdateOrDelete 对 UPDATE/DELETE 先 SELECT 旧数据，再生成反向 INSERT。
// 使用统一正则 updateRe/deleteRe，同时支持 MySQL 和 Oracle 风格。
func undoForUpdateOrDelete(stmt, dbType string, dbConn *sqlx.DB, op string) string {
	var schema, table, where string
	if op == "UPDATE" {
		m := updateRe.FindStringSubmatch(stmt)
		if m == nil {
			return fmt.Sprintf("-- 无法解析%s，需人工回滚: %s", op, truncate(stmt, 80))
		}
		// updateRe 捕获组：[0]=full [1..3]=schema [4..6]=table [7]=set [8]=where
		schema = extractIdent(m, 1)
		table = extractIdent(m, 4)
		where = m[8]
	} else {
		m := deleteRe.FindStringSubmatch(stmt)
		if m == nil {
			return fmt.Sprintf("-- 无法解析%s，需人工回滚: %s", op, truncate(stmt, 80))
		}
		// deleteRe 捕获组：[0]=full [1..3]=schema [4..6]=table [7]=where
		schema = extractIdent(m, 1)
		table = extractIdent(m, 4)
		where = m[7]
	}

	if dbConn == nil {
		return fmt.Sprintf("-- 无数据库连接，无法生成%s撤销SQL: %s", op, truncate(stmt, 80))
	}

	// 查询旧数据。WHERE 子句已包含引号标识符，直接拼接。
	qi := newQuoteInfo(dbType)
	var selectSQL string
	if schema != "" {
		selectSQL = fmt.Sprintf("SELECT * FROM %s%s%s.%s%s%s WHERE %s",
			qi.col, schema, qi.colR, qi.col, table, qi.colR, where)
	} else {
		selectSQL = fmt.Sprintf("SELECT * FROM %s%s%s WHERE %s",
			qi.col, table, qi.colR, where)
	}
	rows, err := dbConn.Queryx(selectSQL)
	if err != nil {
		return fmt.Sprintf("-- 查询旧数据失败，需人工回滚: %s", truncate(err.Error(), 80))
	}
	defer rows.Close()
	oldRows, err := database.GetResultRows(dbType, rows)
	if err != nil || len(oldRows) == 0 {
		// 无旧数据：UPDATE/DELETE 实际未影响行，无需撤销
		return "-- 原语句未影响任何行，无需撤销"
	}

	var buf bytes.Buffer
	for _, row := range oldRows {
		cols := make([]string, 0, len(row))
		for k := range row {
			cols = append(cols, k)
		}
		sort.Strings(cols)
		colStrs := make([]string, len(cols))
		valStrs := make([]string, len(cols))
		for i, c := range cols {
			colStrs[i] = fmt.Sprintf("%s%s%s", qi.col, c, qi.colR)
			valStrs[i] = fmt.Sprintf("'%s'", escapeSQLValue(row[c]))
		}
		if schema != "" {
			buf.WriteString(fmt.Sprintf("INSERT INTO %s%s%s.%s%s%s (%s) VALUES (%s);\n",
				qi.col, schema, qi.colR, qi.col, table, qi.colR,
				strings.Join(colStrs, ", "), strings.Join(valStrs, ", ")))
		} else {
			buf.WriteString(fmt.Sprintf("INSERT INTO %s%s%s (%s) VALUES (%s);\n",
				qi.col, table, qi.colR,
				strings.Join(colStrs, ", "), strings.Join(valStrs, ", ")))
		}
	}
	return strings.TrimRight(buf.String(), "\n")
}

// splitTopLevel 按分隔符切分，忽略括号/引号内的分隔符。
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth := 0
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' {
			inQuote = !inQuote
		}
		if !inQuote {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
			} else if ch == sep && depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// splitSQLValues 切分 SQL VALUES 列表，正确处理单引号字符串与转义 ”。
func splitSQLValues(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' {
			if inQuote && i+1 < len(s) && s[i+1] == '\'' {
				cur.WriteString("''")
				i++
				continue
			}
			inQuote = !inQuote
			cur.WriteByte(ch)
			continue
		}
		if !inQuote && ch == ',' {
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	if cur.Len() > 0 {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}
	return parts
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// GetRollbackLog 返回指定会话的回滚日志（用于前端确认对话框预览）。
func GetRollbackLog(c *gin.Context) {
	sessionId := c.Query("sessionId")
	if sessionId == "" {
		response.WriteOK(c, map[string]any{"error": "sessionId 不能为空"})
		return
	}
	v, ok := rollbackStore.Load(sessionId)
	if !ok {
		response.WriteOK(c, map[string]any{"error": "回滚日志不存在或已过期（保留 30 分钟）"})
		return
	}
	log := v.(*RollbackLog)
	log.mu.Lock()
	defer log.mu.Unlock()
	response.WriteOK(c, map[string]any{
		"sessionId":    log.SessionId,
		"connId":       log.ConnId,
		"schema":       log.Schema,
		"undoCount":    len(log.UndoSQLs),
		"undoSQLs":     log.UndoSQLs,
		"originalSQLs": log.OriginalSQLs,
		"createdAt":    log.CreatedAt.Format("2006-01-02 15:04:05"),
		"expiresIn":    int((rollbackLogTTL - time.Since(log.CreatedAt)).Seconds()),
	})
}

// RollbackSync 执行指定会话的回滚：按逆序执行撤销 SQL。
// 需要对目标连接有写权限。
func RollbackSync(c *gin.Context) {
	sessionId := c.PostForm("sessionId")
	if sessionId == "" {
		response.WriteOK(c, map[string]any{"success": false, "message": "sessionId 不能为空"})
		return
	}
	v, ok := rollbackStore.Load(sessionId)
	if !ok {
		response.WriteOK(c, map[string]any{"success": false, "message": "回滚日志不存在或已过期"})
		return
	}
	log := v.(*RollbackLog)

	authorization := appctx.Ctx.GetAuthorization(c)
	dbConn := conn.GetConn(log.ConnId, authorization)
	if dbConn == nil {
		response.WriteOK(c, map[string]any{"success": false, "message": "目标数据库连接不可用"})
		return
	}

	// 权限校验：回滚等同于写操作
	if !config.IsLocalMode() && !permission.CheckUserCanModify(authorization) {
		response.WriteOK(c, map[string]any{"success": false, "message": "当前角色禁止修改数据，无法执行回滚"})
		return
	}

	log.mu.Lock()
	undoSQLs := make([]string, len(log.UndoSQLs))
	copy(undoSQLs, log.UndoSQLs)
	log.mu.Unlock()

	// 逆序执行撤销 SQL
	tx, err := dbConn.Beginx()
	if err != nil {
		response.WriteOK(c, map[string]any{"success": false, "message": fmt.Sprintf("开启事务失败: %v", err)})
		return
	}

	executed := 0
	errors := make([]string, 0)
	for i := len(undoSQLs) - 1; i >= 0; i-- {
		stmt := strings.TrimSpace(undoSQLs[i])
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			// 跳过注释/空行
			continue
		}
		// 撤销 SQL 为 INSERT/DELETE/UPDATE，通过 sqlguard 校验
		if err := validateDataSQL(stmt); err != nil {
			errors = append(errors, fmt.Sprintf("校验失败: %s - %s", truncate(stmt, 60), err.Error()))
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			errors = append(errors, fmt.Sprintf("执行失败: %s - %s", truncate(stmt, 60), err.Error()))
		} else {
			executed++
		}
	}

	if len(errors) > 0 {
		tx.Rollback()
	} else {
		tx.Commit()
	}

	// 审计
	userVal, _ := c.Get("currentUser")
	user, _ := userVal.(*admin.User)
	if user != nil {
		recordRollbackAudit(c, sessionId, log, executed, len(errors), user)
	}

	if len(errors) > 0 {
		response.WriteOK(c, map[string]any{
			"success":    false,
			"message":    fmt.Sprintf("回滚完成但存在 %d 条错误", len(errors)),
			"executed":   executed,
			"errorCount": len(errors),
			"errors":     errors,
		})
		return
	}

	// 回滚成功后清理日志
	rollbackStore.Delete(sessionId)
	response.WriteOK(c, map[string]any{
		"success":  true,
		"message":  fmt.Sprintf("回滚成功，执行 %d 条撤销语句", executed),
		"executed": executed,
	})
}

// ===== 报告导出 =====
//
// ExportSyncReport 接收前端提交的同步结果 JSON，渲染为 HTML（含 Mermaid 流程图）
// 或 CSV，写入 exports/ 目录并返回下载链接。

// SyncReportInput 报告输入。
type SyncReportInput struct {
	Format           string            `json:"format"` // html | csv
	SyncMode         string            `json:"syncMode"`
	Direction        string            `json:"direction"`
	ConflictStrategy string            `json:"conflictStrategy"`
	Source           ReportEndpoint    `json:"source"`
	Target           ReportEndpoint    `json:"target"`
	Table            string            `json:"table"`
	Results          []ReportTableStat `json:"results"`
	Errors           []string          `json:"errors"`
	StartedAt        string            `json:"startedAt"`
	DurationMs       int64             `json:"durationMs"`
	DryRun           bool              `json:"dryRun"`
}

// ReportEndpoint 报告中的连接端点信息。
type ReportEndpoint struct {
	ConnId   string `json:"connId"`
	ConnName string `json:"connName"`
	Schema   string `json:"schema"`
}

// ReportTableStat 单表同步结果统计。
type ReportTableStat struct {
	TableName string `json:"tableName"`
	Insert    int    `json:"insert"`
	Update    int    `json:"update"`
	Delete    int    `json:"delete"`
	Failed    int    `json:"failed"`
}

// ExportSyncReport 处理报告导出请求。
func ExportSyncReport(c *gin.Context) {
	var input SyncReportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.WriteOK(c, map[string]any{"error": "请求参数解析失败: " + err.Error()})
		return
	}
	if input.Format == "" {
		input.Format = "html"
	}

	// 确保 exports 目录存在
	if err := os.MkdirAll("exports", 0755); err != nil {
		response.WriteOK(c, map[string]any{"error": "创建导出目录失败: " + err.Error()})
		return
	}

	stamp := time.Now().Format("20060102_150405")
	var filename, content string
	var err error
	if input.Format == "csv" {
		filename = fmt.Sprintf("syncreport_%s.csv", stamp)
		content, err = renderReportCSV(&input)
	} else {
		filename = fmt.Sprintf("syncreport_%s.html", stamp)
		content = renderReportHTML(&input)
	}
	if err != nil {
		response.WriteOK(c, map[string]any{"error": "生成报告失败: " + err.Error()})
		return
	}

	filePath := filepath.Join("exports", filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		response.WriteOK(c, map[string]any{"error": "写入报告文件失败: " + err.Error()})
		return
	}

	// 返回可经 /exports/<filename> 下载的链接
	response.WriteOK(c, map[string]any{
		"filename": filename,
		"url":      "/exports/" + filename,
		"format":   input.Format,
	})
}

// renderReportHTML 生成包含 Mermaid 流程图的 HTML 报告。
func renderReportHTML(input *SyncReportInput) string {
	var buf bytes.Buffer
	buf.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8">`)
	buf.WriteString(fmt.Sprintf(`<title>数据同步报告 - %s</title>`, input.Table))
	buf.WriteString(`<style>
		*{margin:0;padding:0;box-sizing:border-box}
		body{font-family:'Microsoft YaHei','PingFang SC',Arial,sans-serif;color:#2c3e50;background:#f8f9fa;padding:30px 40px;line-height:1.6}
		h1{font-size:24px;margin-bottom:8px}
		.meta{display:flex;gap:24px;flex-wrap:wrap;padding:14px 20px;background:#fff;border-radius:8px;margin-bottom:20px;box-shadow:0 1px 4px rgba(0,0,0,0.06);font-size:13px;color:#7f8c8d}
		.meta strong{color:#2c3e50}
		.section{background:#fff;border-radius:8px;padding:20px 25px;margin-bottom:20px;box-shadow:0 1px 4px rgba(0,0,0,0.06)}
		.section h2{font-size:16px;margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid #ecf0f1}
		.mermaid{background:#fafbfc;padding:15px;border-radius:6px;text-align:center;margin:10px 0}
		table{width:100%;border-collapse:collapse;font-size:13px;margin-top:8px}
		th{padding:10px 12px;text-align:left;background:#f7f9fc;border-bottom:2px solid #dce4ec;font-weight:600}
		td{padding:9px 12px;border-bottom:1px solid #f0f3f5}
		tr:nth-child(even){background:#fafbfc}
		.tag{display:inline-block;padding:2px 8px;border-radius:3px;font-size:11px;font-weight:600}
		.tag-ins{background:#d4edda;color:#155724}
		.tag-upd{background:#fff3cd;color:#856404}
		.tag-del{background:#f8d7da;color:#721c24}
		.tag-fail{background:#e2e3e5;color:#383d41}
		.errors{max-height:300px;overflow:auto;background:#1e1e1e;color:#d4d4d4;padding:12px;border-radius:6px;font-family:Consolas,monospace;font-size:12px;white-space:pre-wrap}
		.footer{text-align:center;padding:15px 0;margin-top:20px;border-top:1px solid #ecf0f1;color:#95a5a6;font-size:11px}
	</style>`)
	// 引入 Mermaid CDN，页面加载后自动渲染 .mermaid 块
	buf.WriteString(`<script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>`)
	buf.WriteString(`<script>mermaid.initialize({startOnLoad:true,theme:'default'});</script>`)
	buf.WriteString(`</head><body>`)

	buf.WriteString(fmt.Sprintf(`<h1>数据同步报告</h1>`))
	modeLabel := "数据同步"
	if input.SyncMode == "structure" {
		modeLabel = "结构同步"
	}
	if input.DryRun {
		modeLabel += "（Dry-Run 试运行）"
	}
	direction := "源 → 目标"
	if input.Direction == "target_to_source" {
		direction = "目标 → 源"
	}
	buf.WriteString(fmt.Sprintf(`<div class="meta">
		<span><strong>类型：</strong>%s</span>
		<span><strong>方向：</strong>%s</span>
		<span><strong>冲突策略：</strong>%s</span>
		<span><strong>开始时间：</strong>%s</span>
		<span><strong>耗时：</strong>%d ms</span>
		<span><strong>源：</strong>%s / %s</span>
		<span><strong>目标：</strong>%s / %s</span>
	</div>`, modeLabel, direction, input.ConflictStrategy, input.StartedAt, input.DurationMs,
		input.Source.ConnName, input.Source.Schema,
		input.Target.ConnName, input.Target.Schema))

	// Mermaid 流程图
	buf.WriteString(`<div class="section"><h2>同步流程</h2>`)
	buf.WriteString(`<div class="mermaid">`)
	buf.WriteString("flowchart LR\n")
	buf.WriteString(fmt.Sprintf("    S[\"源数据库<br/>%s/%s\"]\n", input.Source.ConnName, input.Source.Schema))
	if input.DryRun {
		buf.WriteString("    D[\"Dry-Run 比对<br/>生成示例 SQL\"]\n")
		buf.WriteString("    R[\"试运行报告<br/>不实际写入\"]\n")
		buf.WriteString("    S --> D --> R\n")
	} else {
		buf.WriteString("    C[\"差异比对<br/>分块计算\"]\n")
		buf.WriteString("    A[\"执行同步<br/>事务批量写入\"]\n")
		buf.WriteString(fmt.Sprintf("    T[\"目标数据库<br/>%s/%s\"]\n", input.Target.ConnName, input.Target.Schema))
		buf.WriteString("    S --> C --> A --> T\n")
	}
	buf.WriteString(`</div></div>`)

	// 各表结果
	totalIns, totalUpd, totalDel, totalFail := 0, 0, 0, 0
	buf.WriteString(`<div class="section"><h2>同步结果</h2>`)
	buf.WriteString(`<table><thead><tr><th>表名</th><th>新增</th><th>更新</th><th>删除</th><th>失败</th></tr></thead><tbody>`)
	for _, r := range input.Results {
		buf.WriteString(fmt.Sprintf(`<tr><td>%s</td><td><span class="tag tag-ins">%d</span></td><td><span class="tag tag-upd">%d</span></td><td><span class="tag tag-del">%d</span></td><td><span class="tag tag-fail">%d</span></td></tr>`,
			r.TableName, r.Insert, r.Update, r.Delete, r.Failed))
		totalIns += r.Insert
		totalUpd += r.Update
		totalDel += r.Delete
		totalFail += r.Failed
	}
	buf.WriteString(fmt.Sprintf(`<tr style="font-weight:bold;background:#f0f7ff"><td>合计</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td></tr>`, totalIns, totalUpd, totalDel, totalFail))
	buf.WriteString(`</tbody></table></div>`)

	// 错误详情
	if len(input.Errors) > 0 {
		buf.WriteString(`<div class="section"><h2>错误详情（共 `)
		buf.WriteString(fmt.Sprintf("%d", len(input.Errors)))
		buf.WriteString(` 条）</h2><div class="errors">`)
		buf.WriteString(escapeHTML(strings.Join(input.Errors, "\n")))
		buf.WriteString(`</div></div>`)
	}

	buf.WriteString(fmt.Sprintf(`<div class="footer">由 WebSQL 数据同步工具生成 · %s</div>`, time.Now().Format("2006-01-02 15:04:05")))
	buf.WriteString(`</body></html>`)
	return buf.String()
}

// renderReportCSV 生成 CSV 格式的报告。
func renderReportCSV(input *SyncReportInput) (string, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	// 元信息
	_ = w.Write([]string{"字段", "值"})
	_ = w.Write([]string{"同步类型", input.SyncMode})
	_ = w.Write([]string{"同步方向", input.Direction})
	_ = w.Write([]string{"冲突策略", input.ConflictStrategy})
	_ = w.Write([]string{"开始时间", input.StartedAt})
	_ = w.Write([]string{"耗时(ms)", fmt.Sprintf("%d", input.DurationMs)})
	_ = w.Write([]string{"源连接", input.Source.ConnName})
	_ = w.Write([]string{"源Schema", input.Source.Schema})
	_ = w.Write([]string{"目标连接", input.Target.ConnName})
	_ = w.Write([]string{"目标Schema", input.Target.Schema})
	_ = w.Write([]string{""})
	// 表结果
	_ = w.Write([]string{"表名", "新增", "更新", "删除", "失败"})
	totalIns, totalUpd, totalDel, totalFail := 0, 0, 0, 0
	for _, r := range input.Results {
		_ = w.Write([]string{r.TableName, fmt.Sprintf("%d", r.Insert), fmt.Sprintf("%d", r.Update), fmt.Sprintf("%d", r.Delete), fmt.Sprintf("%d", r.Failed)})
		totalIns += r.Insert
		totalUpd += r.Update
		totalDel += r.Delete
		totalFail += r.Failed
	}
	_ = w.Write([]string{"合计", fmt.Sprintf("%d", totalIns), fmt.Sprintf("%d", totalUpd), fmt.Sprintf("%d", totalDel), fmt.Sprintf("%d", totalFail)})
	_ = w.Write([]string{""})
	// 错误
	if len(input.Errors) > 0 {
		_ = w.Write([]string{"错误详情"})
		for _, e := range input.Errors {
			_ = w.Write([]string{e})
		}
	}
	w.Flush()
	return buf.String(), w.Error()
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// recordRollbackAudit 记录回滚操作的审计日志。
func recordRollbackAudit(c *gin.Context, sessionId string, log *RollbackLog, executed, errCount int, user *admin.User) {
	status := "success"
	if errCount > 0 {
		status = "failed"
	}
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:       "datasync-rollback",
		SQLText:      fmt.Sprintf("[DataSyncRollback] session=%s undo=%d executed=%d errors=%d", sessionId, len(log.UndoSQLs), executed, errCount),
		SQLType:      "SYNC",
		RiskLevel:    "high",
		Status:       status,
		ConnID:       log.ConnId,
		SchemaName:   log.Schema,
		UserID:       user.Id,
		UserName:     user.Name,
		ClientIP:     c.ClientIP(),
		AffectedRows: executed,
	})
}
