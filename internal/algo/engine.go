package algo

import (
	"log"
	"sync"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"

	kitemodels "github.com/zerodha/gokiteconnect/v4/models"
)

type AlgoEngine struct {
	trackingManager *tracking.TrackingManager
	broadcaster     *kite.TickBroadcaster

	tickChan      chan []kitemodels.Tick
	sellOrderChan chan TradeSignal
	stopChan      chan struct{}
	wg            sync.WaitGroup
	running       bool
	mu            sync.Mutex
}

func NewAlgoEngine(
	trackingManager *tracking.TrackingManager,
	broadcaster *kite.TickBroadcaster,
	sellOrderChan chan TradeSignal,
) *AlgoEngine {
	return &AlgoEngine{
		trackingManager: trackingManager,
		broadcaster:     broadcaster,
		sellOrderChan:   sellOrderChan,
		stopChan:        make(chan struct{}),
	}
}

// Start begins listening to tick data from broadcaster
func (ae *AlgoEngine) Start() {
	ae.mu.Lock()
	if ae.running {
		ae.mu.Unlock()
		return
	}
	ae.running = true
	ae.stopChan = make(chan struct{})
	ae.tickChan = ae.broadcaster.Subscribe(100)
	ae.mu.Unlock()

	ae.wg.Add(1)
	go ae.processLoop()

	log.Println("🚀 AlgoEngine started")
}

// Stop stops the algo engine
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

// IsRunning returns whether the engine is currently running
func (ae *AlgoEngine) IsRunning() bool {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	return ae.running
}

// processLoop continuously processes incoming ticks
func (ae *AlgoEngine) processLoop() {
	defer ae.wg.Done()

	for {
		select {
		case <-ae.stopChan:
			return
		case ticks := <-ae.tickChan:
			ae.processTicks(ticks)
		}
	}
}

// processTicks processes a batch of ticks and checks for target/stoploss hits
func (ae *AlgoEngine) processTicks(ticks []kitemodels.Tick) {
	for _, tick := range ticks {
		ae.processSingleTick(tick)
	}
}

// processSingleTick processes a single tick against tracked stocks
func (ae *AlgoEngine) processSingleTick(tick kitemodels.Tick) {
	token := tick.InstrumentToken
	log.Printf("Received tick for token %d: LastPrice %.2f", token, tick.LastPrice)

	// Check if this stock is being tracked
	trackedStock, exists := ae.trackingManager.GetStock(token)

	if !exists || trackedStock.SellQuantity == 0 || trackedStock.Locked {
		return
	}

	// Safety: skip if BasePrice hasn't been set yet (e.g. after restart)
	if trackedStock.BasePrice == 0 {
		log.Printf("⚠️ Skipping signal for %s: BasePrice not set yet", trackedStock.TradingSymbol)
		return
	}

	// Get current price (using LastPrice from tick)
	currentPrice := tick.LastPrice
	if currentPrice == 0 {
		return
	}

	// Calculate target and stoploss prices based on base price and percentages
	targetPrice := trackedStock.BasePrice + trackedStock.Target
	stoplossPrice := trackedStock.BasePrice - trackedStock.StopLoss

	// Check for target hit (price >= target price)
	if currentPrice >= targetPrice {
		// Lock BEFORE sending signal to prevent duplicate signals from subsequent ticks
		if !ae.trackingManager.TryLockStock(token) {
			log.Printf("⚠️ Target hit for %d, but already locked. Skipping.", token)
			return
		}

		signal := TradeSignal{
			TrackingStockID: trackedStock.ID,
			InstrumentToken: token,
			TradingSymbol:   trackedStock.TradingSymbol,
			Exchange:        trackedStock.Exchange,
			SignalType:      SignalTargetHit,
			TriggerPrice:    currentPrice,
			BasePrice:       trackedStock.BasePrice,
			Target:          trackedStock.Target,
			StopLoss:        trackedStock.StopLoss,
			Quantity:        trackedStock.SellQuantity,
			Timestamp:       time.Now(),
		}
		// make sell order and lock the stock until order is complete or cancelled
		ae.sellOrderChan <- signal
		log.Printf("🎯 Target hit for %s: Price %.2f >= Target %.2f (Base: %.2f)",
			trackedStock.TradingSymbol, currentPrice, targetPrice, trackedStock.BasePrice)

		return
	}

	// Check for stoploss hit (price <= stoploss price)
	if currentPrice <= stoplossPrice {
		// Lock BEFORE sending signal to prevent duplicate signals from subsequent ticks
		if !ae.trackingManager.TryLockStock(token) {
			log.Printf("⚠️ Stoploss hit for %d, but already locked. Skipping.", token)
			return
		}

		signal := TradeSignal{
			TrackingStockID: trackedStock.ID,
			InstrumentToken: token,
			TradingSymbol:   trackedStock.TradingSymbol,
			Exchange:        trackedStock.Exchange,
			SignalType:      SignalStopLossHit,
			TriggerPrice:    currentPrice,
			BasePrice:       trackedStock.BasePrice,
			Target:          trackedStock.Target,
			StopLoss:        trackedStock.StopLoss,
			Quantity:        trackedStock.SellQuantity,
			Timestamp:       time.Now(),
		}
		// make sell order and lock the stock until order is complete or cancelled
		ae.sellOrderChan <- signal
		log.Printf("🛑 Stoploss hit for %s: Price %.2f <= Stoploss %.2f (Base: %.2f)",
			trackedStock.TradingSymbol, currentPrice, stoplossPrice, trackedStock.BasePrice)

		return
	}

	log.Printf("No signal for %s: Price %.2f (Target: %.2f, Stoploss: %.2f)",
		trackedStock.TradingSymbol, currentPrice, targetPrice, stoplossPrice)

}
