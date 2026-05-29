package algo

import "time"

type SignalType string

const (
	SignalEntryBuy    SignalType = "ENTRY_BUY"    // open a long position
	SignalEntrySell   SignalType = "ENTRY_SELL"   // open a short position
	SignalTargetHit   SignalType = "TARGET_HIT"   // close position at target (LIMIT)
	SignalStopLossHit SignalType = "STOPLOSS_HIT" // close position at stoploss (MARKET)
	SignalForceExit   SignalType = "FORCE_EXIT"   // 15:10 PM forced close (MARKET)
	SignalNone        SignalType = "NONE"
)

type TradeSignal struct {
	TrackingStockID int64
	InstrumentToken uint32
	TradingSymbol   string
	Exchange        string
	SignalType      SignalType
	Direction       string // "BUY" or "SELL" – direction of the open position
	TriggerPrice    float64
	BasePrice       float64
	Target          float64
	StopLoss        float64
	Quantity        uint32
	Timestamp       time.Time
}
