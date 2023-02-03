package store

import (
	"context"
	"go-web/config"
	"log"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client
var ctx = context.Background()

func InitRedis() {
	RDB = redis.NewClient(&redis.Options{
		Addr:     config.Cfg.Redis.Addr,
		Password: config.Cfg.Redis.Password, // no password set
		DB:       config.Cfg.Redis.DB,       // use default DB
	})
	err := RDB.Ping(ctx).Err()
	if err == nil {
		log.Println("Redis 连接成功")
	}
}
