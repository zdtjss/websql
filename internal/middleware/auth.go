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

var authPathMap = map[string]bool{
	"/api/login":       true,
	"/api/logout":      true,
	"/api/healthCheck": true,
	"/api/sysMode":     true,
}

func getTokenFromRequest(c *gin.Context) string {
	authorization := c.GetHeader("Authorization")
	if authorization == "" && strings.HasPrefix(c.Request.URL.Path, "/api/exports/") {
		authorization = c.Query("token")
	}
	return authorization
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		skipPaths := []string{
			"/api/login",
			"/api/logout",
			"/api/healthCheck",
			"/api/sysMode",
			"/assets",
		}

		path := c.Request.URL.Path
		for _, skipPath := range skipPaths {
			if strings.HasPrefix(path, skipPath) {
				c.Next()
				return
			} else if strings.EqualFold(path, "/") || strings.EqualFold(path, "/index.html") {
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