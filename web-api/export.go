package webapi

import (
	"database/sql"
	"fmt"
	"go-web/utils"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

func ExportCsv(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	table := r.Form.Get("table")
	connId := r.Form.Get("connId")
	schema := r.Form.Get("schema")
	w.Header().Add("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Add("content-disposition", "attachment;filename="+table+".xlsx")
	queryAndWrite(schema+"."+table, w, connId)
}

func queryAndWrite(table string, out io.Writer, connId string) {
	log.Println("正在导出：", table)
	rows, err := getConn(connId).Query(fmt.Sprintf("SELECT * from %s", table))
	utils.Panicln(err)

	columns, err := rows.Columns()
	utils.Panicln(err)

	columnComment := make([]string, len(columns))
	columnMap := columnMap(table, connId)
	for i := 0; i < len(columns); i++ {
		columnComment[i] = columnMap[columns[i]]
	}

	excel := excelize.NewFile()
	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	excel.SetSheetRow("Sheet1", "A1", &columns)
	excel.SetSheetRow("Sheet1", "A2", &columnComment)

	//values：一行的所有值,把每一行的各个字段放到values中，values长度==列数
	values := make([]sql.RawBytes, len(columns))

	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	count := 2
	//存所有行的内容totalValues
	for rows.Next() {

		//把每行的内容添加到scanArgs，也添加到了values
		err = rows.Scan(scanArgs...)
		utils.Panicln(err)

		//存每一行的内容
		var row []string
		for _, v := range values {
			row = append(row, string(v))
		}

		count++
		excel.SetSheetRow("Sheet1", "A"+strconv.Itoa(count), &row)
	}

	if err = rows.Err(); err != nil {
		utils.Panicln(err)
	}

	excel.Write(out)
}

func columnMap(table string, connId string) map[string]string {
	columnMap := make(map[string]string)
	stmt, err := getConn(connId).Prepare("SELECT COLUMN_NAME,column_comment FROM information_schema.COLUMNS WHERE TABLE_NAME = ?")
	utils.Println(err)
	rs, err2 := stmt.Query(table[strings.Index(table, ".")+1:])
	utils.Println(err2)
	var name, comment string
	for rs.Next() {
		rs.Scan(&name, &comment)
		columnMap[name] = comment
	}
	return columnMap
}
