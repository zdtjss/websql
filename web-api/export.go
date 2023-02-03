package webapi

import (
	"fmt"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

func ExportXlsx(w http.ResponseWriter, r *http.Request) {
	authorization := r.Header.Get("Authorization")
	r.ParseForm()
	table := r.Form.Get("table")
	connId := utils.AtoUint64(r.Form.Get("connId"))
	schema := r.Form.Get("schema")
	w.Header().Add("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Add("content-disposition", "attachment;filename="+table+".xlsx")
	queryAndWrite(schema+"."+table, w, connId, authorization)
}

func queryAndWrite(table string, out io.Writer, connId uint64, authorization string) {
	log.Println("正在导出：", table)
	rows, err := admin.GetConn(connId, authorization).Query(fmt.Sprintf("SELECT * from %s", table))
	logutils.Panicln(err)

	columns, err := rows.Columns()
	logutils.Panicln(err)

	columnComment := make([]string, len(columns))
	columnMap := columnMap(table, connId, authorization)
	for i := 0; i < len(columns); i++ {
		columnComment[i] = columnMap[columns[i]]
	}

	cts, err := rows.ColumnTypes()
	logutils.Panicf("获取字段类型失败，%x", err)

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
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
	values := make([]any, len(columns))
	scanArgs := make([]any, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	count := 2
	//存所有行的内容totalValues
	for rows.Next() {

		//把每行的内容添加到scanArgs，也添加到了values
		err = rows.Scan(scanArgs...)
		logutils.Panicln(err)

		//存每一行的内容
		var row []any
		for i, v := range values {
			row = append(row, ConvertCol(colTypeMap[columns[i]], v))
		}

		count++
		excel.SetSheetRow("Sheet1", "A"+strconv.Itoa(count), &row)
	}

	if err = rows.Err(); err != nil {
		logutils.Panicln(err)
	}

	excel.Write(out)
}

func columnMap(table string, connId uint64, authorization string) map[string]string {
	columnMap := make(map[string]string)
	stmt, err := admin.GetConn(connId, authorization).Prepare("SELECT COLUMN_NAME,column_comment FROM information_schema.COLUMNS WHERE TABLE_NAME = ?")
	logutils.Println(err)
	rs, err2 := stmt.Query(table[strings.Index(table, ".")+1:])
	logutils.Println(err2)
	var name, comment string
	for rs.Next() {
		rs.Scan(&name, &comment)
		columnMap[name] = comment
	}
	return columnMap
}
