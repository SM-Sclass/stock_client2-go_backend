package database

import (
	"context"
	"log"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/config"
)


func ConnectPostgresDB() *pgxpool.Pool {
	var DB *pgxpool.Pool

	dsn:= config.ServerConfig.DatabaseURL
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	log.Println("DATABASE URL: ", dsn)

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatal("Failed to parse DB config:", err)
	}

	DB, err = pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		log.Fatal("Failed to create DB pool:", err)
	}

	if err := DB.Ping(context.Background()); err != nil {
		log.Fatal("DB ping failed:", err)
	}

	log.Println("✅ Postgres connected")
	return DB
}
