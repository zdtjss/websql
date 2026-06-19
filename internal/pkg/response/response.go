package response

import (
	"fmt"
	"net/http"

	"websql/internal/pkg/sanitize"

	"github.com/gin-gonic/gin"
)

// 统一响应状态码。与现有前端契约一致：200=成功，401=未授权，500=系统错误。
const (
	CodeOK    = 200
	CodeAuth  = 401
	CodeError = 500
)

// Response 统一响应体。字段名 code/data/msg 与现有前端契约一致。
type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg,omitempty"`
	Data any    `json:"data,omitempty"`
}

// WriteOK 写入成功响应。data 为 nil 时仅返回 code=200。
func WriteOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: CodeOK, Data: data})
}

// WriteErr 写入业务错误响应。msg 会经过脱敏。
// httpCode 默认 200（前端按 code 而非 HTTP 状态判断），鉴权类错误可传 401。
func WriteErr(c *gin.Context, httpCode int, code int, msg string) {
	c.JSON(httpCode, Response{Code: code, Msg: sanitize.SanitizeErrMsg(msg)})
}

// WriteErrf 支持格式化的错误响应。
func WriteErrf(c *gin.Context, httpCode int, code int, format string, args ...any) {
	WriteErr(c, httpCode, code, fmt.Sprintf(format, args...))
}

// WriteInternalErr 写入系统内部错误响应（HTTP 200, code=500）。
func WriteInternalErr(c *gin.Context, detail string) {
	WriteErr(c, http.StatusOK, CodeError, "系统内部错误，请稍后重试")
}

// WriteAuthErr 写入鉴权错误响应（HTTP 401, code=401）。
func WriteAuthErr(c *gin.Context, msg string) {
	if msg == "" {
		msg = "未授权访问，请先登录"
	}
	WriteErr(c, http.StatusUnauthorized, CodeAuth, msg)
}
