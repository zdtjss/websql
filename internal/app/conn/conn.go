package conn

import (
	"errors"
	"strconv"

	"websql/internal/app/admin"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func SaveConn(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	cfg := &ConnCfg{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, cfg); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}

	ensureDefaultConn()
	saved, err := defaultConnService.SaveConn(cfg)
	if err != nil {
		if errors.Is(err, ErrConnOpenFailed) {
			response.WriteErr(c, 200, 500, err.Error())
		} else {
			response.WriteErr(c, 200, 500, "操作失败")
		}
		return
	}
	if saved != nil {
		response.WriteOK(c, *saved)
	} else {
		response.WriteOK(c, "")
	}
}

func TestDbConn(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	cfg := &ConnCfg{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, cfg); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}

	ensureDefaultConn()
	dbSchema, dbVersion, err := defaultConnService.TestDbConn(cfg)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	response.WriteOK(c, gin.H{
		"msg":       "连接成功",
		"dbSchema":  dbSchema,
		"dbVersion": dbVersion,
	})
}

func DelConn(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	ensureDefaultConn()
	defaultConnService.DeleteConn(c.Query("id"))
	response.WriteOK(c, "")
}

func ListConn2(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}

	name := c.Query("name")
	parentId := c.Query("parentId")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	ensureDefaultConn()
	cfgList, total, err := defaultConnService.ListConn2(name, parentId, page, pageSize)
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	for idx := range cfgList {
		cfgList[idx].Pwd = nil
	}
	response.WriteOK(c, map[string]any{
		"data":     cfgList,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func ListConnBase(c *gin.Context) {
	ensureDefaultConn()
	cfgList, err := defaultConnService.ListConnBase()
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, cfgList)
}

func ListUserConn(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	userPower := admin.GetUserPower(authorization)

	ensureDefaultConn()
	dtoList, err := defaultConnService.ListUserConn(userPower)
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	response.WriteOK(c, dtoList)
}
