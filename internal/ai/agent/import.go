package agent

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"websql/internal/ai/agent/fileparse"
	"websql/internal/ai/agent/uploadstore"
	admin "websql/internal/app/admin"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// legacy 上传接口（deprecated）。
//
// 保留原因：桌面端/未刷新的前端仍调用 /ai/agent/uploadExcel，且其响应契约
// 依赖「上传即解析」产出的列名/行数/预览。新前端请使用 /ai/agent/uploadFile
// （仅存储不解析，解析时机推迟到 read_file_data 按需触发）。
//
// 与既有行为保持一致的要点：
//   - 白名单固定为 .xlsx/.xls/.csv/.md/.markdown（不随新链路扩展）；
//   - 非 Markdown 限 20MB，Markdown 不限；
//   - 表格类上传即校验「至少表头 + 一行数据」，失败即拒绝且不留盘。

// legacyUploadMaxSize 旧接口的体积上限（Excel/CSV）
const legacyUploadMaxSize = 20 * 1024 * 1024

// legacyUploadFileType 旧接口的 fileType 命名（响应兼容）
func legacyUploadFileType(ext string) (string, bool) {
	switch fileparse.NormalizeExt(ext) {
	case ".xlsx", ".xls":
		return "excel", true
	case ".csv":
		return "csv", true
	case ".md", ".markdown":
		return "markdown", true
	default:
		return "", false
	}
}

// HandleUploadExcel POST /ai/agent/uploadExcel（deprecated，保持旧契约）
func HandleUploadExcel(c *gin.Context) {
	ownerID := uploaderID(c)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		log.Printf("[Upload] 文件上传失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 400, "文件上传失败，请检查文件是否正确选择")
		return
	}

	ext := fileparse.NormalizeExt(filepath.Ext(fileHeader.Filename))
	fileType, ok := legacyUploadFileType(ext)
	if !ok {
		response.WriteErr(c, http.StatusBadRequest, 400, "仅支持 .xlsx/.xls/.csv/.md/.markdown 格式的文件")
		return
	}

	isMarkdown := fileType == "markdown"
	if !isMarkdown && fileHeader.Size > legacyUploadMaxSize {
		response.WriteErr(c, http.StatusBadRequest, 400, "文件大小不能超过 20MB")
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		log.Printf("[Upload] 打开文件失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 400, "打开文件失败，请重试")
		return
	}
	defer src.Close()

	limit := int64(0)
	if !isMarkdown {
		limit = legacyUploadMaxSize
	}
	fileID, diskPath, size, err := saveUploadedTemp(src, ext, limit)
	if err != nil {
		if err == errFileTooLarge {
			response.WriteErr(c, http.StatusBadRequest, 400, "文件大小不能超过 20MB")
			return
		}
		log.Printf("[Upload] 保存文件失败 - name=%s, err=%v\n", fileHeader.Filename, err)
		response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
		return
	}

	// legacy 行为：上传即解析，用于返回列名/行数/字符数/预览
	res, parseErr := fileparse.Parse(context.Background(), fileparse.Source{
		Path:     diskPath,
		FileName: fileHeader.Filename,
		Ext:      ext,
		Size:     size,
	})
	if parseErr != nil {
		os.Remove(diskPath)
		log.Printf("[Upload] legacy 解析失败 - id=%s, name=%s, err=%v\n", fileID, fileHeader.Filename, parseErr)
		response.WriteErr(c, http.StatusBadRequest, 400, parseErr.Error())
		return
	}

	meta := &uploadstore.FileMeta{
		ID:       fileID,
		UserID:   ownerID,
		FileName: fileHeader.Filename,
		DiskPath: diskPath,
		Ext:      ext,
		Size:     size,
		MIME:     mimeOfExt(ext),
		Kind:     string(res.Kind),
		FileType: fileType,
	}
	globalUploadStore.Save(meta)

	if res.Kind == fileparse.KindText {
		log.Printf("[Upload] Markdown 已暂存 - id=%s, name=%s, chars=%d\n", fileID, fileHeader.Filename, res.TotalChars)
		response.WriteOK(c, gin.H{
			"fileId":      fileID,
			"fileName":    fileHeader.Filename,
			"fileType":    fileType,
			"charCount":   res.TotalChars,
			"textPreview": markdownPreview(res.Text),
		})
		return
	}

	preview := res.Rows
	if len(preview) > 10 {
		preview = preview[:10]
	}
	log.Printf("[Upload] 表格文件已暂存 - id=%s, name=%s, type=%s, columns=%v, rows=%d\n",
		fileID, fileHeader.Filename, fileType, res.Columns, res.TotalRows)
	response.WriteOK(c, gin.H{
		"fileId":    fileID,
		"fileName":  fileHeader.Filename,
		"fileType":  fileType,
		"columns":   res.Columns,
		"totalRows": res.TotalRows,
		"preview":   preview,
	})
}

// HandlePreMatchColumns POST /ai/agent/preMatchColumns
//
// 按上传文件的列名与目标表列做预匹配。
// 说明：解析改为按需触发（GetUploadedFile），因此这里也会执行一次归属校验——
// 旧实现只按 fileId 取缓存、未校验归属，属于 IDOR 面，本次一并收紧。
func HandlePreMatchColumns(c *gin.Context) {
	var req struct {
		FileID    string `json:"fileId"`
		ConnID    string `json:"connId"`
		TableName string `json:"tableName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.WriteErr(c, http.StatusBadRequest, 400, "参数格式错误")
		return
	}
	if req.FileID == "" || req.ConnID == "" || req.TableName == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "fileId、connId、tableName 不能为空")
		return
	}

	// 身份解析：文件归属校验优先使用请求上下文中的当前用户
	// （与上传/删除/内容接口的 uploaderID 保持一致，避免本地模式下
	// 上传者为 local、校验方解析为 "" 而误判为非本人文件）；
	// 取不到时回退到 Authorization 头解析。连接访问控制沿用既有身份来源。
	authUserID := ""
	if authorization := c.GetHeader("Authorization"); authorization != "" {
		if user := admin.GetUser(authorization); user != nil {
			authUserID = user.Id
		}
	}
	fileOwner := uploaderID(c)
	if fileOwner == "" {
		fileOwner = authUserID
	}

	upload, err := GetUploadedFile(c.Request.Context(), req.FileID, fileOwner)
	if err != nil {
		log.Printf("[Upload] 列预匹配读取文件失败 - id=%s, user=%s, err=%v\n", req.FileID, fileOwner, err)
		response.WriteErr(c, http.StatusBadRequest, 400, "上传文件不存在或已过期，请重新上传")
		return
	}
	if !upload.IsTabular() {
		response.WriteErr(c, http.StatusBadRequest, 400, "该文件不是表格数据，无法进行列匹配")
		return
	}
	columns := upload.Columns

	conn, dbType := GetConn(req.ConnID, authUserID)
	if conn == nil {
		response.WriteErr(c, http.StatusBadRequest, 400, "数据库连接不存在")
		return
	}

	_, dbSchema, _ := GetDBInfo(req.ConnID)

	tableColumns, err := getTableColumns(conn, dbType, dbSchema, req.TableName)
	if err != nil || len(tableColumns) == 0 {
		response.WriteErrf(c, http.StatusBadRequest, 400, "获取表 %s 的列信息失败", req.TableName)
		return
	}

	mapping, _ := buildFinalMapping(columns, tableColumns, nil)

	type matchItem struct {
		ExcelColumn string `json:"excelColumn"`
		DBColumn    string `json:"dbColumn"`
		Matched     bool   `json:"matched"`
	}
	var matches []matchItem
	for i, excelCol := range columns {
		if dbCol, ok := mapping[i]; ok {
			matches = append(matches, matchItem{ExcelColumn: excelCol, DBColumn: dbCol, Matched: true})
		} else {
			matches = append(matches, matchItem{ExcelColumn: excelCol, DBColumn: "", Matched: false})
		}
	}

	response.WriteOK(c, gin.H{
		"matches":      matches,
		"matchedCount": len(mapping),
		"totalExcel":   len(columns),
		"totalDB":      len(tableColumns),
		"tableColumns": tableColumns,
	})
}

// markdownPreview 截取前 30 行（且不超过 2000 字符）作为预览文本。
func markdownPreview(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	preview := strings.Join(lines, "\n")
	if utf8.RuneCountInString(preview) > 2000 {
		preview = string([]rune(preview)[:2000]) + "…"
	}
	return preview
}
