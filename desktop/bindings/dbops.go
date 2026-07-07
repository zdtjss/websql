//go:build desktop

package bindings

import (
	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/pkg/rpc"
)

// registerDbops 注册 dbops 模块的所有 binding。
//
// 对应 HTTP 路由 (internal/app/router.go):
//   - GET  /api/listTable       → dbops.ListTableFat
//   - POST /api/tableOptions    → dbops.TableOptions
//   - POST /api/tableStatistics → dbops.TableStatistics
//   - POST /api/listIndexes      → dbops.ListIndexes
//   - GET  /api/db/objects       → dbops.ListObjects
//   - GET  /api/db/object/ddl   → dbops.GetObjectDDL
//
// 调用 service: internal/app/dbops/operate_service.go
func registerDbops(r *Registry) {
	// ListTableFat: 列出指定 schema 下的表 (含权限过滤)
	// 入参 (Params): schema
	r.register("dbops", "ListTableFat", func(req rpc.Request) rpc.Response {
		tables := dbops.ListTableFatByService(req.ConnID, req.StringParam("schema"), req.Authorization)
		return okResponse(tables)
	})

	// TableOptions: 查询表选项
	// 入参 (Body): conn.ColumnsQuery (ConnId/Schema/TableName)
	r.register("dbops", "TableOptions", func(req rpc.Request) rpc.Response {
		var param conn.ColumnsQuery
		if err := decodeBody(req.Body, &param); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		data, err := dbops.GetTableOptionsByService(param.ConnId, param.Schema, param.TableName, req.Authorization)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(data)
	})

	// TableStatistics: 查询表统计信息
	r.register("dbops", "TableStatistics", func(req rpc.Request) rpc.Response {
		var param conn.ColumnsQuery
		if err := decodeBody(req.Body, &param); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		data, err := dbops.GetTableStatisticsByService(param.ConnId, param.Schema, param.TableName, req.Authorization)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(data)
	})

	// ListIndexes: 查询索引列表
	r.register("dbops", "ListIndexes", func(req rpc.Request) rpc.Response {
		var param conn.ColumnsQuery
		if err := decodeBody(req.Body, &param); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		data, err := dbops.ListIndexesByService(param.ConnId, param.Schema, param.TableName, req.Authorization)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(data)
	})

	// ListObjects: 列出指定类型的数据库对象
	// 入参 (Params): schema, type (默认 "view")
	r.register("dbops", "ListObjects", func(req rpc.Request) rpc.Response {
		objType := req.StringParam("type")
		if objType == "" {
			objType = "view"
		}
		data, err := dbops.ListObjectsByService(req.ConnID, req.StringParam("schema"), objType, req.Authorization)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(data)
	})

	// GetObjectDDL: 获取对象 DDL 文本
	// 入参 (Params): schema, name, type (默认 "view")
	r.register("dbops", "GetObjectDDL", func(req rpc.Request) rpc.Response {
		objType := req.StringParam("type")
		if objType == "" {
			objType = "view"
		}
		ddl, err := dbops.GetObjectDDLByService(req.ConnID, req.StringParam("schema"), req.StringParam("name"), objType, req.Authorization)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(ddl)
	})
}
