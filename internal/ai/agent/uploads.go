package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/ai/agent/fileparse"
	"websql/internal/ai/agent/uploadstore"
	admin "websql/internal/app/admin"
	"websql/internal/pkg/idgen"

	"github.com/gin-gonic/gin"
)

// 上传暂存域的 Agent 侧装配与共享辅助函数。
//
// 分层：uploadstore（元数据/TTL/归属） + fileparse（按需解析） + 本文件（装配与兼容壳）。
// 既有调用方（read_file_data、会话请求归属校验、legacy 上传接口）都经此处访问，
// 保证「上传只存储、读取才解析」的链路唯一。

const (
	// uploadDir 上传文件暂存目录（与既有部署一致）
	uploadDir = "./data/uploads"
	// uploadMaxAge 暂存有效期（与既有 30 分钟保持一致）
	uploadMaxAge = 30 * time.Minute
	// uploadCleanSec 后台清理周期
	uploadCleanSec = 5 * time.Minute
	// maxUploadEntries 暂存条目上限，防内存无限增长（淘汰时同步删盘）
	maxUploadEntries = 256
)

// globalUploadStore 全局上传暂存库（进程内单例）
var globalUploadStore = uploadstore.NewStore(uploadDir, uploadMaxAge, uploadCleanSec, maxUploadEntries)

// InitUploadStoreLog 启动阶段输出格式支持概览（由 main/router 初始化流程调用）
func InitUploadStoreLog() {
	fileparse.InitLog()
}

// uploaderID 从请求上下文取上传者用户 ID（AuthMiddleware 已注入 currentUser）。
func uploaderID(c *gin.Context) string {
	if v, ok := c.Get("currentUser"); ok {
		if u, ok := v.(*admin.User); ok && u != nil {
			return u.Id
		}
	}
	return ""
}

// IsUploadOwner 判断上传文件是否属于指定用户（供会话请求侧做归属校验）。
// 文件不存在时返回 false（过期文件不再向提示词暴露元信息）。
func IsUploadOwner(id, userID string) bool {
	return globalUploadStore.IsOwner(id, userID)
}

// LookupUploadedFile 读取文件元数据（不解析内容），供上传响应与能力校验使用。
func LookupUploadedFile(id string) (*uploadstore.FileMeta, bool) {
	return globalUploadStore.Get(id)
}

// GetUploadedFile 读取已上传文件内容（延迟解析：首次调用时才解析，结果缓存复用），
// 仅允许文件所有者访问。
//
// 归属校验：fileId 为加密安全随机 token（不可枚举），且必须与上传者一致；
// 两者缺一都会让任一登录用户通过枚举 fileId 读取他人上传的数据文件（IDOR）。
// 不存在与无权访问返回同一提示，避免通过报错探测 fileId 是否存在。
func GetUploadedFile(ctx context.Context, id, userID string) (*fileparse.Result, error) {
	meta, ok := globalUploadStore.Get(id)
	if !ok {
		return nil, fmt.Errorf("上传文件不存在或已过期（id=%s），请重新上传", id)
	}
	if meta.UserID != "" && meta.UserID != userID {
		log.Printf("[Upload] 拒绝跨用户读取上传文件 - id=%s, owner=%s, requester=%s\n", id, meta.UserID, userID)
		return nil, fmt.Errorf("上传文件不存在或已过期，请重新上传")
	}

	res, _, err := fileparse.ParseCached(ctx, id, fileparse.Source{
		Path:     meta.DiskPath,
		FileName: meta.FileName,
		Ext:      meta.Ext,
		Size:     meta.Size,
	})
	if err != nil {
		log.Printf("[Upload] 解析上传文件失败 - id=%s, name=%s, err=%v\n", id, meta.FileName, err)
		return nil, err
	}
	return res, nil
}

// RemoveUploadedFile 删除上传文件（元数据 + 磁盘文件 + 解析缓存）。
func RemoveUploadedFile(id string) {
	globalUploadStore.Delete(id)
	fileparse.Purge(id)
}

// RemoveSessionAttachments 会话删除后清理其引用的暂存文件。
//
// 归属校验后逐个删除：非本人持有、已过期或已清理的文件静默跳过（幂等），
// 防止借删除会话清理他人上传的文件。
func RemoveSessionAttachments(fileIDs []string, userID string) {
	for _, fileID := range fileIDs {
		if !IsUploadOwner(fileID, userID) {
			continue
		}
		RemoveUploadedFile(fileID)
	}
}

// maxChatAttachments 单轮对话最多携带的附件数（防提示词膨胀与批量枚举探测）
const maxChatAttachments = 10

// SanitizeChatAttachments 校验会话请求携带的文件引用，返回可信的附件列表。
//
// 过滤规则（fail-closed）：
//   - 空 ID / 重复 ID 丢弃；
//   - 文件不存在或已过期（暂存 30 分钟）丢弃；
//   - 非本人文件丢弃（防 IDOR：文件内容与元信息均属私有，与 read_file_data 同源校验）；
//   - 超过 maxChatAttachments 的引用丢弃。
//
// 通过校验的引用，其展示名 / 类别 / 大小 / 过期时间一律按服务端暂存元数据回填，
// 不信任客户端传值（防伪造文件名或类别注入系统提示词）。
//
// 返回 nil 表示本轮没有可用附件：不阻断对话，模型侧无文件上下文。
func SanitizeChatAttachments(refs []AttachmentRef, userID string) []AttachmentRef {
	out := make([]AttachmentRef, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		id := strings.TrimSpace(ref.FileID)
		if id == "" || seen[id] {
			continue
		}
		meta, ok := globalUploadStore.Get(id)
		if !ok {
			log.Printf("[Upload] 丢弃不存在或已过期的上传文件引用 - user=%s, fileId=%s\n", userID, id)
			continue
		}
		if meta.UserID != "" && meta.UserID != userID {
			log.Printf("[Upload] 丢弃非本人上传文件引用 - user=%s, fileId=%s\n", userID, id)
			continue
		}
		if len(out) >= maxChatAttachments {
			log.Printf("[Upload] 单轮附件数超过上限，丢弃多余引用 - user=%s, limit=%d\n", userID, maxChatAttachments)
			break
		}
		seen[id] = true
		out = append(out, AttachmentRef{
			FileID:   id,
			FileName: meta.FileName,
			FileKind: meta.Kind,
			FileExt:  meta.Ext,
			FileSize: meta.Size,
			ExpireAt: meta.ExpireAt.Unix(),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// saveUploadedTemp 把上传文件写入暂存目录，返回 fileId、磁盘路径与实际写入字节数。
//
// 磁盘文件名恒为 fileId + 扩展名：用户提供的原始文件名只作为展示名保存，
// 不参与路径拼接（防路径穿越与重名覆盖）。
// limit > 0 时写入超过 limit 立即失败并清理磁盘残留
// （防御客户端声明小体积、实际发送大体积的绕过行为）。
func saveUploadedTemp(src io.Reader, ext string, limit int64) (fileID, diskPath string, size int64, err error) {
	fileID = idgen.SecureRandomToken()
	diskPath = filepath.Join(globalUploadStore.Dir(), fileID+ext)

	dst, err := os.Create(diskPath)
	if err != nil {
		return "", "", 0, fmt.Errorf("保存文件失败，请重试")
	}

	reader := src
	if limit > 0 {
		reader = io.LimitReader(src, limit+1)
	}
	written, copyErr := io.Copy(dst, reader)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(diskPath)
		return "", "", 0, fmt.Errorf("写入文件失败，请重试")
	}
	if limit > 0 && written > limit {
		os.Remove(diskPath)
		return "", "", 0, errFileTooLarge
	}
	return fileID, diskPath, written, nil
}

// errFileTooLarge 实际写入体积超过上限（防御声明与大体积不一致的客户端）
var errFileTooLarge = errors.New("文件大小超过限制")

// humanFileSize 把字节数格式化为易读文本（错误提示使用）
func humanFileSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%dMB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%dKB", n>>10)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// legacyFileType 把扩展名映射为既有 fileType 命名（响应兼容）。
func legacyFileType(ext string) string {
	switch fileparse.NormalizeExt(ext) {
	case ".xlsx", ".xls":
		return "excel"
	case ".csv":
		return "csv"
	case ".md", ".markdown":
		return "markdown"
	case ".jpg", ".jpeg", ".png":
		return "image"
	default:
		return strings.TrimPrefix(fileparse.NormalizeExt(ext), ".")
	}
}

// mimeOfExt 返回扩展名对应的展示 MIME（下载/内联响应使用）
func mimeOfExt(ext string) string {
	switch fileparse.NormalizeExt(ext) {
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".txt", ".md", ".markdown":
		return "text/plain; charset=utf-8"
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".mp4":
		return "video/mp4"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".xls":
		return "application/vnd.ms-excel"
	default:
		return "application/octet-stream"
	}
}
