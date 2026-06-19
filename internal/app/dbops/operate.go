package dbops

import (
	"websql/internal/app/conn"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func ListTableFat(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	connIdVal := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	ensureDefaultOperate()
	filteredTables := defaultOperateService.ListTableFat(connIdVal, schema, authorization)
	response.WriteOK(c, filteredTables)
}

func TableOptions(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	param := conn.ColumnsQuery{}
	c.ShouldBindJSON(&param)
	ensureDefaultOperate()
	data, err := defaultOperateService.GetTableOptions(param.ConnId, param.Schema, param.TableName, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	response.WriteOK(c, data)
}

func TableStatistics(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	param := conn.ColumnsQuery{}
	c.ShouldBindJSON(&param)
	ensureDefaultOperate()
	data, err := defaultOperateService.GetTableStatistics(param.ConnId, param.Schema, param.TableName, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	response.WriteOK(c, data)
}

func ListIndexes(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	param := conn.ColumnsQuery{}
	c.ShouldBindJSON(&param)
	ensureDefaultOperate()
	data, err := defaultOperateService.ListIndexes(param.ConnId, param.Schema, param.TableName, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	response.WriteOK(c, data)
}
