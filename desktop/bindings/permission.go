//go:build desktop

package bindings

import (
	"websql/internal/app/permission"
	"websql/internal/pkg/rpc"
)

// registerPermission 注册 permission 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 permission 模块的方法:
//   - POST /api/saveTree     → permission.SaveTree      (保存目录树)
//   - GET  /api/listDirTree   → permission.ListDirTree   (列出目录树)
//   - POST /api/delTreeNode   → permission.DelTreeNode   (删除目录节点)
//   - GET  /api/connBaseTree  → permission.ConnBaseTree  (目录树+连接)
//
// 调用 service: internal/app/permission/binding_delegates.go
// 桌面模式默认 IsRemote=false,无需 admin 权限校验。
func registerPermission(r *Registry) {
	r.register("permission", "SaveTree", func(req rpc.Request) rpc.Response {
		var tree []*permission.DirTree
		if err := decodeBody(req.Body, &tree); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		if err := permission.SaveTreeByService(tree); err != nil {
			return errResponse(err)
		}
		return okResponse("")
	})

	r.register("permission", "ListDirTree", func(req rpc.Request) rpc.Response {
		tree := permission.ListDirTreeByService()
		return okResponse(tree)
	})

	r.register("permission", "DelTreeNode", func(req rpc.Request) rpc.Response {
		id := req.StringBody("id")
		if id == "" {
			id = req.StringParam("id")
		}
		if err := permission.DelTreeNodeByService(id); err != nil {
			return errResponse(err)
		}
		return okResponse("")
	})

	r.register("permission", "ConnBaseTree", func(req rpc.Request) rpc.Response {
		tree := permission.ConnBaseTreeByService()
		return okResponse(tree)
	})
}
