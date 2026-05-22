package sql

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/permission"
	"websql/internal/database"
	"websql/internal/logger"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

func ExportXlsx(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	table := c.Query("table")
	connId := c.Query("connId")
	schema := c.Query("schema")

	permission.CheckTablePermission(connId, schema, table, authorization)

	current := time.Now().Format(time.DateOnly)
	c.Header("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("content-disposition", "attachment;filename="+table+current+".xlsx")
	queryAndWrite(schema+"."+table, schema, c.Writer, connId, authorization)
}
func queryAndWrite(table, schema string, out io.Writer, connId string, authorization string) {
	log.Println("正在导出: ", table)

	connCtx := conn.GetConn(connId, authorization)
	rows, err := connCtx.Query("SELECT * from " + table)
	logger.PanicErr(err)

	allColumns, err := rows.Columns()
	logger.PanicErr(err)

	columnComment := make([]string, 0)
	columnMap := dbops.ColumnMapFiltered(table, schema, connId, authorization, connCtx)

	for i := range allColumns {
		columnComment = append(columnComment, columnMap[allColumns[i]])
	}

	cts, err := rows.ColumnTypes()
	logger.PanicErrf("获取字段类型失败", err)

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

	streamWriter, err := excel.NewStreamWriter("Sheet1")
	logger.PanicErr(err)

	var columns2 = make([]any, len(allColumns))
	for idx := range allColumns {
		columns2[idx] = allColumns[idx]
	}
	var columnComment2 = make([]any, len(columnComment))
	for idx := range columnComment {
		columnComment2[idx] = columnComment[idx]
	}
	streamWriter.SetRow("A1", columns2)
	streamWriter.SetRow("A2", columnComment2)

	values := make([]any, len(allColumns))
	scanArgs := make([]any, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	driverName := connCtx.DriverName()
	count := 2
	for rows.Next() {
		err = rows.Scan(scanArgs...)
		logger.PanicErr(err)

		var row = make([]any, 0, len(allColumns))
		for i := range allColumns {
			colType := colTypeMap[allColumns[i]]
			row = append(row, *database.ConvertCol(&driverName, &colType, &values[i], false))
		}

		count++
		streamWriter.SetRow("A"+strconv.Itoa(count), row)
	}

	if err = rows.Err(); err != nil {
		logger.PanicErr(err)
	}
	if err := streamWriter.Flush(); err != nil {
		logger.PanicErrf("导出excel失败", err)
		return
	}
	excel.Write(out)
	log.Println("导出完成: ", table)

}

func ExportXlsxBySql(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	filename := c.PostForm("filename")
	sqlStr := c.PostForm("sql")
	if filename == "" {
		filename = "export"
	}

	if schema == "" && connId != "" {
		dc := conn.GetConn(connId, authorization)
		switch dc.DriverName() {
		case "mysql", "mariadb":
			dc.Get(&schema, "SELECT DATABASE()")
		case "oracle":
			dc.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		case "sqlite":
			schema = "main"
		}
	}

	analysis := permission.AnalyzeSQL(sqlStr, schema)
	permResult := permission.CheckAnalysisPermission(analysis, connId, authorization)
	if !permResult.Allowed {
		c.JSON(200, gin.H{"code": 500, "msg": permResult.Message})
		return
	}

	current := time.Now().Format(time.DateOnly)
	c.Header("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("content-disposition", "attachment;filename="+filename+"-"+current+".xlsx")
	queryAndWriteBySql(sqlStr, c.Writer, connId, authorization)
}

func queryAndWriteBySql(sqlStr string, out io.Writer, connId string, authorization string) {
	log.Println("正在导出SQL: ", sqlStr)

	connCtx := conn.GetConn(connId, authorization)
	rows, err := connCtx.Query(sqlStr)
	logger.PanicErr(err)

	columns, err := rows.Columns()
	logger.PanicErr(err)

	cts, err := rows.ColumnTypes()
	logger.PanicErrf("获取字段类型失败", err)

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

	streamWriter, err := excel.NewStreamWriter("Sheet1")
	logger.PanicErr(err)

	var columns2 = make([]any, len(columns))
	for idx := range columns {
		columns2[idx] = columns[idx]
	}
	streamWriter.SetRow("A1", columns2)

	values := make([]any, len(columns))
	scanArgs := make([]any, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	driverName := connCtx.DriverName()
	count := 1
	for rows.Next() {
		err = rows.Scan(scanArgs...)
		logger.PanicErr(err)

		var row = make([]any, 0, len(columns))
		for i, col := range columns {
			colType := colTypeMap[col]
			row = append(row, *database.ConvertCol(&driverName, &colType, &values[i], false))
		}

		count++
		streamWriter.SetRow("A"+strconv.Itoa(count), row)
	}

	if err = rows.Err(); err != nil {
		logger.PanicErr(err)
	}
	if err := streamWriter.Flush(); err != nil {
		logger.PanicErrf("导出 excel 失败", err)
		return
	}
	excel.Write(out)
	log.Println("导出完成")
}

func handleExportDownload(c *gin.Context) {
	fileName := c.Param("filename")
	if fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法文件名"})
		return
	}

	cleanPath := filepath.Clean("exports/" + fileName)
	if !strings.HasPrefix(cleanPath, "exports") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法文件路径"})
		return
	}

	contentTypes := map[string]string{
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".png":  "image/png",
		".jpg":  "image/jpeg",
	}

	ext := ""
	ct := ""
	for e, t := range contentTypes {
		if strings.HasSuffix(fileName, e) {
			ext = e
			ct = t
			break
		}
	}
	if ext == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的文件类型"})
		return
	}

	filePath := "exports/" + fileName

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", "attachment; filename=\""+fileName+"\"")

	file, err := os.Open(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取文件失败"})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取文件信息失败"})
		return
	}
	c.Header("Content-Length", fmt.Sprintf("%d", stat.Size()))

	io.Copy(c.Writer, file)
	c.Writer.Flush()
	file.Close()
}