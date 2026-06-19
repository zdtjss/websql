package app

import (
	"websql/internal/pkg/appctx"

	"github.com/gin-gonic/gin"
)

// ContextMiddleware 解析公共请求参数并写入 gin.Context。
// 必须注册在 AuthMiddleware 之后（AuthMiddleware 写入 userId/currentUser）。
func ContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := c.GetHeader("Authorization")
		if authorization == "" {
			// 导出接口可能通过 query 传 token
			authorization = c.Query("token")
		}
		if authorization != "" {
			c.Set(appctx.KeyAuthorization, authorization)
		}
		if connID := c.Query("connId"); connID != "" {
			c.Set(appctx.KeyConnID, connID)
		}
		c.Next()
	}
}
