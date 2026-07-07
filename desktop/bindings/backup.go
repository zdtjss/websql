//go:build desktop

package bindings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"websql/internal/app/backup"
	"websql/internal/pkg/rpc"
)

// registerBackup 注册 backup 模块的所有 binding。
//
// 对应 HTTP 路由 (internal/app/router.go):
//   - POST /api/backup/create   → backup.CreateBackup (异步,返回 taskId)
//   - GET  /api/backup/progress  → backup.GetBackupProgress
//   - GET  /api/backup/list      → backup.ListBackups
//   - POST /api/backup/restore   → backup.RestoreBackup
//   - POST /api/backup/delete    → backup.DeleteBackup
//   - GET  /api/backup/tables    → backup.GetBackupTables
//   - GET  /api/backup/download  → backup.DownloadBackup (BlobHandler)
//
// 调用 service: internal/app/backup/backup_service.go
func registerBackup(r *Registry) {
	// CreateBackup: 异步启动备份任务
	// 入参 (Body): schema, name, description, tables, withData, encrypt
	// 返回: taskId, status
	r.register("backup", "CreateBackup", func(req rpc.Request) rpc.Response {
		// 默认值与 HTTP handler 一致
		withData := req.StringBody("withData")
		if withData == "" {
			withData = "true"
		}
		encrypt := req.StringBody("encrypt")
		if encrypt == "" {
			encrypt = "false"
		}
		taskId := backup.CreateBackupAsyncByService(
			req.ConnID,
			req.StringBody("schema"),
			req.StringBody("name"),
			req.StringBody("description"),
			req.StringBody("tables"),
			withData,
			encrypt,
			req.Authorization,
		)
		return okResponse(map[string]any{
			"taskId": taskId,
			"status": "running",
		})
	})

	// GetBackupProgress: 查询备份任务进度
	// 入参 (Params): taskId
	r.register("backup", "GetBackupProgress", func(req rpc.Request) rpc.Response {
		taskId := req.StringParam("taskId")
		if taskId == "" {
			return rpc.Err(400, "缺少 taskId 参数")
		}
		progress, ok := backup.GetBackupProgressByService(taskId)
		if !ok {
			return okResponse(map[string]any{
				"status":  "not_found",
				"message": "进度信息不存在或已过期",
			})
		}
		return okResponse(progress)
	})

	// ListBackups: 列出指定连接/schema 下的备份记录
	// 入参 (Params): schema
	r.register("backup", "ListBackups", func(req rpc.Request) rpc.Response {
		result, err := backup.ListBackupsByService(req.ConnID, req.StringParam("schema"))
		if err != nil {
			return errResponse(err)
		}
		return okResponse(result)
	})

	// RestoreBackup: 从备份恢复数据到指定连接
	// 入参 (Body): backupId
	r.register("backup", "RestoreBackup", func(req rpc.Request) rpc.Response {
		backupId := req.StringBody("backupId")
		if backupId == "" {
			return rpc.Err(400, "缺少 backupId 参数")
		}
		result, err := backup.RestoreBackupByService(backupId, req.ConnID, req.Authorization)
		if err != nil {
			return rpc.Err(400, err.Error())
		}
		return okResponse(result)
	})

	// DeleteBackup: 删除备份记录
	// 入参 (Body): backupId
	r.register("backup", "DeleteBackup", func(req rpc.Request) rpc.Response {
		backupId := req.StringBody("backupId")
		if backupId == "" {
			return rpc.Err(400, "缺少 backupId 参数")
		}
		if err := backup.DeleteBackupByService(backupId); err != nil {
			return errResponse(err)
		}
		return okResponse(map[string]any{"success": true})
	})

	// GetBackupTables: 查询指定连接/schema 下的表和视图列表，供备份前选择
	// 入参 (Params): schema
	r.register("backup", "GetBackupTables", func(req rpc.Request) rpc.Response {
		result, err := backup.GetBackupTablesByService(req.ConnID, req.StringParam("schema"), req.Authorization)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(result)
	})

	// DownloadBackup: 下载备份文件 (BlobHandler)
	// 入参 (Params): backupId
	r.registerBlob("backup", "DownloadBackup", func(req rpc.Request) (BlobResult, error) {
		backupId := req.StringParam("backupId")
		if backupId == "" {
			return BlobResult{}, fmt.Errorf("缺少 backupId 参数")
		}

		// 先查文件名 (实际下载内容时不重复查询)
		// 调用 DownloadBackupByService 时会返回文件名
		var fileName string
		path, err := writeBlobToTemp("backup.tmp", func(w *os.File) error {
			name, err := backup.DownloadBackupByService(backupId, w)
			if err != nil {
				return err
			}
			fileName = name
			return nil
		})
		if err != nil {
			return BlobResult{}, err
		}
		if fileName == "" {
			fileName = "backup.sql"
		}
		// 重命名临时文件为正确的文件名，避免前端保存时仍是 backup.tmp
		finalPath := path
		if finalName := sanitizeBackupFilename(fileName); finalName != "" {
			finalPath = filepath.Join(filepath.Dir(path), finalName)
			if renameErr := os.Rename(path, finalPath); renameErr != nil {
				// 重命名失败时回退使用原路径，文件名仍用 fileName
				finalPath = path
			}
		}
		return BlobResult{
			Path:     finalPath,
			Filename: fileName,
			Mime:     "application/octet-stream",
		}, nil
	})
}

// sanitizeBackupFilename 把 service 返回的文件名清理为安全的本地文件名。
// 防止路径穿越，仅保留 basename。
func sanitizeBackupFilename(name string) string {
	if name == "" {
		return ""
	}
	// 去除路径分隔符
	name = strings.ReplaceAll(name, "/", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "..", "")
	return name
}
