package utils

import "time"

// MarketPhase represents the current algorithmic phase of the trading day.
type MarketPhase int

const (
	PhasePreMarket  MarketPhase = iota // before 9:15
	PhaseFifteen                       // 9:15–9:30: build opening-range candle
	PhaseSignal                        // 9:30–9:35: detect entry signal
	PhaseMonitor                       // 9:35–15:10: monitor open positions
	PhaseExit                          // 15:10–15:15: force-exit all open positions
	PhasePostMarket                    // after 15:15
)

// GetMarketPhase returns the current algorithmic phase based on IST time.
func GetMarketPhase(now time.Time) MarketPhase {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	t := now.In(ist)
	h, m, _ := t.Clock()
	mins := h*60 + m

	switch {
	case mins < 9*60+15:
		return PhasePreMarket
	case mins < 9*60+30:
		return PhaseFifteen
	case mins < 9*60+35:
		return PhaseSignal
	case mins < 15*60+10:
		return PhaseMonitor
	case mins < 15*60+15:
		return PhaseExit
	default:
		return PhasePostMarket
	}
}
