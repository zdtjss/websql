package middleware

import (
	"log"
	"net/http"
	"slices"
	"strings"

	"websql/internal/app/admin"
	"websql/internal/config"

	"github.com/gin-gonic/gin"
)

func getTokenFromRequest(c *gin.Context) string {
	authorization := c.GetHeader("Authorization")
	if authorization == "" && (strings.HasPrefix(c.Request.URL.Path, "/api/exports/") || strings.HasPrefix(c.Request.URL.Path, "/exports/")) {
		authorization = c.Query("token")
	}
	return authorization
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

		if authorization == "" {
			c.Abort()
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未授权访问，请先登录",
			})
			return
		}

		user, userPower := admin.GetCachedUserAndPower(authorization)
		if user == nil || user.Id == "" {
			c.Abort()
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "登录已过期，请重新登录",
			})
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
		// 本地模式（IsRemote=false）时检查 IP 白名单：
		// 本系统基于浏览器访问，局域网内任何人可通过网络访问，需要 IP 校验防止未授权使用
		if !config.Cfg.IsRemote {
			clientIP := c.ClientIP()
			if len(config.Cfg.AllowedIP) > 0 && !slices.Contains(config.Cfg.AllowedIP, clientIP) {
				c.Writer.Write([]byte("<div style=\"text-align: center;font-size: xxx-large;\">非法 IP</div>"))
				c.Header("content-type", "text/html; charset=utf-8")
				log.Println("非法 IP:" + clientIP)
				c.Abort()
				return
			}
		}
	}
}