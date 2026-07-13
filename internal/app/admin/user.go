package admin

import (
	"log"

	"websql/internal/audit"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func FindUserBase(c *gin.Context) {
	loginName := c.Query("loginName")
	key := c.Query("key")
	userList, err := getDefaultUserService().FindUserBase(loginName, key)
	if err != nil {
		log.Printf("查询用户失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, userList)
}

func FindUser(c *gin.Context) {
	key := c.Query("key")
	name := c.Query("name")
	loginName := c.Query("loginName")
	roleId := c.Query("roleId")
	userIdList, _ := c.GetPostFormArray("userIdList")
	userList, err := getDefaultUserService().FindUser(roleId, name, loginName, key, userIdList)
	if err != nil {
		log.Printf("查询用户失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, userList)
}

func SaveUser(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	user := &User{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, user); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	currentUser := GetUser(appctx.Ctx.GetAuthorization(c))
	err := getDefaultUserService().Save(user, currentUser.Id, currentUser.Name)
	if err != nil {
		if err.Error() == "此登录名已存在" {
			response.WriteErr(c, 200, 400, err.Error())
		} else {
			log.Printf("保存用户失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
		}
		return
	}
	response.WriteOK(c, "")
}

func SaveUserBio(c *gin.Context) {
	bioKey := c.PostForm("bioKey")
	authorization := appctx.Ctx.GetAuthorization(c)
	user := GetUser(authorization)
	err := getDefaultUserService().SaveUserBio(user.Id, bioKey)
	if err != nil {
		log.Printf("保存用户失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, "设置成功")
}

func ChangePassword(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	user := GetUser(authorization)
	if user == nil || user.Id == "" {
		response.WriteErr(c, 200, 400, "未登录")
		return
	}

	oldPwd := c.PostForm("oldPassword")
	newPwd := c.PostForm("newPassword")

	if oldPwd == "" || newPwd == "" {
		response.WriteErr(c, 200, 400, "旧密码和新密码不能为空")
		return
	}
	if len(newPwd) < 6 {
		response.WriteErr(c, 200, 400, "新密码长度不能少于6位")
		return
	}

	err := getDefaultUserService().ChangePassword(user.Id, oldPwd, newPwd)
	if err != nil {
		if err.Error() == "用户信息异常" || err.Error() == "旧密码不正确" {
			response.WriteErr(c, 200, 400, err.Error())
		} else {
			log.Printf("密码加密失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
		}
		return
	}

	response.WriteOK(c, "密码修改成功")
}

func GetUserToken(c *gin.Context) {
	authorization := appctx.Ctx.GetAuthorization(c)
	user := GetUser(authorization)
	response.WriteOK(c, map[string]any{
		"id":    user.Id,
		"name":  user.Name,
		"token": authorization,
	})
}

func InitUser(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	err := getDefaultUserService().InitUser()
	if err != nil {
		log.Printf("初始化用户失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, "初始化成功")
}

func DelUser(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	getDefaultUserService().Delete(c.PostForm("id"))
	response.WriteOK(c, "")
}

// recordPermissionAudit 记录权限变更审计日志
func recordPermissionAudit(toolName, sqlText, userId, userName string) {
	audit.GetAuditService().Record(&audit.AuditEntry{
		Source:    "permission",
		ToolName:  toolName,
		SQLText:   sqlText,
		SQLType:   "PERMISSION_CHANGE",
		RiskLevel: "high",
		Status:    "success",
		UserID:    userId,
		UserName:  userName,
	})
}

func init() {
	// 注入管理员权限校验函数，打破 admin ↔ audit 循环依赖
	audit.SetAdminChecker(func(c *gin.Context) { CheckAdminPower(c) })
}
