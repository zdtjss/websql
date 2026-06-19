package appctx

import "github.com/gin-gonic/gin"

// gin.Context 键名常量。userID 与 middleware/auth.go 的 "userId" 对齐。
const (
	KeyAuthorization = "app.authorization"
	KeyConnID        = "app.connID"
	KeyUserID        = "userId" // AuthMiddleware 已写入此键，此处仅复用
	KeyConn          = "app.conn"
)

// Ctx 提供类型安全的上下文读取 helper，避免 handler 中反复 c.GetHeader/c.Get。
type AppCtx struct{}

// Ctx 是 AppCtx 的单例，通过 appctx.Ctx.GetAuthorization(c) 等方式调用。
var Ctx = AppCtx{}

// GetAuthorization 从上下文获取 Authorization token。
func (AppCtx) GetAuthorization(c *gin.Context) string {
	if v, ok := c.Get(KeyAuthorization); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return c.GetHeader("Authorization")
}

// GetConnID 从上下文获取连接 ID。优先 query 参数，兜底 PostForm。
func (AppCtx) GetConnID(c *gin.Context) string {
	if v, ok := c.Get(KeyConnID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	connID := c.Query("connId")
	if connID == "" {
		connID = c.PostForm("connId")
	}
	return connID
}

// GetUserID 从上下文获取当前用户 ID（由 AuthMiddleware 写入）。
func (AppCtx) GetUserID(c *gin.Context) string {
	if v, ok := c.Get(KeyUserID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// SetConn 将数据库连接缓存到请求上下文，避免同一请求内重复获取。
func (AppCtx) SetConn(c *gin.Context, db any) {
	c.Set(KeyConn, db)
}

// GetCachedConn 获取已缓存的数据库连接（同一请求内多次调用时复用）。
// 返回 nil 表示尚未缓存。
func (AppCtx) GetCachedConn(c *gin.Context) any {
	if v, ok := c.Get(KeyConn); ok {
		return v
	}
	return nil
}
