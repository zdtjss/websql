package webapi

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type ipLimiterStore struct {
	limiters map[string]*ipLimiter
	mu       sync.RWMutex
}

func newIPLimiterStore() *ipLimiterStore {
	return &ipLimiterStore{
		limiters: make(map[string]*ipLimiter),
	}
}

func (s *ipLimiterStore) getLimiter(ip string, rps float64, burst int) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.limiters[ip]
	if !exists {
		limiter := rate.NewLimiter(rate.Limit(rps), burst)
		s.limiters[ip] = &ipLimiter{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (s *ipLimiterStore) cleanup(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for ip, entry := range s.limiters {
		if now.Sub(entry.lastSeen) > maxAge {
			delete(s.limiters, ip)
		}
	}
}

var (
	loginLimiters = newIPLimiterStore()
	apiLimiters   = newIPLimiterStore()
)

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			loginLimiters.cleanup(3 * time.Minute)
			apiLimiters.cleanup(3 * time.Minute)
		}
	}()
}

func LoginRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path != "/api/login" {
			c.Next()
			return
		}

		ip := c.ClientIP()
		limiter := loginLimiters.getLimiter(ip, 0.5, 5)
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code": 429,
				"msg":  "登录尝试过于频繁，请稍后再试",
			})
			return
		}
		c.Next()
	}
}

func APIRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api/") {
			c.Next()
			return
		}

		if path == "/api/login" || path == "/api/logout" ||
			path == "/api/healthCheck" || path == "/api/sysMode" {
			c.Next()
			return
		}

		ip := c.ClientIP()
		var limiter *rate.Limiter
		if strings.HasPrefix(path, "/api/ai/") {
			limiter = apiLimiters.getLimiter(ip, 2, 5)
		} else {
			limiter = apiLimiters.getLimiter(ip, 50, 100)
		}

		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code": 429,
				"msg":  "请求过于频繁，请稍后再试",
			})
			return
		}
		c.Next()
	}
}
