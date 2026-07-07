package sql

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/app/conn"
	"websql/internal/app/permission"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// ExportXlsx 按表导出 XLSX。
// handler 只负责协议层 (HTTP header、权限校验、Writer 注入)，
// 业务逻辑下沉到 ExportService.ExportTable。
func ExportXlsx(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	table := c.Query("table")
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")

	permission.CheckTablePermission(connId, schema, table, authorization)

	c.Header("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("content-disposition", "attachment;filename="+table+time.Now().Format(time.DateOnly)+".xlsx")

	req := &ExportRequest{
		ConnID:        connId,
		Schema:        schema,
		Table:         table,
		Authorization: authorization,
		Writer:        c.Writer,
	}
	if err := ensureDefaultExport().ExportTable(req); err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
}

// ExportXlsxBySql 按自定义 SQL 导出 XLSX。
// handler 只负责协议层，业务逻辑下沉到 ExportService.ExportBySQL。
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
		if dc != nil {
			switch dc.DriverName() {
			case "mysql", "mariadb":
				dc.Get(&schema, "SELECT DATABASE()")
			case "oracle":
				dc.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
			case "sqlite":
				schema = "main"
			}
		}
	}

	analysis := permission.AnalyzeSQL(sqlStr, schema)
	permResult := permission.CheckAnalysisPermission(analysis, connId, authorization)
	if !permResult.Allowed {
		response.WriteErr(c, 200, 500, permResult.Message)
		return
	}

	c.Header("content-type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("content-disposition", "attachment;filename="+filename+"-"+time.Now().Format(time.DateOnly)+".xlsx")

	req := &ExportBySQLRequest{
		ConnID:        connId,
		Schema:        schema,
		Filename:      filename,
		SQL:           sqlStr,
		Authorization: authorization,
		Writer:        c.Writer,
	}
	if err := ensureDefaultExport().ExportBySQL(req); err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
}

// handleExportDownload 是静态文件下载 (导出归档目录的复用)，与 service 抽象无关。
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
