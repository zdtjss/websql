package store

import (
	"go-web/config"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

func InitRedis() {
	RDB = redis.NewClient(&redis.Options{
		Addr:     config.Cfg.Redis.Addr,
		Password: config.Cfg.Redis.Password, // no password set
		DB:       config.Cfg.Redis.DB,       // use default DB
	})
}
