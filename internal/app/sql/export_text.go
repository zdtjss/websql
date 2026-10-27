package sql

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/audit"
	"websql/internal/dialect"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/apperror"
	"websql/internal/pkg/response"
	"websql/internal/pkg/sqlguard"

	"github.com/gin-gonic/gin"
)

// 文本格式导出（CSV / JSON / SQL）。
//
// 背景（计划 2.4）：原先 CSV/JSON/SQL 由前端一次拉取最多 10 万行（maxLine=100000）
// 再在浏览器里拼文件，大结果集会同时压垮内存与带宽。
// 这里改为服务端边读边写：逐行从数据库读、逐行写 ResponseWriter，
// 进程内存只保留一行数据。
//
// 安全：与 /exportXlsxBySql 一致——只读语句白名单 + 表/列权限校验 + 审计。

const exportTextFlushEvery = 200

// ExportText POST /api/exportText
func ExportText(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	connId := appctx.Ctx.GetConnID(c)
	schema := c.PostForm("schema")
	sqlStr := strings.TrimSpace(c.PostForm("sql"))
	format := strings.ToLower(strings.TrimSpace(c.PostForm("format")))
	filename := strings.TrimSpace(c.PostForm("filename"))
	if filename == "" {
		filename = "export"
	}
	if sqlStr == "" {
		response.WriteErr(c, 200, 400, "SQL 语句不能为空")
		return
	}
	switch format {
	case "csv", "json", "sql":
	default:
		response.WriteErr(c, 200, 400, "不支持的导出格式: "+format)
		return
	}

	if schema == "" && connId != "" {
		dc := conn.GetConn(connId, authorization)
		switch dc.DriverName() {
		case "mysql", "mariadb":
			dc.Get(&schema, "SELECT DATABASE()")
		case "oracle":
			dc.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		case "sqlite":
			schema = "main"
		}
	}

	// 只读白名单：fail-closed，防止导出链路被用来执行写操作
	if !sqlguard.IsReadOnlyStatement(sqlStr) {
		response.WriteErr(c, 200, 500, "导出仅支持只读查询（SELECT/SHOW/DESCRIBE/EXPLAIN/WITH）")
		return
	}
	analysis := permission.AnalyzeSQL(sqlStr, schema)
	if permResult := permission.CheckAnalysisPermission(analysis, connId, authorization); !permResult.Allowed {
		response.WriteErr(c, 200, 500, permResult.Message)
		return
	}

	start := time.Now()
	written, exportErr := streamExport(c, connId, authorization, schema, sqlStr, format, filename)

	exportOp := audit.OpFromContext(c, "export", "exportText", "SELECT", "medium")
	exportOp.ConnID = connId
	exportOp.SchemaName = schema
	exportOp.Detail = fmt.Sprintf("format=%s, rows=%d, cost=%dms", format, written, time.Since(start).Milliseconds())
	if exportErr != nil {
		exportOp.Status = "failed"
		exportOp.ErrorMsg = exportErr.Error()
	}
	audit.GetAuditService().RecordOp(exportOp)

	if exportErr != nil {
		log.Printf("[ExportText] 导出失败 - connId=%s, format=%s, err=%v", connId, format, exportErr)
	}
}

// streamExport 逐行读取并直接写响应；返回成功写出的行数。
func streamExport(c *gin.Context, connId, authorization, schema, sqlStr, format, filename string) (int, error) {
	db := conn.GetConn(connId, authorization)
	if db == nil {
		response.WriteErr(c, 200, 500, "数据库连接不可用")
		return 0, fmt.Errorf("连接不可用")
	}

	rows, err := db.Queryx(sqlStr)
	if err != nil {
		response.WriteAppErr(c, apperror.FromDatabase(err))
		return 0, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		response.WriteErr(c, 200, 500, "读取列信息失败")
		return 0, err
	}
	cts, err := rows.ColumnTypes()
	if err != nil {
		response.WriteErr(c, 200, 500, "读取列类型失败")
		return 0, err
	}
	colTypes := make([]string, len(cts))
	for i, ct := range cts {
		colTypes[i] = ct.DatabaseTypeName()
	}

	contentType, ext := exportContentType(format)
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"."+ext+"\"")
	c.Status(http.StatusOK)

	writer := c.Writer
	// CSV 表头
	if format == "csv" {
		header := make([]any, len(columns))
		for i, name := range columns {
			header[i] = name
		}
		if _, err := writer.WriteString(csvLine(header)); err != nil {
			return 0, err
		}
	}
	if format == "json" {
		if _, err := writer.WriteString("[\n"); err != nil {
			return 0, err
		}
	}
	if format == "sql" {
		// 表名不可靠（查询可能是 JOIN/子查询），用导出文件名做占位表名
		if _, err := writer.WriteString(sqlCommentHeader(columns, filename) + "\n"); err != nil {
			return 0, err
		}
	}

	driverName := db.DriverName()
	values := make([]any, len(columns))
	scanArgs := make([]any, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	count := 0
	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return count, err
		}
		converted := make([]any, len(values))
		for i := range values {
			colType := colTypes[i]
			val := values[i]
			if out := dialect.ConvertValue(driverName, &colType, &val, false); out != nil {
				converted[i] = *out
			} else {
				converted[i] = nil
			}
		}

		var line string
		switch format {
		case "csv":
			line = csvLine(converted)
		case "json":
			obj := make(map[string]any, len(columns))
			for i, name := range columns {
				obj[name] = converted[i]
			}
			payload, marshalErr := json.Marshal(obj)
			if marshalErr != nil {
				return count, marshalErr
			}
			if count > 0 {
				line = ",\n"
			}
			line += string(payload)
		case "sql":
			line = sqlInsertLine(filename, columns, converted, driverName)
		}
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return count, err
		}
		count++
		// 分批 flush，避免大结果集在缓冲里堆积
		if count%exportTextFlushEvery == 0 {
			writer.Flush()
		}
	}
	if err := rows.Err(); err != nil {
		return count, err
	}

	if format == "json" {
		if _, err := writer.WriteString("\n]\n"); err != nil {
			return count, err
		}
	}
	writer.Flush()
	return count, nil
}

func exportContentType(format string) (contentType, ext string) {
	switch format {
	case "csv":
		return "text/csv; charset=utf-8", "csv"
	case "json":
		return "application/json; charset=utf-8", "json"
	default:
		return "text/plain; charset=utf-8", "sql"
	}
}

// csvLine 生成一行 CSV（按 RFC4180 需要时加引号）。
func csvLine(values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = csvEscape(cellText(v))
	}
	return strings.Join(parts, ",")
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// cellText 把单元格转成文本，NULL 输出空串。
func cellText(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case []byte:
		return string(t)
	case string:
		return t
	case time.Time:
		return t.Format(time.DateTime)
	default:
		return fmt.Sprint(v)
	}
}

func sqlCommentHeader(columns []string, table string) string {
	return "-- 导出结果集: " + table + "\n-- 列: " + strings.Join(columns, ", ")
}

// sqlInsertLine 生成单行 INSERT（值按方言转义）。
func sqlInsertLine(table string, columns []string, values []any, dbType string) string {
	quotedTable := quoteExportIdent(table, dbType)
	quotedCols := make([]string, len(columns))
	for i, c := range columns {
		quotedCols[i] = quoteExportIdent(c, dbType)
	}
	lits := make([]string, len(values))
	for i, v := range values {
		lits[i] = exportLiteral(v, dbType)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
		quotedTable, strings.Join(quotedCols, ", "), strings.Join(lits, ", "))
}

// quoteExportIdent 导出用标识符引用：Oracle 双引号，其余反引号。
func quoteExportIdent(name, dbType string) string {
	if dbType == "oracle" {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// exportLiteral 生成 SQL 字面量：NULL / 数字 / 布尔原样，字符串转义。
func exportLiteral(v any, dbType string) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case bool:
		if t {
			return "1"
		}
		return "0"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(t)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case time.Time:
		return "'" + t.Format(time.DateTime) + "'"
	}
	raw := cellText(v)
	escaped := strings.ReplaceAll(raw, "'", "''")
	if dbType == "mysql" || dbType == "mariadb" || dbType == "" {
		escaped = strings.ReplaceAll(escaped, `\`, `\\`)
	}
	return "'" + escaped + "'"
}
