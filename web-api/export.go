package webapi

import (
	"fmt"
	"go-web/config"
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

	tableNameOnly := table
	if strings.Contains(table, ".") {
		tableNameOnly = table[strings.Index(table, ".")+1:]
	}

	access := admin.GetTableColumnAccess(connId, schema, tableNameOnly, authorization)
	var allowedColumns map[string]bool
	if access.Level == admin.AccessColumn {
		allowedColumns = access.AllowedColumns
	} else if access.Level == admin.AccessNone {
		return
	}

	columnComment := make([]string, 0)
	columnMap := admin.ColumnMapFiltered(table, schema, connId, authorization, connCtx)

	var filteredColumns []string
	for i := 0; i < len(allColumns); i++ {
		if allowedColumns != nil && !allowedColumns[allColumns[i]] {
			continue
		}
		filteredColumns = append(filteredColumns, allColumns[i])
		columnComment = append(columnComment, columnMap[allColumns[i]])
	}

	cts, err := rows.ColumnTypes()
	logutils.PanicErrf("获取字段类型失败", err)

	colTypeMap := map[string]string{}
	for _, ct := range cts {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}

	allowedIdx := make(map[int]bool)
	for i, col := range allColumns {
		if allowedColumns == nil || allowedColumns[col] {
			allowedIdx[i] = true
		}
	}

	excel := excelize.NewFile()

	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	streamWriter, err := excel.NewStreamWriter("Sheet1")
	logutils.PanicErr(err)

	var columns2 = make([]any, len(filteredColumns))
	for idx := range filteredColumns {
		columns2[idx] = filteredColumns[idx]
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

		var row = make([]any, 0, len(filteredColumns))
		for i := range allColumns {
			if allowedIdx[i] {
				colType := colTypeMap[allColumns[i]]
				row = append(row, *admin.ConvertCol(&driverName, &colType, &values[i], false))
			}
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

	analysis := admin.AnalyzeSQL(sqlStr, schema)
	admin.CheckSQLPermission(analysis, connId, authorization)

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

	analysis := admin.AnalyzeSQL(sqlStr, "")
	anyColumnLevel := false
	if config.Cfg.IsRemote {
		userPower := admin.GetUserPower(authorization)
		if userPower != nil && userPower.UserId != config.AdminId {
			for _, t := range analysis.ReadTables {
				access := admin.GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
				if access.Level == admin.AccessColumn {
					anyColumnLevel = true
					break
				}
			}
		}
	}

	allowedSet := make(map[string]bool)
	if anyColumnLevel {
		for _, t := range analysis.ReadTables {
			access := admin.GetTableColumnAccess(connId, t.Schema, t.Name, authorization)
			if access.Level == admin.AccessFull {
				for _, col := range columns {
					allowedSet[col] = true
				}
			} else if access.Level == admin.AccessColumn {
				for _, col := range columns {
					if access.AllowedColumns[col] {
						allowedSet[col] = true
					}
				}
			}
		}
	}

	var filteredColumns []string
	if anyColumnLevel {
		for _, col := range columns {
			if allowedSet[col] {
				filteredColumns = append(filteredColumns, col)
			}
		}
	} else {
		filteredColumns = columns
	}

	excel := excelize.NewFile()
	defer func() {
		if err := excel.Close(); err != nil {
			log.Println(err)
		}
	}()

	streamWriter, err := excel.NewStreamWriter("Sheet1")
	logutils.PanicErr(err)

	var columns2 = make([]any, len(filteredColumns))
	for idx := range filteredColumns {
		columns2[idx] = filteredColumns[idx]
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

		var row = make([]any, 0, len(filteredColumns))
		for i, col := range columns {
			if !anyColumnLevel || allowedSet[col] {
				colType := colTypeMap[col]
				row = append(row, *admin.ConvertCol(&driverName, &colType, &values[i], false))
			}
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
	os.Remove(filePath)
	log.Printf("导出文件已删除：%s", filePath)
}
