// import_upload.go — Excel 文件上传暂存（磁盘）与解析
//
// 流程：
// 1. 前端上传 Excel → HandleUploadExcel 保存原始文件到磁盘，解析表头和预览，返回 fileId
// 2. 前端显示预览，用户输入导入指令
// 3. 大模型收到 fileId + 列名，查表结构做字段匹配，调用 import_data 工具
// 4. import_data 工具通过 fileId 从磁盘读取文件并解析全量数据执行导入
// 5. 后台定时清理超过 30 分钟的暂存文件
package agentv2

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-web/utils"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

const uploadDir = "./data/uploads"
const uploadMaxAge = 30 * time.Minute

// uploadMeta 暂存文件的元信息（内存索引，文件在磁盘）
type uploadMeta struct {
	ID        string
	FileName  string
	DiskPath  string
	Columns   []string
	TotalRows int
	CreatedAt time.Time
}

var (
	uploadIndex   = make(map[string]*uploadMeta)
	uploadIndexMu sync.RWMutex
)

func init() {
	os.MkdirAll(uploadDir, 0o755)

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			cleanExpiredUploads()
		}
	}()
}

func cleanExpiredUploads() {
	uploadIndexMu.Lock()
	defer uploadIndexMu.Unlock()
	now := time.Now()
	for id, m := range uploadIndex {
		if now.Sub(m.CreatedAt) > uploadMaxAge {
			os.Remove(m.DiskPath)
			delete(uploadIndex, id)
			log.Printf("[UploadStore] 清理过期文件 - id=%s, name=%s\n", id, m.FileName)
		}
	}
}

// UploadedFile 供 import_data 工具使用的完整数据
type UploadedFile struct {
	Columns []string
	Data    [][]string
}

// GetUploadedFile 从磁盘读取暂存的 Excel 文件并解析全量数据
func GetUploadedFile(id string) (*UploadedFile, error) {
	uploadIndexMu.RLock()
	meta, ok := uploadIndex[id]
	uploadIndexMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("上传文件不存在或已过期（id=%s），请重新上传", id)
	}

	f, err := excelize.OpenFile(meta.DiskPath)
	if err != nil {
		return nil, fmt.Errorf("读取暂存文件失败：%w", err)
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("解析工作表失败：%w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("文件数据不足")
	}

	columns := rows[0]
	var data [][]string
	for _, row := range rows[1:] {
		hasValue := false
		for _, cell := range row {
			if cell != "" {
				hasValue = true
				break
			}
		}
		if hasValue {
			padded := make([]string, len(columns))
			copy(padded, row)
			data = append(data, padded)
		}
	}

	return &UploadedFile{Columns: columns, Data: data}, nil
}

// RemoveUploadedFile 删除暂存文件
func RemoveUploadedFile(id string) {
	uploadIndexMu.Lock()
	defer uploadIndexMu.Unlock()
	if m, ok := uploadIndex[id]; ok {
		os.Remove(m.DiskPath)
		delete(uploadIndex, id)
	}
}

// HandleUploadExcel 处理 Excel 文件上传
// POST /ai/agent/uploadExcel  (multipart/form-data, field: file)
func HandleUploadExcel(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		log.Printf("[UploadExcel] 文件上传失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件上传失败，请检查文件是否正确选择"})
		return
	}

	// 文件大小限制（20MB）
	const maxFileSize = 20 * 1024 * 1024
	if fileHeader.Size > maxFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件大小不能超过 20MB"})
		return
	}

	// 扩展名校验
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if ext != ".xlsx" && ext != ".xls" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 .xlsx 和 .xls 格式的 Excel 文件"})
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		log.Printf("[UploadExcel] 打开文件失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "打开文件失败，请重试"})
		return
	}
	defer src.Close()

	// 保存到磁盘
	fileID := utils.RandomStr()
	diskPath := filepath.Join(uploadDir, fileID+ext)

	dst, err := os.Create(diskPath)
	if err != nil {
		log.Printf("[UploadExcel] 保存文件失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败，请重试"})
		return
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(diskPath)
		log.Printf("[UploadExcel] 写入文件失败 - err=%v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入文件失败，请重试"})
		return
	}
	dst.Close()

	// 解析表头和预览
	xlsx, err := excelize.OpenFile(diskPath)
	if err != nil {
		os.Remove(diskPath)
		log.Printf("[UploadExcel] 读取 Excel 文件失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取 Excel 文件失败，请检查文件格式"})
		return
	}
	defer xlsx.Close()

	sheetName := xlsx.GetSheetName(0)
	allRows, err := xlsx.GetRows(sheetName)
	if err != nil {
		os.Remove(diskPath)
		log.Printf("[UploadExcel] 读取工作表失败 - err=%v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取工作表失败，请检查文件内容"})
		return
	}
	if len(allRows) < 2 {
		os.Remove(diskPath)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Excel 文件至少需要包含表头行和一行数据"})
		return
	}

	columns := allRows[0]

	// 统计有效数据行数 + 取前10行预览
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

	// 写入索引
	uploadIndexMu.Lock()
	uploadIndex[fileID] = &uploadMeta{
		ID:        fileID,
		FileName:  fileHeader.Filename,
		DiskPath:  diskPath,
		Columns:   columns,
		TotalRows: totalRows,
		CreatedAt: time.Now(),
	}
	uploadIndexMu.Unlock()

	log.Printf("[UploadExcel] 文件已暂存 - id=%s, name=%s, columns=%v, rows=%d\n",
		fileID, fileHeader.Filename, columns, totalRows)

	c.JSON(http.StatusOK, gin.H{
		"fileId":    fileID,
		"fileName":  fileHeader.Filename,
		"columns":   columns,
		"totalRows": totalRows,
		"preview":   preview,
	})
}

// HandlePreMatchColumns 预匹配 Excel 列与数据库表字段
// POST /ai/agent/preMatchColumns  { fileId, connId, tableName }
// 返回自动匹配结果，供前端展示确认
func HandlePreMatchColumns(c *gin.Context) {
	var req struct {
		FileID    string `json:"fileId"`
		ConnID    string `json:"connId"`
		TableName string `json:"tableName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数格式错误"})
		return
	}
	if req.FileID == "" || req.ConnID == "" || req.TableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fileId、connId、tableName 不能为空"})
		return
	}

	// 获取 Excel 列名
	uploadIndexMu.RLock()
	meta, ok := uploadIndex[req.FileID]
	uploadIndexMu.RUnlock()
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上传文件不存在或已过期，请重新上传"})
		return
	}

	// 获取数据库连接和表列名
	conn, dbType := GetConn(req.ConnID)
	if conn == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据库连接不存在"})
		return
	}

	// 从连接配置获取 schema
	_, dbSchema, _ := GetDBInfo(req.ConnID)

	tableColumns, err := getTableColumns(conn, dbType, dbSchema, req.TableName)
	if err != nil || len(tableColumns) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("获取表 %s 的列信息失败", req.TableName)})
		return
	}

	// 自动匹配
	mapping, _ := buildFinalMapping(meta.Columns, tableColumns, nil)

	// 构建结果
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
