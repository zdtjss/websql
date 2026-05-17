package webapi

import (
	"fmt"
	"go-web/logutils"
	admin "go-web/web-api/admin"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	admin.CheckTablePermission(connId, schema, table, authorization)

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

	allColumns, err := rows.Columns()
	logutils.PanicErr(err)

	columnComment := make([]string, 0)
	columnMap := admin.ColumnMapFiltered(table, schema, connId, authorization, connCtx)

	for i := 0; i < len(allColumns); i++ {
		columnComment = append(columnComment, columnMap[allColumns[i]])
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

	streamWriter, err := excel.NewStreamWriter("Sheet1")
	logutils.PanicErr(err)

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
		logutils.PanicErr(err)

		var row = make([]any, 0, len(allColumns))
		for i := range allColumns {
			colType := colTypeMap[allColumns[i]]
			row = append(row, *admin.ConvertCol(&driverName, &colType, &values[i], false))
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

func ExportXlsxBySql(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	connId := c.PostForm("connId")
	schema := c.PostForm("schema")
	filename := c.PostForm("filename")
	sqlStr := c.PostForm("sql")
	if filename == "" {
		filename = "export"
	}

	// 当 schema 为空时，从数据库连接获取实际 schema
	if schema == "" && connId != "" {
		dc := admin.GetConn(connId, authorization)
		switch dc.DriverName() {
		case "mysql", "mariadb":
			dc.Get(&schema, "SELECT DATABASE()")
		case "oracle":
			dc.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
		case "sqlite":
			schema = "main"
		}
	}

	analysis := admin.AnalyzeSQL(sqlStr, schema)
	permResult := admin.CheckAnalysisPermission(analysis, connId, authorization)
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
	log.Println("正在导出SQL：", sqlStr)

	connCtx := admin.GetConn(connId, authorization)
	rows, err := connCtx.Query(sqlStr)
	logutils.PanicErr(err)

	columns, err := rows.Columns()
	logutils.PanicErr(err)

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

	streamWriter, err := excel.NewStreamWriter("Sheet1")
	logutils.PanicErr(err)

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
		logutils.PanicErr(err)

		var row = make([]any, 0, len(columns))
		for i, col := range columns {
			colType := colTypeMap[col]
			row = append(row, *admin.ConvertCol(&driverName, &colType, &values[i], false))
		}

		count++
		streamWriter.SetRow("A"+strconv.Itoa(count), row)
	}

	if err = rows.Err(); err != nil {
		logutils.PanicErr(err)
	}
	if err := streamWriter.Flush(); err != nil {
		logutils.PanicErrf("导出 excel 失败", err)
		return
	}
	excel.Write(out)
	log.Println("导出完成")
}

// handleExportDownload 处理导出文件下载，下载完成后自动删除文件
func handleExportDownload(c *gin.Context) {
	fileName := c.Param("filename")
	if fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	// 防止路径穿越攻击
	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法文件名"})
		return
	}

	// 二次验证：确保清理后的路径仍在 exports 目录内
	cleanPath := filepath.Clean("exports/" + fileName)
	if !strings.HasPrefix(cleanPath, "exports") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法文件路径"})
		return
	}

	// 支持的文件类型
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

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	// 设置响应头
	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", "attachment; filename=\""+fileName+"\"")

	// 读取并发送文件
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

	// 下载后删除
	// os.Remove(filePath)
	// log.Printf("导出文件已删除：%s", filePath)
}
