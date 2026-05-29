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
	FrontendURL2  string
	Port          string
	ApiKey        string
	ApiSecret     string
	CallbackURL   string
	TokenFilePath string
	CookieDomain  string
	GoEnv         string
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
		FrontendURL2:  os.Getenv("FRONTEND_URL2"),
		Port:          os.Getenv("PORT"),
		ApiKey:        os.Getenv("KITE_API_KEY"),
		ApiSecret:     os.Getenv("KITE_API_SECRET"),
		CallbackURL:   os.Getenv("KITE_CALLBACK_URL"),
		TokenFilePath: "./go_stock-tracker/.token.json",
		CookieDomain:  os.Getenv("COOKIE_DOMAIN"),
		GoEnv:         os.Getenv("GO_ENV"),
	}

}
