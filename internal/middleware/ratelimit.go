package middleware

import (
	"hash/fnv"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const numLimiterShards = 64

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type ipLimiterShard struct {
	mu       sync.RWMutex
	limiters map[string]*ipLimiter
}

type ipLimiterStore struct {
	shards [numLimiterShards]*ipLimiterShard
}

func newIPLimiterStore() *ipLimiterStore {
	s := &ipLimiterStore{}
	for i := range numLimiterShards {
		s.shards[i] = &ipLimiterShard{limiters: make(map[string]*ipLimiter)}
	}
	return s
}

func (s *ipLimiterStore) getShard(ip string) *ipLimiterShard {
	h := fnv.New32a()
	h.Write([]byte(ip))
	return s.shards[h.Sum32()%numLimiterShards]
}

func (s *ipLimiterStore) getLimiter(ip string, rps float64, burst int) *rate.Limiter {
	sh := s.getShard(ip)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	entry, exists := sh.limiters[ip]
	if !exists {
		limiter := rate.NewLimiter(rate.Limit(rps), burst)
		sh.limiters[ip] = &ipLimiter{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (s *ipLimiterStore) cleanup(maxAge time.Duration) {
	now := time.Now()
	for i := range numLimiterShards {
		sh := s.shards[i]
		sh.mu.Lock()
		for ip, entry := range sh.limiters {
			if now.Sub(entry.lastSeen) > maxAge {
				delete(sh.limiters, ip)
			}
		}
		sh.mu.Unlock()
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
			limiter = apiLimiters.getLimiter(ip, 10, 50)
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
