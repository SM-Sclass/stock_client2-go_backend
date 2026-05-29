package order

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/algo"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

const (
	entryLimitOffsetPct = 0.0003 // 0.03%
	tickSize            = 0.10
	entryLimitTimeout   = 10 * time.Second
)

type OrderEngine struct {
	kiteClient      *kite.KiteClient
	signalChan      chan algo.TradeSignal
	trackingManager *tracking.TrackingManager
	OrderSvc        *services.OrderService
	algoEngine      *algo.AlgoEngine
	stopChan        chan struct{}
	wg              sync.WaitGroup
	running         bool
	mu              sync.Mutex
}

func NewOrderEngine(
	kiteClient *kite.KiteClient,
	trackingManager *tracking.TrackingManager,
	orderSvc *services.OrderService,
	signalChan chan algo.TradeSignal,
	algoEngine *algo.AlgoEngine,
) *OrderEngine {
	return &OrderEngine{
		kiteClient:      kiteClient,
		signalChan:      signalChan,
		trackingManager: trackingManager,
		OrderSvc:        orderSvc,
		algoEngine:      algoEngine,
		stopChan:        make(chan struct{}),
	}
}

// Start begins the order engine loop.
func (oe *OrderEngine) Start() {
	oe.mu.Lock()
	if oe.running {
		oe.mu.Unlock()
		return
	}
	oe.running = true
	oe.stopChan = make(chan struct{})
	oe.mu.Unlock()

	oe.wg.Add(1)
	go oe.processLoop()

	log.Println("🚀 OrderEngine started")
}

// Stop stops the order engine.
func (oe *OrderEngine) Stop() {
	oe.mu.Lock()
	if !oe.running {
		oe.mu.Unlock()
		return
	}
	oe.running = false
	close(oe.stopChan)
	oe.mu.Unlock()

	oe.wg.Wait()
	log.Println("🛑 OrderEngine stopped")
}

func (oe *OrderEngine) IsRunning() bool {
	oe.mu.Lock()
	defer oe.mu.Unlock()
	return oe.running
}

// processLoop drains the signal channel and routes each signal to the correct handler.
func (oe *OrderEngine) processLoop() {
	defer oe.wg.Done()

	for {
		select {
		case <-oe.stopChan:
			return
		case signal := <-oe.signalChan:
			oe.processSignal(signal)
		}
	}
}

// processSignal dispatches a trade signal to the appropriate order handler.
func (oe *OrderEngine) processSignal(signal algo.TradeSignal) {
	switch signal.SignalType {
	case algo.SignalEntryBuy:
		oe.processEntry(signal, kiteconnect.TransactionTypeBuy)
	case algo.SignalEntrySell:
		oe.processEntry(signal, kiteconnect.TransactionTypeSell)
	case algo.SignalTargetHit:
		oe.processExit(signal, kiteconnect.OrderTypeLimit)
	case algo.SignalStopLossHit, algo.SignalForceExit:
		oe.processExit(signal, kiteconnect.OrderTypeMarket)
	default:
		log.Printf("⚠️ Unknown signal type: %s for %s", signal.SignalType, signal.TradingSymbol)
	}
}

// processEntry places a market order to open a new long or short position.
func (oe *OrderEngine) processEntry(signal algo.TradeSignal, txType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// if signal.BasePrice <= 0 {
	// 	log.Printf("❌ Invalid LTP %.2f for entry %s", signal.BasePrice, signal.TradingSymbol)
	// 	oe.trackingManager.UnlockStock(signal.InstrumentToken)
	// 	return
	// }

	limitPrice := signal.BasePrice
	if txType == kiteconnect.TransactionTypeBuy {
		limitPrice = signal.BasePrice * (1 + entryLimitOffsetPct)
	} else {
		limitPrice = signal.BasePrice * (1 - entryLimitOffsetPct)
	}
	limitPrice = roundToTick(limitPrice, tickSize)

	orderParams := kiteconnect.OrderParams{
		Exchange:        signal.Exchange,
		Tradingsymbol:   signal.TradingSymbol,
		TransactionType: txType,
		Quantity:        int(signal.Quantity),
		Product:         kiteconnect.ProductMIS,
		OrderType:       kiteconnect.OrderTypeLimit,
		Price:           limitPrice,
		Validity:        kiteconnect.ValidityDay,
	}

	log.Printf("📤 Placing entry %s LIMIT order for %s qty=%d @ %.2f",
		txType, signal.TradingSymbol, signal.Quantity, limitPrice)

	orderResponse, err := oe.kiteClient.PlaceRegularOrder(orderParams)
	if err != nil {
		log.Printf("❌ Failed to place entry order for %s: %v", signal.TradingSymbol, err)
		// Allow a new trade since this one failed
		oe.algoEngine.DecrementDailyTrade()
		oe.algoEngine.DecrementOpenTrade()
		oe.trackingManager.ResetFiringAndDirection(signal.InstrumentToken)
		oe.trackingManager.UnlockStock(signal.InstrumentToken)
		return
	}

	// Persist target/stoploss/direction immediately so the exit logic has them
	// before the fill confirmation arrives via WebSocket.
	oe.trackingManager.UpdateTradeParams(signal.InstrumentToken, signal.Target, signal.StopLoss, signal.Direction)

	order := &models.Order{
		TrackingStockID: signal.TrackingStockID,
		OrderID:         orderResponse.OrderID,
		OrderType:       kiteconnect.OrderTypeLimit,
		EventType:       string(signal.SignalType),
		BasePrice:       signal.BasePrice,
		Quantity:        float64(signal.Quantity),
		Status:          "PENDING",
		PlacedAt:        time.Now(),
	}

	if err := oe.OrderSvc.AddPlacedOrder(ctx, order); err != nil {
		log.Printf("⚠️ Failed to save entry order for %s: %v", signal.TradingSymbol, err)
	}

	go oe.ensureEntryCompletionAfter(signal, txType, orderResponse.OrderID, int(signal.Quantity), entryLimitTimeout)
}

func (oe *OrderEngine) RecoverPendingEntryOrder(order models.Order) {
	stock, exists := oe.trackingManager.GetTrackedStockByID(order.TrackingStockID)
	if !exists {
		log.Printf("⚠️ Recovery: tracked stock not found for order %s", order.OrderID)
		return
	}

	txType := kiteconnect.TransactionTypeBuy
	direction := "BUY"
	switch order.EventType {
	case string(algo.SignalEntryBuy):
		txType = kiteconnect.TransactionTypeBuy
		direction = "BUY"
	case string(algo.SignalEntrySell):
		txType = kiteconnect.TransactionTypeSell
		direction = "SELL"
	default:
		return
	}

	requestedQty := int(math.Round(order.Quantity))
	if requestedQty <= 0 {
		return
	}

	delay := time.Until(order.PlacedAt.Add(entryLimitTimeout))
	if delay < 0 {
		delay = 0
	}

	signal := algo.TradeSignal{
		TrackingStockID: order.TrackingStockID,
		InstrumentToken: stock.InstrumentToken,
		TradingSymbol:   stock.TradingSymbol,
		Exchange:        stock.Exchange,
		SignalType:      algo.SignalType(order.EventType),
		Direction:       direction,
		BasePrice:       order.BasePrice,
		Quantity:        uint32(requestedQty),
		Timestamp:       order.PlacedAt,
	}

	log.Printf("♻️ Recovery: re-arming entry timeout for %s order=%s in %s",
		signal.TradingSymbol, order.OrderID, delay.Round(time.Second))

	go oe.ensureEntryCompletionAfter(signal, txType, order.OrderID, requestedQty, delay)
}

func (oe *OrderEngine) ensureEntryCompletionAfter(signal algo.TradeSignal, txType, entryOrderID string, requestedQty int, wait time.Duration) {
	if wait < 0 {
		wait = 0
	}

	select {
	case <-oe.stopChan:
		return
	case <-time.After(wait):
	}

	history, err := oe.kiteClient.GetOrderHistory(entryOrderID)
	if err != nil {
		log.Printf("⚠️ Cannot verify entry order %s status: %v", entryOrderID, err)
		return
	}
	if len(history) == 0 {
		log.Printf("⚠️ Empty order history for %s", entryOrderID)
		return
	}

	latest := history[len(history)-1]
	if latest.Status == "COMPLETE" || latest.Status == "CANCELLED" || latest.Status == "REJECTED" {
		return
	}

	filledQty := int(latest.FilledQuantity)
	remainingQty := requestedQty - filledQty
	if remainingQty <= 0 {
		return
	}

	if _, err := oe.kiteClient.CancelRegularOrder(entryOrderID); err != nil {
		log.Printf("⚠️ Failed to cancel stale entry order %s: %v", entryOrderID, err)
		return
	}

	marketParams := kiteconnect.OrderParams{
		Exchange:         signal.Exchange,
		Tradingsymbol:    signal.TradingSymbol,
		TransactionType:  txType,
		Quantity:         remainingQty,
		Product:          kiteconnect.ProductMIS,
		OrderType:        kiteconnect.OrderTypeMarket,
		Validity:         kiteconnect.ValidityDay,
		MarketProtection: 1,
	}

	log.Printf("⏱️ Entry %s not complete in 10s for %s. Placing MARKET for remaining qty=%d",
		entryOrderID, signal.TradingSymbol, remainingQty)

	marketResp, err := oe.kiteClient.PlaceRegularOrder(marketParams)
	if err != nil {
		log.Printf("❌ Failed fallback market entry for %s: %v", signal.TradingSymbol, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	marketOrder := &models.Order{
		TrackingStockID: signal.TrackingStockID,
		OrderID:         marketResp.OrderID,
		OrderType:       kiteconnect.OrderTypeMarket,
		EventType:       string(signal.SignalType),
		BasePrice:       signal.BasePrice,
		Quantity:        float64(remainingQty),
		Status:          "PENDING",
		PlacedAt:        time.Now(),
	}

	if err := oe.OrderSvc.AddPlacedOrder(ctx, marketOrder); err != nil {
		log.Printf("⚠️ Failed to save fallback market entry for %s: %v", signal.TradingSymbol, err)
	}
}

// processExit closes an open position with either a LIMIT (target) or MARKET (stoploss/force) order.
func (oe *OrderEngine) processExit(signal algo.TradeSignal, orderType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	trackedStock, exists := oe.trackingManager.GetStock(signal.InstrumentToken)
	if !exists {
		log.Printf("⚠️ No tracked stock for token %d", signal.InstrumentToken)
		return
	}

	openQty := trackedStock.BuyQuantity
	if signal.Direction == "SELL" {
		openQty = trackedStock.SellQuantity
	}

	if openQty == 0 {
		log.Printf("⚠️ %s SellQuantity is 0, skipping stale exit signal", trackedStock.TradingSymbol)
		oe.trackingManager.UnlockStock(signal.InstrumentToken)
		return
	}

	// The closing side is the opposite of the open direction.
	closeTxType := kiteconnect.TransactionTypeSell
	if signal.Direction == "SELL" {
		closeTxType = kiteconnect.TransactionTypeBuy
	}

	orderParams := kiteconnect.OrderParams{
		Exchange:         signal.Exchange,
		Tradingsymbol:    signal.TradingSymbol,
		TransactionType:  closeTxType,
		Quantity:         int(openQty),
		Product:          kiteconnect.ProductMIS,
		OrderType:        orderType,
		Price:            signal.BasePrice, // For LIMIT orders, this is adjusted in processExit based on target/stoploss
		MarketProtection: 1,                // Avoid orders getting executed at crazy prices due to stale signals or sudden price spikes
		Validity:         kiteconnect.ValidityDay,
	}

	// For LIMIT orders (target hit), set the limit price.
	if orderType == kiteconnect.OrderTypeLimit {
		if signal.Direction == "SELL" {
			orderParams.Price = signal.BasePrice - signal.Target
		} else {
			orderParams.Price = signal.BasePrice + signal.Target
		}
	}

	log.Printf("📤 Placing exit %s %s order for %s qty=%d type=%s",
		closeTxType, signal.SignalType, signal.TradingSymbol, openQty, orderType)

	orderResponse, err := oe.kiteClient.PlaceRegularOrder(orderParams)
	if err != nil {
		log.Printf("❌ Failed to place exit order for %s: %v", signal.TradingSymbol, err)
		oe.trackingManager.UnlockStock(signal.InstrumentToken)
		return
	}

	order := &models.Order{
		TrackingStockID: signal.TrackingStockID,
		OrderID:         orderResponse.OrderID,
		OrderType:       orderType,
		EventType:       string(signal.SignalType),
		BasePrice:       signal.BasePrice,
		Quantity:        float64(openQty),
		Status:          "PENDING",
		PlacedAt:        time.Now(),
	}

	if err := oe.OrderSvc.AddPlacedOrder(ctx, order); err != nil {
		log.Printf("⚠️ Failed to save exit order for %s: %v", signal.TradingSymbol, err)
	}

	// Notify the algo engine that the position is being closed so it can
	// accept a new trade if the daily limit allows.
	oe.algoEngine.DecrementOpenTrade()
}

func roundToTick(price float64, tick float64) float64 {
	return math.Round(price/tick) * tick
}
