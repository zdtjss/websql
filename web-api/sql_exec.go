package webapi

import (
	"bytes"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	dbutils "go-web/utils/db"
	admin "go-web/web-api/admin"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

func ExecSQL(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()
	connId := r.Form.Get("connId")
	sqlStr := r.Form.Get("sql")
	maxLine := r.Form.Get("maxLine")
	sqlStr = strings.TrimSpace(sqlStr)

	authorization := r.Header.Get("Authorization")
	conn := admin.GetConn(connId, authorization)
	user := admin.GetUser(authorization)

	blankIdx := strings.Index(sqlStr, " ")
	nlIdx := strings.Index(sqlStr, "\n")

	// 关键字转小写 select delete update
	sqlStr = strings.Join([]string{strings.ToLower(sqlStr[0:min(blankIdx, nlIdx)]), sqlStr[min(blankIdx, nlIdx):]}, "")

	if strings.HasPrefix(sqlStr, "create ") || strings.HasPrefix(sqlStr, "update ") || strings.HasPrefix(sqlStr, "delete ") || strings.HasPrefix(sqlStr, "insert ") || strings.HasPrefix(sqlStr, "alter ") || strings.HasPrefix(sqlStr, "drop ") {
		rspData := TableDataList{Columns: []Column{{Name: "受影响行数", Type: "VARCHAR(10)"}}}
		rspData.Data = batchExec(&sqlStr, conn, user, connId)
		utils.WriteJson(w, rspData)
	} else {
		params := make([]any, 0)
		if checkPrefx(sqlStr, []string{"select ", "select\n"}) && !checkContains(sqlStr, []string{" limit ", " LIMIT ", "\nlimit\n", "\nLIMIT\n"}) {
			sqlStr = *page(conn.DriverName(), &sqlStr)
			maxLineI, _ := strconv.Atoi(maxLine)
			params = append(params, maxLineI)
		}
		rows, err2 := conn.Queryx(sqlStr, params...)
		logutils.PanicErr(err2)
		cts, err3 := rows.ColumnTypes()
		logutils.PanicErr(err3)
		columnList := make([]Column, len(cts))
		for idx, val := range cts {
			columnList[idx] = Column{Name: val.Name(), Type: val.DatabaseTypeName()}
		}

		data := dbutils.GetResultRows(conn.DriverName(), rows)

		rspData := &TableDataList{Columns: columnList, Data: data}

		utils.WriteJson(w, rspData)
	}
}

func min(a, b int) int {
	if a > b {
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

func batchExec(sql *string, db *sqlx.DB, user *admin.User, connId string) []map[string]any {
	sqlArr := strings.Split(*sql, ";")
	tx, err := db.Beginx()
	defer tx.Rollback()
	logutils.PanicErrf("事务开启失败， %s", err)
	resultData := []map[string]any{}
	mgntTx, _ := config.Mngtdb.Beginx()
	defer mgntTx.Rollback()
	for idx := range sqlArr {
		sqlStr := strings.TrimSpace(sqlArr[idx])
		if sqlStr == "" {
			continue
		}
		if strings.HasPrefix(sqlStr, "update ") || strings.HasPrefix(sqlStr, "delete ") {
			backup(sqlStr, user, tx, mgntTx, connId)
		}
		rs, err2 := tx.Exec(sqlStr)
		logutils.PanicErr(err2)
		affected, err := rs.RowsAffected()
		logutils.PanicErr(err)
		resultData = append(resultData, map[string]any{"受影响行数": affected})
	}
	err = mgntTx.Commit()
	logutils.PanicErr(err)
	err = tx.Commit()
	logutils.PanicErr(err)
	return resultData
}

func backup(ddlSql string, user *admin.User, datTtx, mgntTx *sqlx.Tx, connId string) {
	backupSql := bytes.NewBufferString("select * from ")
	if strings.HasPrefix(ddlSql, "update ") {
		tmp := strings.TrimSpace(strings.TrimPrefix(ddlSql, "update "))
		backupSql.WriteString(tmp[:strings.Index(tmp, " ")])
	} else if strings.HasPrefix(ddlSql, "delete ") {
		tmp := strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(ddlSql, "delete ")), "from ")
		backupSql.WriteString(tmp[:strings.Index(tmp, " ")])
	}
	backupSql.WriteString(ddlSql[strings.Index(ddlSql, " where "):])
	rows, err := datTtx.Queryx(backupSql.String())
	logutils.PanicErr(err)
	data := dbutils.GetResultRows(datTtx.DriverName(), rows)
	backupInsertSql := "insert into t_backup (id,user,conn_id,exec_time,exec_sql,data) values(?,?,?,?,?,?)"
	stmt, err := mgntTx.Preparex(backupInsertSql)
	logutils.PanicErr(err)
	time.Sleep(time.Microsecond)
	_, err3 := stmt.Exec(time.Now().UnixMicro(), user.LoginName, connId, time.Now(), ddlSql, string(utils.ToJsonString(data)))
	logutils.PanicErr(err3)
}

func checkPrefx(src string, prefix []string) bool {
	for _, p := range prefix {
		if strings.HasPrefix(src, p) {
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

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type TableDataList struct {
	Columns []Column                 `json:"columns"`
	Data    []map[string]interface{} `json:"data"`
}
