package backup

import (
	"fmt"
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

	"github.com/gin-gonic/gin"
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

// CreateBackup 创建数据库备份
func (s *BackupService) CreateBackup(connId, schema, name, description, tablesStr, withData, encrypt, authorization string) (map[string]any, error) {
	s.repo.EnsureBackupTable()

	dbConn := conn.GetConn(connId, authorization)
	dbType := dbConn.DriverName()

	if name == "" {
		name = fmt.Sprintf("%s_%s_backup_%s", connId[:8], schema, time.Now().Format("20060102_150405"))
	}

	var tables []string
	if tablesStr != "" {
		tables = strings.Split(tablesStr, ",")
	} else {
		tables = getAllTables(dbConn, dbType, schema)
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

		ddl := getCreateDDL(dbConn, dbType, schema, table)
		chunk := fmt.Sprintf("\n-- ----------------------------\n")
		chunk += fmt.Sprintf("-- Table structure for `%s`\n", table)
		chunk += fmt.Sprintf("-- ----------------------------\n")
		chunk += fmt.Sprintf("DROP TABLE IF EXISTS `%s`;\n", table)
		chunk += ddl + ";\n"
		file.WriteString(chunk)
		totalSize += int64(len(chunk))

		if withData == "true" {
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
		Type:        "full",
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
		tables = append(tables, BackupTables{Table: t, Checked: true})
	}

	var tableCounts []map[string]any
	for _, table := range allTables {
		var count int
		dbConn.Get(&count, fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", schema, table))
		tableCounts = append(tableCounts, map[string]any{
			"table": table,
			"rows":  count,
		})
	}

	return map[string]any{
		"tables":      tables,
		"tableCounts": tableCounts,
	}, nil
}

// DownloadBackup 下载备份文件
func (s *BackupService) DownloadBackup(c *gin.Context, backupId string) error {
	s.repo.EnsureBackupTable()

	record, err := s.repo.FindBackupById(backupId)
	if err != nil {
		return fmt.Errorf("备份不存在")
	}

	content, err1 := os.ReadFile(record.FilePath)
	if err1 != nil {
		return fmt.Errorf("备份文件不存在")
	}

	if record.Encrypted {
		content = []byte(crypto.AESDecode(string(content)))
	}

	fileName := record.Name + ".sql"
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", fmt.Sprintf("%d", len(content)))
	c.Writer.Write(content)
	return nil
}

// ===== 以下为外部数据库查询辅助函数 =====

func getAllTables(conn *sqlx.DB, dbType, schema string) []string {
	sqlTmpl, ok := dialect.SQL_DIALECT[dbType]["listTable"]
	if !ok {
		sqlTmpl = "SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema = ? order by TABLE_NAME"
	}
	result := make([]string, 0)
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
			result = append(result, strings.TrimSpace(tableName))
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
			result = append(result, strings.TrimSpace(tableName))
		}
	}
	sort.Strings(result)
	return result
}

func getCreateDDL(conn *sqlx.DB, dbType, schema, table string) string {
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

// describeTable 获取表列信息，优先用 information_schema（强类型 string 扫描），避免 []byte 输出
func describeTable(conn *sqlx.DB, dbType, schema, table string) []map[string]string {
	if dbType == "mysql" {
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
