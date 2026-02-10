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
	signalQueue     *SignalQueue

	tickChan chan []kitemodels.Tick
	stopChan chan struct{}
	wg       sync.WaitGroup
	running  bool
	mu       sync.Mutex
}

func NewAlgoEngine(
	trackingManager *tracking.TrackingManager,
	broadcaster *kite.TickBroadcaster,
	signalQueue *SignalQueue,
) *AlgoEngine {
	return &AlgoEngine{
		trackingManager: trackingManager,
		broadcaster:     broadcaster,
		signalQueue:     signalQueue,
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

	// Check if this stock is being tracked
	trackedStock, exists := ae.trackingManager.GetStock(token)
	if !exists {
		return
	}

	// Skip if already processed this minute
	if ae.signalQueue.IsTokenProcessedThisMinute(token) {
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

	var signal *TradeSignal

	// Check for target hit (price >= target price)
	if currentPrice >= targetPrice {
		signal = &TradeSignal{
			InstrumentToken: token,
			TradingSymbol:     trackedStock.TradingSymbol,
			Exchange:        trackedStock.Exchange,
			SignalType:      SignalTargetHit,
			TriggerPrice:    currentPrice,
			BasePrice:       trackedStock.BasePrice,
			Target:          trackedStock.Target,
			StopLoss:        trackedStock.StopLoss,
			Quantity:        trackedStock.Quantity,
			Timestamp:       time.Now(),
		}
		ae.signalQueue.Push(*signal)
		log.Printf("🎯 Target hit for %s: Price %.2f >= Target %.2f (Base: %.2f)",
			trackedStock.TradingSymbol, currentPrice, targetPrice, trackedStock.BasePrice)

		return
	}

	// Check for stoploss hit (price <= stoploss price)
	if currentPrice <= stoplossPrice {
		signal = &TradeSignal{
			InstrumentToken: token,
			TradingSymbol:     trackedStock.TradingSymbol,
			Exchange:        trackedStock.Exchange,
			SignalType:      SignalStopLossHit,
			TriggerPrice:    currentPrice,
			BasePrice:       trackedStock.BasePrice,
			Target:          trackedStock.Target,
			StopLoss:        trackedStock.StopLoss,
			Quantity:        trackedStock.Quantity,
			Timestamp:       time.Now(),
		}
		ae.signalQueue.Push(*signal)
		log.Printf("🛑 Stoploss hit for %s: Price %.2f <= Stoploss %.2f (Base: %.2f)",
			trackedStock.TradingSymbol, currentPrice, stoplossPrice, trackedStock.BasePrice)

		return
	}

}

// GetSignalQueue returns the signal queue
func (ae *AlgoEngine) GetSignalQueue() *SignalQueue {
	return ae.signalQueue
}
