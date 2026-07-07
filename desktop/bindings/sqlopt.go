//go:build desktop

package bindings

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"websql/internal/app/sqlopt"
	"websql/internal/pkg/rpc"
	"websql/internal/pkg/safego"
)

// registerSqlopt 注册 sqlopt 模块的所有 binding。
//
// 对应 HTTP 路由 (internal/app/router.go):
//   - POST /api/sqlopt/explain   → sqlopt.ExplainSQL (普通 Handler)
//   - POST /api/sqlopt/optimize   → sqlopt.OptimizeSQLStream (StreamHandler)
//
// 调用 service:
//   - internal/app/sqlopt/explain_service.go
//   - internal/app/sqlopt/optimize_service.go
func registerSqlopt(r *Registry) {
	// ExplainSQL: 执行 EXPLAIN 并返回结构化结果。
	// 入参 (Body): sql, schema
	r.register("sqlopt", "ExplainSQL", func(req rpc.Request) rpc.Response {
		explainReq := &sqlopt.ExplainRequest{
			ConnID:        req.ConnID,
			Schema:        req.StringBody("schema"),
			SQL:           req.StringBody("sql"),
			Authorization: req.Authorization,
		}
		result, err := sqlopt.ExplainByService(explainReq)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(result)
	})

	// OptimizeSQLStream: SQL 优化的流式响应。
	// 入参 (StreamRequest.Body): sql, schema, explainResult
	// 输出: 通过 emit 回调推送 sse:<sessionId>:data / done / error 事件。
	r.registerStream("sqlopt", "OptimizeSQLStream", func(ctx context.Context, sreq StreamRequest, emit EmitFunc) {
		dataName := "sse:" + sreq.SessionID + ":data"
		doneName := "sse:" + sreq.SessionID + ":done"
		errName := "sse:" + sreq.SessionID + ":error"

		sqlStr := stringBodyField(sreq.Body, "sql")
		if sqlStr == "" {
			emit(errName, "SQL不能为空")
			return
		}

		// 5 分钟超时，与 HTTP 模式一致
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		// 监听 ctx 取消（用户关闭前端窗口等场景）
		safego.GoWithName("sqlopt-stream-ctx-watch", func() {
			<-ctx.Done()
		})

		// 心跳：与 HTTP 模式 5s 间隔一致
		kaStop := make(chan struct{})
		defer close(kaStop)
		safego.GoWithName("sqlopt-stream-keepalive", func() {
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
		chunkEmit := func(chunk sqlopt.StreamChunk) {
			mu.Lock()
			defer mu.Unlock()
			data, _ := json.Marshal(chunk)
			emit(dataName, string(data))
		}

		optReq := &sqlopt.OptimizeRequest{
			ConnID:        sreq.ConnID,
			Schema:        stringBodyField(sreq.Body, "schema"),
			SQL:           sqlStr,
			Authorization: sreq.Authorization,
		}

		// explainResult 是可选参数，前端可能传入
		if er := stringBodyField(sreq.Body, "explainResult"); er != "" {
			if decoded, err := decodeExplainResult(er); err == nil {
				optReq.ExplainResult = decoded
			}
		}

		if err := sqlopt.OptimizeByService(ctx, optReq, chunkEmit); err != nil {
			chunkEmit(sqlopt.StreamChunk{Type: "error", Content: err.Error()})
		}
		chunkEmit(sqlopt.StreamChunk{Type: "done"})
		emit(doneName, nil)
	})
}

// stringBodyField 从 StreamRequest.Body (interface{}) 中按 key 取字符串。
// 复用 rpc.Request.StringBody 的逻辑，但 StreamRequest 没有该方法。
func stringBodyField(body interface{}, key string) string {
	m, ok := body.(map[string]interface{})
	if !ok {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// decodeExplainResult 把前端传入的 JSON 字符串解析为 *sqlopt.ExplainResult。
// 失败时返回 nil，让 service 走"无 explain 上下文"分支。
func decodeExplainResult(raw string) (*sqlopt.ExplainResult, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty")
	}
	var r sqlopt.ExplainResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, err
	}
	return &r, nil
}
