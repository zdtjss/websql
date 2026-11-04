package admin

import (
	"errors"
	"log"
	"strconv"
	"strings"

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

	// page 参数存在时进入管理员全量分页模式：空条件也返回全部用户。
	// 未传 page 保持旧契约（返回数组，空条件返回空列表），兼容旧客户端。
	if rawPage := c.Query("page"); rawPage != "" {
		page, _ := strconv.Atoi(rawPage)
		pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
		if page < 1 {
			page = 1
		}
		if pageSize <= 0 || pageSize > 100 {
			pageSize = 20
		}
		userList, total, err := getDefaultUserService().FindUserPage(roleId, name, loginName, key, page, pageSize)
		if err != nil {
			log.Printf("查询用户失败: %v", err)
			response.WriteErr(c, 200, 500, "操作失败")
			return
		}
		response.WriteOK(c, gin.H{"list": userList, "total": total, "page": page, "pageSize": pageSize})
		return
	}

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
		switch err.Error() {
		case "此登录名已存在", "登录名不能为空", "姓名不能为空", "密码不能为空",
			"密码长度不能少于6位", "系统至少保留一个管理员用户", "用户信息不能为空":
			response.WriteErr(c, 200, 400, err.Error())
		default:
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
	if err := ValidatePassword(newPwd); err != nil {
		response.WriteErr(c, 200, 400, err.Error())
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

// GetUserEffectivePermissions 返回用户合并后的有效数据权限，供管理员核对「配置」与「实际生效」。
//
// 数据来源与 AI 链路一致：多角色权限并集 + 内置管理员全量连接。
// 这里只做可观测性展示，不改变授权判定。
func GetUserEffectivePermissions(c *gin.Context) {
	userId := strings.TrimSpace(c.Query("userId"))
	if userId == "" {
		response.WriteErr(c, 200, 400, "缺少用户 ID")
		return
	}
	userList, err := getDefaultUserService().FindUser("", "", "", "", []string{userId})
	if err != nil {
		log.Printf("查询用户失败: %v", err)
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	if len(userList) == 0 {
		response.WriteErr(c, 200, 404, "用户不存在")
		return
	}
	user := userList[0]
	roles := FindUserRoles(userId)
	if roles == nil {
		roles = []*Role{}
	}
	powers := FindUserPowerDetails(userId)
	if powers == nil {
		powers = []*PowerDetail{}
	}
	enforced := AIPolicyEnforced()
	allowModify := RolesAllowModify(roles)
	aiAllowModify := RolesAllowAIModify(roles)
	aiModifyEffective := aiAllowModify
	if !enforced {
		// 兼容期：AI 写能力等同于「修改数据」，与运行层一致。
		aiModifyEffective = allowModify
	}
	response.WriteOK(c, gin.H{
		"userId":            user.Id,
		"userName":          user.Name,
		"loginName":         user.LoginName,
		"isAdmin":           isUserAdmin(userId),
		"allowModify":       allowModify,
		"aiAllowModify":     aiAllowModify,
		"aiModifyEffective": aiModifyEffective,
		"aiPolicyEnforce":   enforced,
		"roles":             roles,
		"powers":            powers,
	})
}

func DelUser(c *gin.Context) {
	if !CheckAdminPower(c) {
		return
	}
	// 前端历史上以 query 传参，后端读 form 导致删除恒失效；两者取非空值兼容。
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		id = strings.TrimSpace(c.PostForm("id"))
	}
	user := GetUser(appctx.Ctx.GetAuthorization(c))
	if user == nil {
		response.WriteErr(c, 200, 400, "未登录")
		return
	}
	if err := getDefaultUserService().Delete(id, user.Id, user.Name); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			response.WriteErr(c, 200, 404, err.Error())
			return
		}
		response.WriteErr(c, 200, 400, err.Error())
		return
	}
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
	audit.SetAdminChecker(func(c *gin.Context) bool { return CheckAdminPower(c) })
}
