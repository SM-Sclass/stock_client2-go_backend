package kite

import (
	"encoding/json"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/config"
	"os"
	"path/filepath"
	"time"
)

type KiteToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

func IsTokenValid(token *KiteToken) bool {
	if token == nil {
		return false
	}

	return time.Now().Before(token.Expiry)
}

func Save(token *KiteToken) error {
	os.MkdirAll(filepath.Dir(config.ServerConfig.TokenFilePath), 0755)
	b, _ := json.MarshalIndent(token, "", "  ")
	return os.WriteFile(config.ServerConfig.TokenFilePath, b, 0644)
}

func Load() (*KiteToken, error) {
	b, err := os.ReadFile(config.ServerConfig.TokenFilePath)
	if err != nil {
		return nil, err
	}
	var token KiteToken
	err = json.Unmarshal(b, &token)
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func Delete() error {
	return os.Remove(config.ServerConfig.TokenFilePath)
}
