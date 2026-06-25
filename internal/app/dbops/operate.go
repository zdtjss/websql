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

// ListObjects 列出指定类型的数据库对象。
// 对应 GET /api/db/objects?connId=xxx&schema=xxx&type=view|procedure|function|trigger|event|table
func ListObjects(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	objType := c.DefaultQuery("type", "view")
	ensureDefaultOperate()
	data, err := defaultOperateService.ListObjects(connId, schema, objType, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	response.WriteOK(c, data)
}

// GetObjectDDL 获取指定对象的 DDL 定义文本。
// 对应 GET /api/db/object/ddl?connId=xxx&schema=xxx&type=xxx&name=xxx
func GetObjectDDL(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	connId := appctx.Ctx.GetConnID(c)
	schema := c.Query("schema")
	name := c.Query("name")
	objType := c.DefaultQuery("type", "view")
	ensureDefaultOperate()
	ddl, err := defaultOperateService.GetObjectDDL(connId, schema, name, objType, authorization)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}
	response.WriteOK(c, ddl)
}
