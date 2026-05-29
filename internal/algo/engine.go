package algo

import (
	// "context"
	"log"
	"sync"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/utils"

	kitemodels "github.com/zerodha/gokiteconnect/v4/models"
)

const (
	maxDailyTrades   = 2      // max entry trades per day across all stocks
	maxLossPerTrade  = 4500.0 // ₹4500 max stoploss exposure per trade
	maxDailyLoss     = 9000.0 // ₹9000 max total loss per day
	minVolatilityPct = 0.35   // minimum (HIGH-LOW)/LOW*100 % required to enter
)

type AlgoEngine struct {
	trackingManager *tracking.TrackingManager
	broadcaster     *kite.TickBroadcaster
	kiteClient      *kite.KiteClient

	tickChan   chan []kitemodels.Tick
	signalChan chan TradeSignal
	stopChan   chan struct{}
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex

	ist *time.Location

	// Daily risk counters — reset to zero on every engine Start()
	dailyTradeCount int
	openTradeCount  int
}

func NewAlgoEngine(
	trackingManager *tracking.TrackingManager,
	broadcaster *kite.TickBroadcaster,
	kiteClient *kite.KiteClient,
	signalChan chan TradeSignal,
) *AlgoEngine {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	return &AlgoEngine{
		trackingManager: trackingManager,
		broadcaster:     broadcaster,
		kiteClient:      kiteClient,
		signalChan:      signalChan,
		stopChan:        make(chan struct{}),
		ist:             ist,
	}
}

// Start begins the engine. It loads historical candle data for all tracked stocks then
// launches the tick goroutine (live OHLC updates + real-time stoploss) and the
// 5-min candle-roll goroutine (entry signal checks).
func (ae *AlgoEngine) Start() {
	ae.mu.Lock()
	if ae.running {
		ae.mu.Unlock()
		return
	}
	ae.running = true
	ae.stopChan = make(chan struct{})
	ae.tickChan = ae.broadcaster.Subscribe(100)
	ae.dailyTradeCount = 0
	ae.openTradeCount = 0
	ae.mu.Unlock()

	// ctx := context.Background()

	// Load the 9:15–9:30 opening-range candle for every tracked stock.
	ae.loadFifteenCandles()

	// Crash-recovery: if we are past 9:30, also prime the live current candle
	// from historical data so we don't start with an empty partial candle.
	phase := utils.GetMarketPhase(time.Now().In(ae.ist))
	if phase >= utils.PhaseSignal {
		ae.loadCurrentCandles()
	}

	ae.wg.Add(2)
	go ae.tickLoop()
	go ae.candleRollLoop()

	log.Println("🚀 AlgoEngine started")
}

// Stop gracefully shuts down the engine.
func (ae *AlgoEngine) Stop() {
	ae.mu.Lock()
	if !ae.running {
		ae.mu.Unlock()
		return
	}
	ae.running = false
	close(ae.stopChan)
	ae.mu.Unlock()

	ae.wg.Wait()
	log.Println("🛑 AlgoEngine stopped")
}

func (ae *AlgoEngine) IsRunning() bool {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	return ae.running
}

// DecrementOpenTrade decreases the open-position counter when an exit fills.
// Called by the OrderEngine after a successful exit order placement.
func (ae *AlgoEngine) DecrementOpenTrade() {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	if ae.openTradeCount > 0 {
		ae.openTradeCount--
	}
}

func (ae *AlgoEngine) DecrementDailyTrade() {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	if ae.dailyTradeCount > 0 {
		ae.dailyTradeCount--
	}
}

// ─── Tick loop ────────────────────────────────────────────────────────────────

// tickLoop subscribes to the broadcaster and processes every incoming tick.
func (ae *AlgoEngine) tickLoop() {
	defer ae.wg.Done()
	for {
		select {
		case <-ae.stopChan:
			return
		case ticks := <-ae.tickChan:
			for _, tick := range ticks {
				ae.processTick(tick)
			}
		}
	}
}

// processTick updates the live Current candle and checks real-time stoploss.
func (ae *AlgoEngine) processTick(tick kitemodels.Tick) {
	price := tick.LastPrice
	if price == 0 {
		return
	}
	token := tick.InstrumentToken

	// Update the Current 5-min candle with this tick price.
	ae.trackingManager.UpdateCurrentCandle(token, price)

	// Real-time stoploss check for any open position.
	stock, exists := ae.trackingManager.GetStock(token)
	if !exists || stock.Locked {
		return
	}
	ae.checkTargetAndSl(stock, price, token)
}

// checkTargetAndSl sends signals for target and stoploss based on the stock's direction.
func (ae *AlgoEngine) checkTargetAndSl(stock tracking.TrackedStock, price float64, token uint32) {
	if stock.Direction == "" || stock.BasePrice == 0 {
		return
	}
	sl := stock.StopLoss
	target := stock.Target
	basePrice := stock.BasePrice
	if sl == 0 || target == 0 {
		return
	}
	// For BUY: target hit if price >= target, sl hit if price <= sl
	if stock.Direction == "BUY" {
		if price >= basePrice+target {
			if !ae.trackingManager.TryLockStock(token) {
				return
			}
			ae.signalChan <- ae.buildExitSignal(stock, token, price, SignalTargetHit)
			log.Printf("🎯 Target hit for BUY direc. %s: price=%.2f target=%.2f", stock.TradingSymbol, price, basePrice+target)
		} else if price <= basePrice-sl {
			if !ae.trackingManager.TryLockStock(token) {
				return
			}
			ae.signalChan <- ae.buildExitSignal(stock, token, price, SignalStopLossHit)
			log.Printf("🛑 Stoploss hit for BUY direc. %s: price=%.2f sl=%.2f", stock.TradingSymbol, price, basePrice-sl)
		}
		return

	}

	// For SELL: target hit if price <= target, sl hit if price >= sl
	if stock.Direction == "SELL" {
		if price <= basePrice-target {
			if !ae.trackingManager.TryLockStock(token) {
				return
			}
			ae.signalChan <- ae.buildExitSignal(stock, token, price, SignalTargetHit)
			log.Printf("🎯 Target hit for SELL direc. %s: price=%.2f target=%.2f", stock.TradingSymbol, price, basePrice-target)
		} else if price >= basePrice+sl {
			if !ae.trackingManager.TryLockStock(token) {
				return
			}
			ae.signalChan <- ae.buildExitSignal(stock, token, price, SignalStopLossHit)
			log.Printf("🛑 Stoploss hit for SELL direc. %s: price=%.2f sl=%.2f", stock.TradingSymbol, price, basePrice+sl)
		}
		return
	}
}

// ─── Candle-roll loop ─────────────────────────────────────────────────────────

// candleRollLoop waits for each 5-min boundary (9:30, 9:35, 9:40 …) then rolls
// candles and checks entry signals using the just-completed candle's Close.
func (ae *AlgoEngine) candleRollLoop() {
	defer ae.wg.Done()
	for {
		next := ae.next5MinBoundary(time.Now())
		select {
		case <-ae.stopChan:
			return
		case <-time.After(time.Until(next)):
			ae.onCandleRoll()
		}
	}
}

// onCandleRoll is the main strategy loop: called at every 5-min boundary.
func (ae *AlgoEngine) onCandleRoll() {
	now := time.Now().In(ae.ist)
	phase := utils.GetMarketPhase(now)

	if phase == utils.PhasePreMarket || phase == utils.PhasePostMarket {
		return
	}

	stocks := ae.trackingManager.GetAllStock()

	for _, stock := range stocks {
		// Roll Current → Previous, reset Current for the new interval.
		ae.trackingManager.RollCandle(stock.InstrumentToken)

		// Re-read the stock to get the updated candle state.
		stock, exists := ae.trackingManager.GetStock(stock.InstrumentToken)
		if !exists {
			continue
		}

		switch phase {
		case utils.PhaseSignal, utils.PhaseMonitor:
			if !stock.SignalFired {
				ae.checkEntryOnCandle(stock)
			}

		case utils.PhaseExit:
			// Force-close any open position at 15:10.
			if stock.SellQuantity > 0 && !stock.Locked {
				if ae.trackingManager.TryLockStock(stock.InstrumentToken) {
					ae.signalChan <- ae.buildExitSignal(
						stock, stock.InstrumentToken,
						stock.Candles.Previous.Close, SignalForceExit,
					)
					log.Printf("⏰ Force exit for %s at 15:10", stock.TradingSymbol)
				}
			}
		}
	}
}

// checkEntryOnCandle fires a BUY or SELL signal when the completed candle's
// Close breaks the 9:15–9:30 opening-range HIGH or LOW.
func (ae *AlgoEngine) checkEntryOnCandle(stock tracking.TrackedStock) {
	if ae.openTradeCount != 0 {
		log.Printf("⏸️ Skipping entry check for %s because there's already an open trade", stock.TradingSymbol)
		return
	}

	if stock.MaxExecutableOrders <= 0 {
		log.Printf("⏸️ Max executable orders reached for %s", stock.TradingSymbol)
		return
	}
	fifteen := stock.FifteenCandle
	log.Printf("🔍 Checking entry for %s at %v: 15m H=%.2f L=%.2f Close=%.2f previous=%.2f current=%.2f",
		stock.TradingSymbol,
		time.Now(), fifteen.High, fifteen.Low, fifteen.Close,
		stock.Candles.Previous.Close, stock.Candles.Current.Close)

	if !fifteen.IsValid() {
		log.Printf("⚠️ Fifteen candle not valid for %s — skipping entry check", stock.TradingSymbol)
		return
	}

	previous := stock.Candles.Previous
	if !previous.IsValid() {
		log.Printf("⚠️ Previous candle not valid for %s — skipping entry check", stock.TradingSymbol)
		return
	}

	// Volatility filter: (HIGH - LOW) / LOW * 100 must exceed 0.35 %
	rangeSize := fifteen.High - fifteen.Low
	// if rangeSize <= 0 {
	// 	log.Printf("⚠️ Invalid range size for %s — skipping entry check", stock.TradingSymbol)
	// 	return
	// }
	// volatilityPct := rangeSize / fifteen.Low * 100
	// if volatilityPct <= minVolatilityPct {
	// 	log.Printf("📊 %s: vol %.2f%% < %.2f%% — no trade",
	// 		stock.TradingSymbol, volatilityPct, minVolatilityPct)
	// 	return
	// }

	// Daily risk guards
	ae.mu.Lock()
	if ae.dailyTradeCount >= maxDailyTrades || ae.openTradeCount >= 1 {
		ae.mu.Unlock()
		return
	}
	ae.mu.Unlock()

	// Entry direction based on previous candle's Close vs fifteen HIGH/LOW
	prevClosePrice := previous.Close
	var signalType SignalType
	var direction string

	switch {
	case prevClosePrice > fifteen.High:
		signalType = SignalEntryBuy
		direction = "BUY"
	case prevClosePrice < fifteen.Low:
		signalType = SignalEntrySell
		direction = "SELL"
	default:
		return // price inside range — skip this candle
	}

	// Position sizing: QUANTITY = 4500 / SL
	target := rangeSize
	sl := target * 0.5
	quantity := uint32(maxLossPerTrade / sl)

	ltp, exists := ae.trackingManager.GetTSLtpByToken(stock.InstrumentToken)
	if !exists || ltp <= 0 {
		log.Printf("⚠️ LTP is %.2f not available for %s — skipping entry", ltp, stock.TradingSymbol)
		return
	}

	if ltp != 0 && stock.OrderPriceLimit != 0 && float64(quantity)*ltp > stock.OrderPriceLimit {
		// If the order price limit is set and the current LTP exceeds it, we adjust the quantity downwards to respect the order price limit
		quantity = uint32(stock.OrderPriceLimit / ltp)
		log.Printf("⚠️ Adjusted quantity for %s due to order price limit: new qty=%d", stock.TradingSymbol, quantity)
	} else if float64(quantity)*ltp > 200000 {
		quantity = uint32(200000 / ltp)
		log.Printf("⚠️ Adjusted quantity for %s due to max order value: new qty=%d", stock.TradingSymbol, quantity)
	}

	if quantity == 0 {
		quantity = 1
	}

	if !ae.trackingManager.TryLockStock(stock.InstrumentToken) {
		return
	}
	ae.trackingManager.SetSignalFired(stock.InstrumentToken)
	ae.trackingManager.SetDirection(stock.InstrumentToken, direction)

	ae.mu.Lock()
	ae.dailyTradeCount++
	ae.openTradeCount++
	ae.mu.Unlock()

	ae.signalChan <- TradeSignal{
		TrackingStockID: stock.ID,
		InstrumentToken: stock.InstrumentToken,
		TradingSymbol:   stock.TradingSymbol,
		Exchange:        stock.Exchange,
		SignalType:      signalType,
		Direction:       direction,
		TriggerPrice:    ltp,
		BasePrice:       ltp,
		Target:          target,
		StopLoss:        sl,
		Quantity:        quantity,
		Timestamp:       time.Now(),
	}

	log.Printf("📈 Entry %s for %s: close=%.2f H=%.2f L=%.2f target=%.2f sl=%.2f qty=%d",
		direction, stock.TradingSymbol, ltp,
		fifteen.High, fifteen.Low, target, sl, quantity)
}

// ─── Historical data loaders ──────────────────────────────────────────────────

// loadFifteenCandles fetches the 9:15–9:30 opening-range candle from the Kite
// historical API for every tracked stock and stores it in memory.
func (ae *AlgoEngine) loadFifteenCandles() {
	now := time.Now().In(ae.ist)
	from := time.Date(now.Year(), now.Month(), now.Day(), 9, 15, 0, 0, ae.ist)
	to := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, ae.ist)

	for _, stock := range ae.trackingManager.GetAllStock() {
		data, err := ae.kiteClient.GetHistoricOHLC(int64(stock.InstrumentToken), "15minute", from, to)
		if err != nil {
			log.Printf("⚠️ Cannot load fifteen candle for %s: %v", stock.TradingSymbol, err)
			continue
		}
		if len(data) == 0 {
			log.Printf("⚠️ No fifteen candle data for %s (market not yet opened?)", stock.TradingSymbol)
			continue
		}
		candle := tracking.Candle{
			Open:  data[0].Open,
			High:  data[0].High,
			Low:   data[0].Low,
			Close: data[0].Close,
		}
		ae.trackingManager.SetFifteenCandle(stock.InstrumentToken, candle)
		log.Printf("📊 Fifteen candle for %s: O=%.2f H=%.2f L=%.2f C=%.2f",
			stock.TradingSymbol, candle.Open, candle.High, candle.Low, candle.Close)
	}
}

// loadCurrentCandles fetches the in-progress 5-min candle from the Kite historical
// API for crash recovery. E.g., server restarts at 12:37 → fetches 12:35-12:37 data.
func (ae *AlgoEngine) loadCurrentCandles() {
	now := time.Now().In(ae.ist)
	marketStart := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, ae.ist)

	elapsed := now.Sub(marketStart)
	intervalIdx := int(elapsed.Minutes()) / 5
	intervalStart := marketStart.Add(time.Duration(intervalIdx*5) * time.Minute)

	// Nothing to load if we are exactly on the boundary.
	if !now.After(intervalStart) {
		return
	}

	for _, stock := range ae.trackingManager.GetAllStock() {
		data, err := ae.kiteClient.GetHistoricOHLC(int64(stock.InstrumentToken), "5minute", intervalStart, now)
		if err != nil {
			log.Printf("⚠️ Cannot load current candle for %s: %v", stock.TradingSymbol, err)
			continue
		}
		if len(data) == 0 {
			continue
		}
		candle := tracking.Candle{
			Open:  data[0].Open,
			High:  data[0].High,
			Low:   data[0].Low,
			Close: data[0].Close,
		}
		ae.trackingManager.SetCurrentCandle(stock.InstrumentToken, candle)
		log.Printf("🕯️ Crash-recovery candle for %s: O=%.2f H=%.2f L=%.2f C=%.2f",
			stock.TradingSymbol, candle.Open, candle.High, candle.Low, candle.Close)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (ae *AlgoEngine) buildExitSignal(stock tracking.TrackedStock, token uint32, price float64, sigType SignalType) TradeSignal {
	var qty uint32
	if stock.Direction == "BUY" {
		qty = stock.BuyQuantity
	} else {
		qty = stock.SellQuantity
	}

	if price == 0 {
		price = stock.BasePrice
	}

	return TradeSignal{
		TrackingStockID: stock.ID,
		InstrumentToken: token,
		TradingSymbol:   stock.TradingSymbol,
		Exchange:        stock.Exchange,
		SignalType:      sigType,
		Direction:       stock.Direction,
		TriggerPrice:    price,
		BasePrice:       price,
		Target:          stock.Target,
		StopLoss:        stock.StopLoss,
		Quantity:        qty,
		Timestamp:       time.Now(),
	}
}

// next5MinBoundary returns the next 5-min candle boundary after now, aligned
// to 9:30 IST (e.g., 9:30, 9:35, 9:40 …).
func (ae *AlgoEngine) next5MinBoundary(now time.Time) time.Time {
	t := now.In(ae.ist)
	marketStart := time.Date(t.Year(), t.Month(), t.Day(), 9, 30, 0, 0, ae.ist)
	if t.Before(marketStart) {
		return marketStart
	}
	elapsed := t.Sub(marketStart)
	next := elapsed.Truncate(5*time.Minute) + 5*time.Minute
	return marketStart.Add(next)
}

func (ae *AlgoEngine) SyncCounters(dailyCount, openCount int) {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	ae.dailyTradeCount = dailyCount
	ae.openTradeCount = openCount
	log.Printf("🛡️ Risk Counters Recovered: DailyTrades=%d, OpenTrades=%d", dailyCount, openCount)
}
