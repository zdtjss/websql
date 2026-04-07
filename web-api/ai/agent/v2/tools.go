package agentv2

import (
	"context"
	"fmt"
	"log"
	"strings"

	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"

	"github.com/jmoiron/sqlx"
)

// ──────────────────────────────────────────────
// Tool 输入/输出结构体
// ──────────────────────────────────────────────

type QueryInput struct {
	SQL string `json:"sql" jsonschema:"required" jsonschema_description:"要执行的 SELECT SQL 语句"`
}

type QueryOutput struct {
	Columns []string         `json:"columns"`
	Data    []map[string]any `json:"data"`
	Count   int              `json:"count"`
}

type ExecInput struct {
	SQL string `json:"sql" jsonschema:"required" jsonschema_description:"要执行的写操作 SQL 语句"`
}

type ExecOutput struct {
	AffectedRows int64  `json:"affectedRows"`
	Message      string `json:"message"`
}

type SchemaInput struct {
	Tables []string `json:"tables" jsonschema:"required" jsonschema_description:"要查询结构的表名列表"`
}

type SchemaOutput struct {
	Schema string `json:"schema"`
}

type ExportInput struct {
	SQL      string `json:"sql" jsonschema:"required" jsonschema_description:"用于导出数据的 SELECT SQL"`
	FileName string `json:"fileName" jsonschema_description:"导出文件名（不含扩展名）"`
}

type ExportOutput struct {
	Message     string `json:"message"`
	RowCount    int    `json:"rowCount"`
	DownloadURL string `json:"downloadUrl"`
}

// ──────────────────────────────────────────────
// 数据库连接
// ──────────────────────────────────────────────

func getConn(connId string) (*sqlx.DB, string) {
	cfgList := []admin.ConnCfg{}
	err := config.Mngtdb.Select(&cfgList, "select * from t_conn where id = ?", connId)
	if err != nil || len(cfgList) == 0 {
		return nil, ""
	}
	cfg := &cfgList[0]

	pwd := ""
	if cfg.Pwd != nil {
		pwd = utils.AESDecode(*cfg.Pwd)
	}
	name := ""
	if cfg.Name != nil {
		name = *cfg.Name
	}
	user := ""
	if cfg.User != nil {
		user = *cfg.User
	}
	url := ""
	if cfg.Url != nil {
		url = *cfg.Url
	}

	conn := config.GetConn(&config.DBParam{
		Id: cfg.Id, Name: name, DbType: cfg.DbType,
		User: user, Pwd: pwd, Url: url,
	})
	return conn, cfg.DbType
}

// ──────────────────────────────────────────────
// Tool 实现
// ──────────────────────────────────────────────

// NewQueryFunc 查询工具
func NewQueryFunc(connId string) func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
	return func(ctx context.Context, input *QueryInput) (*QueryOutput, error) {
		log.Printf("[Tool:query_data] sql=%s\n", input.SQL)

		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		sql := strings.TrimSpace(input.SQL)
		upper := strings.ToUpper(sql)
		if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "SHOW") &&
			!strings.HasPrefix(upper, "DESCRIBE") && !strings.HasPrefix(upper, "EXPLAIN") {
			return nil, fmt.Errorf("query_data 仅支持 SELECT/SHOW/DESCRIBE/EXPLAIN 语句")
		}

		rows, err := conn.Queryx(sql)
		if err != nil {
			log.Printf("[Tool:query_data] 查询失败 - err=%v\n", err)
			return nil, fmt.Errorf("查询失败：%w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("获取列信息失败：%w", err)
		}

		data := dbutils.GetResultRows(conn.DriverName(), rows)
		log.Printf("[Tool:query_data] 成功 - columns=%d, rows=%d\n", len(cols), len(data))
		return &QueryOutput{Columns: cols, Data: data, Count: len(data)}, nil
	}
}

// NewExecFunc 写操作工具
func NewExecFunc(connId string) func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
	return func(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
		log.Printf("[Tool:exec_sql] sql=%s\n", input.SQL)

		conn, _ := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		sql := strings.TrimSpace(input.SQL)

		// 没有确认标记的写操作一律拦截
		if !strings.Contains(sql, "-- CONFIRMED:") {
			if isDangerousSQL(sql) {
				return nil, &DangerousSQLError{SQL: sql}
			}
			return nil, fmt.Errorf("此操作需要用户确认")
		}

		// 提取实际 SQL
		var actualLines []string
		for _, line := range strings.Split(sql, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "-- CONFIRMED:") {
				actualLines = append(actualLines, line)
			}
		}
		actualSQL := strings.TrimSpace(strings.Join(actualLines, "\n"))
		if actualSQL == "" {
			return nil, fmt.Errorf("SQL 不能为空")
		}

		result, err := conn.Exec(actualSQL)
		if err != nil {
			log.Printf("[Tool:exec_sql] 执行失败 - err=%v\n", err)
			return nil, fmt.Errorf("执行失败：%w", err)
		}

		affected, _ := result.RowsAffected()
		log.Printf("[Tool:exec_sql] 成功 - affectedRows=%d\n", affected)
		return &ExecOutput{AffectedRows: affected, Message: fmt.Sprintf("执行成功，影响 %d 行", affected)}, nil
	}
}

// NewSchemaFunc 表结构工具
func NewSchemaFunc(connId, dbType, dbSchema string) func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
	return func(ctx context.Context, input *SchemaInput) (*SchemaOutput, error) {
		log.Printf("[Tool:get_table_schema] tables=%v\n", input.Tables)

		conn, actualDBType := getConn(connId)
		if conn == nil {
			return nil, fmt.Errorf("数据库连接不存在：%s", connId)
		}

		var sb strings.Builder
		for _, table := range input.Tables {
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
				logutils.PrintErr(fmt.Errorf("获取表结构失败 %s: %w", table, err))
				sb.WriteString(fallbackColumnInfo(conn, actualDBType, dbSchema, table))
				continue
			}
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

func fallbackColumnInfo(conn *sqlx.DB, dbType, dbSchema, table string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "-- Table: %s\n", table)

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
		fmt.Fprintf(&sb, "  %v\n", row)
	}
	sb.WriteString("\n")
	return sb.String()
}

// Export tool functions are defined in export_tools.go
