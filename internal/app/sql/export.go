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
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/sanitize"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

func ExportXlsx(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	table := c.Query("table")
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")

	permission.CheckTablePermission(connId, schema, table, authorization)

	current := time.Now().Format(time.DateOnly)
	c.Header("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("content-disposition", "attachment;filename="+table+current+".xlsx")
	queryAndWrite(c, schema+"."+table, schema, connId, authorization)
}
func queryAndWrite(c *gin.Context, table, schema string, connId string, authorization string) {
	log.Println("正在导出: ", table)

	// 标识符白名单校验，防止 SQL 注入
	parts := strings.SplitN(table, ".", 2)
	var safeTable string
	if len(parts) == 2 {
		if !sanitize.IsValidIdentifier(parts[0]) || !sanitize.IsValidIdentifier(parts[1]) {
			response.WriteErr(c, 200, 400, "非法的表名")
			return
		}
		safeTable = fmt.Sprintf("`%s`.`%s`", parts[0], parts[1])
	} else {
		if !sanitize.IsValidIdentifier(table) {
			response.WriteErr(c, 200, 400, "非法的表名")
			return
		}
		safeTable = fmt.Sprintf("`%s`", table)
	}

	connCtx := conn.GetConn(connId, authorization)
	rows, err := connCtx.Query("SELECT * from " + safeTable)
	if err != nil {
		log.Printf("查询失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	allColumns, err := rows.Columns()
	if err != nil {
		log.Printf("获取字段失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	columnComment := make([]string, 0)
	columnMap := dbops.ColumnMapFiltered(table, schema, connId, authorization, connCtx)

	for i := range allColumns {
		columnComment = append(columnComment, columnMap[allColumns[i]])
	}

	cts, err := rows.ColumnTypes()
	if err != nil {
		log.Printf("获取字段类型失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

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
	if err != nil {
		log.Printf("创建流写入器失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

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
		if err != nil {
			log.Printf("扫描行失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
			return
		}

		var row = make([]any, 0, len(allColumns))
		for i := range allColumns {
			colType := colTypeMap[allColumns[i]]
			row = append(row, *database.ConvertCol(&driverName, &colType, &values[i], false))
		}

		count++
		streamWriter.SetRow("A"+strconv.Itoa(count), row)
	}

	if err = rows.Err(); err != nil {
		log.Printf("遍历行失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	if err := streamWriter.Flush(); err != nil {
		log.Printf("导出excel失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	excel.Write(c.Writer)
	log.Println("导出完成: ", table)

}

func ExportXlsxBySql(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	connId := appctx.Ctx.GetConnID(c)
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
		response.WriteErr(c, 200, 500, permResult.Message)
		return
	}

	current := time.Now().Format(time.DateOnly)
	c.Header("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("content-disposition", "attachment;filename="+filename+"-"+current+".xlsx")
	queryAndWriteBySql(c, sqlStr, connId, authorization)
}

func queryAndWriteBySql(c *gin.Context, sqlStr string, connId string, authorization string) {
	log.Println("正在导出SQL: ", sqlStr)

	connCtx := conn.GetConn(connId, authorization)
	rows, err := connCtx.Query(sqlStr)
	if err != nil {
		log.Printf("查询失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	columns, err := rows.Columns()
	if err != nil {
		log.Printf("获取字段失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	cts, err := rows.ColumnTypes()
	if err != nil {
		log.Printf("获取字段类型失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

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
	if err != nil {
		log.Printf("创建流写入器失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

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
		if err != nil {
			log.Printf("扫描行失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
			return
		}

		var row = make([]any, 0, len(columns))
		for i, col := range columns {
			colType := colTypeMap[col]
			row = append(row, *database.ConvertCol(&driverName, &colType, &values[i], false))
		}

		count++
		streamWriter.SetRow("A"+strconv.Itoa(count), row)
	}

	if err = rows.Err(); err != nil {
		log.Printf("遍历行失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	if err := streamWriter.Flush(); err != nil {
		log.Printf("导出 excel 失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	excel.Write(c.Writer)
	log.Println("导出完成")
}

func handleExportDownload(c *gin.Context) {
	fileName := c.Param("filename")
	if fileName == "" {
		response.WriteErr(c, http.StatusBadRequest, 500, "文件名不能为空")
		return
	}

	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		response.WriteErr(c, http.StatusBadRequest, 500, "非法文件名")
		return
	}

	cleanPath := filepath.Clean("exports/" + fileName)
	if !strings.HasPrefix(cleanPath, "exports") {
		response.WriteErr(c, http.StatusBadRequest, 500, "非法文件路径")
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
		response.WriteErr(c, http.StatusBadRequest, 500, "不支持的文件类型")
		return
	}

	filePath := "exports/" + fileName

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		response.WriteErr(c, http.StatusNotFound, 500, "文件不存在")
		return
	}

	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", "attachment; filename=\""+fileName+"\"")

	file, err := os.Open(filePath)
	if err != nil {
		response.WriteErr(c, http.StatusInternalServerError, 500, "读取文件失败")
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		response.WriteErr(c, http.StatusInternalServerError, 500, "获取文件信息失败")
		return
	}
	c.Header("Content-Length", fmt.Sprintf("%d", stat.Size()))

	io.Copy(c.Writer, file)
	c.Writer.Flush()
	file.Close()
}