package webapi

import (
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"net/http"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

func ExecSQL(w http.ResponseWriter, r *http.Request) {
	authorization := r.Header.Get("Authorization")

	r.ParseForm()
	connId := r.Form.Get("connId")
	// schema := r.Form.Get("schema")
	sqlStr := r.Form.Get("sql")
	maxLine := r.Form.Get("maxLine")
	sqlStr = strings.TrimSpace(sqlStr)

	conn := admin.GetConn(connId, authorization)

	if strings.HasPrefix(sqlStr, "create ") || strings.HasPrefix(sqlStr, "update ") || strings.HasPrefix(sqlStr, "delete ") || strings.HasPrefix(sqlStr, "insert ") || strings.HasPrefix(sqlStr, "alter ") || strings.HasPrefix(sqlStr, "CREATE ") || strings.HasPrefix(sqlStr, "UPDATE ") || strings.HasPrefix(sqlStr, "DELETE ") || strings.HasPrefix(sqlStr, "INSERT ") || strings.HasPrefix(sqlStr, "ALTER ") {
		rspData := TableDataList{Columns: []Column{{Name: "受影响行数", Type: "VARCHAR(10)"}}}
		rspData.Data = batchExec(&sqlStr, conn)
		utils.WriteJson(w, rspData)
	} else {
		params := make([]any, 0)
		if checkPrefx(sqlStr, []string{"select ", "SELECT ", "select\n", "SELECT\n"}) && !checkContains(sqlStr, []string{" limit ", " LIMIT ", "\nlimit\n", "\nLIMIT\n"}) {
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

		data := GetResultRows(conn.DriverName(), rows)

		rspData := TableDataList{Columns: columnList, Data: data}

		utils.WriteJson(w, rspData)
	}
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

func batchExec(sql *string, db *sqlx.DB) []map[string]any {
	sqlArr := strings.Split(*sql, ";")
	tx, err := db.DB.Begin()
	defer tx.Rollback()
	logutils.PanicErrf("事务开启失败， %s", err)
	resultData := []map[string]any{}
	for _, s := range sqlArr {
		relSql := utils.ExtractSql(s)
		if relSql == "" {
			continue
		}
		rs, err2 := tx.Exec(relSql)
		logutils.PanicErr(err2)
		affected, err := rs.RowsAffected()
		logutils.PanicErr(err)
		resultData = append(resultData, map[string]any{"受影响行数": affected})
	}
	err = tx.Commit()
	logutils.PanicErr(err)
	return resultData
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

func GetResultRows(dbtype string, rows *sqlx.Rows) []map[string]any {
	dataMaps := make([]map[string]any, 0)
	cts, err := rows.ColumnTypes()
	logutils.PanicErrf("获取字段类型失败", err)

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	// 1. 查询到的数据列名、返回值
	columns, _ := rows.Columns() //列名
	count := len(columns)

	values, valuesPoints := make([]any, count), make([]any, count)

	// 2. 遍历Rows读取每一行
	for rows.Next() {
		// for i, v := range values { // 这种写法获取不到地址
		// 	valuesPoints[i] = &v
		// }
		for i := 0; i < count; i++ {
			valuesPoints[i] = &values[i]
		}

		// 2.1 数据库中读取出每一行数据
		rows.Scan(valuesPoints...) //将所有内容读取进values

		// 2.2 相当于准备接收数据的结构体Product
		row := make(map[string]any)

		// 2.3 将读取到的数据填充到product
		for i, val := range values { // val是每个列对应的值
			key := columns[i] //列名
			// 列名与值对应
			row[key] = admin.ConvertColHandler[dbtype](colTypeMap[key], val)
		}
		// 将product归到集合中
		dataMaps = append(dataMaps, row)
	}
	return dataMaps
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type TableDataList struct {
	Columns []Column                 `json:"columns"`
	Data    []map[string]interface{} `json:"data"`
}
