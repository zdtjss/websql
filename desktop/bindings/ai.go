//go:build desktop

package bindings

import (
	"websql/internal/ai"
	system "websql/internal/app/system"
	"websql/internal/pkg/rpc"
)

// registerAi 注册 ai 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 ai 模块的方法:
//   - POST /ai/config/save → ai.HandleSaveConfig
//   - GET  /ai/config/get   → ai.HandleGetConfig
//   - POST /ai/config/test  → ai.HandleTestConfig
//
// 调用 service: internal/ai/binding_delegates.go 以及已导出的 ai.SaveAIConfig / ai.GetAIConfig。
func registerAi(r *Registry) {
	r.register("ai", "SaveConfig", func(req rpc.Request) rpc.Response {
		var cfg system.AIConfig
		if err := decodeBody(req.Body, &cfg); err != nil {
			return rpc.Err(500, "参数解析失败")
		}
		if err := ai.SaveAIConfig(cfg); err != nil {
			return rpc.Err(500, "保存配置失败")
		}
		return okResponse("保存成功")
	})

	r.register("ai", "GetConfig", func(req rpc.Request) rpc.Response {
		cfg, err := ai.GetAIConfig()
		if err != nil {
			return rpc.Err(500, "获取配置失败")
		}
		return okResponse(cfg)
	})

	r.register("ai", "TestConfig", func(req rpc.Request) rpc.Response {
		var cfg system.AIConfig
		if err := decodeBody(req.Body, &cfg); err != nil {
			return rpc.Err(500, "参数解析失败")
		}
		msg, err := ai.TestAIConfigByService(&cfg)
		if err != nil {
			return rpc.Err(500, err.Error())
		}
		return rpc.Response{Code: 200, Msg: msg}
	})
}
