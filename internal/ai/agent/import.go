package agent

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	admin "websql/internal/app/admin"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

const uploadDir = "./data/uploads"
const uploadMaxAge = 30 * time.Minute
const uploadCleanSec = 5 * time.Minute
const maxUploadSize = 20 * 1024 * 1024 // Excel/CSV 最大 20MB；Markdown 不限

// uploadFileType 上传文件类型
type uploadFileType string

const (
	fileTypeExcel    uploadFileType = "excel"
	fileTypeCSV      uploadFileType = "csv"
	fileTypeMarkdown uploadFileType = "markdown"
)

func classifyUploadExt(ext string) (uploadFileType, bool) {
	switch ext {
	case ".xlsx", ".xls":
		return fileTypeExcel, true
	case ".csv":
		return fileTypeCSV, true
	case ".md", ".markdown":
		return fileTypeMarkdown, true
	}
	return "", false
}

type uploadMeta struct {
	ID        string
	FileName  string
	DiskPath  string
	Type      uploadFileType // excel | csv | markdown
	Columns   []string       // 表格类（excel/csv）
	TotalRows int            // 表格类（excel/csv）
	CharCount int            // markdown 字符数
}

var uploadCache = NewTTLCache[*uploadMeta](uploadMaxAge, uploadCleanSec, func(key string, meta *uploadMeta) {
	if meta != nil {
		os.Remove(meta.DiskPath)
		log.Printf("[UploadStore] 清理过期文件 - id=%s, name=%s\n", key, meta.FileName)
	}
})

func init() {
	os.MkdirAll(uploadDir, 0o755)
}

type UploadedFile struct {
	Type    string     // "table"（excel/csv）| "text"（markdown）
	Columns []string   // table
	Data    [][]string // table
	Text    string     // text（markdown 全文）
}

func GetUploadedFile(id string) (*UploadedFile, error) {
	meta, ok := uploadCache.Get(id)
	if !ok {
		return nil, fmt.Errorf("上传文件不存在或已过期（id=%s），请重新上传", id)
	}

	switch meta.Type {
	case fileTypeMarkdown:
		raw, err := os.ReadFile(meta.DiskPath)
		if err != nil {
			return nil, fmt.Errorf("读取暂存文件失败：%w", err)
		}
		return &UploadedFile{Type: "text", Text: string(raw)}, nil
	case fileTypeCSV:
		f, err := os.Open(meta.DiskPath)
		if err != nil {
			return nil, fmt.Errorf("读取暂存文件失败：%w", err)
		}
		defer f.Close()
		reader := csv.NewReader(f)
		reader.LazyQuotes = true
		reader.FieldsPerRecord = -1 // 允许行列数不一致
		allRows, err := reader.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("解析 CSV 失败：%w", err)
		}
		return buildTabularUploaded(allRows)
	default: // excel
		f, err := excelize.OpenFile(meta.DiskPath)
		if err != nil {
			return nil, fmt.Errorf("读取暂存文件失败：%w", err)
		}
		defer f.Close()
		sheetName := f.GetSheetName(0)
		allRows, err := f.GetRows(sheetName)
		if err != nil {
			return nil, fmt.Errorf("解析工作表失败：%w", err)
		}
		return buildTabularUploaded(allRows)
	}
}

// buildTabularUploaded 将二维行数据（首行表头）转为 UploadedFile，跳过空行并按列数补齐。
func buildTabularUploaded(allRows [][]string) (*UploadedFile, error) {
	if len(allRows) < 2 {
		return nil, errors.New("文件数据不足")
	}
	columns := allRows[0]
	var data [][]string
	for _, row := range allRows[1:] {
		hasValue := false
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				hasValue = true
				break
			}
		}
		if !hasValue {
			continue
		}
		padded := make([]string, len(columns))
		copy(padded, row)
		data = append(data, padded)
	}
	return &UploadedFile{Type: "table", Columns: columns, Data: data}, nil
}

func RemoveUploadedFile(id string) {
	uploadCache.Delete(id)
}

func HandleUploadExcel(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		log.Printf("[Upload] 文件上传失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 400, "文件上传失败，请检查文件是否正确选择")
		return
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	ft, ok := classifyUploadExt(ext)
	if !ok {
		response.WriteErr(c, http.StatusBadRequest, 400, "仅支持 .xlsx/.xls/.csv/.md/.markdown 格式的文件")
		return
	}

	// Markdown 不做数据量限制；Excel/CSV 限制 20MB
	if ft != fileTypeMarkdown && fileHeader.Size > maxUploadSize {
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

	fileID := idgen.RandomStr()
	diskPath := filepath.Join(uploadDir, fileID+ext)

	dst, err := os.Create(diskPath)
	if err != nil {
		log.Printf("[Upload] 保存文件失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "保存文件失败，请重试")
		return
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(diskPath)
		log.Printf("[Upload] 写入文件失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusInternalServerError, 500, "写入文件失败，请重试")
		return
	}
	dst.Close()

	// Markdown：读取全文，不做数据量限制
	if ft == fileTypeMarkdown {
		raw, err := os.ReadFile(diskPath)
		if err != nil {
			os.Remove(diskPath)
			log.Printf("[Upload] 读取 Markdown 失败 - err=%v\n", err)
			response.WriteErr(c, http.StatusBadRequest, 400, "读取文件失败，请检查文件内容")
			return
		}
		text := string(raw)
		charCount := utf8.RuneCountInString(text)
		uploadCache.Set(fileID, &uploadMeta{
			ID:        fileID,
			FileName:  fileHeader.Filename,
			DiskPath:  diskPath,
			Type:      ft,
			CharCount: charCount,
		})
		log.Printf("[Upload] Markdown 已暂存 - id=%s, name=%s, chars=%d\n", fileID, fileHeader.Filename, charCount)
		c.JSON(http.StatusOK, gin.H{
			"fileId":      fileID,
			"fileName":    fileHeader.Filename,
			"fileType":    string(ft),
			"charCount":   charCount,
			"textPreview": markdownPreview(text),
		})
		return
	}

	// 表格类：Excel / CSV
	allRows, err := readTabularRows(diskPath, ft)
	if err != nil {
		os.Remove(diskPath)
		log.Printf("[Upload] 解析表格失败 - type=%s, err=%v\n", ft, err)
		response.WriteErr(c, http.StatusBadRequest, 400, err.Error())
		return
	}
	if len(allRows) < 2 {
		os.Remove(diskPath)
		response.WriteErr(c, http.StatusBadRequest, 400, "文件至少需要包含表头行和一行数据")
		return
	}

	columns := allRows[0]
	var preview [][]string
	totalRows := 0
	for _, row := range allRows[1:] {
		hasValue := false
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				hasValue = true
				break
			}
		}
		if !hasValue {
			continue
		}
		totalRows++
		if len(preview) < 10 {
			padded := make([]string, len(columns))
			copy(padded, row)
			preview = append(preview, padded)
		}
	}

	uploadCache.Set(fileID, &uploadMeta{
		ID:        fileID,
		FileName:  fileHeader.Filename,
		DiskPath:  diskPath,
		Type:      ft,
		Columns:   columns,
		TotalRows: totalRows,
	})

	log.Printf("[Upload] 表格文件已暂存 - id=%s, name=%s, type=%s, columns=%v, rows=%d\n",
		fileID, fileHeader.Filename, ft, columns, totalRows)

	c.JSON(http.StatusOK, gin.H{
		"fileId":    fileID,
		"fileName":  fileHeader.Filename,
		"fileType":  string(ft),
		"columns":   columns,
		"totalRows": totalRows,
		"preview":   preview,
	})
}

// readTabularRows 读取表格文件（excel/csv）的全部行。
func readTabularRows(diskPath string, ft uploadFileType) ([][]string, error) {
	if ft == fileTypeCSV {
		f, err := os.Open(diskPath)
		if err != nil {
			return nil, fmt.Errorf("读取文件失败，请重试")
		}
		defer f.Close()
		reader := csv.NewReader(f)
		reader.LazyQuotes = true
		reader.FieldsPerRecord = -1 // 允许行列数不一致
		rows, err := reader.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("解析 CSV 失败，请检查文件格式")
		}
		return rows, nil
	}
	// excel
	xlsx, err := excelize.OpenFile(diskPath)
	if err != nil {
		return nil, fmt.Errorf("读取 Excel 文件失败，请检查文件格式")
	}
	defer xlsx.Close()
	sheetName := xlsx.GetSheetName(0)
	rows, err := xlsx.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("读取工作表失败，请检查文件内容")
	}
	return rows, nil
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

	meta, ok := uploadCache.Get(req.FileID)
	if !ok {
		response.WriteErr(c, http.StatusBadRequest, 400, "上传文件不存在或已过期，请重新上传")
		return
	}

	var preMatchUserId string
	if authorization := c.GetHeader("Authorization"); authorization != "" {
		if user := admin.GetUser(authorization); user != nil {
			preMatchUserId = user.Id
		}
	}
	conn, dbType := GetConn(req.ConnID, preMatchUserId)
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

	mapping, _ := buildFinalMapping(meta.Columns, tableColumns, nil)

	type matchItem struct {
		ExcelColumn string `json:"excelColumn"`
		DBColumn    string `json:"dbColumn"`
		Matched     bool   `json:"matched"`
	}
	var matches []matchItem
	for i, excelCol := range meta.Columns {
		if dbCol, ok := mapping[i]; ok {
			matches = append(matches, matchItem{ExcelColumn: excelCol, DBColumn: dbCol, Matched: true})
		} else {
			matches = append(matches, matchItem{ExcelColumn: excelCol, DBColumn: "", Matched: false})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"matches":      matches,
		"matchedCount": len(mapping),
		"totalExcel":   len(meta.Columns),
		"totalDB":      len(tableColumns),
		"tableColumns": tableColumns,
	})
}
