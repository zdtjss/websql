//go:build desktop

package bindings

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"websql/internal/app/monitor"
	"websql/internal/pkg/rpc"
	"websql/internal/pkg/safego"
)

// registerMonitor 注册 monitor 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 monitor 模块的方法:
//   - GET  /api/monitor/metrics         → monitor.GetMetrics
//   - GET  /api/monitor/history         → monitor.GetMetricHistory
//   - GET  /api/monitor/resources        → monitor.GetResources
//   - GET  /api/monitor/processes        → monitor.GetProcesses
//   - GET  /api/monitor/variables         → monitor.GetServerVariables
//   - GET  /api/monitor/variables/all     → monitor.GetAllServerVariables
//   - GET  /api/monitor/status/all        → monitor.GetAllServerStatus
//   - GET  /api/monitor/innodb-status     → monitor.GetInnodbStatus
//   - GET  /api/monitor/locks             → monitor.GetLocks
//   - GET  /api/monitor/slow-queries      → monitor.GetSlowQueries
//   - GET  /api/monitor/top-tables        → monitor.GetTopTables
//   - POST /api/monitor/aiAnalyze          → monitor.AIAnalyze (SSE 流式)
//
// 调用 service: internal/app/monitor/binding_delegates.go
func registerMonitor(r *Registry) {
	r.register("monitor", "GetMetrics", func(req rpc.Request) rpc.Response {
		snapshot, err := monitor.GetMetricsByService(req.ConnID, req.Authorization)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(snapshot)
	})

	r.register("monitor", "GetMetricHistory", func(req rpc.Request) rpc.Response {
		connId := req.StringParam("connId")
		if connId == "" {
			connId = req.ConnID
		}
		metric := req.StringParam("metric")
		from := req.StringParam("from")
		to := req.StringParam("to")
		interval := req.StringParam("interval")
		if interval == "" {
			interval = "raw"
		}
		result, err := monitor.GetMetricHistoryByService(connId, metric, from, to, interval)
		if err != nil {
			return rpc.Err(400, err.Error())
		}
		return okResponse(result)
	})

	r.register("monitor", "GetResources", func(req rpc.Request) rpc.Response {
		schema := req.StringParam("schema")
		result := monitor.GetResourcesByService(req.ConnID, schema, req.Authorization)
		return okResponse(result)
	})

	r.register("monitor", "GetProcesses", func(req rpc.Request) rpc.Response {
		result := monitor.GetProcessesByService(req.ConnID, req.Authorization)
		return okResponse(result)
	})

	r.register("monitor", "GetServerVariables", func(req rpc.Request) rpc.Response {
		result := monitor.GetServerVariablesByService(req.ConnID, req.Authorization)
		return okResponse(result)
	})

	r.register("monitor", "GetAllServerVariables", func(req rpc.Request) rpc.Response {
		scope := req.StringParam("scope")
		if scope == "" {
			scope = "global"
		}
		result := monitor.GetAllServerVariablesByService(req.ConnID, scope, req.Authorization)
		return okResponse(result)
	})

	r.register("monitor", "GetAllServerStatus", func(req rpc.Request) rpc.Response {
		result := monitor.GetAllServerStatusByService(req.ConnID, req.Authorization)
		return okResponse(result)
	})

	r.register("monitor", "GetInnodbStatus", func(req rpc.Request) rpc.Response {
		result := monitor.GetInnodbStatusByService(req.ConnID, req.Authorization)
		return okResponse(result)
	})

	r.register("monitor", "GetLocks", func(req rpc.Request) rpc.Response {
		result := monitor.GetLocksByService(req.ConnID, req.Authorization)
		return okResponse(result)
	})

	r.register("monitor", "GetSlowQueries", func(req rpc.Request) rpc.Response {
		limit := req.StringParam("limit")
		result := monitor.GetSlowQueriesByService(req.ConnID, limit, req.Authorization)
		return okResponse(result)
	})

	r.register("monitor", "GetTopTables", func(req rpc.Request) rpc.Response {
		schema := req.StringParam("schema")
		limit := req.StringParam("limit")
		result := monitor.GetTopTablesByService(req.ConnID, schema, limit, req.Authorization)
		return okResponse(result)
	})

	// AIAnalyze: SSE 流式
	r.registerStream("monitor", "AIAnalyze", func(ctx context.Context, sreq StreamRequest, emit EmitFunc) {
		dataName := "sse:" + sreq.SessionID + ":data"
		doneName := "sse:" + sreq.SessionID + ":done"
		errName := "sse:" + sreq.SessionID + ":error"

		// 解析 AIAnalyzeRequest
		var aiReq monitor.AIAnalyzeRequest
		if err := decodeBody(sreq.Body, &aiReq); err != nil {
			emit(errName, "参数解析失败: "+err.Error())
			return
		}
		if aiReq.ConnID == "" {
			aiReq.ConnID = sreq.ConnID
		}

		// 5 分钟超时，与 HTTP 模式一致
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		safego.GoWithName("monitor-stream-ctx-watch", func() {
			<-ctx.Done()
		})

		// 心跳：5s 间隔，与 HTTP 模式一致
		kaStop := make(chan struct{})
		defer close(kaStop)
		safego.GoWithName("monitor-stream-keepalive", func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-kaStop:
					return
				case <-ticker.C:
					emit(dataName, "")
				}
			}
		})

		var mu sync.Mutex
		chunkEmit := func(chunk monitor.StreamChunk) {
			mu.Lock()
			defer mu.Unlock()
			data, _ := json.Marshal(chunk)
			emit(dataName, string(data))
		}

		if err := monitor.AIAnalyzeByService(ctx, &aiReq, chunkEmit); err != nil {
			chunkEmit(monitor.StreamChunk{Type: "error", Content: err.Error()})
		}
		emit(doneName, nil)
	})
}
