package middleware

import (
	"log"
	"slices"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/config"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

func getTokenFromRequest(c *gin.Context) string {
	authorization := c.GetHeader("Authorization")
	if authorization == "" && (strings.HasPrefix(c.Request.URL.Path, "/api/exports/") || strings.HasPrefix(c.Request.URL.Path, "/exports/")) {
		authorization = c.Query("token")
	}
	return authorization
}

// isLocalMode 本地/桌面模式判定：IsDesktop 为权威判据，即使 IsRemote 误为 true，
// 桌面模式也强制走本地免登录。避免配置覆盖类 bug 再次导致弹登录框。
func isLocalMode() bool {
	return config.IsLocalMode()
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 非 API 路径直接放行（静态资源、SPA 前端路由等由 NoRoute 处理）
		// 但 /exports/ 路径需要鉴权（导出文件下载）
		if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/exports/") {
			c.Next()
			return
		}

		skipPaths := []string{
			"/api/login",
			"/api/logout",
			"/api/healthCheck",
			"/api/sysMode",
		}

		for _, skipPath := range skipPaths {
			if strings.HasPrefix(path, skipPath) {
				c.Next()
				return
			}
		}

		authorization := getTokenFromRequest(c)

		// 本地/桌面模式：如果请求没有携带 token，自动使用本地固定 token（兜底初始化时序竞争）
		if authorization == "" && isLocalMode() {
			authorization = admin.LocalAutoToken
		}

		if authorization == "" {
			c.Abort()
			response.WriteAuthErr(c, "未授权访问，请先登录")
			return
		}

		user, userPower := admin.GetCachedUserAndPower(authorization)
		// 本地/桌面模式自愈：本地会话可能因长时间空闲超过 store TTL 而失效，
		// 此时重新注入 local 会话后再取一次，避免桌面模式偶发 401。
		if (user == nil || user.Id == "") && isLocalMode() && authorization == admin.LocalAutoToken {
			if admin.EnsureLocalSession() {
				user, userPower = admin.GetCachedUserAndPower(authorization)
			}
		}
		if user == nil || user.Id == "" {
			c.Abort()
			response.WriteAuthErr(c, "登录已过期，请重新登录")
			return
		}

		c.Set("currentUser", user)
		c.Set("userId", user.Id)
		c.Set("userPower", userPower)
		c.Next()
	}
}

func HostCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := config.Get()
		// 本地模式（IsRemote=false）时检查 IP 白名单：
		// 本系统基于浏览器访问，局域网内任何人可通过网络访问，需要 IP 校验防止未授权使用
		if !cfg.IsRemote {
			clientIP := c.ClientIP()
			if len(cfg.AllowedIP) > 0 && !slices.Contains(cfg.AllowedIP, clientIP) {
				c.Writer.Write([]byte("<div style=\"text-align: center;font-size: xxx-large;\">非法 IP</div>"))
				c.Header("content-type", "text/html; charset=utf-8")
				log.Println("非法 IP:" + clientIP)
				c.Abort()
				return
			}
		}
	}
}
