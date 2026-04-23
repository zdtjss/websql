package webapi

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// rateLimitEntry 记录某个 IP 的请求次数和窗口起始时间
type rateLimitEntry struct {
	count       int
	windowStart time.Time
}

var (
	loginLimiter   = make(map[string]*rateLimitEntry)
	loginLimiterMu sync.Mutex
	// 登录接口：每个 IP 每分钟最多 10 次
	loginMaxAttempts = 10
	loginWindow      = 1 * time.Minute
)

func init() {
	// 后台定期清理过期的限流记录
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			loginLimiterMu.Lock()
			now := time.Now()
			for k, v := range loginLimiter {
				if now.Sub(v.windowStart) > loginWindow*2 {
					delete(loginLimiter, k)
				}
			}
			loginLimiterMu.Unlock()
		}
	}()
}

// LoginRateLimitMiddleware 登录接口限流中间件
func LoginRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 仅对登录接口生效
		if c.Request.URL.Path != "/api/login" {
			c.Next()
			return
		}

		ip := c.ClientIP()
		loginLimiterMu.Lock()
		entry, exists := loginLimiter[ip]
		now := time.Now()

		if !exists || now.Sub(entry.windowStart) > loginWindow {
			// 新窗口
			loginLimiter[ip] = &rateLimitEntry{count: 1, windowStart: now}
			loginLimiterMu.Unlock()
			c.Next()
			return
		}

		entry.count++
		if entry.count > loginMaxAttempts {
			loginLimiterMu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code": 429,
				"msg":  "登录尝试过于频繁，请稍后再试",
			})
			return
		}
		loginLimiterMu.Unlock()
		c.Next()
	}
}
