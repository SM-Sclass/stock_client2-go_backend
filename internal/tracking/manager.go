package tracking

import (
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	kcws "github.com/SM-Sclass/stock_client2-go_backend/internal/kite/ws"
	"log"
	"sync"
	"time"
)

type TrackedStock struct {
	ID                  int64
	TradingSymbol       string
	InstrumentToken     uint32
	BasePrice           float64
	Target              float64
	StopLoss            float64
	OrderPriceLimit     float64
	BuyQuantity         uint32
	SellQuantity        uint32
	Locked              bool
	Exchange            string
	LastLTP             float64
	MaxExecutableOrders uint32
	// FifteenCandle holds the 9:15–9:30 opening-range OHLC fetched from Kite's
	// historical API at engine start (or on crash recovery).
	FifteenCandle Candle

	// Candles holds the rolling 5-min Current and Previous candles built live
	// from websocket ticks and rolled by the 5-min time ticker.
	Candles CandleState

	// Intraday state for the entry/exit strategy
	Direction      string // "BUY" or "SELL" – direction of the open position
	SignalFired    bool   // true once today's entry signal has been sent
	TradingAllowed bool   // false if we have hit the max trades for the day and should ignore further signals
}

type TrackingManager struct {
	tracked    map[uint32]TrackedStock
	kiteClient *kite.KiteClient
	mu         sync.RWMutex
	ws         *kcws.KiteWS
}

type TokenSubscriber interface {
	SubscribeToken(token uint32)
	UnsubscribeToken(token uint32)
}

func NewTrackingManager(ws *kcws.KiteWS, kiteClient *kite.KiteClient) *TrackingManager {
	return &TrackingManager{
		tracked:    make(map[uint32]TrackedStock),
		kiteClient: kiteClient,
		ws:         ws,
	}
}

func (tm *TrackingManager) AddTrackingStock(stock TrackedStock) bool {
	tm.mu.Lock()
	tm.tracked[stock.InstrumentToken] = stock
	tm.mu.Unlock()

	tm.loadFifteenCandlesForTS(stock)
	tm.loadCurrentCandlesForTS(stock)
	if _, exists := tm.tracked[stock.InstrumentToken]; exists {
		fifteen := tm.tracked[stock.InstrumentToken].FifteenCandle
		// Volatility filter: (HIGH - LOW) / LOW * 100 must exceed 0.35 %
		rangeSize := fifteen.High - fifteen.Low
		volatilityPct := rangeSize / fifteen.Low * 100
		if volatilityPct < 0.35 {
			log.Printf("⚠️ Skipping %s due to low volatility: %.2f%% (H=%.2f L=%.2f)", stock.TradingSymbol, volatilityPct, fifteen.High, fifteen.Low)
			tm.RemoveStockFromTracking(stock.InstrumentToken)
			return false
		}
	}

	if existing, exists := tm.tracked[stock.InstrumentToken]; exists {
		fifteen := tm.tracked[stock.InstrumentToken].FifteenCandle
		rangeSize := fifteen.High - fifteen.Low
		existing.Target = rangeSize
		existing.StopLoss = rangeSize * 0.5
		tm.mu.Lock()
		tm.tracked[stock.InstrumentToken] = existing
		tm.mu.Unlock()
	}

	tm.SetTradingAllowed(stock.InstrumentToken, false)
	tm.ws.SubscribeToken(stock.InstrumentToken)
	return true
}

func (tm *TrackingManager) RemoveStockFromTracking(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.tracked, token)
	tm.ws.UnsubscribeToken(token)
}

func (tm *TrackingManager) UpdateStockParameters(stock TrackedStock) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if existing, exists := tm.tracked[stock.InstrumentToken]; exists {
		// existing.Target = stock.Target
		// existing.StopLoss = stock.StopLoss
		existing.OrderPriceLimit = stock.OrderPriceLimit
		// existing.Locked = stock.Locked

		tm.tracked[stock.InstrumentToken] = existing
	}
}

func (tm *TrackingManager) GetStock(token uint32) (TrackedStock, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	stock, exists := tm.tracked[token]
	return stock, exists
}

func (tm *TrackingManager) GetTrackedStockByID(id int64) (TrackedStock, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	for _, stock := range tm.tracked {
		if stock.ID == id {
			return stock, true
		}
	}
	return TrackedStock{}, false
}

func (tm *TrackingManager) GetStockByTradingSymbol(tradingSymbol string) (TrackedStock, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, stock := range tm.tracked {
		if stock.TradingSymbol == tradingSymbol {
			return stock, true
		}
	}
	return TrackedStock{}, false
}

func (tm *TrackingManager) GetStockIDByTradingSymbol(tradingSymbol string) (int64, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, stock := range tm.tracked {
		if stock.TradingSymbol == tradingSymbol {
			return stock.ID, true
		}
	}
	return 0, false
}

func (tm *TrackingManager) GetAllStock() []TrackedStock {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	stocks := make([]TrackedStock, 0, len(tm.tracked))
	for _, stock := range tm.tracked {
		stocks = append(stocks, stock)
	}
	return stocks
}

func (tm *TrackingManager) GetBuyAndSellQuantityByToken(token uint32) (uint32, uint32, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if stock, exists := tm.tracked[token]; exists {
		return stock.BuyQuantity, stock.SellQuantity, true
	}
	return 0, 0, false
}

func (tm *TrackingManager) GetBasePriceByToken(token uint32) (float64, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if stock, exists := tm.tracked[token]; exists {
		return stock.BasePrice, true
	}
	return 0, false
}

// get tracking stock LTP from current candle close price.
func (tm *TrackingManager) GetTSLtpByToken(token uint32) (float64, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if stock, exists := tm.tracked[token]; exists {
		if stock.Candles.Current.Close != 0 {
			return stock.Candles.Current.Close, true
		}
		return stock.LastLTP, true
	}
	return 0, false
}

func (tm *TrackingManager) SetSellQuantity(token uint32, quantity uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.SellQuantity = quantity
		tm.tracked[token] = stock
	}
}

func (tm *TrackingManager) SetBuyQuantity(token uint32, quantity uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.BuyQuantity = quantity
		tm.tracked[token] = stock
	}
}

func (tm *TrackingManager) ToggleLock(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if stock, exists := tm.tracked[token]; exists {
		stock.Locked = !stock.Locked
		tm.tracked[token] = stock
	}
}

func (tm *TrackingManager) LockStock(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if stock, exists := tm.tracked[token]; exists {
		stock.Locked = true
		tm.tracked[token] = stock
	}
}

func (tm *TrackingManager) TryLockStock(token uint32) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	stock, exists := tm.tracked[token]

	if !exists {
		return false // Stock doesn't exist, can't lock it
	}
	if stock.Locked {
		return false // Someone else beat us to it!
	}

	stock.Locked = true
	tm.tracked[token] = stock

	return true // We successfully claimed the stock!
}

func (tm *TrackingManager) UnlockStock(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if stock, exists := tm.tracked[token]; exists {
		stock.Locked = false
		tm.tracked[token] = stock
	}
}
func (tm *TrackingManager) CountStocks() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.tracked)
}

// SetFifteenCandle stores the 9:15–9:30 opening-range candle fetched from the historical API.
func (tm *TrackingManager) SetFifteenCandle(token uint32, candle Candle) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.FifteenCandle = candle
		tm.tracked[token] = stock
	}
}

// SetCurrentCandle primes the Current 5-min candle from historical data (crash recovery).
func (tm *TrackingManager) SetCurrentCandle(token uint32, candle Candle) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.Candles.Current = candle
		tm.tracked[token] = stock
	}
}

// UpdateCurrentCandle incorporates a tick price into the live Current 5-min candle.
func (tm *TrackingManager) UpdateCurrentCandle(token uint32, price float64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.Candles.Current.Update(price)
		tm.tracked[token] = stock
	}
}

// RollCandle closes the current 5-min candle: moves it to Previous and resets Current.
// Called by the AlgoEngine's 5-min time ticker at each candle boundary.
func (tm *TrackingManager) RollCandle(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.LastLTP = stock.Candles.Previous.Close
		stock.Candles.Roll()
		tm.tracked[token] = stock
	}
}

// SetSignalFired marks that the entry signal for today has been sent.
func (tm *TrackingManager) SetSignalFired(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.SignalFired = true
		tm.tracked[token] = stock
	}
}

// SetDirection sets the intended trade direction ("BUY" or "SELL") for a stock.
func (tm *TrackingManager) SetDirection(token uint32, direction string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.Direction = direction
		tm.tracked[token] = stock
	}
}

func (tm *TrackingManager) SetTradingAllowed(token uint32, allowed bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.TradingAllowed = allowed
		tm.tracked[token] = stock
	}
}

// UpdateTradeParams stores the computed target, stoploss, and direction on a
// tracked stock immediately after an entry order is placed so that the exit
// logic has these values when the fill confirmation arrives.
func (tm *TrackingManager) UpdateTradeParams(token uint32, target, stopLoss float64, direction string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.Target = target
		stock.StopLoss = stopLoss
		stock.Direction = direction
		tm.tracked[token] = stock
	}
}

func (tm *TrackingManager) IsStockTracked(token uint32) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	_, exists := tm.tracked[token]
	return exists
}

// UpdateBasePrice updates the base price of a tracked stock when an order completes. This implements the services.BasePriceUpdater interface.
func (tm *TrackingManager) UpdateBasePrice(instrumentToken uint32, price float64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if stock, exists := tm.tracked[instrumentToken]; exists {
		stock.BasePrice = price
		tm.tracked[instrumentToken] = stock
	}
}

// Decrement max executable orders by 1 after an order is placed and exited. This prevents overtrading in case of stale signals or missed fills.
func (tm *TrackingManager) DecrementMaxExecutableOrders(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		if stock.MaxExecutableOrders > 0 {
			stock.MaxExecutableOrders--
		}
		tm.tracked[token] = stock
	}
}

// Reset Parameters like Fifteen candle, candles and direction as "" for all stocks. This is called at market close to reset the state for the new day.
func (tm *TrackingManager) ResetParameters(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.FifteenCandle = Candle{}
		stock.Candles.Current = Candle{}
		stock.Candles.Previous = Candle{}
		stock.Direction = ""
		stock.SignalFired = false
		stock.TradingAllowed = false
		tm.tracked[token] = stock
	}
}

// Reset Firing and direction as "" for all stocks. This is called at market close to reset the state for the new day.
func (tm *TrackingManager) ResetFiringAndDirection(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stock, exists := tm.tracked[token]; exists {
		stock.SignalFired = false
		stock.Direction = ""
		tm.tracked[token] = stock
	}
}

// loadFifteenCandles fetches the 9:15–9:30 opening-range candle from the Kite
func (tm *TrackingManager) loadFifteenCandlesForTS(stock TrackedStock) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Now().In(ist)
	from := time.Date(now.Year(), now.Month(), now.Day(), 9, 15, 0, 0, ist)
	to := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, ist)

	data, err := tm.kiteClient.GetHistoricOHLC(int64(stock.InstrumentToken), "15minute", from, to)
	if err != nil {
		log.Printf("⚠️ Cannot load fifteen candle for %s: %v", stock.TradingSymbol, err)

	}
	if len(data) == 0 {
		log.Printf("⚠️ No fifteen candle data for %s (market not yet opened?)", stock.TradingSymbol)
	}
	candle := Candle{
		Open:  data[0].Open,
		High:  data[0].High,
		Low:   data[0].Low,
		Close: data[0].Close,
	}
	tm.SetFifteenCandle(stock.InstrumentToken, candle)
	log.Printf("📊 Fifteen candle for %s: O=%.2f H=%.2f L=%.2f C=%.2f", stock.TradingSymbol, candle.Open, candle.High, candle.Low, candle.Close)
}

// loadCurrentCandles fetches the in-progress 5-min candle from the Kite historical
// API for crash recovery. E.g., server restarts at 12:37 → fetches 12:35-12:37 data.
func (tm *TrackingManager) loadCurrentCandlesForTS(stock TrackedStock) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Now().In(ist)
	marketStart := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, ist)

	elapsed := now.Sub(marketStart)
	intervalIdx := int(elapsed.Minutes()) / 5
	intervalStart := marketStart.Add(time.Duration(intervalIdx*5) * time.Minute)

	// Nothing to load if we are exactly on the boundary.
	if !now.After(intervalStart) {
		return
	}

	data, err := tm.kiteClient.GetHistoricOHLC(int64(stock.InstrumentToken), "5minute", intervalStart, now)
	if err != nil {
		log.Printf("⚠️ Cannot load current candle for %s: %v", stock.TradingSymbol, err)

	}
	if len(data) == 0 {
		log.Printf("⚠️ No Current candle data for %s (market not yet opened?)", stock.TradingSymbol)
	}
	candle := Candle{
		Open:  data[0].Open,
		High:  data[0].High,
		Low:   data[0].Low,
		Close: data[0].Close,
	}
	tm.SetCurrentCandle(stock.InstrumentToken, candle)

	log.Printf("🕯️ Crash-recovery candle for %s: O=%.2f H=%.2f L=%.2f C=%.2f",
		stock.TradingSymbol, candle.Open, candle.High, candle.Low, candle.Close)
}
