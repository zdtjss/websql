//go:build desktop

package bindings

import (
	"strconv"

	"websql/internal/audit"
	"websql/internal/pkg/rpc"
)

// registerAudit 注册 audit 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 audit 模块的方法:
//   - GET  /api/audit/logs         → audit.HandleGetAuditLogs
//   - GET  /api/audit/stats        → audit.HandleGetAuditStats
//   - GET  /api/audit/config/get    → audit.HandleGetAuditConfig
//   - POST /api/audit/config/save   → audit.HandleSaveAuditConfig
//
// 调用 service: internal/audit/binding_delegates.go
// 桌面模式默认 IsRemote=false,无 admin 权限校验。
func registerAudit(r *Registry) {
	r.register("audit", "GetAuditLogs", func(req rpc.Request) rpc.Response {
		q := &audit.AuditLogsQuery{
			UserID:    req.StringParam("userId"),
			ConnID:    req.StringParam("connId"),
			SessionID: req.StringParam("sessionId"),
			SQLType:   req.StringParam("sqlType"),
			RiskLevel: req.StringParam("riskLevel"),
			Source:    req.StringParam("source"),
			StartTime: req.StringParam("startTime"),
			EndTime:   req.StringParam("endTime"),
			Keyword:   req.StringParam("keyword"),
			Page:      parseIntParam(req.StringParam("page"), 1),
			PageSize:  parseIntParam(req.StringParam("pageSize"), 20),
		}
		result, err := audit.GetAuditLogsByService(q)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(result)
	})

	r.register("audit", "GetAuditStats", func(req rpc.Request) rpc.Response {
		stats := audit.GetAuditStatsByService()
		return okResponse(stats)
	})

	r.register("audit", "GetAuditConfig", func(req rpc.Request) rpc.Response {
		cfg := audit.GetAuditConfigByService()
		return okResponse(cfg)
	})

	r.register("audit", "SaveAuditConfig", func(req rpc.Request) rpc.Response {
		var cfg audit.AuditConfig
		if err := decodeBody(req.Body, &cfg); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		if err := audit.SaveAuditConfigByService(&cfg); err != nil {
			return rpc.Err(400, err.Error())
		}
		return okResponse("审计配置已保存")
	})
}

// parseIntParam 字符串转 int,空串或解析失败返回 def。
func parseIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
