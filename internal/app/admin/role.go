package admin

import (
	"log"

	"websql/internal/pkg/appctx"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func SaveRole(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	role := &RoleSave{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, role); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	currentUser := GetUser(appctx.Ctx.GetAuthorization(c))
	err := getDefaultRole().SaveRole(role, currentUser.Id, currentUser.Name)
	if err != nil {
		log.Printf("保存角色失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, "保存成功")
}

func DelRole(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	id := c.Query("id")
	currentUser := GetUser(appctx.Ctx.GetAuthorization(c))
	err := getDefaultRole().DeleteRole(id, currentUser.Id, currentUser.Name)
	if err != nil {
		log.Printf("删除角色失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, "")
}

func RoleList(c *gin.Context) {
	roleList, err := getDefaultRole().RoleList()
	if err != nil {
		log.Printf("查询角色列表失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, roleList)
}

func RoleBaseList(c *gin.Context) {
	roleList, err := getDefaultRole().RoleBaseList()
	if err != nil {
		log.Printf("查询角色列表失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, roleList)
}

func FindUserByRole(c *gin.Context) {
	userList, err := getDefaultRole().FindUserByRole(c.PostForm("roleId"))
	if err != nil {
		log.Printf("查询用户失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, userList)
}

func SaveRolePermission(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	role := &RoleSave{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, role); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	currentUser := GetUser(appctx.Ctx.GetAuthorization(c))
	err := getDefaultRole().SaveRolePermission(role, currentUser.Id, currentUser.Name)
	if err != nil {
		log.Printf("保存权限失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, "保存成功")
}
