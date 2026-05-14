package webapi

import (
	"bytes"
	"context"
	"fmt"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

type historyRecord struct {
	Id            string
	User          string
	ConnId        string
	OperationType string
	ExecTime      time.Time
	ExecSql       string
	Data          string
}

type asyncHistoryWriter struct {
	ch     chan *historyRecord
	once   sync.Once
	closed bool
	mu     sync.Mutex
}

var historyWriter = &asyncHistoryWriter{
	ch: make(chan *historyRecord, 4096),
}

func init() {
	go historyWriter.consume()
}

func (w *asyncHistoryWriter) enqueue(record *historyRecord) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	select {
	case w.ch <- record:
	default:
		logutils.PrintErrf("历史记录队列已满，丢弃记录: %s", nil, record.ExecSql[:min(100, len(record.ExecSql))])
	}
}

func (w *asyncHistoryWriter) consume() {
	batch := make([]*historyRecord, 0, 64)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case record, ok := <-w.ch:
			if !ok {
				w.flushBatch(batch)
				return
			}
			batch = append(batch, record)
			if len(batch) >= 64 {
				w.flushBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flushBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *asyncHistoryWriter) flushBatch(batch []*historyRecord) {
	if len(batch) == 0 || config.Mngtdb == nil {
		return
	}

	tx, err := config.Mngtdb.Beginx()
	if err != nil {
		logutils.PrintErrf("历史记录事务创建失败", err)
		return
	}

	hasData := false
	insertSQL := "insert into t_history (id,user,conn_id,operation_type,exec_time,exec_sql,data) values(?,?,?,?,?,?,?)"
	stmt, err := tx.Preparex(insertSQL)
	if err != nil {
		tx.Rollback()
		logutils.PrintErrf("历史记录 Prepare 失败", err)
		return
	}
	defer stmt.Close()

	for _, r := range batch {
		dataVal := r.Data
		if dataVal == "" {
			dataVal = "NULL"
		} else {
			hasData = true
		}
		_, err := stmt.Exec(r.Id, r.User, r.ConnId, r.OperationType, r.ExecTime, r.ExecSql, dataVal)
		if err != nil {
			logutils.PrintErrf("历史记录写入失败: %s", err, r.ExecSql[:min(100, len(r.ExecSql))])
		}
	}

	if err := tx.Commit(); err != nil {
		logutils.PrintErrf("历史记录事务提交失败", err)
	}

	_ = hasData
}

func ShutdownHistoryWriter() {
	historyWriter.mu.Lock()
	historyWriter.closed = true
	historyWriter.mu.Unlock()
	close(historyWriter.ch)
}

func ExecSQL(c *gin.Context) {

	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	tableName := c.PostForm("tableName")
	sqlStr := c.PostForm("sql")
	maxLine := c.PostForm("maxLine")
	sqlStr = strings.TrimSpace(sqlStr)

	// SQL 长度限制（防止超大 SQL 攻击）
	const maxSQLLength = 1024 * 1024 // 1MB
	if len(sqlStr) > maxSQLLength {
		c.JSON(200, gin.H{"code": 500, "msg": "SQL 语句过长，请拆分执行"})
		return
	}
	if sqlStr == "" {
		c.JSON(200, gin.H{"code": 500, "msg": "SQL 语句不能为空"})
		return
	}

	authorization := c.GetHeader("Authorization")
	conn := admin.GetConn(connId, authorization)
	userVal, _ := c.Get("currentUser")
	user, _ := userVal.(*admin.User)

	// 当 schema 为空时，从数据库连接获取实际 schema
	// 确保权限检查使用的 schema 与实际查询的 schema 一致
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

	analysis := admin.AnalyzeSQL(sqlStr, schema)
	permResult := admin.CheckAnalysisPermission(analysis, connId, authorization)
	if !permResult.Allowed {
		c.JSON(200, gin.H{"code": 500, "msg": permResult.Message})
		return
	}

	// 多条 SQL（分号分隔）时，逐条检查权限
	if strings.Contains(sqlStr, ";") {
		for _, singleSQL := range strings.Split(sqlStr, ";") {
			singleSQL = strings.TrimSpace(singleSQL)
			if singleSQL == "" {
				continue
			}
			subAnalysis := admin.AnalyzeSQL(singleSQL, schema)
			subResult := admin.CheckAnalysisPermission(subAnalysis, connId, authorization)
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

		// 查询超时控制（5 分钟）
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

		// 尝试从元数据中获取字段注释和主键信息
		// 适用于单表查询场景
		if IsAlphaNumeric(realTableName) && isSimpleQuery(sqlStr) {
			columnMap = admin.ColumnMapFiltered(strings.ToLower(realTableName), strings.ToLower(realSchema), connId, authorization, conn)
			keys = admin.QueryPrimaryKeyCached(connId, schema, realTableName, conn)
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

		if analysis.OperationType == "SELECT" {
			userPowerVal, _ := c.Get("userPower")
			userPower, _ := userPowerVal.(*admin.UserPower)
			rspData = filterResultByPermission(rspData, connId, analysis, authorization, userPower)
		}

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
		logutils.PrintErrf("备份数据查询失败", err)
		return
	}
	defer rows.Close()
	data := dbutils.GetResultRows(conn.DriverName(), rows)

	historyWriter.enqueue(&historyRecord{
		Id:            fmt.Sprintf("%d", time.Now().UnixMicro()),
		User:          user.LoginName,
		ConnId:        connId,
		OperationType: operationType,
		ExecTime:      time.Now(),
		ExecSql:       ddlSql,
		Data:          string(utils.ToJsonString(data)),
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

func filterResultByPermission(data *TableDataList, connId string, analysis *admin.SQLAnalysis, authorization string, userPower *admin.UserPower) *TableDataList {
	if !config.Cfg.IsRemote {
		return data
	}

	if userPower == nil {
		return data
	}
	if userPower.UserId == config.AdminId {
		return data
	}

	anyColumnLevel := false
	for _, t := range analysis.ReadTables {
		access := admin.GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		if access.Level == admin.AccessColumn {
			anyColumnLevel = true
			break
		}
	}

	if !anyColumnLevel {
		return data
	}

	allowedSet := make(map[string]bool)
	for _, t := range analysis.ReadTables {
		access := admin.GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
		if access.Level == admin.AccessFull {
			for _, col := range data.Columns {
				allowedSet[col.Name] = true
			}
		} else if access.Level == admin.AccessColumn {
			for _, col := range data.Columns {
				if access.AllowedColumns[col.Name] {
					allowedSet[col.Name] = true
				}
			}
		}
	}

	var filteredColumns []Column
	filteredKeys := make([]string, 0)
	for _, col := range data.Columns {
		if allowedSet[col.Name] {
			filteredColumns = append(filteredColumns, col)
		}
	}

	for _, k := range data.Keys {
		if allowedSet[k] {
			filteredKeys = append(filteredKeys, k)
		}
	}

	filteredData := make([]map[string]any, 0, len(data.Data))
	for _, row := range data.Data {
		filteredRow := make(map[string]any, len(filteredColumns))
		for k, v := range row {
			if allowedSet[k] {
				filteredRow[k] = v
			}
		}
		filteredData = append(filteredData, filteredRow)
	}

	canEdit := len(filteredKeys) == len(data.Keys) && len(data.Keys) > 0

	return &TableDataList{
		Columns: filteredColumns,
		Data:    filteredData,
		CanEdit: canEdit,
		Keys:    filteredKeys,
	}
}
