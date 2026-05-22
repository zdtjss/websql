package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type circuitState int32

const (
	circuitClosed   circuitState = 0
	circuitOpen     circuitState = 1
	circuitHalfOpen circuitState = 2
)

type circuitBreaker struct {
	mu               sync.Mutex
	state            circuitState
	failures         int64
	successes        int64
	lastFailure      time.Time
	name             string
	failureThreshold int
	successThreshold int
	timeout          time.Duration
}

func newCircuitBreaker(name string, maxFailures int, timeout time.Duration) *circuitBreaker {
	return &circuitBreaker{
		name:             name,
		failureThreshold: maxFailures,
		successThreshold: 3,
		timeout:          timeout,
	}
}

func (cb *circuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case circuitClosed:
		return true
	case circuitOpen:
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.state = circuitHalfOpen
			cb.successes = 0
			return true
		}
		return false
	case circuitHalfOpen:
		return true
	}
	return false
}

func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == circuitHalfOpen {
		cb.successes++
		if cb.successes >= int64(cb.successThreshold) {
			cb.state = circuitClosed
			cb.failures = 0
		}
	}
}

func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	switch cb.state {
	case circuitHalfOpen:
		cb.state = circuitOpen
	case circuitClosed:
		if cb.failures >= int64(cb.failureThreshold) {
			cb.state = circuitOpen
		}
	}
}

var (
	sqlBreaker = newCircuitBreaker("sql", 10, 30*time.Second)
	aiBreaker  = newCircuitBreaker("ai", 5, 60*time.Second)
)

func CircuitBreakerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		var cb *circuitBreaker

		if strings.HasPrefix(path, "/api/ai/") {
			cb = aiBreaker
		} else if path == "/api/execSQL" {
			cb = sqlBreaker
		} else {
			c.Next()
			return
		}

		if !cb.allow() {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"code": 503,
				"msg":  "服务暂时不可用，请稍后重试",
			})
			return
		}

		c.Next()

		if c.Writer.Status() >= 500 {
			cb.recordFailure()
		} else {
			cb.recordSuccess()
		}
	}
}