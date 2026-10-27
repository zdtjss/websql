package response

import (
	"fmt"
	"net/http"

	"websql/internal/pkg/apperror"

	"github.com/gin-gonic/gin"
)

// 统一响应状态码。与现有前端契约一致：200=成功，401=未授权，500=系统错误。
const (
	CodeOK    = 200
	CodeAuth  = 401
	CodeError = 500
)

// Response 统一响应体。字段名 code/data/msg 与现有前端契约一致。
// errCode/hint 为增量字段：用于错误分类与修复建议，旧客户端可忽略。
type Response struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg,omitempty"`
	Data    any    `json:"data,omitempty"`
	ErrCode string `json:"errCode,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

// WriteOK 写入成功响应。data 为 nil 时仅返回 code=200。
func WriteOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: CodeOK, Data: data})
}

// WriteErr 写入业务错误响应。msg 会经过脱敏。
// httpCode 默认 200（前端按 code 而非 HTTP 状态判断），鉴权类错误可传 401。
func WriteErr(c *gin.Context, httpCode int, code int, msg string) {
	c.JSON(httpCode, Response{Code: code, Msg: msg})
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

// WriteAppErr 写入分类后的应用错误。
// HTTP 状态与 code 保持原有语义（200/500），errCode/hint 为增量字段，
// 便于前端按类型给出修复建议而不必解析驱动原文。
func WriteAppErr(c *gin.Context, appErr *apperror.Error) {
	if appErr == nil {
		WriteErr(c, http.StatusOK, CodeError, "操作失败")
		return
	}
	c.JSON(http.StatusOK, Response{
		Code:    CodeError,
		Msg:     appErr.Msg,
		ErrCode: appErr.Code,
		Hint:    appErr.Hint,
	})
}
