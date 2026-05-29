package models

import "time"

type TrackingStock struct {
	ID              int64      `json:"id"`
	TradingSymbol   string     `json:"trading_symbol"`
	Exchange        string     `json:"exchange"`
	InstrumentToken int64      `json:"instrument_token"`
	Target          float64    `json:"target"`
	StopLoss        float64    `json:"stoploss"`
	OrderPriceLimit float64    `json:"order_price_limit"`
	Quantity        uint32     `json:"quantity"`
	AllowedTrades   uint32     `json:"allowed_trades"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
	IsDeleted       bool       `json:"is_deleted"`
}
