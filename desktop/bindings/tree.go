//go:build desktop

package bindings

import (
	"context"
	"encoding/json"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/app/dbops"
	"websql/internal/app/permission"
	tree "websql/internal/app/treehandler"
	"websql/internal/pkg/rpc"
	"websql/internal/pkg/strutil"
)

// registerTree 注册 tree 模块的所有 binding。
//
// 对应 HTTP 路由 (internal/app/router.go):
//   - GET  /api/showTree             → treehandler.ShowTree
//   - POST /api/listTableColumns    → treehandler.ListTableColumns
//   - GET  /api/listTableNames       → treehandler.ListTableNames
//   - GET  /api/listUserConnSchemasStream → treehandler.ListUserConnSchemasStream (SSE 流式)
//
// tree 模块的 handler 是薄包装，调用 dbops/permission 包级函数。
// 桌面模式 IsRemote=false 时权限校验直接放行，可以复用同样的包级函数。
func registerTree(r *Registry) {
	// ShowTree: 按类型返回树节点 (dir/conn/schema/table/column)
	// 入参 (Params): key, type, level, schema
	r.register("tree", "ShowTree", func(req rpc.Request) rpc.Response {
		connId := req.ConnID
		key := req.StringParam("key")
		curType := req.StringParam("type")
		level := req.StringParam("level")
		schema := req.StringParam("schema")
		authorization := req.Authorization
		userPower := admin.GetUserPower(authorization)

		nextType := getNextTypeBinding(curType)
		data := make([]*conn.Tree, 0)
		switch nextType {
		case conn.TREE_NODE_TYPE_DIR:
			if !strings.EqualFold(curType, conn.TREE_NODE_TYPE_COLUMN) {
				if level == "0" {
					data = permission.FilterDirTreeWithPermission(key, userPower)
					data = append(data, permission.FilterConnsWithPermission("noneParent", userPower)...)
				} else {
					data = permission.FilterDirTreeWithPermission(key, userPower)
					if len(data) == 0 || data[0] == nil {
						data = permission.FilterConnsWithPermission(key, userPower)
					}
				}
			}
		case conn.TREE_NODE_TYPE_CONN:
			data = permission.FilterConnsWithPermission(key, userPower)
		case conn.TREE_NODE_TYPE_SCHEMA:
			data = permission.FilterSchemasWithPermission(connId, authorization)
		case conn.TREE_NODE_TYPE_TABLE:
			data = permission.FilterTablesWithPermission(connId, key, authorization)
		case conn.TREE_NODE_TYPE_COLUMN:
			data = dbops.ListColumns(connId, key, schema, authorization)
		case conn.TREE_NODE_TYPE_ALLCOLUMN:
			data = dbops.ListAllColumns(connId, key, authorization)
		}
		return okResponse(data)
	})

	// ListTableColumns: 列出表的所有列 (带权限过滤)
	// 入参 (Body): conn.ColumnsQuery (ConnId/Schema/TableName)
	r.register("tree", "ListTableColumns", func(req rpc.Request) rpc.Response {
		var param conn.ColumnsQuery
		if err := decodeBody(req.Body, &param); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		columns := dbops.ListTableColumns(param.ConnId, param.TableName, param.Schema, req.Authorization)
		return okResponse(strutil.SnakeToCamel(columns))
	})

	// ListTableNames: 列出指定 schema 下的表名
	// 入参 (Params): schema, schemas (JSON 数组，可选)
	r.register("tree", "ListTableNames", func(req rpc.Request) rpc.Response {
		authorization := req.Authorization
		connId := req.ConnID
		schema := req.StringParam("schema")
		schemasJSON := req.StringParam("schemas")

		if schemasJSON != "" {
			var refs []SchemaRef
			if err := json.Unmarshal([]byte(schemasJSON), &refs); err != nil || len(refs) == 0 {
				return okResponse([]any{})
			}
			userPower := admin.GetUserPower(authorization)
			seen := make(map[string]bool)
			result := []tableNameDTO{}
			for _, ref := range refs {
				if ref.ConnId == "" || ref.Schema == "" {
					continue
				}
				key := ref.ConnId + "::" + ref.Schema
				if seen[key] {
					continue
				}
				seen[key] = true
				tables := dbops.QueryTableInfo(ref.ConnId, ref.Schema, authorization)
				filteredTables := conn.FilterTablesByPermission(tables, ref.ConnId, ref.Schema, userPower)
				for _, table := range filteredTables {
					result = append(result, tableNameDTO{
						Name:    table.Name,
						Comment: table.Comment,
						Schema:  ref.Schema,
					})
				}
			}
			return okResponse(result)
		}

		if connId == "" {
			return okResponse([]any{})
		}

		if schema == "" {
			dc := conn.GetConn(connId, authorization)
			if dc != nil {
				switch dc.DriverName() {
				case "mysql", "mariadb":
					dc.Get(&schema, "SELECT DATABASE()")
				case "oracle":
					dc.Get(&schema, "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL")
				case "sqlite":
					schema = "main"
				}
			}
		}

		userPower := admin.GetUserPower(authorization)
		tables := dbops.QueryTableInfo(connId, schema, authorization)
		filteredTables := conn.FilterTablesByPermission(tables, connId, schema, userPower)

		result := make([]tableNameDTO, len(filteredTables))
		for i, table := range filteredTables {
			result[i] = tableNameDTO{Name: table.Name, Comment: table.Comment}
		}
		return okResponse(result)
	})

	// ListUserConnSchemasStream: SSE 流式返回用户连接的 schema 列表
	// 每完成一个 conn 的 schema 查询就推送一条 data 事件，全部完成后推送 done 事件。
	// 前端通过监听 sse:<sessionId>:data 接收每条 conn 的 DTO，sse:<sessionId>:done 标识完成。
	r.registerStream("tree", "ListUserConnSchemasStream", func(ctx context.Context, sreq StreamRequest, emit EmitFunc) {
		dataName := "sse:" + sreq.SessionID + ":data"
		doneName := "sse:" + sreq.SessionID + ":done"
		errName := "sse:" + sreq.SessionID + ":error"

		chunkEmit := func(dto tree.UserConnSchemaDTO) {
			data, _ := json.Marshal(dto)
			emit(dataName, string(data))
		}

		result := tree.ListUserConnSchemasStreamByService(ctx, sreq.Authorization, chunkEmit)
		// result 为 "ok" 或 "empty"，统一作为 done 事件 payload 推送给前端
		emit(doneName, result)
		// 错误事件保留给运行时异常使用；正常完成时不发 error
		_ = errName
	})
}

// SchemaRef 对应前端传入的 schemas JSON 数组项。
type SchemaRef struct {
	ConnId string `json:"connId"`
	Schema string `json:"schema"`
}

type tableNameDTO struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
	Schema  string `json:"schema,omitempty"`
}

// getNextTypeBinding 与 treehandler.getNextType 逻辑一致。
// 复制为 binding 内私有函数，避免依赖 treehandler 包。
func getNextTypeBinding(curType string) string {
	t := conn.TREE_NODE_TYPE_DIR
	switch curType {
	case conn.TREE_NODE_TYPE_DIR:
		t = conn.TREE_NODE_TYPE_DIR
	case conn.TREE_NODE_TYPE_CONN:
		t = conn.TREE_NODE_TYPE_SCHEMA
	case conn.TREE_NODE_TYPE_SCHEMA:
		t = conn.TREE_NODE_TYPE_TABLE
	case conn.TREE_NODE_TYPE_TABLE:
		t = conn.TREE_NODE_TYPE_COLUMN
	case conn.TREE_NODE_TYPE_ALLCOLUMN:
		t = conn.TREE_NODE_TYPE_ALLCOLUMN
	}
	return t
}
