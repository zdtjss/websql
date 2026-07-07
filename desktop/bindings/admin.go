//go:build desktop

package bindings

import (
	"websql/internal/app/admin"
	"websql/internal/config"
	"websql/internal/pkg/rpc"
)

// registerAdmin 注册 admin 模块的所有 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 admin 模块的方法:
//   登录:
//     - POST /api/login       → admin.Login
//     - POST /api/logout      → admin.Logout
//   用户:
//     - GET  /api/findUser       → admin.FindUser
//     - GET  /api/findUserBase   → admin.FindUserBase
//     - GET  /api/findUserByRole → admin.FindUserByRole
//     - POST /api/saveUser       → admin.SaveUser
//     - POST /api/delUser        → admin.DelUser
//     - POST /api/changePassword → admin.ChangePassword
//     - POST /api/saveUserBio    → admin.SaveUserBio
//   角色:
//     - GET  /api/roleList       → admin.RoleList
//     - GET  /api/roleBaseList   → admin.RoleBaseList
//     - POST /api/saveRole       → admin.SaveRole
//     - POST /api/delRole        → admin.DelRole
//     - GET  /api/userPermissions → admin.UserPermissions
//   权限/视图标记:
//     - GET  /api/canModifyData       → admin.CanModifyData
//     - GET  /api/canUseClassicView  → admin.CanUseClassicView
//
// 暂未实现 (桌面模式默认 IsRemote=false 时不需要，远程模式走 HTTP):
//   - GET  /api/permissionTree   → admin.GetPermissionTree  (权限树)
//   - GET  /api/listBackupData   → admin.ListBackupData     (备份历史)
//   - GET  /api/showBackupData    → admin.ShowBackupData      (备份内容预览)
//   - Prompt 系列 (PromptList/PromptDetail/SavePrompt/DelPrompt/PromptListByRole)
//
// 调用 service: internal/app/admin/binding_delegates.go
func registerAdmin(r *Registry) {
	// ===== 登录 =====

	r.register("admin", "Login", func(req rpc.Request) rpc.Response {
		loginName := req.StringBody("name")
		pwd := req.StringBody("password")
		key := req.StringBody("key")
		loginType := req.StringBody("loginType")

		result, err := admin.LoginByService(loginName, pwd, key, loginType)
		if err != nil {
			return rpc.Err(400, err.Error())
		}
		return okResponse(result)
	})

	r.register("admin", "Logout", func(req rpc.Request) rpc.Response {
		admin.LogoutByService(req.Authorization)
		return okResponse("退出成功")
	})

	// ===== 用户 =====

	r.register("admin", "FindUserBase", func(req rpc.Request) rpc.Response {
		// 桌面模式默认 IsRemote=false，无需 admin 权限校验
		loginName := req.StringParam("loginName")
		key := req.StringParam("key")
		userList, err := admin.FindUserBaseByService(loginName, key)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(userList)
	})

	r.register("admin", "FindUser", func(req rpc.Request) rpc.Response {
		key := req.StringParam("key")
		name := req.StringParam("name")
		loginName := req.StringParam("loginName")
		roleId := req.StringParam("roleId")
		// userIdList 在 HTTP 模式是表单数组，binding 模式需要前端传 string slice
		var userIdList []string
		if arr, ok := req.Params["userIdList"].([]interface{}); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					userIdList = append(userIdList, s)
				}
			}
		}
		userList, err := admin.FindUserByService(roleId, name, loginName, key, userIdList)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(userList)
	})

	r.register("admin", "FindUserByRole", func(req rpc.Request) rpc.Response {
		roleId := req.StringBody("roleId")
		if roleId == "" {
			roleId = req.StringParam("roleId")
		}
		userList, err := admin.FindUserByRoleByService(roleId)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(userList)
	})

	r.register("admin", "SaveUser", func(req rpc.Request) rpc.Response {
		var user admin.User
		if err := decodeBody(req.Body, &user); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		// 桌面模式默认 admin，currentUserId/currentUserName 取当前登录用户
		userId := extractUserId(req.Authorization)
		userName := ""
		if u := admin.GetUser(req.Authorization); u != nil {
			userName = u.Name
		}
		if err := admin.SaveUserByService(&user, userId, userName); err != nil {
			// 与 HTTP handler 一致：登录名重复返回 400
			if err.Error() == "此登录名已存在" {
				return rpc.Err(400, err.Error())
			}
			return errResponse(err)
		}
		return okResponse("")
	})

	r.register("admin", "DelUser", func(req rpc.Request) rpc.Response {
		id := req.StringBody("id")
		if id == "" {
			return rpc.Err(400, "缺少 id 参数")
		}
		if err := admin.DeleteUserByService(id); err != nil {
			return errResponse(err)
		}
		return okResponse("")
	})

	r.register("admin", "ChangePassword", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(400, "未登录")
		}
		oldPwd := req.StringBody("oldPassword")
		newPwd := req.StringBody("newPassword")
		if oldPwd == "" || newPwd == "" {
			return rpc.Err(400, "旧密码和新密码不能为空")
		}
		if len(newPwd) < 6 {
			return rpc.Err(400, "新密码长度不能少于6位")
		}
		if err := admin.ChangePasswordByService(userId, oldPwd, newPwd); err != nil {
			// 与 HTTP handler 一致：业务错误返回 400
			if err.Error() == "用户信息异常" || err.Error() == "旧密码不正确" {
				return rpc.Err(400, err.Error())
			}
			return errResponse(err)
		}
		return okResponse("密码修改成功")
	})

	r.register("admin", "SaveUserBio", func(req rpc.Request) rpc.Response {
		bioKey := req.StringBody("bioKey")
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(400, "未登录")
		}
		if err := admin.SaveUserBioByService(userId, bioKey); err != nil {
			return errResponse(err)
		}
		return okResponse("设置成功")
	})

	// ===== 角色 =====

	r.register("admin", "RoleList", func(req rpc.Request) rpc.Response {
		list, err := admin.RoleListByService()
		if err != nil {
			return errResponse(err)
		}
		return okResponse(list)
	})

	r.register("admin", "RoleBaseList", func(req rpc.Request) rpc.Response {
		list, err := admin.RoleBaseListByService()
		if err != nil {
			return errResponse(err)
		}
		return okResponse(list)
	})

	r.register("admin", "SaveRole", func(req rpc.Request) rpc.Response {
		var role admin.RoleSave
		if err := decodeBody(req.Body, &role); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		userId := extractUserId(req.Authorization)
		userName := ""
		if u := admin.GetUser(req.Authorization); u != nil {
			userName = u.Name
		}
		if err := admin.SaveRoleByService(&role, userId, userName); err != nil {
			return errResponse(err)
		}
		return okResponse("保存成功")
	})

	r.register("admin", "DelRole", func(req rpc.Request) rpc.Response {
		id := req.StringParam("id")
		if id == "" {
			return rpc.Err(400, "缺少 id 参数")
		}
		userId := extractUserId(req.Authorization)
		userName := ""
		if u := admin.GetUser(req.Authorization); u != nil {
			userName = u.Name
		}
		if err := admin.DeleteRoleByService(id, userId, userName); err != nil {
			return errResponse(err)
		}
		return okResponse("")
	})

	// ===== 权限/视图标记 =====

	// UserPermissions: 桌面模式 IsRemote=false 时返回 ["__all__"] 表示全权限。
	// 远程模式走真实权限查询，与 HTTP handler 行为一致。
	r.register("admin", "UserPermissions", func(req rpc.Request) rpc.Response {
		if !config.Cfg.IsRemote {
			return okResponse([]string{"__all__"})
		}
		return okResponse(admin.UserPermissionsByService(req.Authorization))
	})

	// CanModifyData: 桌面模式默认允许修改，远程模式查 t_role.allow_modify
	r.register("admin", "CanModifyData", func(req rpc.Request) rpc.Response {
		if !config.Cfg.IsRemote {
			return okResponse(map[string]any{"allowed": true})
		}
		allowed := admin.CanModifyDataByService(req.Authorization)
		return okResponse(map[string]any{"allowed": allowed})
	})

	// CanUseClassicView: 桌面模式默认允许，远程模式查 t_role.view_classic
	r.register("admin", "CanUseClassicView", func(req rpc.Request) rpc.Response {
		if !config.Cfg.IsRemote {
			return okResponse(map[string]any{"allowed": true})
		}
		allowed := admin.CanUseClassicViewByService(req.Authorization)
		return okResponse(map[string]any{"allowed": allowed})
	})

	// ===== 远程模式专用方法 (桌面模式 IsRemote=false 时不可用) =====
	// 以下方法 (Prompt 系列 / GetPermissionTree / ListBackupData / ShowBackupData)
	// 仅在 IsRemote=true 的多用户部署场景下使用，桌面模式默认禁用。
	// 业务逻辑尚未提取 ByService 函数；如需在桌面模式启用，请先在
	// internal/app/admin/binding_delegates.go 中提取 service，再替换占位实现。
	registerAdminRemoteOnly(r)
}

// registerAdminRemoteOnly 注册远程模式专用方法的占位 binding。
// 桌面模式 (IsRemote=false) 调用时返回 403，明确告知前端此功能不可用。
// 远程模式 (IsRemote=true) 调用时返回 501，提示走 HTTP API 或后续提取 service。
func registerAdminRemoteOnly(r *Registry) {
	placeholder := func(method string) Handler {
		return func(req rpc.Request) rpc.Response {
			if !config.Cfg.IsRemote {
				return rpc.Err(403, "桌面模式不支持此功能: admin."+method)
			}
			return rpc.Err(501, "此方法尚未在桌面 binding 实现，请走 HTTP API: admin."+method)
		}
	}

	// Prompt 系列
	r.register("admin", "PromptList", placeholder("PromptList"))
	r.register("admin", "PromptListByRole", placeholder("PromptListByRole"))
	r.register("admin", "PromptDetail", placeholder("PromptDetail"))
	r.register("admin", "SavePrompt", placeholder("SavePrompt"))
	r.register("admin", "DelPrompt", placeholder("DelPrompt"))

	// 权限树与备份历史
	r.register("admin", "GetPermissionTree", placeholder("GetPermissionTree"))
	r.register("admin", "ListBackupData", placeholder("ListBackupData"))
	r.register("admin", "ShowBackupData", placeholder("ShowBackupData"))
}
