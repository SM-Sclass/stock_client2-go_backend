package models

import "time"

type TrackingStock struct {
	ID							int64     	`json:"id"`
	StockSymbol			string    	`json:"stock_symbol"`
	Exchange				string    	`json:"exchange"`
	InstrumentToken	int64     	`json:"instrument_token"`
	Target 					float64   	`json:"target"`
	StopLoss				float64   	`json:"stoploss"`
	Quantity				uint32    	`json:"quantity"`
	Status 					string    	`json:"status"`
	CreatedAt 			time.Time 	`json:"created_at"`
}