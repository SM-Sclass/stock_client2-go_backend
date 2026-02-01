package database

import (
	"context"
	"github.com/redis/go-redis/v9"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/config"
	"log"
)

var RedisDB *redis.Client

func ConnectRedisDB() {
	redisAddr := config.ServerConfig.RedisAddr
	if redisAddr == "" {
		log.Fatal("REDIS_ADDR is not set")
	}

	RedisDB = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	_, err := RedisDB.Ping(context.Background()).Result()
	if err != nil {
		log.Fatal("Failed to connect to Redis: " + err.Error())
	}
	log.Println("✅ Connected to Redis")
}
