package webapi

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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
	state            atomic.Int32
	failures         atomic.Int64
	successes        atomic.Int64
	lastFailure      atomic.Int64
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
	if cb.state.Load() == int32(circuitClosed) {
		return true
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch circuitState(cb.state.Load()) {
	case circuitClosed:
		return true
	case circuitOpen:
		if time.Since(time.Unix(0, cb.lastFailure.Load())) > cb.timeout {
			cb.state.Store(int32(circuitHalfOpen))
			cb.successes.Store(0)
			return true
		}
		return false
	case circuitHalfOpen:
		return true
	}
	return false
}

func (cb *circuitBreaker) recordSuccess() {
	if cb.state.Load() != int32(circuitHalfOpen) {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state.Load() == int32(circuitHalfOpen) {
		if cb.successes.Add(1) >= int64(cb.successThreshold) {
			cb.state.Store(int32(circuitClosed))
			cb.failures.Store(0)
		}
	}
}

func (cb *circuitBreaker) recordFailure() {
	cb.failures.Add(1)
	cb.lastFailure.Store(time.Now().UnixNano())

	state := circuitState(cb.state.Load())

	if state == circuitHalfOpen {
		cb.mu.Lock()
		if cb.state.Load() == int32(circuitHalfOpen) {
			cb.state.Store(int32(circuitOpen))
		}
		cb.mu.Unlock()
		return
	}

	if state == circuitClosed && cb.failures.Load() >= int64(cb.failureThreshold) {
		cb.mu.Lock()
		if cb.state.Load() == int32(circuitClosed) && cb.failures.Load() >= int64(cb.failureThreshold) {
			cb.state.Store(int32(circuitOpen))
		}
		cb.mu.Unlock()
	}
}

func (cb *circuitBreaker) getState() circuitState {
	return circuitState(cb.state.Load())
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