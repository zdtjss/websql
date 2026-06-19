package backup

import (
	"net/http"

	"websql/internal/config"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func init() {
	_ = config.Cfg
}

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

	result, err := defaultBackupService.CreateBackup(connId, schema, name, description, tablesStr, withData, encrypt, authorization)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 500, err.Error())
		return
	}
	response.WriteOK(c, result)
}

func ListBackups(c *gin.Context) {
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")

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

	result, err := defaultBackupService.RestoreBackup(backupId, connId, authorization)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 400, err.Error())
		return
	}
	response.WriteOK(c, result)
}

func DeleteBackup(c *gin.Context) {
	backupId := c.PostForm("backupId")

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

	result, err := defaultBackupService.GetBackupTables(connId, schema, authorization)
	if err != nil {
		response.WriteErr(c, http.StatusOK, 500, err.Error())
		return
	}
	response.WriteOK(c, result)
}

func DownloadBackup(c *gin.Context) {
	backupId := c.Query("backupId")

	err := defaultBackupService.DownloadBackup(c, backupId)
	if err != nil {
		response.WriteErr(c, http.StatusNotFound, 500, err.Error())
		return
	}
}
