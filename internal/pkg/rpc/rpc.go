package rpc

import "fmt"

// Request 是 Wails binding 的通用入参，所有桌面版绑定方法接收此结构。
// 字段说明：
//   - Module: 业务模块名（如 "conn"、"sql"、"admin"），与前端 route-table.ts 中的 module 字段对应
//   - Method: 业务方法名（如 "ListConn2"、"ExecSQL"），与后端 gin handler 同名
//   - Authorization: 登录令牌，桌面模式下从 sessionStorage 获取并透传
//   - ConnID: 数据库连接 ID，方便 binding 提取后注入到 service 调用
//   - Params: GET query 参数（map[string]interface{}）
//   - Body:  POST 请求体（任意类型：普通对象/URLSearchParams/FormData 等）
type Request struct {
	Module        string                 `json:"module"`
	Method        string                 `json:"method"`
	Authorization string                 `json:"authorization"`
	ConnID        string                 `json:"connId,omitempty"`
	Params        map[string]interface{} `json:"params,omitempty"`
	Body          interface{}            `json:"body,omitempty"`
}

// Response 是 Wails binding 的通用响应，与前端 ApiResponse 接口对齐。
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

// OK 构造成功响应。
func OK(data interface{}) Response {
	return Response{Code: 200, Data: data}
}

// Err 构造错误响应。
func Err(code int, msg string) Response {
	return Response{Code: code, Msg: msg}
}

// ErrFromError 从 error 构造 500 错误响应。
func ErrFromError(err error) Response {
	if err == nil {
		return OK(nil)
	}
	return Err(500, err.Error())
}

// StringParam 从 Params 中取字符串值，缺失返回空串。
func (r *Request) StringParam(key string) string {
	if r.Params == nil {
		return ""
	}
	v, _ := r.Params[key].(string)
	return v
}

// IntParam 从 Params 中取整数值（兼容 json.Number），缺失返回 0。
func (r *Request) IntParam(key string) int {
	if r.Params == nil {
		return 0
	}
	switch v := r.Params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// StringBody 将 Body 反序列化为 map[string]interface{}。
// 用于 FormData/URLSearchParams 在前端序列化为 JSON 对象的情况。
func (r *Request) StringBody(key string) string {
	m, ok := r.Body.(map[string]interface{})
	if !ok {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// BodyMap 返回 Body 作为 map[string]interface{}，无法转换时返回 nil。
func (r *Request) BodyMap() map[string]interface{} {
	m, _ := r.Body.(map[string]interface{})
	return m
}

// Validate 检查必填字段，缺失时返回错误。
func (r *Request) RequireModule() error {
	if r.Module == "" {
		return fmt.Errorf("rpc.Request: module is required")
	}
	return nil
}
