package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/algo"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	kcws "github.com/SM-Sclass/stock_client2-go_backend/internal/kite/ws"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/order"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/scheduler"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/utils"
)

func StartKiteRuntime(runtime *Runtime) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	if runtime.KiteReady {
		return nil
	}

	if !runtime.KiteClient.IsTokenValid() {
		return fmt.Errorf("kite token is not valid")
	}

	broadcaster := kite.NewTickBroadcaster()

	kiteWs, err := kcws.NewKiteWS(runtime.KiteClient, broadcaster, runtime.OrderSvc)
	if err != nil {
		return err
	}

	// Start WebSocket connection
	kiteWs.Start()
	log.Println("🔌 WebSocket connection initiated")

	trackingManager := tracking.NewTrackingManager(kiteWs)

	// Wire TrackingManager as the Manager for OrderService. This breaks the circular dependency by using an interface
	runtime.OrderSvc.SetManager(trackingManager)

	sellOrderChan := make(chan algo.TradeSignal, 25)

	algoEngine := algo.NewAlgoEngine(
		trackingManager,
		broadcaster,
		sellOrderChan,
	)

	orderEngine := order.NewOrderEngine(
		runtime.KiteClient,
		trackingManager,
		runtime.OrderSvc,
		sellOrderChan,
	)

	runtime.Broadcaster = broadcaster
	runtime.KiteWS = kiteWs
	runtime.TrackingManager = trackingManager
	runtime.AlgoEngine = algoEngine
	runtime.OrderEngine = orderEngine
	runtime.KiteReady = true

	return nil
}

// SetupScheduler sets up the cron jobs for the application
func SetupScheduler(runtime *Runtime) *scheduler.Scheduler {
	sched := scheduler.NewScheduler()

	// Job 1: Fetch instruments at 8:30 AM
	sched.AddJob(
		"FetchInstruments",
		8, 30,
		scheduler.CreateInstrumentFetchJob(runtime.InstrumentSvc),
	)

	// Job 2: Start algo at 9:15 AM (market open)
	sched.AddJob(
		"MarketOpen",
		9, 15,
		scheduler.CreateMarketOpenJob(
			runtime.TrackingStockRepo,
			runtime.InstrumentSvc,
			runtime.TrackingManager,
			func() error {
				runtime.AlgoEngine.Start()
				runtime.OrderEngine.Start()
				return nil
			},
		),
	)

	// Job 3: Set stocks to AUTO_INACTIVE at 3:26 PM (market close)
	sched.AddJob(
		"MarketClose",
		15, 26,
		scheduler.CreateMarketCloseJob(
			runtime.TrackingStockRepo,
			runtime.TrackingManager,
			func() {
				runtime.AlgoEngine.Stop()
				runtime.OrderEngine.Stop()
			},
		),
	)

	runtime.Scheduler = sched
	return sched
}

// Sync OrdersOnStartup syncs orders from Kite to DB on server startup
func SyncOrdersOnStartup(runtime *Runtime) error {
	if !utils.IsTradingDay() {
		log.Println("⏸️ Not a trading day, skipping order sync")
		return nil
	}

	orders, err := runtime.KiteClient.GetOrders()
	if err != nil {
		return err
	}

	istLoc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		log.Fatalf("Failed to load timezone: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stocks, err := runtime.TrackingStockRepo.GetAllTrackingStocks(ctx)

	currentYear, currentMonth, currentDay := time.Now().In(istLoc).Date()
	for _, order := range orders {
		orderTimeIST := order.OrderTimestamp.Time.In(istLoc)
		orderYear, orderMonth, orderDay := orderTimeIST.Date()
		if orderYear != currentYear || orderMonth != currentMonth || orderDay != currentDay {
			continue
		}

		dbFormatOrder := &models.Order{
			OrderID:         order.OrderID,
			ExchangeOrderID: utils.ToNullString(order.ExchangeOrderID),
			ParentOrderID:   utils.ToNullString(order.ParentOrderID),
			TransactionType: utils.ToNullString(order.TransactionType),
			OrderType:       order.OrderType,
			EventType:       string(algo.SignalNone),
			Quantity:        order.Quantity,
			BasePrice:       order.TriggerPrice,
			PurchasePrice:   utils.ToNullFloat(order.AveragePrice),
			TriggerPrice:    utils.ToNullFloat(order.TriggerPrice),
			Exchange:        order.Exchange,
			Product:         utils.ToNullString(order.Product),
			Status:          order.Status,
			PlacedAt:        order.OrderTimestamp.Time,
		}
		for _, stock := range stocks {
			if stock.TradingSymbol == order.TradingSymbol {
				dbFormatOrder.TrackingStockID = stock.ID
				break
			}
		}
		err := runtime.OrderSvc.SyncOrder(dbFormatOrder)
		if err != nil {
			log.Printf("⚠️ Failed to sync order %s: %v", order.OrderID, err)
		}
	}

	log.Printf("✅ Synced %d orders from Kite on startup", len(orders))
	return nil

}

// LoadTrackedStocksOnStartup loads AUTO_INACTIVE stocks to tracking manager on server startup. This is called when Kite is authenticated and it's a trading day
func LoadTrackedStocksOnStartup(runtime *Runtime) error {
	if !utils.IsTradingDay() {
		log.Println("⏸️ Not a trading day, skipping stock loading")
		return nil
	}

	ctx := context.Background()

	stocks, err := runtime.TrackingStockRepo.GetAllTrackingStocks(ctx)
	if err != nil {
		return err
	}

	var trackingStocksId []int64
	for _, stock := range stocks {
		trackingStocksId = append(trackingStocksId, stock.ID)
	}

	imbalances, err := runtime.OrderSvc.AllStocksImbalance(trackingStocksId)
	if err != nil {
		log.Printf("⚠️ Failed to fetch order imbalances: %v", err)
	} else {
		log.Printf("📊 Order imbalances fetched for %d stocks", len(imbalances))
	}

	loadedCount := 0
	for _, stock := range stocks {
		log.Println("Stock: ", stock)
		if stock.Status == "INACTIVE" {
			continue
		}

		instrument, exists := runtime.InstrumentSvc.NSESymbolToInstrument[stock.TradingSymbol]
		if !exists {
			log.Printf("⚠️ Instrument not found for %s, skipping", stock.TradingSymbol)
			continue
		}

		instStr := fmt.Sprintf("%s:%s", stock.Exchange, stock.TradingSymbol)

		// Get LTP using the symbol string
		Ltp, err := runtime.KiteClient.KiteConnect.GetLTP(instStr)
		if err != nil {
			log.Printf("⚠️ Failed to get LTP for %s: %v", stock.TradingSymbol, err)
			continue
		}

		// Access the result using the same key
		quote, exists := Ltp[instStr]
		if !exists {
			log.Printf("⚠️ No data returned for %s", instStr)
			continue
		}

		trackedStock := tracking.TrackedStock{
			ID:              stock.ID,
			TradingSymbol:   stock.TradingSymbol,
			InstrumentToken: uint32(instrument.InstrumentToken),
			BasePrice:       quote.LastPrice,
			Target:          stock.Target,
			StopLoss:        stock.StopLoss,
			BuyQuantity:     stock.Quantity,
			SellQuantity:    uint32(imbalances[stock.ID]),
			Locked:          false,
			Exchange:        stock.Exchange,
		}
		runtime.TrackingManager.AddTrackingStock(trackedStock)
		loadedCount++

		if err := runtime.TrackingStockRepo.UpdateTrackingStockStatus(ctx, stock.ID, "AUTO_ACTIVE"); err != nil {
			log.Printf("⚠️ Failed to update status for %s: %v", stock.TradingSymbol, err)
		}
	}

	log.Printf("📈 Loaded %d stocks to tracking manager on startup", loadedCount)
	return nil
}

// StartEnginesIfMarketOpen starts algo and order engines if market is currently open
func StartEnginesIfMarketOpen(runtime *Runtime) {
	if !utils.IsMarketTime() {
		log.Println("⏸️ Market not open, engines will start at 9:15")
		return
	}

	log.Println("🚀 Market is open, starting engines...")
	runtime.AlgoEngine.Start()
	runtime.OrderEngine.Start()
}
