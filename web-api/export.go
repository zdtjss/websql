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

	connCtx := admin.GetConn(connId, authorization)
	rows, err := connCtx.Query(fmt.Sprintf("SELECT * from %s", table))
	logutils.PanicErr(err)

	columns, err := rows.Columns()
	logutils.PanicErr(err)

	columnComment := make([]string, len(columns))
	columnMap := admin.ColumnMap(table, connId, authorization)
	for i := 0; i < len(columns); i++ {
		columnComment[i] = columnMap[columns[i]]
	}

	cts, err := rows.ColumnTypes()
	logutils.PanicErrf("获取字段类型失败", err)

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

	sheetName := table[strings.Index(table, ".")+1:]
	excel.SetSheetName("Sheet1", sheetName)
	excel.SetSheetRow(sheetName, "A1", &columns)
	excel.SetSheetRow(sheetName, "A2", &columnComment)

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
		logutils.PanicErr(err)

		//存每一行的内容
		var row []any
		for i, v := range values {
			row = append(row, admin.ConvertCol(connCtx.DriverName(), colTypeMap[columns[i]], v))
		}

		count++
		excel.SetSheetRow(sheetName, "A"+strconv.Itoa(count), &row)
	}

	if err = rows.Err(); err != nil {
		logutils.PanicErr(err)
	}

	excel.Write(out)
}
