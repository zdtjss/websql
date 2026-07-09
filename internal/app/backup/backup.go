package backup

import (
	"net/http"

	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func CreateBackup(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.PostForm("schema")
	name := c.PostForm("name")
	description := c.PostForm("description")
	tablesStr := c.PostForm("tables")
	withData := c.DefaultPostForm("withData", "true")
	encrypt := c.DefaultPostForm("encrypt", "false")
	_ = c.DefaultPostForm("compress", "false")
	authorization := appctx.Ctx.GetAuthorization(c)

	ensureDefaultBackup()
	// 异步执行备份，立即返回 taskId 供前端轮询进度
	taskId := defaultBackupService.CreateBackupAsync(connId, schema, name, description, tablesStr, withData, encrypt, authorization)
	response.WriteOK(c, map[string]any{
		"taskId": taskId,
		"status": "running",
	})
}

// GetBackupProgress 查询备份任务进度，对应 GET /backup/progress
func GetBackupProgress(c *gin.Context) {
	taskId := c.Query("taskId")

	ensureDefaultBackup()
	progress, ok := FetchBackupProgress(taskId)
	if !ok {
		// 进度不存在（已被清理或 taskId 非法），返回 not_found 状态
		response.WriteOK(c, map[string]any{
			"status":  "not_found",
			"message": "进度信息不存在或已过期",
		})
		return
	}
	response.WriteOK(c, progress)
}

func ListBackups(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")

	ensureDefaultBackup()
	result, err := defaultBackupService.ListBackups(connId, schema)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 500, err.Error())
		return
	}
	response.WriteOK(c, result)
}

func RestoreBackup(c *gin.Context) {
	backupId := c.PostForm("backupId")
	connId := appctx.Ctx.GetConnID(c)
	authorization := appctx.Ctx.GetAuthorization(c)

	ensureDefaultBackup()
	result, err := defaultBackupService.RestoreBackup(backupId, connId, authorization)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 400, err.Error())
		return
	}
	response.WriteOK(c, result)
}

func DeleteBackup(c *gin.Context) {
	backupId := c.PostForm("backupId")

	ensureDefaultBackup()
	err := defaultBackupService.DeleteBackup(backupId)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 500, err.Error())
		return
	}
	response.WriteOK(c, map[string]any{"success": true})
}

func GetBackupTables(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	authorization := appctx.Ctx.GetAuthorization(c)

	ensureDefaultBackup()
	result, err := defaultBackupService.GetBackupTables(connId, schema, authorization)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 500, err.Error())
		return
	}
	response.WriteOK(c, result)
}

func DownloadBackup(c *gin.Context) {
	backupId := c.Query("backupId")

	ensureDefaultBackup()
	err := defaultBackupService.DownloadBackup(c, backupId)
	if err != nil {
		response.WriteErr(c, http.StatusNotFound, 500, err.Error())
		return
	}
}
