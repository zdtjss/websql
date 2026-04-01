package agent

import (
	"context"
	"fmt"
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

// --- Tool 输入/输出结构体（供 Eino InferTool 自动推断 JSON Schema）---

// QueryInput 执行 SELECT 查询的输入。
type QueryInput struct {
	SQL string `json:"sql" jsonschema:"required" jsonschema_description:"要执行的 SELECT SQL 语句"`
}

// QueryOutput SELECT 查询结果。
type QueryOutput struct {
	Columns []string                 `json:"columns"`
	Data    []map[string]interface{} `json:"data"`
	Count   int                      `json:"count"`
}

// ExecInput 执行写操作 SQL 的输入。
type ExecInput struct {
	SQL string `json:"sql" jsonschema:"required" jsonschema_description:"要执行的 INSERT/UPDATE/DELETE/ALTER SQL 语句"`
}

// ExecOutput 写操作结果。
type ExecOutput struct {
	AffectedRows int64  `json:"affectedRows"`
	Message      string `json:"message"`
}

// SchemaInput 获取表结构的输入。
type SchemaInput struct {
	Tables []string `json:"tables" jsonschema:"required" jsonschema_description:"要查询结构的表名列表"`
}

// SchemaOutput 表结构信息。
type SchemaOutput struct {
	Schema string `json:"schema"`
}

// ExportInput 数据导出的输入。
type ExportInput struct {
	SQL      string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出数据的 SELECT SQL"`
	FileName string `json:"fileName" jsonschema_description:"导出文件名（不含扩展名）"`
}

// ExportOutput 导出结果。
type ExportOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
}

// --- Tool 实现函数 ---

// getConn 根据 connId 获取数据库连接。
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

// NewQueryFunc 创建执行 SELECT 查询的 Tool 函数。
func NewQueryFunc(connId string) func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
	return func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		sql := strings.TrimSpace(input.SQL)
		if !strings.HasPrefix(strings.ToUpper(sql), "SELECT") &&
			!strings.HasPrefix(strings.ToUpper(sql), "SHOW") &&
			!strings.HasPrefix(strings.ToUpper(sql), "DESCRIBE") &&
			!strings.HasPrefix(strings.ToUpper(sql), "EXPLAIN") {
			return nil, fmt.Errorf("query_data 仅支持 SELECT/SHOW/DESCRIBE/EXPLAIN 语句")
		}

		rows, err := conn.Queryx(sql)
		if err != nil {
			return nil, fmt.Errorf("查询失败：%w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("获取列信息失败：%w", err)
		}

		data := dbutils.GetResultRows(conn.DriverName(), rows)
		return &QueryOutput{
			Columns: cols,
			Data:    data,
			Count:   len(data),
		}, nil
	}
}

// NewExecFunc 创建执行写操作 SQL 的 Tool 函数。
func NewExecFunc(connId string) func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
	return func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		sql := strings.TrimSpace(input.SQL)
		result, err := conn.Exec(sql)
		if err != nil {
			return nil, fmt.Errorf("执行失败：%w", err)
		}

		affected, _ := result.RowsAffected()
		return &ExecOutput{
			AffectedRows: affected,
			Message:      fmt.Sprintf("执行成功，影响 %d 行", affected),
		}, nil
	}
}

// NewSchemaFunc 创建获取表结构的 Tool 函数（支持 MySQL/SQLite/Oracle）。
func NewSchemaFunc(connId string, dbSchema string) func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
	return func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
		conn, dbType := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		var sb strings.Builder
		for _, table := range input.Tables {
			var schemaSQL string
			switch dbType {
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
				logutils.PrintErr(fmt.Errorf("获取表结构失败 %s: %w", table, err))
				// 回退：尝试用 information_schema 获取列信息
				sb.WriteString(fallbackColumnInfo(conn, dbType, dbSchema, table))
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
					// MySQL: SHOW CREATE TABLE 返回 (tableName, createStatement)
					var tableName, createTable string
					if err := rows.Scan(&tableName, &createTable); err == nil {
						sb.WriteString(createTable)
						sb.WriteString(";\n\n")
					}
				}
			}
			rows.Close()
		}
		return &SchemaOutput{Schema: sb.String()}, nil
	}
}

// fallbackColumnInfo 当 DDL 获取失败时，通过 information_schema 获取列信息。
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

// NewExportFunc 创建数据导出的 Tool 函数，生成 Excel 文件并返回下载链接。
func NewExportFunc(connId string) func(ctx context.Context, input *ExportInput) (*ExportOutput, error) {
	return func(ctx context.Context, input *ExportInput) (*ExportOutput, error) {
		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		sql := strings.TrimSpace(input.SQL)
		rows, err := conn.Queryx(sql)
		if err != nil {
			return nil, fmt.Errorf("导出查询失败：%w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("获取列信息失败：%w", err)
		}

		// 创建 Excel 文件
		excel := excelize.NewFile()
		defer excel.Close()

		sw, err := excel.NewStreamWriter("Sheet1")
		if err != nil {
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
		// 确保文件名不包含扩展名（去除可能的 .csv、.xlsx 等后缀）
		fileName = strings.TrimSuffix(fileName, ".csv")
		fileName = strings.TrimSuffix(fileName, ".xlsx")
		fileName = strings.TrimSuffix(fileName, ".xls")
		// 确保 exports 目录存在
		os.MkdirAll("exports", 0755)
		filePath := fmt.Sprintf("exports/%s.xlsx", fileName)
		if err := excel.SaveAs(filePath); err != nil {
			return nil, fmt.Errorf("保存 Excel 文件失败：%w", err)
		}

		downloadURL := fmt.Sprintf("/exports/%s.xlsx", fileName)

		return &ExportOutput{
			Message:     fmt.Sprintf("已导出 %d 条数据到文件 %s.xlsx", len(data), fileName),
			RowCount:    len(data),
			DownloadURL: downloadURL,
		}, nil
	}
}
