package webapi

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"go-web/config"
	"go-web/utils"
	"io"
	"log"
	"net/http"
)

func ExportCsv(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	table := r.Form.Get("table")
	dbParam := &config.DBParam{Env: r.Form.Get("env"), Db: r.Form.Get("db")}
	w.Header().Add("content-type", "application/octet-stream")
	w.Header().Add("content-disposition", "attachment;filename="+table+".csv")
	queryAndWrite(table, w, dbParam)
}

func queryAndWrite(table string, out io.Writer, dbParam *config.DBParam) {
	log.Println("正在导出：", table)
	rows, err := config.GetConn(dbParam).Query(fmt.Sprintf("SELECT * from %s", table))
	utils.Panicln(err)

	columns, err := rows.Columns()
	utils.Panicln(err)

	columnCommons := make([]string, len(columns))
	columnMap := columnMap(table, dbParam)
	for i := 0; i < len(columns); i++ {
		columnCommons[i] = columnMap[columns[i]]
	}

	writeToCSV(out, [][]string{columns, columnCommons})

	//values：一行的所有值,把每一行的各个字段放到values中，values长度==列数
	values := make([]sql.RawBytes, len(columns))

	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	count := -1
	maxLines := 100

	//存所有行的内容totalValues
	totalValues := make([][]string, maxLines)
	for rows.Next() {

		count++

		//存每一行的内容
		var s []string

		//把每行的内容添加到scanArgs，也添加到了values
		err = rows.Scan(scanArgs...)
		utils.Panicln(err)

		for _, v := range values {
			s = append(s, string(v))
		}
		totalValues[count] = s
		if count+1 >= maxLines {
			writeToCSV(out, totalValues)
			count = -1
		}
	}

	if err = rows.Err(); err != nil {
		panic(err.Error())
	}

	if count != -1 {
		writeToCSV(out, totalValues[:count+1])
	}
}

func columnMap(table string, dbParam *config.DBParam) map[string]string {
	columnMap := make(map[string]string)
	stmt, err := config.GetConn(dbParam).Prepare("SELECT COLUMN_NAME,column_comment FROM information_schema.COLUMNS WHERE TABLE_NAME = ?")
	utils.Println(err)
	rs, err2 := stmt.Query(table)
	utils.Println(err2)
	var name, comment string
	for rs.Next() {
		rs.Scan(&name, &comment)
		columnMap[name] = comment
	}
	return columnMap
}

// writeToCSV
func writeToCSV(out io.Writer, totalValues [][]string) {
	w := csv.NewWriter(out)
	for _, row := range totalValues {
		w.Write(row)
	}
	w.Flush()
}
