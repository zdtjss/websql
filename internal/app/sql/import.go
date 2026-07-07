package sql

import (
	"log"
	"net/http"

	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// ImportXlsx 从 XLSX 导入数据到指定表。
// handler 只负责协议层 (提取 multipart 文件、组装请求)，
// 业务逻辑和权限校验下沉到 ImportService.ImportXlsx。
func ImportXlsx(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	connId := appctx.Ctx.GetConnID(c)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		log.Printf("[ImportXlsx] 文件上传失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 500, "文件上传失败，请检查文件是否正确选择")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		log.Printf("[ImportXlsx] 打开文件失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 500, "打开文件失败，请重试")
		return
	}
	defer file.Close()

	req := &ImportRequest{
		ConnID:        connId,
		Schema:        c.PostForm("schema"),
		Table:         c.PostForm("table"),
		OperType:      c.PostForm("optType"),
		Mapping:       c.PostForm("mapping"),
		StartRow:      c.PostForm("startRow"),
		Filename:      fileHeader.Filename,
		Authorization: authorization,
		Reader:        file,
	}

	result, err := ensureDefaultImport().ImportXlsx(req)
	if err != nil {
		response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
		return
	}
	response.WriteOK(c, result)
}
