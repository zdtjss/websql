// Package agentv2 基于 Eino ADK 重构的 AI SQL 智能体 v2。
// 使用 adk.ChatModelAgent 自动处理 tool calling 循环
// 使用 callback 机制处理流式输出
package agentv2

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"

	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
)

// DangerousSQLError 危险 SQL 错误（用于通知前端弹出确认界面）
type DangerousSQLError struct {
	SQL string
}

func (e *DangerousSQLError) Error() string {
	return fmt.Sprintf("DANGEROUS_SQL:%s", e.SQL)
}

// isDangerousSQL 检查 SQL 是否为危险操作
func isDangerousSQL(sql string) bool {
	upperSQL := strings.ToUpper(strings.TrimSpace(sql))
	dangerousPatterns := []string{
		"DROP ", "TRUNCATE ", "DELETE FROM",
		"ALTER ", "CREATE ", "REPLACE ",
		"INSERT ", "UPDATE ",
	}
	for _, pattern := range dangerousPatterns {
		if strings.HasPrefix(upperSQL, pattern) {
			return true
		}
	}
	return false
}

// === Tool 输入/输出结构体 ===

// QueryInput 执行 SELECT 查询的输入
type QueryInput struct {
	SQL string `json:"sql" jsonschema:"required" jsonschema_description:"要执行的 SELECT SQL 语句"`
}

// QueryOutput SELECT 查询结果
type QueryOutput struct {
	Columns []string                 `json:"columns"`
	Data    []map[string]interface{} `json:"data"`
	Count   int                      `json:"count"`
}

// ExecInput 执行写操作 SQL 的输入
type ExecInput struct {
	SQL string `json:"sql" jsonschema:"required" jsonschema_description:"要执行的 INSERT/UPDATE/DELETE/ALTER SQL 语句"`
}

// ExecOutput 写操作结果
type ExecOutput struct {
	AffectedRows int64  `json:"affectedRows"`
	Message      string `json:"message"`
}

// SchemaInput 获取表结构的输入
type SchemaInput struct {
	Tables []string `json:"tables" jsonschema:"required" jsonschema_description:"要查询结构的表名列表"`
}

// SchemaOutput 表结构信息
type SchemaOutput struct {
	Schema string `json:"schema"`
}

// ExportInput 数据导出的输入
type ExportInput struct {
	SQL      string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出数据的 SELECT SQL"`
	FileName string `json:"fileName" jsonschema_description:"导出文件名（不含扩展名）"`
}

// ExportOutput 导出结果
type ExportOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
}

// === Tool 实现函数 ===

// getConn 根据 connId 获取数据库连接
func getConn(connId string) (*sqlx.DB, string) {
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connId)
	if err != nil || len(cfgList) == 0 {
		return nil, ""
	}
	cfg := &cfgList[0]
	cfg.Pwd = utils.AESDecode(cfg.Pwd)
	conn := config.GetConn(&config.DBParam{
		Id: cfg.Id, Name: cfg.Name, DbType: cfg.DbType,
		User: cfg.User, Pwd: cfg.Pwd, Url: cfg.Url,
	})
	return conn, cfg.DbType
}

// NewQueryFunc 创建执行 SELECT 查询的 Tool 函数
func NewQueryFunc(connId string) func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
	return func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
		log.Printf("[Tool:query_data] 开始执行 - connId=%s, sql=%s\n", connId, input.SQL)
		
		conn, _ := getConn(connId)
		if conn == nil {
			log.Printf("[Tool:query_data] 错误 - 数据库连接不存在 connId=%s\n", connId)
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		sql := strings.TrimSpace(input.SQL)
		if !strings.HasPrefix(strings.ToUpper(sql), "SELECT") &&
			!strings.HasPrefix(strings.ToUpper(sql), "SHOW") &&
			!strings.HasPrefix(strings.ToUpper(sql), "DESCRIBE") &&
			!strings.HasPrefix(strings.ToUpper(sql), "EXPLAIN") {
			log.Printf("[Tool:query_data] 错误 - 不支持的 SQL 类型 sql=%s\n", sql)
			return nil, fmt.Errorf("query_data 仅支持 SELECT/SHOW/DESCRIBE/EXPLAIN 语句")
		}

		log.Printf("[Tool:query_data] 开始执行查询 - sql=%s\n", sql)
		rows, err := conn.Queryx(sql)
		if err != nil {
			log.Printf("[Tool:query_data] 查询失败 - sql=%s, err=%v\n", sql, err)
			return nil, fmt.Errorf("查询失败：%w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			log.Printf("[Tool:query_data] 获取列信息失败 - err=%v\n", err)
			return nil, fmt.Errorf("获取列信息失败：%w", err)
		}

		data := dbutils.GetResultRows(conn.DriverName(), rows)
		log.Printf("[Tool:query_data] 查询成功 - columns=%d, rows=%d\n", len(cols), len(data))
		return &QueryOutput{
			Columns: cols,
			Data:    data,
			Count:   len(data),
		}, nil
	}
}

// NewExecFunc 创建执行写操作 SQL 的 Tool 函数
func NewExecFunc(connId string) func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
	return func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
		log.Printf("[Tool:exec_sql] 开始执行 - connId=%s, sql=%s\n", connId, input.SQL)
		
		conn, _ := getConn(connId)
		if conn == nil {
			log.Printf("[Tool:exec_sql] 错误 - 数据库连接不存在 connId=%s\n", connId)
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		sql := strings.TrimSpace(input.SQL)
		log.Printf("[Tool:exec_sql] SQL 类型检测 - sql=%s\n", sql)

		// 检查是否包含用户确认标记
		if !strings.Contains(sql, "-- CONFIRMED:") {
			// 没有确认标记，判断是否为危险 SQL
			if isDangerousSQL(sql) {
				log.Printf("[Tool:exec_sql] 检测到危险 SQL，已拦截 - sql=%s\n", sql)
				// 是危险 SQL，返回特定错误，AI 接收到后会重新生成回复引导用户确认
				return nil, &DangerousSQLError{SQL: sql}
			}
			// 不是危险 SQL，但也需要确认（可能是普通写操作）
			log.Printf("[Tool:exec_sql] 非危险 SQL 但需要用户确认 - sql=%s\n", sql)
			return nil, fmt.Errorf("此操作需要用户确认，请 AI 助手生成 SQL 并告知用户在页面确认执行")
		}

		// 有确认标记，提取实际 SQL 并执行
		lines := strings.Split(sql, "\n")
		var actualSQLLines []string
		for _, line := range lines {
			if !strings.HasPrefix(strings.TrimSpace(line), "-- CONFIRMED:") {
				actualSQLLines = append(actualSQLLines, line)
			}
		}
		actualSQL := strings.TrimSpace(strings.Join(actualSQLLines, "\n"))

		if actualSQL == "" {
			log.Printf("[Tool:exec_sql] 错误 - 提取后的 SQL 为空\n")
			return nil, fmt.Errorf("SQL 不能为空")
		}

		log.Printf("[Tool:exec_sql] 开始执行实际 SQL - sql=%s\n", actualSQL)
		result, err := conn.Exec(actualSQL)
		if err != nil {
			log.Printf("[Tool:exec_sql] 执行失败 - sql=%s, err=%v\n", actualSQL, err)
			return nil, fmt.Errorf("执行失败：%w", err)
		}

		affected, _ := result.RowsAffected()
		log.Printf("[Tool:exec_sql] 执行成功 - affectedRows=%d\n", affected)
		return &ExecOutput{
			AffectedRows: affected,
			Message:      fmt.Sprintf("执行成功，影响 %d 行", affected),
		}, nil
	}
}

// NewSchemaFunc 创建获取表结构的 Tool 函数
func NewSchemaFunc(connId string, dbType string, dbSchema string) func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
	return func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
		log.Printf("[Tool:get_table_schema] 开始执行 - connId=%s, tables=%v\n", connId, input.Tables)
		
		conn, actualDBType := getConn(connId)
		if conn == nil {
			log.Printf("[Tool:get_table_schema] 错误 - 数据库连接不存在 connId=%s\n", connId)
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		var sb strings.Builder
		for _, table := range input.Tables {
			log.Printf("[Tool:get_table_schema] 获取表结构 - table=%s\n", table)
			var schemaSQL string
			switch actualDBType {
			case "mysql", "mariadb":
				schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`", table)
			case "sqlite":
				schemaSQL = fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='table' AND name='%s'", table)
			case "oracle":
				schemaSQL = fmt.Sprintf("SELECT DBMS_METADATA.GET_DDL('TABLE', '%s') FROM DUAL", strings.ToUpper(table))
			default:
				schemaSQL = fmt.Sprintf("SHOW CREATE TABLE `%s`", table)
			}

			rows, err := conn.Query(schemaSQL)
			if err != nil {
				log.Printf("[Tool:get_table_schema] 获取表 %s 结构失败 - err=%v\n", table, err)
				logutils.PrintErr(fmt.Errorf("获取表结构失败 %s: %w", table, err))
				sb.WriteString(fallbackColumnInfo(conn, actualDBType, dbSchema, table))
				continue
			}
			for rows.Next() {
				switch dbType {
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
					var tableName, createTable string
					if err := rows.Scan(&tableName, &createTable); err == nil {
						sb.WriteString(createTable)
						sb.WriteString(";\n\n")
					}
				}
			}
			rows.Close()
			log.Printf("[Tool:get_table_schema] 表 %s 结构获取完成\n", table)
		}
		log.Printf("[Tool:get_table_schema] 所有表结构获取完成 - total_tables=%d\n", len(input.Tables))
		return &SchemaOutput{Schema: sb.String()}, nil
	}
}

// fallbackColumnInfo 当 DDL 获取失败时的回退方案
func fallbackColumnInfo(conn *sqlx.DB, dbType, dbSchema, table string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("-- Table: %s\n", table))

	var query string
	var args []any
	switch dbType {
	case "mysql", "mariadb":
		query = "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION"
		args = []any{dbSchema, table}
	case "sqlite":
		query = fmt.Sprintf("PRAGMA table_info('%s')", table)
	default:
		return sb.String()
	}

	rows, err := conn.Queryx(query, args...)
	if err != nil {
		return sb.String()
	}
	defer rows.Close()

	data := dbutils.GetResultRows(conn.DriverName(), rows)
	for _, row := range data {
		sb.WriteString(fmt.Sprintf("  %v\n", row))
	}
	sb.WriteString("\n")
	return sb.String()
}

// NewExportFunc 创建数据导出的 Tool 函数
func NewExportFunc(connId string) func(ctx context.Context, input *ExportInput) (*ExportOutput, error) {
	return func(ctx context.Context, input *ExportInput) (*ExportOutput, error) {
		log.Printf("[Tool:export_excel] 开始执行 - connId=%s, fileName=%s\n", connId, input.FileName)
		log.Printf("[Tool:export_excel] SQL - %s\n", input.SQL)
		
		conn, _ := getConn(connId)
		if conn == nil {
			log.Printf("[Tool:export_excel] 错误 - 数据库连接不存在 connId=%s\n", connId)
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		sql := strings.TrimSpace(input.SQL)
		log.Printf("[Tool:export_excel] 开始执行导出查询\n")
		rows, err := conn.Queryx(sql)
		if err != nil {
			log.Printf("[Tool:export_excel] 导出查询失败 - sql=%s, err=%v\n", sql, err)
			return nil, fmt.Errorf("导出查询失败：%w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			log.Printf("[Tool:export_excel] 获取列信息失败 - err=%v\n", err)
			return nil, fmt.Errorf("获取列信息失败：%w", err)
		}

		// 创建 Excel 文件
		log.Printf("[Tool:export_excel] 开始创建 Excel 文件 - columns=%d\n", len(cols))
		excel := excelize.NewFile()
		defer excel.Close()

		sw, err := excel.NewStreamWriter("Sheet1")
		if err != nil {
			log.Printf("[Tool:export_excel] 创建 Excel 写入器失败 - err=%v\n", err)
			return nil, fmt.Errorf("创建 Excel 写入器失败：%w", err)
		}

		// 写表头
		header := make([]interface{}, len(cols))
		for i, colName := range cols {
			header[i] = colName
		}
		sw.SetRow("A1", header)

		// 写数据
		data := dbutils.GetResultRowsForExport(conn.DriverName(), rows)
		log.Printf("[Tool:export_excel] 开始写入数据 - rows=%d\n", len(data))
		for rowIdx, row := range data {
			rowData := make([]interface{}, len(cols))
			for colIdx, col := range cols {
				if v, ok := row[col]; ok {
					rowData[colIdx] = v
				}
			}
			cell := fmt.Sprintf("A%d", rowIdx+2)
			sw.SetRow(cell, rowData)
		}
		sw.Flush()

		// 生成文件名并保存到 exports 目录
		fileName := input.FileName
		if fileName == "" {
			fileName = fmt.Sprintf("export_%s", time.Now().Format("20060102_150405"))
		}
		fileName = strings.TrimSuffix(fileName, ".csv")
		fileName = strings.TrimSuffix(fileName, ".xlsx")
		fileName = strings.TrimSuffix(fileName, ".xls")
		os.MkdirAll("exports", 0755)
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		log.Printf("[Tool:export_excel] 开始保存文件 - filePath=%s\n", filePath)
		if err := excel.SaveAs(filePath); err != nil {
			log.Printf("[Tool:export_excel] 保存 Excel 文件失败 - err=%v\n", err)
			return nil, fmt.Errorf("保存 Excel 文件失败：%w", err)
		}

		downloadURL := fmt.Sprintf("/exports/%s.xlsx", fileName)
		log.Printf("[Tool:export_excel] 导出成功 - rowCount=%d, downloadURL=%s\n", len(data), downloadURL)

		return &ExportOutput{
			Message:     fmt.Sprintf("已导出 %d 条数据，[点击下载](%s)", len(data), downloadURL),
			RowCount:    len(data),
			DownloadURL: downloadURL,
		}, nil
	}
}
