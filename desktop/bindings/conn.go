//go:build desktop

package bindings

import (
	"strconv"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/pkg/rpc"
)

// registerConn 注册 conn 模块的所有 binding。
//
// 对应 HTTP 路由 (internal/app/router.go):
//   - POST /api/saveConn      → conn.SaveConn
//   - POST /api/testDbConn    → conn.TestDbConn
//   - POST /api/delConn       → conn.DelConn
//   - GET  /api/listConn2     → conn.ListConn2
//   - GET  /api/listUserConn  → conn.ListUserConn
//
// 调用 service: internal/app/conn/conn_service.go
func registerConn(r *Registry) {
	// SaveConn: 保存连接配置，返回保存后的配置 (不含密码)
	// 入参 (Body): ConnCfg 结构
	r.register("conn", "SaveConn", func(req rpc.Request) rpc.Response {
		// 桌面模式默认 IsRemote=false，无需 admin 权限校验
		var cfg conn.ConnCfg
		if err := decodeBody(req.Body, &cfg); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		saved, err := conn.SaveConnByService(&cfg)
		if err != nil {
			return errResponse(err)
		}
		if saved != nil {
			return okResponse(*saved)
		}
		return okResponse("")
	})

	// TestDbConn: 测试数据库连接
	// 入参 (Body): ConnCfg 结构
	// 返回: msg/dbSchema/dbVersion/dbType
	r.register("conn", "TestDbConn", func(req rpc.Request) rpc.Response {
		var cfg conn.ConnCfg
		if err := decodeBody(req.Body, &cfg); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		dbSchema, dbVersion, dbType, err := conn.TestDbConnByService(&cfg)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(map[string]any{
			"msg":       "连接成功",
			"dbSchema":  dbSchema,
			"dbVersion": dbVersion,
			"dbType":    dbType,
		})
	})

	// DelConn: 删除连接
	// 入参 (Params): id
	r.register("conn", "DelConn", func(req rpc.Request) rpc.Response {
		id := req.StringParam("id")
		if id == "" {
			return rpc.Err(400, "缺少 id 参数")
		}
		conn.DeleteConnByService(id)
		return okResponse("")
	})

	// ListConn2: 分页查询连接列表
	// 入参 (Params): name, parentId, page, pageSize
	// 返回: data, total, page, pageSize
	r.register("conn", "ListConn2", func(req rpc.Request) rpc.Response {
		name := req.StringParam("name")
		parentId := req.StringParam("parentId")
		pageStr := req.StringParam("page")
		pageSizeStr := req.StringParam("pageSize")
		page, _ := strconv.Atoi(pageStr)
		if page == 0 {
			page = 1
		}
		pageSize, _ := strconv.Atoi(pageSizeStr)
		if pageSize == 0 {
			pageSize = 20
		}

		cfgList, total, err := conn.ListConn2ByService(name, parentId, page, pageSize)
		if err != nil {
			return errResponse(err)
		}
		// 清空密码，与 HTTP handler 行为一致
		for idx := range cfgList {
			cfgList[idx].Pwd = nil
		}
		return okResponse(map[string]any{
			"data":     cfgList,
			"total":    total,
			"page":     page,
			"pageSize": pageSize,
		})
	})

	// ListUserConn: 查询当前登录用户有权限的连接列表
	// 入参: Authorization
	// 返回: UserConnDTO 列表
	r.register("conn", "ListUserConn", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(401, "未登录")
		}
		userPower := admin.GetUserPower(req.Authorization)
		dtoList, err := conn.ListUserConnByService(userPower)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(dtoList)
	})
}
