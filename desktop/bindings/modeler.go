//go:build desktop

package bindings

import (
	"context"
	"time"

	"websql/internal/app/modeler"
	"websql/internal/pkg/rpc"
)

// registerModeler 注册 modeler 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 modeler 模块的方法:
//   - POST /api/modeler/reverse            → modeler.ReverseEngineer
//   - POST /api/modeler/forward            → modeler.ForwardEngineer
//   - POST /api/modeler/export              → modeler.ExportModel
//   - POST /api/er/analyzeRelations          → modeler.AnalyzeRelationsHandler
//
// 调用 service: internal/app/modeler/binding_delegates.go
func registerModeler(r *Registry) {
	r.register("modeler", "ReverseEngineer", func(req rpc.Request) rpc.Response {
		schema := req.StringBody("schema")
		if schema == "" {
			schema = req.StringParam("schema")
		}
		includeRelations := req.StringBody("includeRelations")
		if includeRelations == "" {
			includeRelations = req.StringParam("includeRelations")
		}
		model := modeler.ReverseEngineerByService(req.ConnID, schema, includeRelations, req.Authorization)
		return okResponse(model)
	})

	r.register("modeler", "ForwardEngineer", func(req rpc.Request) rpc.Response {
		ddl := req.StringBody("ddl")
		if ddl == "" {
			ddl = req.StringParam("ddl")
		}
		if ddl == "" {
			return rpc.Err(400, "DDL不能为空")
		}
		result := modeler.ForwardEngineerByService(req.ConnID, ddl, req.Authorization)
		if errMsg, ok := result["error"]; ok {
			return rpc.Err(400, errMsg.(string))
		}
		return okResponse(result)
	})

	r.register("modeler", "ExportModel", func(req rpc.Request) rpc.Response {
		schema := req.StringBody("schema")
		if schema == "" {
			schema = req.StringParam("schema")
		}
		format := req.StringBody("format")
		if format == "" {
			format = req.StringParam("format")
		}
		result := modeler.ExportModelByService(req.ConnID, schema, format, req.Authorization)
		return okResponse(result)
	})

	r.register("modeler", "AnalyzeRelations", func(req rpc.Request) rpc.Response {
		var areq modeler.AnalyzeTableRequest
		if err := decodeBody(req.Body, &areq); err != nil {
			return rpc.Err(400, "参数解析失败: "+err.Error())
		}
		if areq.ConnID == "" {
			areq.ConnID = req.ConnID
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		result, err := modeler.AnalyzeRelationsByService(ctx, &areq)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(result)
	})
}
