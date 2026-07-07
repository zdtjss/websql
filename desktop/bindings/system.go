//go:build desktop

package bindings

import (
	"encoding/json"

	system "websql/internal/app/system"
	"websql/internal/pkg/rpc"
)

// registerSystem 注册 system 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 system 模块的方法:
//   - GET  /system/config/list             → system.GetSystemConfig
//   - POST /system/config/save             → system.SaveSystemConfigHandler
//   - GET  /system/config/all/get          → system.GetAllSystemConfigHandler
//   - POST /system/config/all/save         → system.SaveAllSystemConfigHandler
//   - GET  /system/config/ai/get           → system.GetAIConfigHandler
//   - POST /system/config/ai/save          → system.SaveAIConfigHandler
//   - GET  /system/config/outterUser/get   → system.GetOutterUserHandler
//   - POST /system/config/outterUser/save  → system.SaveOutterUserHandler
//   - POST /system/config/outterUser/test  → system.TestOutterUserHandler
//   - GET  /system/config/allowedIP/get    → system.GetAllowedIPHandler
//   - POST /system/config/allowedIP/save   → system.SaveAllowedIPHandler
//   - GET  /system/config/ai/models        → system.GetAIModelListHandler
//   - POST /system/config/ai/model/save    → system.SaveAIModelHandler
//   - POST /system/config/ai/model/delete  → system.DeleteAIModelHandler
//   - POST /system/config/ai/model/select  → system.SelectAIModelHandler
//
// 调用 service: internal/app/system/binding_delegates.go 以及已导出的底层函数。
// 桌面模式默认 IsRemote=false,admin.CheckAdminPower 直接返回 true,无需权限校验。
func registerSystem(r *Registry) {
	r.register("system", "GetSystemConfig", func(req rpc.Request) rpc.Response {
		configType := req.StringParam("type")
		if configType == "" {
			configType = req.StringBody("type")
		}
		return okResponse(system.ListSystemConfig(configType))
	})

	r.register("system", "SaveSystemConfig", func(req rpc.Request) rpc.Response {
		var cfg system.SystemConfigSave
		if err := decodeBody(req.Body, &cfg); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		system.SaveSystemConfig(&cfg)
		return okResponse("")
	})

	r.register("system", "GetAllSystemConfig", func(req rpc.Request) rpc.Response {
		return okResponse(system.GetAllSystemConfigByService())
	})

	r.register("system", "SaveAllSystemConfig", func(req rpc.Request) rpc.Response {
		var cfg system.SystemConfigAll
		if err := decodeBody(req.Body, &cfg); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		system.SaveAllSystemConfigByService(&cfg)
		return okResponse("")
	})

	r.register("system", "GetAIConfig", func(req rpc.Request) rpc.Response {
		cfg := system.GetAIConfigFromDB()
		if cfg == nil {
			return okResponse(map[string]any{
				"provider": "",
				"baseUrl":  "",
				"model":    "",
				"apiKey":   "",
			})
		}
		return okResponse(cfg)
	})

	r.register("system", "SaveAIConfig", func(req rpc.Request) rpc.Response {
		var cfg system.AIConfig
		if err := decodeBody(req.Body, &cfg); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		system.SaveAIConfigToDB(cfg)
		return okResponse("")
	})

	r.register("system", "GetOutterUser", func(req rpc.Request) rpc.Response {
		url := system.GetOutterUserFromDB()
		return okResponse(map[string]string{"outterUser": url})
	})

	r.register("system", "SaveOutterUser", func(req rpc.Request) rpc.Response {
		outterUser := req.StringBody("outterUser")
		if outterUser == "" {
			var body struct {
				OutterUser string `json:"outterUser"`
			}
			if err := decodeBody(req.Body, &body); err == nil {
				outterUser = body.OutterUser
			}
		}
		system.SaveOutterUserToDB(outterUser)
		return okResponse("")
	})

	r.register("system", "TestOutterUser", func(req rpc.Request) rpc.Response {
		msg, code, err := system.TestOutterUserByService()
		if err != nil {
			return rpc.Err(code, err.Error())
		}
		return okResponse(msg)
	})

	r.register("system", "GetAllowedIP", func(req rpc.Request) rpc.Response {
		ips := system.GetAllowedIPFromDB()
		return okResponse(map[string][]string{"allowedIP": ips})
	})

	r.register("system", "SaveAllowedIP", func(req rpc.Request) rpc.Response {
		// 兼容两种 body 形式：{allowedIP: [...]} 或直接 [...]
		var ips []string
		bodyMap := req.BodyMap()
		if rawIps, ok := bodyMap["allowedIP"]; ok {
			if arr, ok := rawIps.([]interface{}); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						ips = append(ips, s)
					}
				}
			}
		}
		if ips == nil {
			if err := json.Unmarshal(toJSONBytes(req.Body), &ips); err == nil && len(ips) > 0 {
				// body 本身就是数组
			}
		}
		system.SaveAllowedIPToDB(ips)
		return okResponse("")
	})

	r.register("system", "GetAIModelList", func(req rpc.Request) rpc.Response {
		return okResponse(system.GetAIModelListByService())
	})

	r.register("system", "SaveAIModel", func(req rpc.Request) rpc.Response {
		var model system.AIModelItem
		if err := decodeBody(req.Body, &model); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		saved := system.SaveAIModelByService(&model)
		return okResponse(saved)
	})

	r.register("system", "DeleteAIModel", func(req rpc.Request) rpc.Response {
		var req2 struct {
			Id string `json:"id"`
		}
		if err := decodeBody(req.Body, &req2); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		if _, err := system.DeleteAIModelByService(req2.Id); err != nil {
			return rpc.Err(400, err.Error())
		}
		return okResponse("")
	})

	r.register("system", "SelectAIModel", func(req rpc.Request) rpc.Response {
		var req2 struct {
			Id string `json:"id"`
		}
		if err := decodeBody(req.Body, &req2); err != nil {
			return rpc.Err(400, "请求参数解析失败")
		}
		if err := system.SelectAIModelByService(req2.Id); err != nil {
			return rpc.Err(400, err.Error())
		}
		return okResponse("")
	})
}

// toJSONBytes 将任意值转为 JSON bytes。
// decodeBody 内部已做 marshal-unmarshal，此函数用于需要原始 bytes 的场景。
func toJSONBytes(v interface{}) []byte {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
