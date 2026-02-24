package order

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/algo"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/utils"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

type OrderEngine struct {
	kiteClient      *kite.KiteClient
	sellOrderChan   chan algo.TradeSignal
	trackingManager *tracking.TrackingManager
	OrderSvc        *services.OrderService
	stopChan        chan struct{}
	wg              sync.WaitGroup
	running         bool
	mu              sync.Mutex
}

func NewOrderEngine(
	kiteClient *kite.KiteClient,
	trackingManager *tracking.TrackingManager,
	orderSvc *services.OrderService,
	sellOrderChan chan algo.TradeSignal,

) *OrderEngine {
	return &OrderEngine{
		kiteClient:      kiteClient,
		sellOrderChan:   sellOrderChan,
		trackingManager: trackingManager,
		OrderSvc:        orderSvc,
		stopChan:        make(chan struct{}),
	}
}

// Start begins the order engine loop
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

// Stop stops the order engine
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

// IsRunning returns whether the engine is currently running
func (oe *OrderEngine) IsRunning() bool {
	oe.mu.Lock()
	defer oe.mu.Unlock()
	return oe.running
}

// processLoop runs at the start of each minute to flush and process signals
func (oe *OrderEngine) processLoop() {
	defer oe.wg.Done()

	// Calculate time until next minute boundary
	ticker := oe.createMinuteTicker()
	defer ticker.Stop()

	for {
		select {
		case <-oe.stopChan:
			return
		case signal := <-oe.sellOrderChan:
			oe.processSellSignal(signal)
		case <-ticker.C:

			if !utils.IsMarketTime() {
				continue
			}

			oe.ProcessBuying()
		}
	}
}

// createMinuteTicker creates a ticker that fires at the start of each minute
func (oe *OrderEngine) createMinuteTicker() *time.Ticker {
	now := time.Now()
	// Calculate duration until next minute
	nextMinute := now.Truncate(time.Minute).Add(time.Minute)
	durationUntilNextMinute := nextMinute.Sub(now)

	// Wait until next minute boundary
	time.Sleep(durationUntilNextMinute)

	// Return ticker for every minute
	return time.NewTicker(time.Minute)
}

// flushAndProcessSignals flushes the queue and places all orders
func (oe *OrderEngine) ProcessBuying() {
	stocks := oe.trackingManager.GetAllBuyingStocks()
	if len(stocks) == 0 {
		return
	}

	for _, stock := range stocks {
		log.Printf("Placing buy order for %s with imbalance %d", stock.TradingSymbol, stock.SellQuantity)
		oe.processBuySignal(stock)
	}

	log.Printf("✅ Finished processing %d signals", len(stocks))
}

// processBuySignal places a buy order for a given signal
func (oe *OrderEngine) processBuySignal(stock tracking.TrackedStock) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if !oe.trackingManager.TryLockStock(stock.InstrumentToken) {
		log.Printf("⚠️ Failed to lock stock %s", stock.TradingSymbol)
		return
	}

	orderParams := kiteconnect.OrderParams{
		Exchange:        stock.Exchange,
		Tradingsymbol:   stock.TradingSymbol,
		TransactionType: kiteconnect.TransactionTypeBuy,
		Quantity:        int(stock.BuyQuantity),
		Product:         kiteconnect.ProductMIS, // Intraday
		OrderType:       kiteconnect.OrderTypeMarket,
		Validity:        kiteconnect.ValidityDay,
	}

	log.Printf("Order Params: %+v", orderParams)

	orderResponse, err := oe.kiteClient.PlaceRegularOrder(orderParams)
	if err != nil {
		log.Printf("❌ Failed to place buy order for %s: %v", stock.TradingSymbol, err)
		oe.trackingManager.UnlockStock(stock.InstrumentToken) // Unlock on failure
		return
	}

	log.Printf("✅ Order placed for %s: OrderID=%s, Type=%s",
		stock.TradingSymbol, orderResponse.OrderID, kiteconnect.TransactionTypeBuy)

	order := &models.Order{
		TrackingStockID: stock.ID,
		OrderID:         orderResponse.OrderID,
		OrderType:       kiteconnect.TransactionTypeBuy,
		EventType:       string(algo.SignalNone),
		BasePrice:       stock.BasePrice,
		Quantity:        float64(stock.BuyQuantity),
		Status:          "PENDING",
		PlacedAt:        time.Now(),
	}

	err = oe.OrderSvc.AddPlacedOrder(ctx, order)
	if err != nil {
		log.Printf("⚠️ Failed to save order to DB for %s: %v", stock.TradingSymbol, err)
	}
}

func (oe *OrderEngine) processSellSignal(signal algo.TradeSignal) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	trackedStock, exists := oe.trackingManager.GetStock(signal.InstrumentToken)
	if !exists {
		log.Printf("⚠️ No tracked stock found for instrument token %d", signal.InstrumentToken)
		return
	}

	if trackedStock.SellQuantity == 0 {
		log.Printf("⚠️ Stock %s SellQuantity is 0, skipping stale sell signal", trackedStock.TradingSymbol)
		oe.trackingManager.UnlockStock(signal.InstrumentToken) // Release AlgoEngine's lock
		return
	}

	// Use current SellQuantity, not the stale signal quantity
	sellQty := trackedStock.SellQuantity

	orderParams := kiteconnect.OrderParams{
		Exchange:        signal.Exchange,
		Tradingsymbol:   signal.TradingSymbol,
		TransactionType: kiteconnect.TransactionTypeSell,
		Quantity:        int(sellQty),
		Product:         kiteconnect.ProductMIS, // Intraday
		OrderType:       kiteconnect.OrderTypeLimit,
		Validity:        kiteconnect.ValidityDay,
	}

	log.Printf("Order Params: %+v", orderParams)

	orderResponse, err := oe.kiteClient.PlaceRegularOrder(orderParams)
	if err != nil {
		log.Printf("❌ Failed to place sell order for %s: %v", signal.TradingSymbol, err)
		oe.trackingManager.UnlockStock(signal.InstrumentToken) // Unlock on failure
		return
	}

	log.Printf("✅ Order placed for %s: OrderID=%s, Type=%s", signal.TradingSymbol, orderResponse.OrderID, "SELL")

	order := &models.Order{
		TrackingStockID: signal.TrackingStockID,
		OrderID:         orderResponse.OrderID,
		OrderType:       kiteconnect.TransactionTypeSell,
		EventType:       string(signal.SignalType),
		BasePrice:       signal.BasePrice,
		Quantity:        float64(sellQty),
		Status:          "PENDING",
		PlacedAt:        time.Now(),
	}

	err = oe.OrderSvc.AddPlacedOrder(ctx, order)
	if err != nil {
		log.Printf("⚠️ Failed to save order to DB for %s: %v", signal.TradingSymbol, err)
	}
}

// determineOrderType determines whether to BUY or SELL based on signal type
func (oe *OrderEngine) determineOrderType(signal algo.TradeSignal) string {
	switch signal.SignalType {
	case algo.SignalTargetHit:
		return kiteconnect.TransactionTypeBuy // Buy when target is hit (book profit)
	case algo.SignalStopLossHit:
		return kiteconnect.TransactionTypeBuy // Buy when stoploss is hit (cut loss)
	default:
		return kiteconnect.TransactionTypeSell
	}
}

// mapOrderStatus maps Kite order status to our internal status
func (oe *OrderEngine) mapOrderStatus(kiteStatus string) string {
	switch kiteStatus {
	case "COMPLETE":
		return "COMPLETED"
	case "CANCELLED", "REJECTED":
		return "CANCELLED"
	default:
		return "PENDING"
	}
}
