//go:build desktop

package bindings

import (
	"encoding/json"

	admin "websql/internal/app/admin"
	"websql/internal/pkg/rpc"
)

// extractUserId 从 authorization token 解出 userId。
// 桌面模式默认 IsRemote=false,token 可能为空,此时返回默认 admin user id。
// 当 admin.GetUser("") 返回 nil (用户表未初始化或未登录) 时返回空串,
// 调用方应当对空 userId 做出适当处理(如要求登录)。
func extractUserId(authorization string) string {
	if authorization == "" {
		if u := admin.GetUser(""); u != nil {
			return u.Id
		}
		return ""
	}
	if u := admin.GetUser(authorization); u != nil {
		return u.Id
	}
	return ""
}

// decodeBody 把 rpc.Request.Body (interface{} 任意类型) 反序列化到目标结构。
// Wails runtime 在 JSON 调用时会把对象转为 map[string]interface{},
// service 期望强类型结构,需要做一次 marshal-unmarshal 转换。
func decodeBody(body interface{}, target interface{}) error {
	if body == nil {
		return nil
	}
	bytes, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, target)
}

// errResponse 把 error 包装为 rpc.Response,500 状态码。
// 与 HTTP 模式的 response.WriteErr 行为对齐。
func errResponse(err error) rpc.Response {
	if err == nil {
		return rpc.OK(nil)
	}
	return rpc.Err(500, err.Error())
}

// okResponse 把 data 包装为 rpc.Response,200 状态码。
func okResponse(data interface{}) rpc.Response {
	return rpc.OK(data)
}
