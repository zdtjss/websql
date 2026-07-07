//go:build desktop

package bindings

import (
	"websql/internal/app/search"
	"websql/internal/pkg/rpc"
)

// registerSearch 注册 search 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 search 模块的方法:
//   - GET /api/search/objects → search.SearchObjects
//   - GET /api/search/data     → search.SearchData
//   - GET /api/search/all      → search.SearchAll
//   - GET /api/search/tables   → search.GetSearchTables
//
// 调用 service: internal/app/search/binding_delegates.go
func registerSearch(r *Registry) {
	r.register("search", "SearchObjects", func(req rpc.Request) rpc.Response {
		schema := req.StringParam("schema")
		keyword := req.StringParam("keyword")
		searchType := req.StringParam("searchType")
		if searchType == "" {
			searchType = "all"
		}
		result := search.SearchObjectsByService(req.ConnID, schema, keyword, searchType, req.Authorization)
		return okResponse(result)
	})

	r.register("search", "SearchData", func(req rpc.Request) rpc.Response {
		schema := req.StringParam("schema")
		keyword := req.StringParam("keyword")
		maxTables := req.StringParam("maxTables")
		if maxTables == "" {
			maxTables = "50"
		}
		timeout := req.StringParam("timeout")
		if timeout == "" {
			timeout = "30"
		}
		result := search.SearchDataByService(req.ConnID, schema, keyword, maxTables, timeout, req.Authorization)
		return okResponse(result)
	})

	r.register("search", "SearchAll", func(req rpc.Request) rpc.Response {
		schema := req.StringParam("schema")
		keyword := req.StringParam("keyword")
		searchType := req.StringParam("searchType")
		if searchType == "" {
			searchType = "all"
		}
		result := search.SearchAllByService(req.ConnID, schema, keyword, searchType, req.Authorization)
		return okResponse(result)
	})

	r.register("search", "GetSearchTables", func(req rpc.Request) rpc.Response {
		schema := req.StringParam("schema")
		result := search.GetSearchTablesByService(req.ConnID, schema, req.Authorization)
		return okResponse(result)
	})
}
