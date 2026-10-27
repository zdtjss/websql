package sql

import (
	"context"
	"sync"

	"websql/internal/database"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// 服务端查询取消。
//
// 背景：原先前端「终止」只 abort 浏览器请求。若客户端因网络问题未真正断连，
// 数据库侧会继续跑完整个查询（特别是大表扫描），既占用连接也拖慢他人。
// 现在每次执行可携带 requestId，服务端登记 context.CancelFunc，
// 前端终止时调用 /api/sql/cancel 主动取消，driver 会中断语句并释放连接。
var runningQueries = struct {
	sync.Mutex
	m map[string]context.CancelFunc
}{m: make(map[string]context.CancelFunc)}

func registerRunningQuery(id string, cancel context.CancelFunc) {
	if id == "" {
		return
	}
	runningQueries.Lock()
	runningQueries.m[id] = cancel
	runningQueries.Unlock()
}

func unregisterRunningQuery(id string) {
	if id == "" {
		return
	}
	runningQueries.Lock()
	delete(runningQueries.m, id)
	runningQueries.Unlock()
}

func cancelRunningQuery(id string) bool {
	runningQueries.Lock()
	cancel, ok := runningQueries.m[id]
	if ok {
		delete(runningQueries.m, id)
	}
	runningQueries.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// withQueryContext 创建一次 SQL 执行的 context：
//   - 超时取连接配置（database.ConnQueryTimeout，默认 5 分钟）；
//   - 携带 requestId 时登记取消函数，供 /api/sql/cancel 主动终止。
//
// 返回的 cancel 函数必须在执行结束时调用（注销 + 释放 context）。
func withQueryContext(c *gin.Context, connId, requestID string) (context.Context, context.CancelFunc) {
	timeout := database.ConnQueryTimeout(connId)
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	registerRunningQuery(requestID, cancel)
	return ctx, func() {
		unregisterRunningQuery(requestID)
		cancel()
	}
}

// CancelHandler POST /api/sql/cancel：按 requestId 终止正在执行的 SQL。
func CancelHandler(c *gin.Context) {
	req := struct {
		RequestId string `json:"requestId"`
	}{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, &req); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	if req.RequestId == "" {
		response.WriteErr(c, 200, 400, "缺少 requestId")
		return
	}
	if !cancelRunningQuery(req.RequestId) {
		// 已执行完成或从未登记：返回成功，避免前端把“刚好结束”当成错误
		response.WriteOK(c, gin.H{"cancelled": false})
		return
	}
	response.WriteOK(c, gin.H{"cancelled": true})
}
