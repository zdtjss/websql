package sql

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"websql/internal/app/admin"
	"websql/internal/app/dbops"
	"websql/internal/audit"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"
	"websql/internal/pkg/sanitize"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type Column struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Comment string `json:"comment"`
}

type TableDataList struct {
	Columns []Column         `json:"columns"`
	Data    []map[string]any `json:"data"`
	CanEdit bool             `json:"canEdit"`
	Keys    []string         `json:"keys"`
}

type SQLResultItem struct {
	SQL        string           `json:"sql"`
	Status     string           `json:"status"`
	Type       string           `json:"type"`
	Error      string           `json:"error,omitempty"`
	AuditError string           `json:"-"`
	Columns    []Column         `json:"columns,omitempty"`
	Data       []map[string]any `json:"data,omitempty"`
	CanEdit    bool             `json:"canEdit,omitempty"`
	Keys       []string         `json:"keys,omitempty"`
	Affected   int64            `json:"affected,omitempty"`
}

type BatchSQLResult struct {
	Results   []SQLResultItem `json:"results"`
	TotalTime int64           `json:"totalTime"`
}

func ExecSQL(c *gin.Context) {
	req := &ExecRequest{
		ConnID:        appctx.Ctx.GetConnID(c),
		Schema:        c.PostForm("schema"),
		TableName:     c.PostForm("tableName"),
		SQL:           c.PostForm("sql"),
		MaxLine:       c.PostForm("maxLine"),
		Batch:         c.PostForm("batch"),
		IsFile:        c.PostForm("isFile") == "true",
		Authorization: appctx.Ctx.GetAuthorization(c),
		ClientIP:      c.ClientIP(),
	}
	if userVal, _ := c.Get("currentUser"); userVal != nil {
		if user, ok := userVal.(*admin.User); ok {
			req.UserID = user.Id
			req.User = user
		}
	}

	result, err := ensureDefaultExec().Exec(c.Request.Context(), req)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	response.WriteOK(c, result)
}

func IsAlphaNumeric(str string) bool {
	for _, ch := range str {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			return true
		}
	}
	return false
}

func page(dbtype string, sql string) string {
	if dbtype == "oracle" {
		return "select a.* from (" + sql + ") a where rownum <= :1"
	} else if dbtype == "mysql" {
		return sql + " limit ?"
	}
	return sql
}

func batchExec(sql string, db *sqlx.DB) ([]map[string]any, error) {
	sqlArr := splitSQLRespectQuotes(sql)
	tx, err := db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	resultData := []map[string]any{}
	for idx := range sqlArr {
		sqlStr := strings.TrimSpace(sqlArr[idx])
		if sqlStr == "" {
			continue
		}
		rs, err2 := tx.Exec(sqlStr)
		if err2 != nil {
			return nil, err2
		}
		affected, err := rs.RowsAffected()
		if err != nil {
			return nil, err
		}
		resultData = append(resultData, map[string]any{"受影响行数": affected})
	}
	err = tx.Commit()
	if err != nil {
		return nil, err
	}
	return resultData, nil
}

func asyncBackup(ddlSql string, user *admin.User, connId string, conn *sqlx.DB) {
	operationType := ""
	backupSql := bytes.NewBufferString("select * from ")
	lowerSql := strings.ToLower(ddlSql)

	if strings.HasPrefix(lowerSql, "update ") {
		operationType = "update"
		tmp := strings.TrimSpace(strings.TrimPrefix(lowerSql, "update "))
		tableName := extractTableToken(tmp)
		if tableName == "" {
			return
		}
		backupSql.WriteString(ddlSql[len("update ") : len("update ")+len(tableName)])
	} else if strings.HasPrefix(lowerSql, "delete ") {
		operationType = "delete"
		tmp := strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(lowerSql, "delete ")), "from ")
		tableName := extractTableToken(tmp)
		if tableName == "" {
			return
		}
		// 找到原始 SQL 中对应的表名位置
		fromIdx := strings.Index(lowerSql, "from ")
		if fromIdx == -1 {
			return
		}
		tableStart := fromIdx + 5
		origTmp := strings.TrimSpace(ddlSql[tableStart:])
		origTableName := extractTableToken(origTmp)
		if origTableName == "" {
			return
		}
		backupSql.WriteString(origTableName)
	}

	whereIdx := strings.Index(lowerSql, " where ")
	if whereIdx == -1 {
		return
	}
	backupSql.WriteString(ddlSql[whereIdx:])

	rows, err := conn.Queryx(backupSql.String())
	if err != nil {
		logger.PrintErrf("备份数据查询失败", err)
		return
	}
	defer rows.Close()
	data, dataErr := database.GetResultRows(conn.DriverName(), rows)
	if dataErr != nil {
		logger.PrintErrf("备份数据读取失败", dataErr)
		return
	}

	historyWriter.enqueue(&historyRecord{
		Id:            fmt.Sprintf("%d", time.Now().UnixMicro()),
		User:          user.LoginName,
		ConnId:        connId,
		OperationType: operationType,
		ExecTime:      time.Now(),
		ExecSql:       ddlSql,
		Data:          string(jsonutil.ToJsonString(data)),
	})
}

// extractTableToken 从 SQL 片段中提取表名 token，支持反引号包裹的 schema.table 格式
func extractTableToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var result strings.Builder
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == '`' {
			// 读取反引号包裹的标识符
			result.WriteByte(ch)
			i++
			for i < len(s) && s[i] != '`' {
				result.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				result.WriteByte(s[i]) // closing backtick
				i++
			}
		} else if ch == '.' {
			result.WriteByte(ch)
			i++
		} else if ch == ' ' || ch == '\n' || ch == '\t' || ch == '\r' {
			break
		} else {
			result.WriteByte(ch)
			i++
		}
	}
	return result.String()
}

func asyncRecordHistory(ddlSql string, user *admin.User, connId string) {
	historyWriter.enqueue(&historyRecord{
		Id:            fmt.Sprintf("%d", time.Now().UnixMicro()),
		User:          user.LoginName,
		ConnId:        connId,
		OperationType: "select",
		ExecTime:      time.Now(),
		ExecSql:       ddlSql,
		Data:          "",
	})
}

func checkPrefx(src string, prefix []string) bool {
	src = strings.ToUpper(src)
	for _, p := range prefix {
		p = strings.ToUpper(p)
		if strings.HasPrefix(src, p+" ") || strings.HasPrefix(src, p+"\n") {
			return true
		}
	}
	return false
}

func checkContains(src string, suffix []string) bool {
	for _, p := range suffix {
		if strings.LastIndex(src, p) != -1 {
			return true
		}
	}
	return false
}

func isSimpleQuery(sqlStr string) bool {
	sqlUpper := strings.ToUpper(sqlStr)
	complexKeywords := []string{" JOIN ", " UNION ", " INTERSECT ", " EXCEPT ", " WITH "}
	for _, keyword := range complexKeywords {
		if strings.Contains(sqlUpper, keyword) {
			return false
		}
	}
	fromCount := strings.Count(sqlUpper, " FROM ")
	if fromCount != 1 {
		return false
	}
	selectCount := strings.Count(sqlUpper, " SELECT ")
	if selectCount > 1 {
		return false
	}
	return true
}

func detectSQLTypeForEditor(sql string) string {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	prefixes := []string{"SELECT", "SHOW", "DESCRIBE", "EXPLAIN", "WITH",
		"INSERT", "UPDATE", "DELETE", "DROP", "TRUNCATE", "ALTER", "CREATE", "REPLACE", "MERGE"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(upper, prefix) {
			return prefix
		}
	}
	return "OTHER"
}

func detectRiskLevelForEditor(sql string) string {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(upper, "DROP") || strings.HasPrefix(upper, "TRUNCATE") {
		return "high"
	}
	if strings.HasPrefix(upper, "DELETE") || strings.HasPrefix(upper, "ALTER") {
		if !strings.Contains(upper, "WHERE") {
			return "high"
		}
		return "medium"
	}
	if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "SHOW") ||
		strings.HasPrefix(upper, "DESCRIBE") || strings.HasPrefix(upper, "EXPLAIN") {
		return "low"
	}
	return "medium"
}

func splitSQL(sql string) []string {
	return splitSQLRespectQuotes(sql)
}

// splitSQLRespectQuotes 感知引号和注释的 SQL 分割器
// 正确处理字符串常量中的分号、注释中的分号
func splitSQLRespectQuotes(sql string) []string {
	var results []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(sql); i++ {
		c := sql[i]

		// 处理行注释
		if inLineComment {
			current.WriteByte(c)
			if c == '\n' {
				inLineComment = false
			}
			continue
		}

		// 处理块注释
		if inBlockComment {
			current.WriteByte(c)
			if c == '*' && i+1 < len(sql) && sql[i+1] == '/' {
				current.WriteByte('/')
				i++
				inBlockComment = false
			}
			continue
		}

		// 处理单引号字符串
		if inSingleQuote {
			current.WriteByte(c)
			if c == '\'' {
				// 检查转义的引号 ''
				if i+1 < len(sql) && sql[i+1] == '\'' {
					current.WriteByte('\'')
					i++
				} else {
					inSingleQuote = false
				}
			} else if c == '\\' && i+1 < len(sql) {
				// MySQL 风格的反斜杠转义
				current.WriteByte(sql[i+1])
				i++
			}
			continue
		}

		// 处理双引号标识符
		if inDoubleQuote {
			current.WriteByte(c)
			if c == '"' {
				if i+1 < len(sql) && sql[i+1] == '"' {
					current.WriteByte('"')
					i++
				} else {
					inDoubleQuote = false
				}
			}
			continue
		}

		// 正常模式
		switch {
		case c == ';':
			s := strings.TrimSpace(current.String())
			if s != "" {
				results = append(results, s)
			}
			current.Reset()
		case c == '\'':
			inSingleQuote = true
			current.WriteByte(c)
		case c == '"':
			inDoubleQuote = true
			current.WriteByte(c)
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			inLineComment = true
			current.WriteByte(c)
		case c == '#':
			inLineComment = true
			current.WriteByte(c)
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			inBlockComment = true
			current.WriteByte(c)
			current.WriteByte('*')
			i++
		default:
			current.WriteByte(c)
		}
	}

	s := strings.TrimSpace(current.String())
	if s != "" {
		results = append(results, s)
	}
	return results
}

func extractTableNameFromSQL(sqlStr string) string {
	lowerSql := strings.ToLower(strings.TrimSpace(sqlStr))
	if !strings.HasPrefix(lowerSql, "select ") && !strings.HasPrefix(lowerSql, "select\n") {
		return ""
	}
	fromIdx := strings.Index(lowerSql, " from ")
	if fromIdx == -1 {
		fromIdx = strings.Index(lowerSql, "\nfrom\n")
		if fromIdx == -1 {
			return ""
		}
		fromIdx += 6
	} else {
		fromIdx += 6
	}
	tableNameArr := strings.Builder{}
	for i := fromIdx; i < len(sqlStr); i++ {
		ch := sqlStr[i]
		if ch != ' ' && ch != '\n' {
			tableNameArr.WriteByte(ch)
		} else if tableNameArr.Len() > 0 {
			break
		}
	}
	return tableNameArr.String()
}

func execSingleSQLCore(sqlStr string, conn *sqlx.DB, tx *sqlx.Tx, schema, tableName, maxLine string, user *admin.User, connId, authorization string, queryCtx context.Context) *SQLResultItem {
	result := &SQLResultItem{
		SQL: sqlStr,
	}

	if checkPrefx(sqlStr, []string{"update", "delete"}) {
		safego.GoWithName("sql-async-backup", func() {
			asyncBackup(sqlStr, user, connId, conn)
		})
	} else {
		asyncRecordHistory(sqlStr, user, connId)
	}

	if checkPrefx(sqlStr, []string{"update", "delete", "alter", "drop", "insert", "create", "truncate", "replace", "merge"}) {
		result.Type = "modify"
		var affected int64
		if tx != nil {
			rs, err := tx.ExecContext(queryCtx, sqlStr)
			if err != nil {
				result.Status = "error"
				result.AuditError = audit.FormatErrorWithStack(err)
				msg := err.Error()
				msg = sanitize.RedactCredentials(msg)
				if len(msg) > 500 {
					msg = msg[:500] + "..."
				}
				result.Error = msg
				return result
			}
			affected, _ = rs.RowsAffected()
		} else {
			rs, err := conn.ExecContext(queryCtx, sqlStr)
			if err != nil {
				result.Status = "error"
				result.AuditError = audit.FormatErrorWithStack(err)
				msg := err.Error()
				msg = sanitize.RedactCredentials(msg)
				if len(msg) > 500 {
					msg = msg[:500] + "..."
				}
				result.Error = msg
				return result
			}
			affected, _ = rs.RowsAffected()
		}
		result.Status = "success"
		result.Affected = affected
		result.Columns = []Column{{Name: "受影响行数", Type: "VARCHAR(10)"}}
		result.Data = []map[string]any{{"受影响行数": affected}}
	} else {
		result.Type = "query"
		params := make([]any, 0)
		execSQL := sqlStr
		if checkPrefx(sqlStr, []string{"select"}) && !checkContains(sqlStr, []string{" limit ", " LIMIT ", "\nlimit\n", "\nLIMIT\n"}) {
			maxLineI, _ := strconv.Atoi(maxLine)
			if maxLineI > 0 {
				execSQL = page(conn.DriverName(), sqlStr)
				params = append(params, maxLineI)
			}
		}

		var rows *sqlx.Rows
		var err error
		if tx != nil {
			if len(params) > 0 {
				rows, err = tx.QueryxContext(queryCtx, execSQL, params...)
			} else {
				rows, err = tx.QueryxContext(queryCtx, execSQL)
			}
		} else {
			if len(params) > 0 {
				rows, err = conn.QueryxContext(queryCtx, execSQL, params...)
			} else {
				rows, err = conn.QueryxContext(queryCtx, execSQL)
			}
		}

		if err != nil {
			result.Status = "error"
			result.AuditError = audit.FormatErrorWithStack(err)
			msg := err.Error()
			msg = sanitize.RedactCredentials(msg)
			if len(msg) > 500 {
				msg = msg[:500] + "..."
			}
			result.Error = msg
			return result
		}
		defer rows.Close()

		cts, err3 := rows.ColumnTypes()
		if err3 != nil {
			result.Status = "error"
			result.AuditError = audit.FormatErrorWithStack(err3)
			msg := err3.Error()
			msg = sanitize.RedactCredentials(msg)
			if len(msg) > 500 {
				msg = msg[:500] + "..."
			}
			result.Error = msg
			return result
		}

		columnList := make([]Column, len(cts))
		columnNameList := make([]string, 0)

		stmtTableName := extractTableNameFromSQL(sqlStr)
		if stmtTableName == "" {
			stmtTableName = tableName
		}
		var realTableName, realSchema = stmtTableName, schema
		if strings.Contains(stmtTableName, ".") {
			realTableName = string(stmtTableName[strings.Index(stmtTableName, ".")+1:])
			realSchema = string(stmtTableName[0:strings.Index(stmtTableName, ".")])
		}

		var keyIdx []int
		var keys []string
		columnMap := map[string]string{}

		if IsAlphaNumeric(realTableName) && isSimpleQuery(sqlStr) {
			keys = dbops.QueryPrimaryKeyCached(connId, schema, realTableName, conn)
			columnMap = dbops.ColumnMapFiltered(strings.ToLower(realTableName), strings.ToLower(realSchema), connId, authorization, conn)
		}

		for idx, val := range cts {
			columnNameList = append(columnNameList, val.Name())
			columnList[idx] = Column{Name: val.Name(), Type: val.DatabaseTypeName(), Comment: columnMap[val.Name()]}
		}

		if len(keys) != 0 {
			keyIdx = database.KeyIdx(keys, columnNameList)
		}

		data, dataErr := database.GetResultRows(conn.DriverName(), rows)
		if dataErr != nil {
			result.Status = "error"
			result.AuditError = audit.FormatErrorWithStack(dataErr)
			msg := dataErr.Error()
			msg = sanitize.RedactCredentials(msg)
			if len(msg) > 500 {
				msg = msg[:500] + "..."
			}
			result.Error = msg
			return result
		}

		result.Status = "success"
		result.Columns = columnList
		result.Data = data
		result.CanEdit = len(keyIdx) != 0
		result.Keys = keys
	}

	return result
}
