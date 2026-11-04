package conn

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"websql/internal/app/admin"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// WriteConnError 把连接获取失败原因映射为稳定的 HTTP 语义：
// 无权访问 403、连接不存在 404、其余连接/隧道故障 500。
func WriteConnError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrConnNoPermission):
		response.WriteErr(c, http.StatusForbidden, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrConnNotFound):
		response.WriteErr(c, http.StatusNotFound, http.StatusNotFound, err.Error())
	case err != nil:
		response.WriteErr(c, 200, 500, err.Error())
	default:
		response.WriteErr(c, 200, 500, "连接不可用")
	}
}

func SaveConn(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	cfg := &ConnCfg{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, cfg); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}

	saved, err := getDefaultConn().SaveConn(cfg)
	if err != nil {
		switch {
		case errors.Is(err, ErrConnNameEmpty), errors.Is(err, ErrConnNameDuplicate):
			response.WriteErr(c, 200, 400, err.Error())
		case errors.Is(err, ErrConnOpenFailed):
			response.WriteErr(c, 200, 500, err.Error())
		default:
			log.Printf("[SaveConn] 保存失败: %v", err)
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

	dbSchema, dbVersion, dbType, err := getDefaultConn().TestDbConn(cfg)
	if err != nil {
		response.WriteErr(c, 200, 500, err.Error())
		return
	}

	response.WriteOK(c, gin.H{
		"msg":       "连接成功",
		"dbSchema":  dbSchema,
		"dbVersion": dbVersion,
		"dbType":    dbType,
	})
}

func DelConn(c *gin.Context) {
	if !admin.CheckAdminPower(c) {
		return
	}
	getDefaultConn().DeleteConn(c.Query("id"))
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

	cfgList, total, err := getDefaultConn().ListConn2(name, parentId, page, pageSize)
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
	cfgList, err := getDefaultConn().ListConnBase()
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, cfgList)
}

func ListUserConn(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	userPower := admin.GetUserPower(authorization)

	dtoList, err := getDefaultConn().ListUserConn(userPower)
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}

	response.WriteOK(c, dtoList)
}
