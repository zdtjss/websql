package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"websql/internal/app/conn"
	"websql/internal/database"
	"websql/internal/dialect"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/safego"
	"websql/internal/pkg/sanitize"
	"websql/internal/pkg/sqlguard"

	"github.com/jmoiron/sqlx"
)

// BackupService 封装备份相关的业务逻辑：文件 I/O、外部数据库查询、加密等
type BackupService struct {
	repo BackupRepo
}

// NewBackupService 创建 BackupService 实例
func NewBackupService(repo BackupRepo) *BackupService {
	return &BackupService{repo: repo}
}

// 默认实例，保持对包级别函数的向后兼容
// 延迟初始化：database.Mngtdb 在 InitMngtDbConn() 之后才可用，
// 包级变量初始化时 Mngtdb 仍为 nil，因此必须 lazy init。
var (
	defaultBackupRepo    BackupRepo
	defaultBackupService *BackupService
	defaultBackupOnce    sync.Once
)

// ensureDefaultBackup 初始化默认的 BackupRepo 和 BackupService
func ensureDefaultBackup() {
	defaultBackupOnce.Do(func() {
		defaultBackupRepo = NewBackupRepo(database.Mngtdb)
		defaultBackupService = NewBackupService(defaultBackupRepo)
	})
}

// CreateBackup 创建数据库备份（同步版本，保留以兼容旧调用）。
// 新的 HTTP 入口请使用 CreateBackupAsync，以支持进度轮询。
func (s *BackupService) CreateBackup(connId, schema, name, description, tablesStr, withData, encrypt, authorization string) (map[string]any, error) {
	return s.createBackupInternal("", connId, schema, name, description, tablesStr, withData, encrypt, authorization)
}

// CreateBackupAsync 异步创建数据库备份，立即返回 taskId，实际备份在后台 goroutine 中执行。
// 调用方可通过 GetBackupProgress(taskId) 轮询进度。
// 进度数据在任务完成或失败后 30 秒自动清理。
func (s *BackupService) CreateBackupAsync(connId, schema, name, description, tablesStr, withData, encrypt, authorization string) string {
	taskId := idgen.RandomStr()

	// 先写入初始进度，确保前端首次轮询能拿到数据
	SetBackupProgress(taskId, BackupProgress{
		TaskId:    taskId,
		ConnId:    connId,
		Schema:    schema,
		Status:    "running",
		StartedAt: time.Now().UnixMilli(),
	})

	safego.GoWithName("backup-"+taskId, func() {
		result, err := s.createBackupInternal(taskId, connId, schema, name, description, tablesStr, withData, encrypt, authorization)

		now := time.Now().UnixMilli()
		// 读取当前进度，保留 StartedAt 和已统计的表数/字节数等字段
		cur, _ := FetchBackupProgress(taskId)
		cur.TaskId = taskId
		cur.ConnId = connId
		cur.Schema = schema
		cur.FinishedAt = now
		if err != nil {
			// 失败：记录错误并标记结束
			cur.Status = "failed"
			cur.Error = err.Error()
		} else {
			// 成功：把最终结果一并写入进度，前端轮询可直接拿到
			cur.Status = "completed"
			cur.Result = result
			cur.Error = ""
		}
		SetBackupProgress(taskId, cur)
		// 30 秒后自动清理进度数据，避免内存泄漏
		scheduleProgressCleanup(taskId, 30*time.Second)
	})

	return taskId
}

// createBackupInternal 执行实际的备份逻辑。
// taskId 为空时表示同步调用（不更新进度）；非空时会在遍历表过程中实时更新进度。
func (s *BackupService) createBackupInternal(taskId, connId, schema, name, description, tablesStr, withData, encrypt, authorization string) (map[string]any, error) {
	s.repo.EnsureBackupTable()

	dbConn := conn.GetConn(connId, authorization)
	dbType := dbConn.DriverName()

	if name == "" {
		name = fmt.Sprintf("%s_%s", schema, time.Now().Format("20060102150405"))
	}

	// 获取所有表及类型，构建类型查找表（用于区分表和视图）
	allTableInfos := getAllTables(dbConn, dbType, schema)
	typeMap := make(map[string]string, len(allTableInfos))
	for _, ti := range allTableInfos {
		typeMap[ti.Name] = ti.Type
	}

	var tables []string
	if tablesStr != "" {
		tables = strings.Split(tablesStr, ",")
	} else {
		for _, ti := range allTableInfos {
			tables = append(tables, ti.Name)
		}
	}

	// 备份类型：未指定表或选中全部表为 full，部分表为 partial
	backupType := "full"
	if tablesStr != "" && len(tables) < len(allTableInfos) {
		backupType = "partial"
	}

	// 初始化进度：已知总表数，已处理 0
	if taskId != "" {
		if cur, ok := FetchBackupProgress(taskId); ok {
			cur.TotalTables = len(tables)
			SetBackupProgress(taskId, cur)
		}
	}

	backupDir := filepath.Join("backups", connId, schema)
	os.MkdirAll(backupDir, 0755)

	filePath := filepath.Join(backupDir, name+".sql")
	file, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("创建备份文件失败: %s", err.Error())
	}
	defer file.Close()

	file.WriteString(fmt.Sprintf("-- WebSQL Backup v2\n"))
	file.WriteString(fmt.Sprintf("-- Database: %s\n", schema))
	file.WriteString(fmt.Sprintf("-- Created: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	file.WriteString(fmt.Sprintf("-- Tables: %d\n\n", len(tables)))

	totalSize := int64(0)
	successCount := 0
	errors := make([]string, 0)

	for _, table := range tables {
		table = strings.TrimSpace(table)
		if table == "" {
			continue
		}

		// 更新进度：标记当前正在处理的表
		if taskId != "" {
			if cur, ok := FetchBackupProgress(taskId); ok {
				cur.CurrentTable = table
				SetBackupProgress(taskId, cur)
			}
		}

		ddl := getCreateDDL(dbConn, dbType, schema, table)
		tblType := typeMap[table]
		isView := isViewType(tblType)
		if isView {
			ddl = getViewDDL(dbConn, dbType, schema, table)
		}
		chunk := fmt.Sprintf("\n-- ----------------------------\n")
		if isView {
			chunk += fmt.Sprintf("-- View structure for `%s`\n", table)
		} else {
			chunk += fmt.Sprintf("-- Table structure for `%s`\n", table)
		}
		chunk += fmt.Sprintf("-- ----------------------------\n")
		if isView {
			chunk += fmt.Sprintf("DROP VIEW IF EXISTS `%s`;\n", table)
		} else {
			chunk += fmt.Sprintf("DROP TABLE IF EXISTS `%s`;\n", table)
		}
		chunk += ddl + ";\n"
		file.WriteString(chunk)
		totalSize += int64(len(chunk))

		// 视图不导出数据（视图数据来自底层表，备份视图定义即可）
		if withData == "true" && !isView {
			data, _, rowCount, err := exportTableData(dbConn, dbType, schema, table)
			if err != nil {
				errors = append(errors, fmt.Sprintf("导出 %s 数据失败: %s", table, err.Error()))
				continue
			}
			if rowCount > 0 {
				file.WriteString(fmt.Sprintf("\n-- ----------------------------\n"))
				file.WriteString(fmt.Sprintf("-- Data for `%s` (%d rows)\n", table, rowCount))
				file.WriteString(fmt.Sprintf("-- ----------------------------\n"))
				file.WriteString(data)
				totalSize += int64(len(data))
			}
		}
		successCount++

		// 更新进度：已完成表数 + 累计字节数
		if taskId != "" {
			if cur, ok := FetchBackupProgress(taskId); ok {
				cur.ProcessedTables = successCount
				cur.ExportedBytes = totalSize
				SetBackupProgress(taskId, cur)
			}
		}
	}

	file.WriteString(fmt.Sprintf("\n-- Backup completed: %d tables, %d rows\n", successCount, totalSize))

	var content []byte
	readContent, readErr := os.ReadFile(filePath)
	if readErr != nil {
		content = make([]byte, 0)
	} else {
		content = readContent
		limit := 1000000
		if len(content) > limit {
			content = content[:limit]
		}
	}

	if encrypt == "true" {
		encryptedContent := crypto.AESEncode(string(content))
		err3 := os.WriteFile(filePath, []byte(encryptedContent), 0644)
		if err3 != nil {
			return nil, fmt.Errorf("加密备份文件失败: %s", err3.Error())
		}
	}

	id := idgen.RandomStr()
	record := &BackupRecord{
		Id:          id,
		Name:        name,
		ConnId:      connId,
		Schema:      schema,
		DbType:      dbType,
		Size:        totalSize,
		Type:        backupType,
		Encrypted:   encrypt == "true",
		CreatedAt:   time.Now().Format("2006-01-02 15:04:05"),
		Description: description,
		Status:      "completed",
		FilePath:    filePath,
	}
	err4 := s.repo.InsertBackupRecord(record)
	if err4 != nil {
		logger.PrintErrf("保存备份记录失败", err4)
		s.repo.InsertBackupRecordShort(record)
	}

	return map[string]any{
		"success":      len(errors) == 0,
		"id":           id,
		"name":         name,
		"size":         totalSize,
		"tables":       successCount,
		"errors":       errors,
		"encrypted":    encrypt == "true",
		"errorMessage": "",
	}, nil
}

// ListBackups 查询备份列表
func (s *BackupService) ListBackups(connId, schema string) (map[string]any, error) {
	s.repo.EnsureBackupTable()

	records, err := s.repo.FindBackups(connId, schema)
	if err != nil {
		return nil, fmt.Errorf("获取备份列表失败: %s", err.Error())
	}

	return map[string]any{
		"records": records,
		"total":   len(records),
	}, nil
}

// RestoreBackup 从备份恢复数据库
func (s *BackupService) RestoreBackup(backupId, connId, authorization string) (map[string]any, error) {
	s.repo.EnsureBackupTable()

	dbConn := conn.GetConn(connId, authorization)

	record, err := s.repo.FindBackupById(backupId)
	if err != nil {
		return nil, fmt.Errorf("未找到备份记录")
	}

	content, err := os.ReadFile(record.FilePath)
	if err != nil && record.Encrypted {
		return map[string]any{"success": false, "message": "备份文件不存在"}, nil
	}

	sqlContent := string(content)
	if record.Encrypted {
		sqlContent = crypto.AESDecode(sqlContent)
	}

	statements := splitBackupSQL(sqlContent)
	executed := 0
	failed := make([]string, 0)

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		// DDL 安全校验
		if err := sqlguard.ValidateDDL(stmt); err != nil {
			errMsg := fmt.Sprintf("安全校验失败 [%.100s]: %s", stmt, err.Error())
			failed = append(failed, errMsg)
			logger.PrintErrf(errMsg, nil)
			continue
		}
		_, err := dbConn.Exec(stmt)
		if err != nil {
			errMsg := fmt.Sprintf("执行失败 [%.100s]: %s", stmt, err.Error())
			failed = append(failed, errMsg)
			logger.PrintErrf(errMsg, nil)
		} else {
			executed++
		}
	}

	return map[string]any{
		"success":     len(failed) == 0,
		"executed":    executed,
		"failed":      failed,
		"failedCount": len(failed),
	}, nil
}

// DeleteBackup 删除备份记录及文件
func (s *BackupService) DeleteBackup(backupId string) error {
	s.repo.EnsureBackupTable()

	record, err := s.repo.FindBackupById(backupId)
	if err == nil && record.FilePath != "" {
		os.Remove(record.FilePath)
	}

	err = s.repo.DeleteBackupRecord(backupId)
	if err != nil {
		return fmt.Errorf("删除备份记录失败: %s", err.Error())
	}
	return nil
}

// GetBackupTables 获取可备份的表列表
func (s *BackupService) GetBackupTables(connId, schema, authorization string) (map[string]any, error) {
	dbConn := conn.GetConn(connId, authorization)
	dbType := dbConn.DriverName()

	allTables := getAllTables(dbConn, dbType, schema)
	tables := make([]BackupTables, 0)
	for _, t := range allTables {
		tables = append(tables, BackupTables{Table: t.Name, Type: t.Type, Checked: true})
	}

	var tableCounts []map[string]any
	for _, t := range allTables {
		var count int
		if sanitize.IsValidIdentifier(schema) && sanitize.IsValidIdentifier(t.Name) {
			dbConn.Get(&count, fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", schema, t.Name))
		}
		tableCounts = append(tableCounts, map[string]any{
			"table": t.Name,
			"rows":  count,
		})
	}

	return map[string]any{
		"tables":      tables,
		"tableCounts": tableCounts,
	}, nil
}

// DownloadBackup 把备份内容写入 writer。
// 返回值 fileName 是建议的下载文件名 (含 .sql 后缀)，由调用方设置 HTTP 头或 BlobResult.Filename。
func (s *BackupService) DownloadBackup(backupId string, w io.Writer) (fileName string, err error) {
	s.repo.EnsureBackupTable()

	record, err := s.repo.FindBackupById(backupId)
	if err != nil {
		return "", fmt.Errorf("备份不存在")
	}

	content, err1 := os.ReadFile(record.FilePath)
	if err1 != nil {
		return "", fmt.Errorf("备份文件不存在")
	}

	if record.Encrypted {
		content = []byte(crypto.AESDecode(string(content)))
	}

	fileName = record.Name + ".sql"
	if _, err := w.Write(content); err != nil {
		return "", fmt.Errorf("写入输出流失败: %w", err)
	}
	return fileName, nil
}

// ===== 以下为外部数据库查询辅助函数 =====

// getAllTables 返回 schema 下所有表和视图的名称及类型（"table" 或 "view"）
func getAllTables(conn *sqlx.DB, dbType, schema string) []TableInfo {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listTable"]
	if !ok {
		sqlTmpl = "SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema = ? order by TABLE_NAME"
	}
	result := make([]TableInfo, 0)
	switch dbType {
	case "oracle":
		rows, err := conn.Query(sqlTmpl, "notexists")
		if err != nil {
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var tableName, tableType, tableComment string
			rows.Scan(&tableName, &tableType, &tableComment)
			result = append(result, TableInfo{
				Name: strings.TrimSpace(tableName),
				Type: normalizeTableType(tableType),
			})
		}
	default:
		rows, err := conn.Query(sqlTmpl, schema)
		if err != nil {
			return result
		}
		defer rows.Close()
		for rows.Next() {
			var tableName, tableType, tableComment string
			rows.Scan(&tableName, &tableType, &tableComment)
			result = append(result, TableInfo{
				Name: strings.TrimSpace(tableName),
				Type: normalizeTableType(tableType),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// normalizeTableType 将不同数据库返回的表类型统一为 "table" 或 "view"
func normalizeTableType(raw string) string {
	t := strings.ToUpper(strings.TrimSpace(raw))
	if t == "VIEW" {
		return "view"
	}
	return "table"
}

// isViewType 判断类型是否为视图
func isViewType(t string) bool {
	return t == "view"
}

func getCreateDDL(conn *sqlx.DB, dbType, schema, table string) string {
	if !sanitize.IsValidIdentifier(table) {
		return ""
	}
	// 尝试 SHOW CREATE TABLE，MySQL 返回两列: Table, Create Table
	var tableName, createDDL string
	row := conn.QueryRow(fmt.Sprintf("SHOW CREATE TABLE `%s`", table))
	if err := row.Scan(&tableName, &createDDL); err == nil && createDDL != "" {
		logger.PrintErrf("[backup] SHOW CREATE TABLE 成功: %s", nil, table)
		return createDDL
	} else {
		logger.PrintErrf("[backup] SHOW CREATE TABLE 失败: %s, err=%v, 回退到 describeTable", nil, table, err)
	}
	// 回退到 information_schema / DESCRIBE
	cols := describeTable(conn, dbType, schema, table)
	logger.PrintErrf("[backup] describeTable 返回 %d 列, table=%s", nil, len(cols), table)
	return generateDDLStmt(table, cols)
}

// getViewDDL 获取视图定义语句，适配不同数据库方言
func getViewDDL(conn *sqlx.DB, dbType, schema, view string) string {
	if !sanitize.IsValidIdentifier(view) {
		return ""
	}
	switch dbType {
	case "sqlite":
		var ddl strScanner
		row := conn.QueryRow("SELECT sql FROM sqlite_master WHERE type = 'view' AND name = ?", view)
		if err := row.Scan(&ddl); err == nil && ddl.Val != "" {
			return ddl.Val
		}
		return ""
	case "oracle":
		var ddl strScanner
		row := conn.QueryRow("SELECT TEXT FROM USER_VIEWS WHERE VIEW_NAME = :1", strings.ToUpper(view))
		if err := row.Scan(&ddl); err == nil && ddl.Val != "" {
			return ddl.Val
		}
		return ""
	default: // mysql, mariadb — SHOW CREATE VIEW 返回 4 列: View, Create View, character_set_client, collation_connection
		var viewName, createDDL, charset, collation strScanner
		row := conn.QueryRow(fmt.Sprintf("SHOW CREATE VIEW `%s`", view))
		if err := row.Scan(&viewName, &createDDL, &charset, &collation); err == nil && createDDL.Val != "" {
			return createDDL.Val
		}
		return ""
	}
}

// describeTable 获取表列信息，优先用 information_schema（强类型 string 扫描），避免 []byte 输出
func describeTable(conn *sqlx.DB, dbType, schema, table string) []map[string]string {
	// MariaDB 与 MySQL 一样拥有 information_schema，复用同一查询路径
	if dbType == "mysql" || dbType == "mariadb" {
		cols := describeTableFromInfoSchema(conn, schema, table)
		if len(cols) > 0 {
			return cols
		}
	}
	return describeTableFallback(conn, table)
}

// describeTableFromInfoSchema 通过 information_schema 获取列信息，用 strScanner 确保转为 string
func describeTableFromInfoSchema(conn *sqlx.DB, schema, table string) []map[string]string {
	sql := `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, 
			COALESCE(COLUMN_DEFAULT,''), COALESCE(EXTRA,''), COLUMN_KEY
		FROM information_schema.COLUMNS 
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`
	rows, err := conn.Queryx(sql, schema, table)
	if err != nil {
		return nil
	}
	defer rows.Close()

	cols := make([]map[string]string, 0)
	for rows.Next() {
		var field, typ, nullable, defVal, extra, key strScanner
		if err := rows.Scan(&field, &typ, &nullable, &defVal, &extra, &key); err != nil {
			continue
		}
		cols = append(cols, map[string]string{
			"Field":   field.Val,
			"Type":    typ.Val,
			"Null":    nullable.Val,
			"Default": defVal.Val,
			"Extra":   extra.Val,
			"Key":     key.Val,
		})
	}
	return cols
}

// describeTableFallback DESCRIBE 回退方案，使用 sql.NullString 强制驱动转为 string
func describeTableFallback(conn *sqlx.DB, table string) []map[string]string {
	sql := fmt.Sprintf("DESCRIBE `%s`", table)
	rows, err := conn.Queryx(sql)
	if err != nil {
		return nil
	}
	defer rows.Close()

	// DESCRIBE 固定返回 6 列: Field, Type, Null, Key, Default, Extra
	cols := make([]map[string]string, 0)
	for rows.Next() {
		var f, t, n, k, d, e strScanner
		if err := rows.Scan(&f, &t, &n, &k, &d, &e); err != nil {
			continue
		}
		cols = append(cols, map[string]string{
			"Field":   f.Val,
			"Type":    t.Val,
			"Null":    n.Val,
			"Key":     k.Val,
			"Default": d.Val,
			"Extra":   e.Val,
		})
	}
	return cols
}

func generateDDLStmt(table string, cols []map[string]string) string {
	if len(cols) == 0 {
		return fmt.Sprintf("-- Unable to get DDL for table `%s`", table)
	}
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("CREATE TABLE `%s` (\n", table))
	for i, col := range cols {
		field := col["Field"]
		typ := col["Type"]
		nullVal := col["Null"]
		defaultVal := col["Default"]
		extra := col["Extra"]
		null := "NOT NULL"
		if nullVal == "YES" {
			null = "NULL"
		}
		line := fmt.Sprintf("  `%s` %s %s", field, typ, null)
		if defaultVal != "" {
			line += fmt.Sprintf(" DEFAULT '%s'", defaultVal)
		}
		if extra != "" {
			line += " " + extra
		}
		if i < len(cols)-1 {
			line += ",\n"
		} else {
			line += "\n"
		}
		buf.WriteString(line)
	}
	pkFields := make([]string, 0)
	for _, col := range cols {
		if col["Key"] == "PRI" {
			pkFields = append(pkFields, fmt.Sprintf("`%s`", col["Field"]))
		}
	}
	if len(pkFields) > 0 {
		buf.WriteString(fmt.Sprintf("  ,PRIMARY KEY (%s)\n", strings.Join(pkFields, ",")))
	}
	buf.WriteString(");")
	return buf.String()
}

func exportTableData(conn *sqlx.DB, dbType, schema, table string) (string, []string, int, error) {
	if !sanitize.IsValidIdentifier(schema) || !sanitize.IsValidIdentifier(table) {
		return "", nil, 0, fmt.Errorf("非法的表名或 schema 名")
	}
	sql := fmt.Sprintf("SELECT * FROM `%s`.`%s`", schema, table)
	rows, err := conn.Queryx(sql)
	if err != nil {
		return "", nil, 0, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", nil, 0, err
	}

	colCount := len(columns)
	var buf strings.Builder
	rowCount := 0
	for rows.Next() {
		// 用 strScanner 确保所有值都转为 string，避免 []byte 输出
		scanners := make([]strScanner, colCount)
		scanPtrs := make([]any, colCount)
		for i := range scanners {
			scanPtrs[i] = &scanners[i]
		}
		if err := rows.Scan(scanPtrs...); err != nil {
			continue
		}

		colNames := make([]string, colCount)
		colValues := make([]string, colCount)
		for i, col := range columns {
			colNames[i] = fmt.Sprintf("`%s`", col)
			if scanners[i].Val == "" && !scanners[i].HasVal {
				colValues[i] = "NULL"
			} else {
				valStr := strings.ReplaceAll(scanners[i].Val, "\\", "\\\\")
				valStr = strings.ReplaceAll(valStr, "'", "\\'")
				colValues[i] = fmt.Sprintf("'%s'", valStr)
			}
		}
		buf.WriteString(fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s);\n",
			table, strings.Join(colNames, ", "), strings.Join(colValues, ", ")))
		rowCount++
	}
	return buf.String(), columns, rowCount, nil
}

func splitBackupSQL(content string) []string {
	stmts := make([]string, 0)
	current := &strings.Builder{}
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			continue
		}
		current.WriteString(trimmed)
		current.WriteString(" ")
		if strings.HasSuffix(trimmed, ";") {
			stmts = append(stmts, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	if current.Len() > 0 {
		stmts = append(stmts, strings.TrimSpace(current.String()))
	}
	return stmts
}

// ===== 供 Wails binding 直接调用的包级委托函数 =====
// 命名采用 <Method>ByService 后缀，与 snippet/conn/dbops 包保持一致。
// 这些函数委托到 defaultBackupService，与对应 HTTP handler 共用同一份业务逻辑。

// CreateBackupAsyncByService 异步启动备份任务，返回 taskId。
// 调用方通过 GetBackupProgressByService(taskId) 轮询进度。
func CreateBackupAsyncByService(connId, schema, name, description, tablesStr, withData, encrypt, authorization string) string {
	ensureDefaultBackup()
	return defaultBackupService.CreateBackupAsync(connId, schema, name, description, tablesStr, withData, encrypt, authorization)
}

// GetBackupProgressByService 查询备份任务进度。
// 进度不存在时返回 (nil, false)，调用方应转为 not_found 状态。
func GetBackupProgressByService(taskId string) (any, bool) {
	ensureDefaultBackup()
	return FetchBackupProgress(taskId)
}

// ListBackupsByService 列出指定连接/schema 下的备份记录。
func ListBackupsByService(connId, schema string) (map[string]any, error) {
	ensureDefaultBackup()
	return defaultBackupService.ListBackups(connId, schema)
}

// RestoreBackupByService 从备份恢复数据到指定连接。
func RestoreBackupByService(backupId, connId, authorization string) (map[string]any, error) {
	ensureDefaultBackup()
	return defaultBackupService.RestoreBackup(backupId, connId, authorization)
}

// DeleteBackupByService 删除备份记录。
func DeleteBackupByService(backupId string) error {
	ensureDefaultBackup()
	return defaultBackupService.DeleteBackup(backupId)
}

// GetBackupTablesByService 查询指定连接/schema 下的表和视图列表，供备份前选择。
func GetBackupTablesByService(connId, schema, authorization string) (map[string]any, error) {
	ensureDefaultBackup()
	return defaultBackupService.GetBackupTables(connId, schema, authorization)
}

// DownloadBackupByService 把备份内容写入 writer，返回建议的下载文件名。
// 调用方负责设置 HTTP 头 (HTTP 模式) 或构造 BlobResult (桌面模式)。
func DownloadBackupByService(backupId string, w io.Writer) (string, error) {
	ensureDefaultBackup()
	return defaultBackupService.DownloadBackup(backupId, w)
}
