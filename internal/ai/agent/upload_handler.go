package agent

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"websql/internal/ai/agent/fileparse"
	"websql/internal/ai/agent/uploadstore"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// 上传接口层（新链路）。
//
// 与 legacy /uploadExcel 的区别：上传阶段只做「校验 + 落盘 + 元数据登记」，
// 不解析内容（延迟解析由 read_file_data 触发，见 fileparse）。
//
// 校验顺序（fail-closed，与前端前置校验保持一致）：
//   1. 扩展名白名单（含未知格式与「已知但当前环境无解析实现」两类明确错误）
//   2. 文件体积（0 字节 / 超限 / 实际写入超限）
//   3. 文件头魔数（防扩展名伪装）
//   4. 图片头信息（尺寸 / 解码有效性）
//   5. 模型能力（image→vision，video→video）

// uploadHeadBufferSize 上传校验读取的头部缓冲大小。
//
// 64KB 足以覆盖魔数嗅探与常见图片格式的头信息解析（PNG IHDR、JPEG SOF 等），
// 从而在「落盘前」完成全部结构校验。
const uploadHeadBufferSize = 64 << 10

// HandleUploadFile POST /ai/agent/uploadFile
//
// multipart 字段：file（必填）、modelId（可选，用于能力校验；缺省用全局选中模型）
func HandleUploadFile(c *gin.Context) {
	ownerID := uploaderID(c)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		log.Printf("[Upload] 文件上传失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 400, "文件上传失败，请检查文件是否正确选择")
		return
	}

	ext := fileparse.NormalizeExt(filepath.Ext(fileHeader.Filename))
	kind, implName, supported := fileparse.Describe(ext)
	if !supported {
		if k, known := fileparse.KindOfExt(ext); known {
			log.Printf("[Upload] 环境不支持该格式解析 - ext=%s, kind=%s\n", ext, k)
			response.WriteErr(c, http.StatusBadRequest, 400,
				fmt.Sprintf("当前环境暂不支持 %s 类型文件的内容解析（%s）", string(k), ext))
			return
		}
		response.WriteErr(c, http.StatusBadRequest, 400, "仅支持 docx/xlsx/txt/csv/pdf/jpg/jpeg/png/mp4 格式的文件")
		return
	}

	if fileHeader.Size <= 0 {
		response.WriteErr(c, http.StatusBadRequest, 400, "文件内容为空")
		return
	}
	maxSize := fileparse.AllowedSize(ext)
	if maxSize > 0 && fileHeader.Size > maxSize {
		response.WriteErr(c, http.StatusBadRequest, 400, fmt.Sprintf("文件大小不能超过 %s", humanFileSize(maxSize)))
		return
	}

	// 前置校验（全部在落盘前完成）：
	//   1. 文件头魔数（防扩展名伪装）
	//   2. 图片头信息（尺寸 / 解码有效性）
	//   3. 模型能力（image→vision，video→video）
	// 顺序上先校验「文件本身是否有效」，再校验「模型能否处理」，
	// 避免对无效文件给出能力相关的误导提示。
	src, err := fileHeader.Open()
	if err != nil {
		log.Printf("[Upload] 打开文件失败 - err=%v\n", err)
		response.WriteErr(c, http.StatusBadRequest, 400, "打开文件失败，请重试")
		return
	}
	defer src.Close()

	// 读取头部缓冲：魔数与图片头校验只需头部，避免先落盘再删盘
	head := make([]byte, uploadHeadBufferSize)
	n, _ := io.ReadFull(src, head)
	head = head[:n]

	if err := fileparse.SniffBytes(ext, head); err != nil {
		response.WriteErr(c, http.StatusBadRequest, 400, err.Error())
		return
	}
	if kind == fileparse.KindImage {
		if _, err := fileparse.DetectImageReader(bytes.NewReader(head)); err != nil {
			response.WriteErr(c, http.StatusBadRequest, 400, err.Error())
			return
		}
	}
	if required := RequiredCapability(string(kind)); required != "" {
		modelID := strings.TrimSpace(c.PostForm("modelId"))
		if !resolveModelCapabilities(modelID)[required] {
			log.Printf("[Upload] 模型能力不匹配 - kind=%s, required=%s, modelId=%s, user=%s\n", kind, required, modelID, ownerID)
			response.WriteErr(c, http.StatusBadRequest, 400, CapabilityRejectText(string(kind)))
			return
		}
	}

	fileID, diskPath, size, err := saveUploadedTemp(io.MultiReader(bytes.NewReader(head), src), ext, maxSize)
	if err != nil {
		if err == errFileTooLarge {
			response.WriteErr(c, http.StatusBadRequest, 400, fmt.Sprintf("文件大小不能超过 %s", humanFileSize(maxSize)))
			return
		}
		log.Printf("[Upload] 保存文件失败 - name=%s, err=%v\n", fileHeader.Filename, err)
		response.WriteErr(c, http.StatusInternalServerError, 500, err.Error())
		return
	}

	displayName := filepath.Base(strings.ReplaceAll(fileHeader.Filename, "\\", "/"))
	meta := &uploadstore.FileMeta{
		ID:       fileID,
		UserID:   ownerID,
		FileName: displayName,
		DiskPath: diskPath,
		Ext:      ext,
		Size:     size,
		MIME:     mimeOfExt(ext),
		Kind:     string(kind),
		FileType: legacyFileType(ext),
	}
	globalUploadStore.Save(meta)

	log.Printf("[Upload] 文件已暂存（未解析）- id=%s, name=%s, ext=%s, kind=%s, size=%d, impl=%s\n",
		fileID, displayName, ext, kind, size, implName)

	_, degraded := degradedParser(implName)
	response.WriteOK(c, gin.H{
		"fileId":   fileID,
		"fileName": displayName,
		"fileSize": size,
		"fileExt":  ext,
		"fileKind": string(kind),
		"fileType": meta.FileType,
		"mime":     meta.MIME,
		"parser":   implName,
		"degraded": degraded,
		"expireAt": meta.ExpireAt.Unix(),
	})
}

// HandleDeleteUploadFile POST /ai/agent/uploadFile/delete
//
// body: {"fileId": "..."}；归属校验后删除暂存文件与解析缓存。
func HandleDeleteUploadFile(c *gin.Context) {
	var req struct {
		FileID string `json:"fileId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.FileID) == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "参数格式错误：fileId 不能为空")
		return
	}

	// 不存在、已过期、非本人文件统一返回同一文案（避免通过报错探测 fileId）
	if !IsUploadOwner(req.FileID, uploaderID(c)) {
		response.WriteErr(c, http.StatusBadRequest, 400, "文件不存在或已过期")
		return
	}

	RemoveUploadedFile(req.FileID)
	log.Printf("[Upload] 文件已删除 - id=%s, user=%s\n", req.FileID, uploaderID(c))
	response.WriteOK(c, gin.H{"ok": true})
}

// HandleUploadFileContent GET /ai/agent/uploadFile/content?fileId=...
//
// 归属校验后按 MIME 内联返回文件内容（供前端卡片缩略图/预览使用），
// 仅放行白名单内的展示型 MIME，并禁止内容嗅探（nosniff）。
func HandleUploadFileContent(c *gin.Context) {
	fileID := c.Query("fileId")
	meta, ok := globalUploadStore.Get(fileID)
	if !ok || (meta.UserID != "" && meta.UserID != uploaderID(c)) {
		response.WriteErr(c, http.StatusBadRequest, 400, "文件不存在或已过期")
		return
	}

	f, err := os.Open(meta.DiskPath)
	if err != nil {
		log.Printf("[Upload] 读取暂存文件失败 - id=%s, err=%v\n", fileID, err)
		response.WriteErr(c, http.StatusBadRequest, 400, "文件不存在或已过期")
		return
	}
	defer f.Close()

	mimeType := meta.MIME
	if mimeType == "" {
		mimeType = mimeOfExt(meta.Ext)
	}

	// 仅图片/视频/PDF/文本允许内联展示，其余一律以附件下载（防 XSS 与内容嗅探）
	disposition := "attachment"
	switch {
	case strings.HasPrefix(mimeType, "image/"),
		strings.HasPrefix(mimeType, "video/"),
		strings.HasPrefix(mimeType, "text/"),
		mimeType == "application/pdf":
		disposition = "inline"
	}

	c.Header("Content-Type", mimeType)
	c.Header("X-Content-Type-Options", "nosniff")
	if cd := mime.FormatMediaType(disposition, map[string]string{"filename": meta.FileName}); cd != "" {
		c.Header("Content-Disposition", cd)
	}

	http.ServeContent(c.Writer, c.Request, meta.FileName, meta.CreatedAt, f)
}

// HandleFileSupport GET /ai/agent/fileSupport
//
// 环境能力自检：返回各扩展名当前支持的解析实现与环境限制，
// 供前端在「文件选择阶段」做前置校验并给出准确提示
// （如「当前环境未启用 PDF 解析」），而不是等解析时才失败。
func HandleFileSupport(c *gin.Context) {
	exts := make([]gin.H, 0, 16)
	for _, ext := range fileparse.SupportedExts() {
		kind, implName, supported := fileparse.Describe(ext)
		_, degraded := degradedParser(implName)
		exts = append(exts, gin.H{
			"ext":       ext,
			"kind":      string(kind),
			"parser":    implName,
			"supported": supported,
			"degraded":  degraded,
			"maxSize":   fileparse.AllowedSize(ext),
		})
	}

	response.WriteOK(c, gin.H{
		"exts":            exts,
		"pythonAvailable": fileparse.PythonAvailable(),
		"pythonModules":   fileparse.PythonModules(),
		"storeTTLSeconds": int(globalUploadStore.TTL().Seconds()),
	})
}

// degradedParser 判断解析实现是否为降级实现（Go 兜底解析质量有限，需提示用户）
func degradedParser(implName string) (name string, degraded bool) {
	return implName, implName == "go-docx"
}
