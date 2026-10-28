package admin

import (
	"errors"
	"log"
	"net/http"

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
	digest, err := getDefaultRole().SaveRole(role, currentUser.Id, currentUser.Name)
	if err != nil {
		writeRoleSaveError(c, err)
		return
	}
	response.WriteOK(c, gin.H{
		"message":          "保存成功",
		"roleId":           role.Id,
		"permissionDigest": digest,
	})
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
		writeRoleSaveError(c, err)
		return
	}
	digest, _ := getDefaultRole().RolePermissionDigest(role.Id)
	response.WriteOK(c, gin.H{"message": "保存成功", "permissionDigest": digest})
}

// writeRoleSaveError 把角色保存错误映射为稳定契约：
//   - 参数非法：HTTP 200 + code 400（沿用前端按 code 判断的既有约定）
//   - 并发冲突：HTTP 409 + code 409（前端据此重新加载，禁止自动覆盖重试）
//   - 其他：HTTP 200 + code 500
func writeRoleSaveError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRoleSave):
		log.Printf("[Role] 参数校验失败 - err=%v", err)
		response.WriteErr(c, 200, 400, err.Error())
	case errors.Is(err, ErrRolePermissionConflict):
		log.Printf("[Role] 权限摘要冲突，拒绝覆盖 - err=%v", err)
		response.WriteErr(c, http.StatusConflict, http.StatusConflict, ErrRolePermissionConflict.Error())
	default:
		log.Printf("[Role] 保存失败 - err=%v", err)
		response.WriteErr(c, 200, 500, "操作失败")
	}
}

// RoleCapabilityInfo 返回角色能力版本与 AI 策略开关，供前端决定是否展示 AI 开关。
type RoleCapabilityInfo struct {
	CapabilityVersion int    `json:"capabilityVersion"`
	AIPolicyEnforce   bool   `json:"aiPolicyEnforce"`
	MaskVersion       string `json:"maskVersion,omitempty"`
}

// GetRoleCapability 查询角色能力配置。
func GetRoleCapability(c *gin.Context) {
	info := RoleCapabilityInfo{
		CapabilityVersion: RoleCapabilityVersion(),
		AIPolicyEnforce:   AIPolicyEnforced(),
	}
	if digest, err := MaskRulesDigest(); err == nil {
		info.MaskVersion = digest
	}
	response.WriteOK(c, info)
}
