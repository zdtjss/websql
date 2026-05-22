package backup

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"websql/internal/app/conn"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/dialect"
	"websql/internal/logger"
	"websql/internal/pkg/crypto"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type BackupRecord struct {
	Id          string `json:"id" db:"id"`
	Name        string `json:"name" db:"name"`
	ConnId      string `json:"connId" db:"conn_id"`
	Schema      string `json:"schema" db:"schema_name"`
	DbType      string `json:"dbType" db:"db_type"`
	Size        int64  `json:"size" db:"size_bytes"`
	Type        string `json:"type" db:"backup_type"`
	Encrypted   bool   `json:"encrypted" db:"encrypted"`
	CreatedAt   string `json:"createdAt" db:"created_at"`
	Description string `json:"description" db:"description"`
	Status      string `json:"status" db:"status"`
	FilePath    string `json:"filePath" db:"file_path"`
}

type BackupTables struct {
	Table   string `json:"table"`
	Checked bool   `json:"checked"`
}

type BackupCreateRequest struct {
	ConnId      string   `json:"connId"`
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tables      []string `json:"tables"`
	WithData    bool     `json:"withData"`
	Encrypt     bool     `json:"encrypt"`
	Compress    bool     `json:"compress"`
}

var migrateOnce sync.Once

func ensureBackupTable() {
	migrateOnce.Do(func() {
		if database.Mngtdb == nil {
			return
		}
		var hasNameCol bool
		row := database.Mngtdb.QueryRow("SELECT COUNT(*) > 0 FROM pragma_table_info('t_backup') WHERE name='name'")
		if err := row.Scan(&hasNameCol); err != nil {
			var colCount int
			row2 := database.Mngtdb.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_name='t_backup' AND column_name='name'")
			if err2 := row2.Scan(&colCount); err2 != nil {
				return
			}
			hasNameCol = colCount > 0
		}
		if hasNameCol {
			return
		}
		database.Mngtdb.Exec("DROP TABLE IF EXISTS t_backup")
		database.Mngtdb.Exec(`CREATE TABLE t_backup (
			id TEXT PRIMARY KEY,
			name TEXT,
			conn_id TEXT,
			schema_name TEXT,
			db_type TEXT,
			size_bytes INTEGER DEFAULT 0,
			backup_type TEXT DEFAULT 'full',
			encrypted INTEGER DEFAULT 0,
			created_at TEXT,
			description TEXT,
			status TEXT DEFAULT 'completed',
			file_path TEXT
		)`)
	})
}

func init() {
	_ = config.Cfg
}

func CreateBackup(c *gin.Context) {
	ensureBackupTable()

	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	name := c.PostForm("name")
	description := c.PostForm("description")
	tablesStr := c.PostForm("tables")
	withData := c.DefaultPostForm("withData", "true")
	encrypt := c.DefaultPostForm("encrypt", "false")
	_ = c.DefaultPostForm("compress", "false")

	authorization := c.GetHeader("Authorization")
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	if name == "" {
		name = fmt.Sprintf("%s_%s_backup_%s", connId[:8], schema, time.Now().Format("20060102_150405"))
	}

	var tables []string
	if tablesStr != "" {
		tables = strings.Split(tablesStr, ",")
	} else {
		tables = getAllTables(conn, dbType, schema)
	}

	backupDir := filepath.Join("backups", connId, schema)
	os.MkdirAll(backupDir, 0755)

	filePath := filepath.Join(backupDir, name+".sql")
	file, err := os.Create(filePath)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "创建备份文件失败: " + err.Error()})
		return
	}
	defer file.Close()

	file.WriteString(fmt.Sprintf("-- WebSQL Backup\n"))
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

		ddl := getCreateDDL(conn, dbType, table)
		chunk := fmt.Sprintf("\n-- ----------------------------\n")
		chunk += fmt.Sprintf("-- Table structure for `%s`\n", table)
		chunk += fmt.Sprintf("-- ----------------------------\n")
		chunk += fmt.Sprintf("DROP TABLE IF EXISTS `%s`;\n", table)
		chunk += ddl + ";\n"
		file.WriteString(chunk)
		totalSize += int64(len(chunk))

		if withData == "true" {
			data, _, rowCount, err := exportTableData(conn, dbType, schema, table)
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
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "加密备份文件失败: " + err3.Error()})
			return
		}
	}

	id := idgen.RandomStr()
	_, err4 := database.Mngtdb.Exec(
		"INSERT INTO t_backup (id, name, conn_id, schema_name, db_type, size_bytes, backup_type, encrypted, created_at, description, status, file_path) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		id, name, connId, schema, dbType, totalSize, "full", encrypt == "true", time.Now().Format("2006-01-02 15:04:05"), description, "completed", filePath,
	)
	if err4 != nil {
		logger.PrintErrf("保存备份记录失败", err4)
		database.Mngtdb.Exec(
			"INSERT INTO t_backup (id, name, conn_id, schema_name, db_type, size_bytes, backup_type, encrypted, created_at, description) VALUES (?,?,?,?,?,?,?,?,?,?)",
			id, name, connId, schema, dbType, totalSize, "full", encrypt == "true", time.Now().Format("2006-01-02 15:04:05"), description,
		)
	}

	jsonutil.WriteJson(c.Writer, map[string]any{
		"success":      len(errors) == 0,
		"id":           id,
		"name":         name,
		"size":         totalSize,
		"tables":       successCount,
		"errors":       errors,
		"encrypted":    encrypt == "true",
		"errorMessage": "",
	})
}

func ListBackups(c *gin.Context) {
	ensureBackupTable()

	connId := c.Query("connId")
	schema := c.Query("schema")

	var records []BackupRecord
	var err error
	if connId != "" && schema != "" {
		err = database.Mngtdb.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup WHERE conn_id=? AND schema_name=? ORDER BY created_at DESC", connId, schema)
	} else if connId != "" {
		err = database.Mngtdb.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup WHERE conn_id=? ORDER BY created_at DESC", connId)
	} else {
		err = database.Mngtdb.Select(&records, "SELECT id,name,conn_id,schema_name,db_type,size_bytes,backup_type,encrypted,created_at,description,COALESCE(status,'completed') status, COALESCE(file_path,'') file_path FROM t_backup ORDER BY created_at DESC")
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "获取备份列表失败: " + err.Error()})
		return
	}

	jsonutil.WriteJson(c.Writer, map[string]any{
		"records": records,
		"total":   len(records),
	})
}

func RestoreBackup(c *gin.Context) {
	ensureBackupTable()

	backupId := c.PostForm("backupId")
	connId := c.PostForm("connId")

	authorization := c.GetHeader("Authorization")
	conn := conn.GetConn(connId, authorization)

	var record BackupRecord
	err := database.Mngtdb.Get(&record, "SELECT * FROM t_backup WHERE id=?", backupId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "未找到备份记录"})
		return
	}

	content, err := os.ReadFile(record.FilePath)
	if err != nil && record.Encrypted {
		jsonutil.WriteJson(c.Writer, map[string]any{"success": false, "message": "备份文件不存在"})
		return
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
		_, err := conn.Exec(stmt)
		if err != nil {
			errMsg := fmt.Sprintf("执行失败 [%.100s]: %s", stmt, err.Error())
			failed = append(failed, errMsg)
			logger.PrintErrf(errMsg, nil)
		} else {
			executed++
		}
	}

	jsonutil.WriteJson(c.Writer, map[string]any{
		"success":     len(failed) == 0,
		"executed":    executed,
		"failed":      failed,
		"failedCount": len(failed),
	})
}

func DeleteBackup(c *gin.Context) {
	ensureBackupTable()

	backupId := c.PostForm("backupId")

	var record BackupRecord
	err := database.Mngtdb.Get(&record, "SELECT * FROM t_backup WHERE id=?", backupId)
	if err == nil && record.FilePath != "" {
		os.Remove(record.FilePath)
	}

	_, err = database.Mngtdb.Exec("DELETE FROM t_backup WHERE id=?", backupId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "删除备份记录失败: " + err.Error()})
		return
	}

	jsonutil.WriteJson(c.Writer, map[string]any{"success": true})
}

func GetBackupTables(c *gin.Context) {
	connId := c.Query("connId")
	schema := c.Query("schema")

	authorization := c.GetHeader("Authorization")
	conn := conn.GetConn(connId, authorization)
	dbType := conn.DriverName()

	allTables := getAllTables(conn, dbType, schema)
	tables := make([]BackupTables, 0)
	for _, t := range allTables {
		tables = append(tables, BackupTables{Table: t, Checked: true})
	}

	var tableCounts []map[string]any
	for _, table := range allTables {
		var count int
		conn.Get(&count, fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", schema, table))
		tableCounts = append(tableCounts, map[string]any{
			"table": table,
			"rows":  count,
		})
	}

	jsonutil.WriteJson(c.Writer, map[string]any{
		"tables":      tables,
		"tableCounts": tableCounts,
	})
}

func DownloadBackup(c *gin.Context) {
	ensureBackupTable()

	backupId := c.Query("backupId")

	var record BackupRecord
	err := database.Mngtdb.Get(&record, "SELECT * FROM t_backup WHERE id=?", backupId)
	if err != nil {
		c.JSON(404, map[string]string{"error": "备份不存在"})
		return
	}

	content, err1 := os.ReadFile(record.FilePath)
	if err1 != nil {
		c.JSON(404, map[string]string{"error": "备份文件不存在"})
		return
	}

	if record.Encrypted {
		content = []byte(crypto.AESDecode(string(content)))
	}

	fileName := record.Name + ".sql"
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", fmt.Sprintf("%d", len(content)))
	c.Writer.Write(content)
}

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

func getCreateDDL(conn *sqlx.DB, dbType, table string) string {
	var result []string
	sql := fmt.Sprintf("SHOW CREATE TABLE `%s`", table)
	err := conn.Select(&result, sql)
	if err != nil || len(result) < 2 {
		cols := describeTable(conn, table)
		return generateDDLStmt(table, cols)
	}
	return result[1]
}

func describeTable(conn *sqlx.DB, table string) []map[string]string {
	sql := fmt.Sprintf("DESCRIBE `%s`", table)
	rows, err := conn.Queryx(sql)
	if err != nil {
		return nil
	}
	defer rows.Close()

	columns, _ := rows.Columns()
	cols := make([]map[string]string, 0)
	for rows.Next() {
		vals := make([]any, len(columns))
		valPtrs := make([]any, len(columns))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		rows.Scan(valPtrs...)
		row := make(map[string]string)
		for i, col := range columns {
			if vals[i] != nil {
				row[col] = fmt.Sprintf("%v", vals[i])
			}
		}
		cols = append(cols, row)
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

	var buf strings.Builder
	rowCount := 0
	for rows.Next() {
		vals := make([]any, len(columns))
		valPtrs := make([]any, len(columns))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		rows.Scan(valPtrs...)

		colNames := make([]string, 0)
		colValues := make([]string, 0)
		for i, col := range columns {
			val := "NULL"
			if vals[i] != nil {
				valStr := fmt.Sprintf("%v", vals[i])
				valStr = strings.ReplaceAll(valStr, "\\", "\\\\")
				valStr = strings.ReplaceAll(valStr, "'", "\\'")
				val = fmt.Sprintf("'%s'", valStr)
			}
			colNames = append(colNames, fmt.Sprintf("`%s`", col))
			colValues = append(colValues, val)
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