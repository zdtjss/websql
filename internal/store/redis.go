package store

import (
	"context"
	"log"

	"websql/internal/config"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client
var ctx = context.Background()

func InitRedis(cfg *config.Config) {
	RDB = redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	err := RDB.Ping(ctx).Err()
	if err == nil {
		log.Println("Redis 连接成功")
	} else {
		log.Printf("Redis 连接失败,err:%s，Redis 功能将不可用\n", err.Error())
	}
}