package webapi

import (
	"bytes"
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

func ExecSQL(c *gin.Context) {

	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	tableName := c.PostForm("tableName")
	sqlStr := c.PostForm("sql")
	maxLine := c.PostForm("maxLine")
	sqlStr = strings.TrimSpace(sqlStr)

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	user := admin.GetUser(authorization)

	blankIdx := strings.Index(sqlStr, " ")
	nlIdx := strings.Index(sqlStr, "\n")
	if nlIdx == -1 {
		nlIdx = len(sqlStr)
	}

	if checkPrefx(sqlStr, []string{"update", "delete"}) {
		if err := backup(sqlStr, user, connId, conn); err != nil {
			writeSQLError(c, err)
			return
		}
	} else {
		recordHistory(sqlStr, user, connId)
	}

	sqlStr = strings.Join([]string{sqlStr[0:min(blankIdx, nlIdx)], sqlStr[min(blankIdx, nlIdx):]}, "")

	if checkPrefx(sqlStr, []string{"update", "delete", "alter", "drop ", "insert", "create"}) {
		rspData := TableDataList{Columns: []Column{{Name: "受影响行数", Type: "VARCHAR(10)"}}}
		result, err := batchExec(&sqlStr, conn)
		if err != nil {
			writeSQLError(c, err)
			return
		}
		rspData.Data = result
		utils.WriteJson(c.Writer, rspData)
	} else {
		params := make([]any, 0)
		if checkPrefx(sqlStr, []string{"select"}) && !checkContains(sqlStr, []string{" limit ", " LIMIT ", "\nlimit\n", "\nLIMIT\n"}) {
			sqlStr = *page(conn.DriverName(), &sqlStr)
			maxLineI, _ := strconv.Atoi(maxLine)
			params = append(params, maxLineI)
		}

		var rows *sqlx.Rows
		var err2 error

		if len(params) > 0 {
			rows, err2 = conn.Queryx(sqlStr, params...)
		} else {
			rows, err2 = conn.Queryx(sqlStr)
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

		// 尝试从元数据中获取字段注释和主键信息
		// 适用于单表查询场景
		if IsAlphaNumeric(realTableName) && isSimpleQuery(sqlStr) {
			columnMap = admin.ColumnMap(strings.ToLower(realTableName), strings.ToLower(realSchema), conn)

			tx, err := conn.Beginx()
			if err == nil {
				defer tx.Rollback()
				keys, err = admin.QueryPrimaryKey(schema, realTableName, tx)
				if err != nil {
					keys = []string{}
				}
			}
		}

		for idx, val := range cts {
			columnNameList = append(columnNameList, val.Name())
			columnList[idx] = Column{Name: val.Name(), Type: val.DatabaseTypeName(), Comment: columnMap[val.Name()]}
		}

		if len(keys) != 0 {
			keyIdx = dbutils.KeyIdx(keys, columnNameList)
		}

		data := dbutils.GetResultRows(conn.DriverName(), rows)

		rspData := &TableDataList{Columns: columnList, Data: data, CanEdit: len(keyIdx) != 0, Keys: keys}

		utils.WriteJson(c.Writer, rspData)
	}
}

func writeSQLError(c *gin.Context, err error) {
	msg := err.Error()
	msg = utils.RedactCredentials(msg)
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func page(dbtype string, sql *string) *string {
	pageSql := ""
	if dbtype == "oracle" {
		pageSql = "select a.* from (" + *sql + ") a where rownum <= :1"
	} else if dbtype == "mysql" {
		pageSql = *sql + " limit ?"
	}
	return &pageSql
}

func batchExec(sql *string, db *sqlx.DB) ([]map[string]any, error) {
	sqlArr := strings.Split(*sql, ";")
	tx, err := db.Beginx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	resultData := []map[string]any{}
	mgntTx, _ := config.Mngtdb.Beginx()
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

func backup(ddlSql string, user *admin.User, connId string, conn *sqlx.DB) error {
	operationType := ""
	backupSql := bytes.NewBufferString("select * from ")
	lowerSql := strings.ToLower(ddlSql)

	if strings.HasPrefix(lowerSql, "update ") {
		operationType = "update"
		tmp := strings.TrimSpace(strings.TrimPrefix(lowerSql, "update "))
		spaceIdx := strings.Index(tmp, " ")
		if spaceIdx == -1 {
			return fmt.Errorf("无法解析 UPDATE 语句的表名")
		}
		backupSql.WriteString(tmp[:spaceIdx])
	} else if strings.HasPrefix(lowerSql, "delete ") {
		operationType = "delete"
		tmp := strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(lowerSql, "delete ")), "from ")
		spaceIdx := strings.Index(tmp, " ")
		if spaceIdx == -1 {
			return fmt.Errorf("无法解析 DELETE 语句的表名")
		}
		backupSql.WriteString(tmp[:spaceIdx])
	}

	whereIdx := strings.Index(lowerSql, " where ")
	if whereIdx == -1 {
		return fmt.Errorf("UPDATE/DELETE 操作必须包含 WHERE 条件")
	}
	backupSql.WriteString(ddlSql[whereIdx:])

	rows, err := conn.Queryx(backupSql.String())
	if err != nil {
		return err
	}
	defer rows.Close()
	data := dbutils.GetResultRows(conn.DriverName(), rows)
	backupInsertSql := "insert into t_history (id,user,conn_id,operation_type,exec_time,exec_sql,data) values(?,?,?,?,?,?,?)"
	stmt, err := config.Mngtdb.Preparex(backupInsertSql)
	if err != nil {
		return err
	}
	defer stmt.Close()
	time.Sleep(time.Microsecond)
	_, err3 := stmt.Exec(time.Now().UnixMicro(), user.LoginName, connId, operationType, time.Now(), ddlSql, string(utils.ToJsonString(data)))
	if err3 != nil {
		return err3
	}
	return nil
}

func recordHistory(ddlSql string, user *admin.User, connId string) {
	backupInsertSql := "insert into t_history (id,user,conn_id,operation_type,exec_time,exec_sql) values(?,?,?,?,?,?)"
	stmt, err := config.Mngtdb.Preparex(backupInsertSql)
	if err != nil {
		logutils.PrintErr(err)
		return
	}
	defer stmt.Close()
	time.Sleep(time.Microsecond)
	_, err3 := stmt.Exec(time.Now().UnixMicro(), user.LoginName, connId, "select", time.Now(), ddlSql)
	logutils.PrintErr(err3)
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

// isSimpleQuery 判断是否是简单查询（单表查询，无 JOIN、子查询等）
func isSimpleQuery(sqlStr string) bool {
	sqlUpper := strings.ToUpper(sqlStr)
	// 检查是否包含复杂查询关键字
	complexKeywords := []string{" JOIN ", " UNION ", " INTERSECT ", " EXCEPT ", " WITH "}
	for _, keyword := range complexKeywords {
		if strings.Contains(sqlUpper, keyword) {
			return false
		}
	}
	// 检查 FROM 子句数量
	fromCount := strings.Count(sqlUpper, " FROM ")
	if fromCount != 1 {
		return false
	}
	// 检查是否包含子查询
	selectCount := strings.Count(sqlUpper, " SELECT ")
	if selectCount > 1 {
		return false
	}
	return true
}

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
