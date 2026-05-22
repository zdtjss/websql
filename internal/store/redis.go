package store

import (
	"context"
	"websql/internal/config"
	"log"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client
var ctx = context.Background()

func InitRedis() {
	RDB = redis.NewClient(&redis.Options{
		Addr:     config.Cfg.Redis.Addr,
		Password: config.Cfg.Redis.Password,
		DB:       config.Cfg.Redis.DB,
	})
	err := RDB.Ping(ctx).Err()
	if err == nil {
		log.Println("Redis 连接成功")
	} else {
		log.Fatalf("Redis 连接失败,err:%s\n", err.Error())
	}
}