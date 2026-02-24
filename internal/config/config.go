package config

import (
	"github.com/joho/godotenv"
	"log"
	"os"
)

type Config struct {
	JWTSecret     string
	DatabaseURL   string
	FrontendURL   string
	Port          string
	ApiKey        string
	ApiSecret     string
	CallbackURL   string
	TokenFilePath string
}

var ServerConfig *Config

func MustLoad() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("No .env file found, relying on environment variables")
	}

	ServerConfig = &Config{
		JWTSecret:     os.Getenv("JWT_SECRET"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		FrontendURL:   os.Getenv("FRONTEND_URL"),
		Port:          os.Getenv("PORT"),
		ApiKey:        os.Getenv("KITE_API_KEY"),
		ApiSecret:     os.Getenv("KITE_API_SECRET"),
		CallbackURL:   os.Getenv("KITE_CALLBACK_URL"),
		TokenFilePath: "./.token.json",
	}

}
