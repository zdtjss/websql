//go:build desktop

package bindings

import (
	"os"
	"path/filepath"

	admin "websql/internal/app/admin"
	syncdb "websql/internal/app/sync"
	"websql/internal/pkg/rpc"
)

// registerSyncdb 注册 syncdb 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 sync 模块的方法:
//   - POST /api/sync/compareSchema       → syncdb.CompareSchema
//   - POST /api/sync/compareData         → syncdb.CompareData
//   - POST /api/sync/compareDataChunked  → syncdb.CompareDataChunked
//   - POST /api/sync/applySchemaDiff     → syncdb.ApplySchemaDiff
//   - POST /api/sync/applyDataSync       → syncdb.ApplyDataSync
//   - POST /api/sync/generateSyncSQL     → syncdb.GenerateSyncSQL
//   - GET  /api/sync/targets             → syncdb.GetSyncTargets
//   - POST /api/sync/dryRun              → syncdb.DryRunSync
//   - GET  /api/sync/rollbackLog         → syncdb.GetRollbackLog
//   - POST /api/sync/rollback            → syncdb.RollbackSync
//   - POST /api/sync/exportReport        → syncdb.ExportSyncReport (BlobHandler)
//
// 调用 service: internal/app/sync/binding_delegates.go
// 桌面模式默认 IsRemote=false,无 admin 权限校验,permission.Check* 自动跳过。
func registerSyncdb(r *Registry) {
	r.register("syncdb", "CompareSchema", func(req rpc.Request) rpc.Response {
		p := &syncdb.CompareSchemaParams{
			SourceConnId:  req.StringBody("sourceConnId"),
			TargetConnId:  req.StringBody("targetConnId"),
			SourceSchema:  req.StringBody("sourceSchema"),
			TargetSchema:  req.StringBody("targetSchema"),
			Tables:        req.StringBody("tables"),
			Authorization: req.Authorization,
		}
		return okResponse(syncdb.CompareSchemaByService(p))
	})

	r.register("syncdb", "CompareData", func(req rpc.Request) rpc.Response {
		p := &syncdb.CompareDataParams{
			SourceConnId:  req.StringBody("sourceConnId"),
			TargetConnId:  req.StringBody("targetConnId"),
			SourceSchema:  req.StringBody("sourceSchema"),
			TargetSchema:  req.StringBody("targetSchema"),
			Table:         req.StringBody("table"),
			KeyColumns:    req.StringBody("keyColumns"),
			Page:          stringOrDefault(req.StringBody("page"), "1"),
			PageSize:      stringOrDefault(req.StringBody("pageSize"), "500"),
			Authorization: req.Authorization,
		}
		return okResponse(syncdb.CompareDataByService(p))
	})

	r.register("syncdb", "CompareDataChunked", func(req rpc.Request) rpc.Response {
		p := &syncdb.CompareDataChunkedParams{
			SourceConnId:     req.StringBody("sourceConnId"),
			TargetConnId:     req.StringBody("targetConnId"),
			SourceSchema:     req.StringBody("sourceSchema"),
			TargetSchema:     req.StringBody("targetSchema"),
			Table:            req.StringBody("table"),
			KeyColumns:       req.StringBody("keyColumns"),
			ChunkSize:        stringOrDefault(req.StringBody("chunkSize"), "5000"),
			ChunkIndex:       stringOrDefault(req.StringBody("chunkIndex"), "0"),
			Direction:        stringOrDefault(req.StringBody("direction"), "source_to_target"),
			GenerateSQL:      stringOrDefault(req.StringBody("generateSQL"), "false"),
			Phase:            stringOrDefault(req.StringBody("phase"), "compare"),
			ConflictStrategy: stringOrDefault(req.StringBody("conflictStrategy"), syncdb.StrategyUpdate),
			Authorization:    req.Authorization,
		}
		return okResponse(syncdb.CompareDataChunkedByService(p))
	})

	r.register("syncdb", "ApplySchemaDiff", func(req rpc.Request) rpc.Response {
		user := admin.GetUser(req.Authorization)
		result := syncdb.ApplySchemaDiffByService(
			req.ConnID,
			req.StringBody("schema"),
			req.StringBody("sql"),
			req.Authorization,
			user,
			"", // 桌面模式无 clientIP
		)
		return okResponse(result)
	})

	r.register("syncdb", "ApplyDataSync", func(req rpc.Request) rpc.Response {
		user := admin.GetUser(req.Authorization)
		result := syncdb.ApplyDataSyncByService(
			req.ConnID,
			req.StringBody("schema"),
			req.StringBody("sql"),
			req.StringBody("syncSessionId"),
			req.Authorization,
			user,
			"", // 桌面模式无 clientIP
		)
		return okResponse(result)
	})

	r.register("syncdb", "GenerateSyncSQL", func(req rpc.Request) rpc.Response {
		p := &syncdb.GenerateSyncSQLParams{
			SourceConnId:     req.StringBody("sourceConnId"),
			TargetConnId:     req.StringBody("targetConnId"),
			SourceSchema:     req.StringBody("sourceSchema"),
			TargetSchema:     req.StringBody("targetSchema"),
			Table:            req.StringBody("table"),
			Direction:        stringOrDefault(req.StringBody("direction"), "source_to_target"),
			ConflictStrategy: stringOrDefault(req.StringBody("conflictStrategy"), syncdb.StrategyUpdate),
			Authorization:    req.Authorization,
		}
		return okResponse(syncdb.GenerateSyncSQLByService(p))
	})

	r.register("syncdb", "GetSyncTargets", func(req rpc.Request) rpc.Response {
		result := syncdb.GetSyncTargetsByService(
			req.ConnID,
			req.StringParam("schema"),
			req.Authorization,
		)
		return okResponse(result)
	})

	r.register("syncdb", "DryRunSync", func(req rpc.Request) rpc.Response {
		p := &syncdb.DryRunParams{
			SourceConnId:     req.StringBody("sourceConnId"),
			TargetConnId:     req.StringBody("targetConnId"),
			SourceSchema:     req.StringBody("sourceSchema"),
			TargetSchema:     req.StringBody("targetSchema"),
			Table:            req.StringBody("table"),
			Direction:        stringOrDefault(req.StringBody("direction"), "source_to_target"),
			ConflictStrategy: stringOrDefault(req.StringBody("conflictStrategy"), syncdb.StrategyUpdate),
			Authorization:    req.Authorization,
		}
		return okResponse(syncdb.DryRunSyncByService(p))
	})

	r.register("syncdb", "GetRollbackLog", func(req rpc.Request) rpc.Response {
		sessionId := req.StringParam("sessionId")
		if sessionId == "" {
			sessionId = req.StringBody("sessionId")
		}
		return okResponse(syncdb.GetRollbackLogByService(sessionId))
	})

	r.register("syncdb", "RollbackSync", func(req rpc.Request) rpc.Response {
		user := admin.GetUser(req.Authorization)
		sessionId := req.StringBody("sessionId")
		result := syncdb.RollbackSyncByService(sessionId, req.Authorization, user, "")
		return okResponse(result)
	})

	// ExportSyncReport: 文件下载
	r.registerBlob("syncdb", "ExportSyncReport", func(req rpc.Request) (BlobResult, error) {
		var input syncdb.SyncReportInput
		if err := decodeBody(req.Body, &input); err != nil {
			return BlobResult{}, err
		}
		filename, content, err := syncdb.ExportSyncReportByService(&input)
		if err != nil {
			return BlobResult{}, err
		}
		path, err := writeSyncReportToTemp(filename, content)
		if err != nil {
			return BlobResult{}, err
		}
		mime := "text/html; charset=utf-8"
		if input.Format == "csv" {
			mime = "text/csv; charset=utf-8"
		}
		return BlobResult{Path: path, Filename: filename, Mime: mime}, nil
	})
}

// stringOrDefault 取字符串值，空则返回 def。
func stringOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// writeSyncReportToTemp 将同步报告写入临时文件，返回临时文件路径。
func writeSyncReportToTemp(filename, content string) (string, error) {
	path := filepath.Join(os.TempDir(), filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}
