package permission

import (
	"sort"
	"strings"

	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// GetTableAccess 返回当前登录用户对指定表的有效访问级别。
//
// level=full：表级/连接级放行；level=column：仅列级授权（经典视图会降级为表级放行，
// 列级限制只约束 AI 链路）；level=none：无权限。仅用于前端提示，不作为授权依据。
func GetTableAccess(c *gin.Context) {
	connId := strings.TrimSpace(c.Query("connId"))
	schemaName := strings.TrimSpace(c.Query("schema"))
	tableName := strings.TrimSpace(c.Query("table"))
	if connId == "" || tableName == "" {
		response.WriteErr(c, 200, 400, "缺少连接或表名")
		return
	}

	access := GetTableColumnAccess(connId, schemaName, tableName, appctx.Ctx.GetAuthorization(c))
	allowedColumns := make([]string, 0, len(access.AllowedColumns))
	for col := range access.AllowedColumns {
		allowedColumns = append(allowedColumns, col)
	}
	sort.Strings(allowedColumns)

	response.WriteOK(c, gin.H{
		"level":          string(access.Level),
		"allowedColumns": allowedColumns,
	})
}
