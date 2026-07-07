//go:build desktop

package bindings

import (
	"websql/internal/app/snippet"
	"websql/internal/pkg/rpc"
)

// registerSnippet 注册 snippet 模块的所有 binding。
//
// 这是 P0 binding 实现的样板文件,展示如何:
//   1. 从 rpc.Request 提取参数 (StringParam / decodeBody)
//   2. 调用 service 包级委托函数 (snippet.XxxByService)
//   3. 包装结果为 rpc.Response
//
// 后续模块的 binding 实现可参照本文件结构。
// 对应 HTTP 路由: internal/app/router.go 中 /api/snippet/* 路由组。
func registerSnippet(r *Registry) {
	// List: 对应 GET /api/snippet/list
	// 入参: keyword, category, tag (Query 参数)
	r.register("snippet", "List", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(401, "未登录")
		}
		keyword := req.StringParam("keyword")
		category := req.StringParam("category")
		tag := req.StringParam("tag")
		// category=all 视为不过滤,与 HTTP handler 保持一致
		if category == "all" {
			category = ""
		}
		list, err := snippet.ListByService(userId, keyword, category, tag)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(map[string]any{"items": list, "total": len(list)})
	})

	// Save: 对应 POST /api/snippet/save
	// 入参: SnippetSave 结构 (Body)
	r.register("snippet", "Save", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(401, "未登录")
		}
		var saveReq snippet.SnippetSave
		if err := decodeBody(req.Body, &saveReq); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		sn, err := snippet.SaveByService(&saveReq, userId)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(sn)
	})

	// Delete: 对应 POST /api/snippet/delete?id=xxx
	// 入参: id (Query 参数)
	r.register("snippet", "Delete", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(401, "未登录")
		}
		id := req.StringParam("id")
		if id == "" {
			return rpc.Err(400, "缺少 id 参数")
		}
		if err := snippet.DeleteByService(id, userId); err != nil {
			return errResponse(err)
		}
		return okResponse("删除成功")
	})

	// Export: 对应 GET /api/snippet/export
	// 返回当前用户全部收藏为 JSON
	r.register("snippet", "Export", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(401, "未登录")
		}
		data, err := snippet.ExportByService(userId)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(data)
	})

	// Import: 对应 POST /api/snippet/import
	// 入参: SnippetImportReq 结构 (Body)
	r.register("snippet", "Import", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(401, "未登录")
		}
		var importReq snippet.SnippetImportReq
		if err := decodeBody(req.Body, &importReq); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		count, err := snippet.ImportByService(importReq.Items, userId)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(map[string]any{"count": count})
	})

	// Categories: 对应 GET /api/snippet/categories
	r.register("snippet", "Categories", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(401, "未登录")
		}
		cats, err := snippet.CategoriesByService(userId)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(cats)
	})

	// Tags: 对应 GET /api/snippet/tags
	r.register("snippet", "Tags", func(req rpc.Request) rpc.Response {
		userId := extractUserId(req.Authorization)
		if userId == "" {
			return rpc.Err(401, "未登录")
		}
		tags, err := snippet.AllTagsByService(userId)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(tags)
	})
}
