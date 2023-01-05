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

	if strings.HasPrefix(sqlStr, "create ") || strings.HasPrefix(sqlStr, "update ") || strings.HasPrefix(sqlStr, "delete ") || strings.HasPrefix(sqlStr, "insert ") || strings.HasPrefix(sqlStr, "alter ") || strings.HasPrefix(sqlStr, "CREATE ") || strings.HasPrefix(sqlStr, "UPDATE ") || strings.HasPrefix(sqlStr, "DELETE ") || strings.HasPrefix(sqlStr, "INSERT ") || strings.HasPrefix(sqlStr, "ALTER ") {
		sqlArr := strings.Split(sqlStr, ";")
		tx, err := getConn(connId).DB.Begin()
		defer tx.Rollback()
		utils.Panicf("事务开启失败， %s", err)
		rspData := TableDataList{Columns: []Column{{Name: "受影响行数", Type: "VARCHAR(10)"}}}
		resultData := []map[string]any{}
		for _, relSql := range sqlArr {
			if relSql == "" {
				continue
			}
			rs, err2 := tx.Exec(relSql, params...)
			utils.Panicln(err2)
			affected, err := rs.RowsAffected()
			utils.Panicln(err)
			resultData = append(resultData, map[string]any{"受影响行数": affected})
		}
		tx.Commit()
		rspData.Data = resultData
		utils.WriteJson(w, rspData)
	} else {
		if (strings.HasPrefix(sqlStr, "select ") || strings.HasPrefix(sqlStr, "SELECT ")) && (strings.Contains(sqlStr, " from ") || strings.Contains(sqlStr, " FROM ")) && (strings.LastIndex(sqlStr, " limit ") == -1 || strings.LastIndex(sqlStr, " LIMIT ") == -1) {
			sqlStr = sqlStr + " limit ?"
			maxLineI, _ := strconv.Atoi(maxLine)
			params = append(params, maxLineI)
		}
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
	}

}

func GetResultRows(rows *sql.Rows) []map[string]interface{} {

	dataMaps := make([]map[string]interface{}, 0)
	cts, err := rows.ColumnTypes()
	utils.Panicf("获取字段类型失败，%x", err)

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	// 1. 查询到的数据列名、返回值
	columns, _ := rows.Columns() //列名
	count := len(columns)
	// values, valuesPoints := make([]interface{}, count), make([]interface{}, count)

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
				switch colTypeMap[key] {
				case "TINYINT", "SMALLINT", "MEDIUMINT", "INT":
					iv, err := strconv.ParseInt(string(b), 10, 32)
					utils.Printf("转换类型失败， %x", err)
					v = int(iv)
				case "BIGINT":
					iv, err := strconv.ParseInt(string(b), 10, 64)
					utils.Printf("转换类型失败， %x", err)
					v = iv
				case "FLOAT":
					iv, err := strconv.ParseFloat(string(b), 32)
					utils.Printf("转换类型失败， %x", err)
					v = float32(iv)
				case "DOUBLE", "DECIMAL":
					iv, err := strconv.ParseFloat(string(b), 64)
					utils.Printf("转换类型失败， %x", err)
					v = iv
				case "BIT":
					v = b[0] == byte(1)
				default:
					v = string(b)
				}
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
