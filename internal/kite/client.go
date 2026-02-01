package kite

import (
	"fmt"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/config"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

type KiteClient struct {
	KiteConnect     *kiteconnect.Client
	APIKey          string
	APISecret       string
	CallbackURL     string
	AccessToken     string
}

func NewKiteClient() *KiteClient {
	kc := kiteconnect.New(config.ServerConfig.ApiKey)

	return &KiteClient{
		KiteConnect: kc,
		APIKey:      config.ServerConfig.ApiKey,
		APISecret:   config.ServerConfig.ApiSecret,
		CallbackURL: config.ServerConfig.CallbackURL,
	}
}

func (kc *KiteClient) GenerateSession(requestToken string) error {
	if kc.APISecret == "" {
		return fmt.Errorf("API secret is missing")
	}

	session, err := kc.KiteConnect.GenerateSession(requestToken, kc.APISecret)
	if err != nil {
		return err
	}

	// Save token
	token := &KiteToken{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		Expiry:       time.Now().Add(24 * time.Hour),
	}

	err = Save(token)
	if err != nil {
		return err
	}

	kc.AccessToken = session.AccessToken
	kc.KiteConnect.SetAccessToken(session.AccessToken)
	return nil
}

func (kc *KiteClient) EnsureAuthenticated() error {
	token, err := Load()
	if err != nil {
		return err
	}

	if token != nil && time.Now().Before(token.Expiry) {
		kc.KiteConnect.SetAccessToken(token.AccessToken)
		return nil
	}
	return fmt.Errorf("token expired or missing")
}

func (kc *KiteClient) GetInstrumentsByExchange(exchange string) ([]kiteconnect.Instrument, error) {
	return kc.KiteConnect.GetInstrumentsByExchange(exchange)
}

func (kc *KiteClient) GetLoginURL() string {
	return kc.KiteConnect.GetLoginURL()
}

func (kc *KiteClient) IsTokenValid() bool {
	_, err := kc.KiteConnect.GetUserProfile()
	if err != nil {
		return false
	}

	return true
}
