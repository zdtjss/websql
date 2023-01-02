package webapi

import (
	"database/sql"
	"go-web/utils"
	"net/http"
	"strconv"
	"strings"
)

func ExecSQL(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	connId := r.Form.Get("connId")
	// schema := r.Form.Get("schema")
	sqlStr := r.Form.Get("sql")
	maxLine := r.Form.Get("maxLine")

	sqlStr = strings.TrimSpace(sqlStr)

	params := make([]any, 0)
	if strings.LastIndex(sqlStr, " limit ") == -1 {
		sqlStr = sqlStr + " limit ?"
		maxLineI, _ := strconv.Atoi(maxLine)
		params = append(params, maxLineI)
	}

	if strings.HasPrefix(sqlStr, "select ") {
		rows, err2 := getConn(connId).Query(sqlStr, params...)
		utils.Panicln(err2)
		cts, err3 := rows.ColumnTypes()
		utils.Panicln(err3)
		columnList := make([]Column, len(cts))
		for idx, val := range cts {
			columnList[idx] = Column{Name: val.Name(), Type: val.DatabaseTypeName()}
		}

		data := GetResultRows(rows)

		rspData := TableDataList{Columns: columnList, Data: data}

		utils.WriteJson(w, rspData)
	} else {
		rs, err2 := getConn(connId).Exec(sqlStr, params...)
		utils.Panicln(err2)
		affected, err := rs.RowsAffected()
		utils.Panicln(err)
		rspData := TableDataList{Columns: []Column{{Name: "受影响行数", Type: "VARCHAR(10)"}}, Data: []map[string]interface{}{{"受影响行数": affected}}}
		utils.WriteJson(w, rspData)
	}

}

func GetResultRows(rows *sql.Rows) []map[string]interface{} {

	dataMaps := make([]map[string]interface{}, 0)
	// 1. 查询到的数据列名、返回值
	columns, _ := rows.Columns() //列名
	count := len(columns)
	values, valuesPoints := make([]interface{}, count), make([]interface{}, count)

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
		row := make(map[string]interface{})

		// 2.3 将读取到的数据填充到product
		for i, val := range values { // val是每个列对应的值
			key := columns[i] //列名

			// 判断val的值的类型
			var v interface{}
			b, ok := val.([]byte) //判断是否为[]byte
			if ok {
				v = string(b)
			} else {
				v = val
			}

			// 列名与值对应
			row[key] = v // row["ID"] = 3, row["Name"] = "笨猪"
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
