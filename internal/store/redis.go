package store

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"websql/internal/config"
	"websql/internal/pkg/safego"

	"github.com/redis/go-redis/v9"
)

var (
	redisMu    sync.RWMutex
	RDB        *redis.Client
	redisReady bool
	ctx        = context.Background()
)

// InitRedis 初始化 Redis；失败时保持本地缓存可用，并在后台周期性重试。
//
// 关键约束：Redis 不可达不能让启动流程长时间阻塞——进程必须优先完成监听，
// 会话/权限缓存按本地模式工作，恢复后自动切回 Redis。
func InitRedis(cfg *config.Config) {
	if cfg == nil || strings.TrimSpace(cfg.Redis.Addr) == "" {
		return
	}
	if tryInitRedis(cfg) {
		return
	}
	safego.GoWithName("redis-retry", func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if tryInitRedis(cfg) {
				return
			}
		}
	})
}

func tryInitRedis(cfg *config.Config) bool {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		MaxRetries:   0,
	})
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		log.Printf("Redis 连接失败,err:%s，Redis 功能将不可用（本地缓存继续工作，30s 后重试）\n", err.Error())
		_ = client.Close()
		return false
	}
	redisMu.Lock()
	old := RDB
	RDB = client
	redisReady = true
	redisMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	log.Println("Redis 连接成功")
	return true
}

// currentRedis 返回可用的 Redis 客户端；不可用时返回 nil，调用方走本地缓存。
func currentRedis() *redis.Client {
	redisMu.RLock()
	defer redisMu.RUnlock()
	if !redisReady {
		return nil
	}
	return RDB
}
