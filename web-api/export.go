package webapi

import (
	"go-web/logutils"
	admin "go-web/web-api/admin"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

func ExportXlsx(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	table := c.Query("table")
	connId := c.Query("connId")
	schema := c.Query("schema")
	current := time.Now().Format(time.DateOnly)
	c.Header("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("content-disposition", strings.Join([]string{"attachment;filename=", table, current, ".xlsx"}, ""))
	queryAndWrite(schema+"."+table, schema, c.Writer, connId, authorization)
}

func queryAndWrite(table, schema string, out io.Writer, connId string, authorization string) {
	log.Println("正在导出：", table)

	connCtx := admin.GetConn(connId, authorization)
	rows, err := connCtx.Query(strings.Join([]string{"SELECT * from ", table}, ""))
	logutils.PanicErr(err)

	columns, err := rows.Columns()
	logutils.PanicErr(err)

	columnComment := make([]string, len(columns))
	columnMap := admin.ColumnMap(table, schema, connCtx)
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

	// sheetName := table[strings.Index(table, ".")+1:]
	// excel.SetSheetName("Sheet1", sheetName)
	streamWriter, err := excel.NewStreamWriter("Sheet1")
	logutils.PanicErr(err)

	var columns2 = make([]any, len(columns))
	for idx := range columns {
		columns2[idx] = columns[idx]
	}
	var columnComment2 = make([]any, len(columnComment))
	for idx := range columnComment {
		columnComment2[idx] = columnComment[idx]
	}
	streamWriter.SetRow("A1", columns2)
	streamWriter.SetRow("A2", columnComment2)

	//values：一行的所有值,把每一行的各个字段放到values中，values长度==列数
	values := make([]any, len(columns))
	scanArgs := make([]any, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	driverName := connCtx.DriverName()
	count := 2
	//存所有行的内容totalValues
	for rows.Next() {

		//把每行的内容添加到scanArgs，也添加到了values
		err = rows.Scan(scanArgs...)
		logutils.PanicErr(err)

		//存每一行的内容
		var row = make([]any, len(values))
		for i := range values {
			colType := colTypeMap[columns[i]]
			row[i] = *admin.ConvertCol(&driverName, &colType, &values[i], false)
		}

		count++
		streamWriter.SetRow("A"+strconv.Itoa(count), row)
	}

	if err = rows.Err(); err != nil {
		logutils.PanicErr(err)
	}
	if err := streamWriter.Flush(); err != nil {
		logutils.PanicErrf("导出excel失败", err)
		return
	}
	excel.Write(out)
	log.Println("导出完成：", table)

}
