package algo

import "time"

type SignalType string

const (
	SignalTargetHit   SignalType = "TARGET_HIT"
	SignalStopLossHit SignalType = "STOPLOSS_HIT"
)

type TradeSignal struct {
	InstrumentToken uint32
	StockSymbol     string
	Exchange        string
	SignalType      SignalType
	TriggerPrice    float64
	BasePrice       float64
	Target          float64
	StopLoss        float64
	Quantity        uint32
	Timestamp       time.Time
}
