package models

import "time"

// type Order struct {
// 	ID              int64     `json:"id"`
// 	TrackingStockID int64     `json:"tracking_stock_id"`
// 	OrderID         string    `json:"order_id"`
// 	ExchangeOrderID string    `json:"exchange_order_id"`
// 	ParentOrderID   string    `json:"parent_order_id"`
// 	OrderType       string    `json:"order_type"`
// 	EventType       string    `json:"event_type"` // TARGET_HIT, STOPLOSS_HIT
// 	TransactionType string    `json:"transaction_type"`
// 	Exchange        string    `json:"exchange"`
// 	Product         string    `json:"product"`
// 	Quantity        float64   `json:"quantity"`
// 	BasePrice       float64   `json:"base_price"`
// 	TriggerPrice    float64   `json:"trigger_price"`
// 	PurchasePrice   float64   `json:"purchase_price"`
// 	StatusMessage   string    `json:"status_message"`
// 	Status          string    `json:"status"`
// 	PlacedAt        time.Time `json:"placed_at"`
// 	UpdatedAt       time.Time `json:"updated_at"`
// }

type Order struct {
	ID              int64     `json:"id"`
	TrackingStockID int64     `json:"tracking_stock_id"`
	OrderID         string    `json:"order_id"`
	ExchangeOrderID *string   `json:"exchange_order_id"` // Pointer for NULL
	ParentOrderID   *string   `json:"parent_order_id"`   // Pointer for NULL
	OrderType       string    `json:"order_type"`
	EventType       string    `json:"event_type"`        // TARGET_HIT, STOPLOSS_HIT
	TransactionType *string   `json:"transaction_type"`  // Pointer for NULL
	Exchange        string    `json:"exchange"`          // Has default, but ensure no NULLs are inserted
	Product         *string   `json:"product"`           // Pointer for NULL
	Quantity        float64   `json:"quantity"`
	BasePrice       float64   `json:"base_price"`
	TriggerPrice    *float64  `json:"trigger_price"`     // Pointer for NULL
	PurchasePrice   *float64  `json:"purchase_price"`    // Pointer for NULL
	StatusMessage   *string   `json:"status_message"`    // Pointer for NULL
	Status          string    `json:"status"`
	PlacedAt        time.Time `json:"placed_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}