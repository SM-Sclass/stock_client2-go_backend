package order

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/algo"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/utils"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

type OrderEngine struct {
	kiteClient   *kite.KiteClient
	signalQueue  *algo.SignalQueue
	OrderSvc     *services.OrderService
	trackingRepo *repository.TrackingStocksRepository
	stopChan     chan struct{}
	wg           sync.WaitGroup
	running      bool
	mu           sync.Mutex
}

func NewOrderEngine(
	kiteClient *kite.KiteClient,
	signalQueue *algo.SignalQueue,
	orderSvc *services.OrderService,
	trackingRepo *repository.TrackingStocksRepository,
) *OrderEngine {
	return &OrderEngine{
		kiteClient:   kiteClient,
		signalQueue:  signalQueue,
		OrderSvc:     orderSvc,
		trackingRepo: trackingRepo,
		stopChan:     make(chan struct{}),
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
		case <-ticker.C:
			// Check if it's trading time
			if !utils.IsMarketTime() {
				continue
			}

			// Flush and process all signals
			oe.flushAndProcessSignals()
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
func (oe *OrderEngine) flushAndProcessSignals() {
	signals := oe.signalQueue.Flush()

	if len(signals) == 0 {
		return
	}

	log.Printf("📥 Processing %d signals from queue", len(signals))

	for _, signal := range signals {
		oe.processSignal(signal)
	}

	log.Printf("✅ Finished processing %d signals", len(signals))
}

// processSignal places an order for a given signal
func (oe *OrderEngine) processSignal(signal algo.TradeSignal) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Determine order type based on signal
	orderType := oe.determineOrderType(signal)

	// Get tracking stock info from DB
	trackingStock, err := oe.trackingRepo.GetTrackingStockByTradingSymbol(ctx, signal.StockSymbol)
	if err != nil {
		log.Printf("❌ Failed to get tracking stock for %s: %v", signal.StockSymbol, err)
		return
	}

	// Place order via Kite
	orderParams := kiteconnect.OrderParams{
		Exchange:        signal.Exchange,
		Tradingsymbol:   signal.StockSymbol,
		TransactionType: orderType,
		Quantity:        int(signal.Quantity),
		Product:         kiteconnect.ProductMIS, // Intraday
		OrderType:       kiteconnect.OrderTypeMarket,
		Validity:        kiteconnect.ValidityDay,
	}

	orderResponse, err := oe.kiteClient.KiteConnect.PlaceOrder(kiteconnect.VarietyRegular, orderParams)
	if err != nil {
		log.Printf("❌ Failed to place order for %s: %v", signal.StockSymbol, err)
		return
	}

	log.Printf("✅ Order placed for %s: OrderID=%s, Type=%s",
		signal.StockSymbol, orderResponse.OrderID, orderType)

	// Save order to database
	order := &models.Order{
		TrackingStockID: trackingStock.ID,
		OrderID:         orderResponse.OrderID,
		OrderType:       orderType,
		EventType:       string(signal.SignalType),
		BasePrice:       signal.BasePrice,
		Quantity:        float64(signal.Quantity),
		Status:          "PENDING",
		PlacedAt:        time.Now(),
	}

	err = oe.OrderSvc.AddPlacedOrder(ctx, order)
	if err != nil {
		log.Printf("⚠️ Failed to save order to DB for %s: %v", signal.StockSymbol, err)
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

// ProcessOrderUpdate handles order updates from Kite websocket
// func (oe *OrderEngine) ProcessOrderUpdate(orderID string, status string) error {
// 	ctx := context.Background()

// 	// Get order from DB
// 	order, err := oe.orderRepo.GetOrderByOrderID(ctx, orderID)
// 	if err != nil {
// 		return fmt.Errorf("failed to get order %s: %v", orderID, err)
// 	}

// 	// Map Kite status to our status
// 	mappedStatus := oe.mapOrderStatus(status)

// 	// Update order status
// 	err = oe.orderRepo.UpdateOrderStatus(ctx, order.ID, mappedStatus)
// 	if err != nil {
// 		return fmt.Errorf("failed to update order status %s: %v", orderID, err)
// 	}

// 	log.Printf("📝 Order %s status updated to %s", orderID, mappedStatus)
// 	return nil
// }

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
