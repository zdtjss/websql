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
	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/permission"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/jsonutil"
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

func ExecSQL(c *gin.Context) {

	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	tableName := c.PostForm("tableName")
	sqlStr := c.PostForm("sql")
	maxLine := c.PostForm("maxLine")
	sqlStr = strings.TrimSpace(sqlStr)

	const maxSQLLength = 1024 * 1024
	if len(sqlStr) > maxSQLLength {
		c.JSON(200, gin.H{"code": 500, "msg": "SQL 语句过长，请拆分执行"})
		return
	}
	if sqlStr == "" {
		c.JSON(200, gin.H{"code": 500, "msg": "SQL 语句不能为空"})
		return
	}

	authorization := c.GetHeader("Authorization")
	conn := conn.GetConn(connId, authorization)
	userVal, _ := c.Get("currentUser")
	user, _ := userVal.(*admin.User)

	if schema == "" {
		switch conn.DriverName() {
		case "mysql", "mariadb":
			conn.Get(&schema, "SELECT DATABASE()")
		case "oracle":
			conn.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		case "sqlite":
			schema = "main"
		}
	}

	analysis := permission.AnalyzeSQL(sqlStr, schema)
	permResult := permission.CheckAnalysisPermission(analysis, connId, authorization)
	if !permResult.Allowed {
		c.JSON(200, gin.H{"code": 500, "msg": permResult.Message})
		return
	}

	if strings.Contains(sqlStr, ";") {
		for _, singleSQL := range strings.Split(sqlStr, ";") {
			singleSQL = strings.TrimSpace(singleSQL)
			if singleSQL == "" {
				continue
			}
			subAnalysis := permission.AnalyzeSQL(singleSQL, schema)
			subResult := permission.CheckAnalysisPermission(subAnalysis, connId, authorization)
			if !subResult.Allowed {
				c.JSON(200, gin.H{"code": 500, "msg": subResult.Message})
				return
			}
		}
	}

	blankIdx := strings.Index(sqlStr, " ")
	nlIdx := strings.Index(sqlStr, "\n")
	if nlIdx == -1 {
		nlIdx = len(sqlStr)
	}

	if checkPrefx(sqlStr, []string{"update", "delete"}) {
		go asyncBackup(sqlStr, user, connId, conn)
	} else {
		asyncRecordHistory(sqlStr, user, connId)
	}

	sqlStr = sqlStr[0:min(blankIdx, nlIdx)] + sqlStr[min(blankIdx, nlIdx):]

	if checkPrefx(sqlStr, []string{"update", "delete", "alter", "drop ", "insert", "create"}) {
		rspData := TableDataList{Columns: []Column{{Name: "受影响行数", Type: "VARCHAR(10)"}}}
		result, err := batchExec(sqlStr, conn)
		if err != nil {
			writeSQLError(c, err)
			return
		}
		rspData.Data = result
		jsonutil.WriteJson(c.Writer, rspData)
	} else {
		params := make([]any, 0)
		if checkPrefx(sqlStr, []string{"select"}) && !checkContains(sqlStr, []string{" limit ", " LIMIT ", "\nlimit\n", "\nLIMIT\n"}) {
			sqlStr = page(conn.DriverName(), sqlStr)
			maxLineI, _ := strconv.Atoi(maxLine)
			params = append(params, maxLineI)
		}

		var rows *sqlx.Rows
		var err2 error

		queryCtx, queryCancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
		defer queryCancel()

		if len(params) > 0 {
			rows, err2 = conn.QueryxContext(queryCtx, sqlStr, params...)
		} else {
			rows, err2 = conn.QueryxContext(queryCtx, sqlStr)
		}

		if err2 != nil {
			writeSQLError(c, err2)
			return
		}
		defer rows.Close()

		cts, err3 := rows.ColumnTypes()
		if err3 != nil {
			writeSQLError(c, err3)
			return
		}
		columnList := make([]Column, len(cts))
		columnNameList := make([]string, 0)

		var realTableName, realSchema = tableName, schema
		if strings.Contains(tableName, ".") {
			realTableName = string(tableName[strings.Index(tableName, ".")+1:])
			realSchema = string(tableName[0:strings.Index(tableName, ".")])
		}
		var keyIdx []int
		var keys []string
		columnMap := map[string]string{}

		if IsAlphaNumeric(realTableName) && isSimpleQuery(sqlStr) {
			columnMap = dbops.ColumnMapFiltered(strings.ToLower(realTableName), strings.ToLower(realSchema), connId, authorization, conn)
			keys = dbops.QueryPrimaryKeyCached(connId, schema, realTableName, conn)
		}

		for idx, val := range cts {
			columnNameList = append(columnNameList, val.Name())
			columnList[idx] = Column{Name: val.Name(), Type: val.DatabaseTypeName(), Comment: columnMap[val.Name()]}
		}

		if len(keys) != 0 {
			keyIdx = database.KeyIdx(keys, columnNameList)
		}

		data := database.GetResultRows(conn.DriverName(), rows)

		rspData := &TableDataList{Columns: columnList, Data: data, CanEdit: len(keyIdx) != 0, Keys: keys}

		jsonutil.WriteJson(c.Writer, rspData)
	}
}

func writeSQLError(c *gin.Context, err error) {
	msg := err.Error()
	msg = sanitize.RedactCredentials(msg)
	if len(msg) > 500 {
		msg = msg[:500] + "..."
	}
	c.JSON(200, gin.H{
		"code": 500,
		"msg":  msg,
	})
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
	sqlArr := strings.Split(sql, ";")
	tx, err := db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	resultData := []map[string]any{}
	mgntTx, _ := database.Mngtdb.Beginx()
	defer mgntTx.Rollback()
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
	err = mgntTx.Commit()
	if err != nil {
		return nil, err
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
		spaceIdx := strings.Index(tmp, " ")
		if spaceIdx == -1 {
			return
		}
		backupSql.WriteString(tmp[:spaceIdx])
	} else if strings.HasPrefix(lowerSql, "delete ") {
		operationType = "delete"
		tmp := strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(lowerSql, "delete ")), "from ")
		spaceIdx := strings.Index(tmp, " ")
		if spaceIdx == -1 {
			return
		}
		backupSql.WriteString(tmp[:spaceIdx])
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
	data := database.GetResultRows(conn.DriverName(), rows)

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