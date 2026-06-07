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
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/order"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/scheduler"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/utils"
)

func StartKiteRuntime(runtime *Runtime) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	if !utils.IsTradingDay() {
		log.Println("⚠️ Not a trading day - skipping Kite runtime initialization")
		return nil
	}

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

	trackingManager := tracking.NewTrackingManager(kiteWs, runtime.KiteClient)

	// Wire TrackingManager as the Manager for OrderService. This breaks the circular dependency by using an interface
	runtime.OrderSvc.SetManager(trackingManager)

	signalChan := make(chan algo.TradeSignal, 25)

	algoEngine := algo.NewAlgoEngine(
		trackingManager,
		broadcaster,
		runtime.KiteClient,
		signalChan,
	)

	orderEngine := order.NewOrderEngine(
		runtime.KiteClient,
		trackingManager,
		runtime.OrderSvc,
		signalChan,
		algoEngine,
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

	// Job 2: Start algo at 9:30 AM (market open)
	sched.AddJob(
		"MarketOpen",
		9, 30,
		scheduler.CreateMarketOpenJob(
			runtime.TrackingStockRepo,
			runtime.InstrumentSvc,
			runtime.TrackingManager,
			func() bool {
				return runtime.KiteClient.IsTokenValid()
			},
			func() error {
				runtime.AlgoEngine.Start()
				runtime.OrderEngine.Start()
				return nil
			},
		),
	)

	// Job 3: Set stocks to AUTO_INACTIVE at 3:12 PM (market close)
	sched.AddJob(
		"MarketClose",
		15, 12,
		scheduler.CreateMarketCloseJob(
			runtime.TrackingStockRepo,
			runtime.TrackingManager,
			func() {
				runtime.KiteWS.Stop()
			},
			func() bool {
				return runtime.KiteClient.IsTokenValid()
			},
			func() {
				runtime.AlgoEngine.Stop()
				runtime.OrderEngine.Stop()
			},
		),
	)

	runtime.Scheduler = sched
	return sched
}

// SyncOrdersOnStartup syncs orders from Kite to DB on server startup
// buildTrackedStock constructs a TrackedStock from stats, LTP quote, instrument token, and stock config.
// This is shared between LoadTrackedStocksOnStartup and RecoverStockState.
func buildTrackedStock(stock *models.TrackingStock, stats repository.TradeStats, lastPrice float64, instrumentToken uint32) tracking.TrackedStock {
	direction := ""
	buyQty := uint32(0)
	sellQty := uint32(0)
	signalFired := stats.EntryCount > 0
	maxExecutableOrders := 1

	if stats.TotalBuy > stats.TotalSell {
		direction = "BUY"
		maxExecutableOrders = maxExecutableOrders - stats.TotalSell
		buyQty = uint32(stats.TotalBuy - stats.TotalSell)
	} else if stats.TotalSell > stats.TotalBuy {
		direction = "SELL"
		maxExecutableOrders = maxExecutableOrders - stats.TotalBuy
		sellQty = uint32(stats.TotalSell - stats.TotalBuy)
	}

	basePrice := 0.0
	if stats.LastPrice != nil && direction != "" {
		basePrice = *stats.LastPrice
	}
	if basePrice == 0 {
		basePrice = lastPrice
	}

	return tracking.TrackedStock{
		ID:                  stock.ID,
		TradingSymbol:       stock.TradingSymbol,
		InstrumentToken:     instrumentToken,
		BasePrice:           basePrice,
		Target:              stock.Target,
		StopLoss:            stock.StopLoss,
		OrderPriceLimit:     stock.OrderPriceLimit,
		BuyQuantity:         buyQty,
		SellQuantity:        sellQty,
		Direction:           direction,
		SignalFired:         signalFired,
		MaxExecutableOrders: uint32(maxExecutableOrders),
		Locked:              false,
		Exchange:            stock.Exchange,
	}
}

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
	if err != nil {
		return err
	}

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
	allStocksSymbols := []string{}
	for _, stock := range stocks {
		trackingStocksId = append(trackingStocksId, stock.ID)
		allStocksSymbols = append(allStocksSymbols, fmt.Sprintf("%s:%s", stock.Exchange, stock.TradingSymbol))
	}
	loadedCount := 0

	allStocksLTP, err := runtime.KiteClient.KiteConnect.GetLTP(allStocksSymbols...)
	if err != nil {
		return err
	}
	statsMap, err := runtime.OrderSvc.AllStocksTradeStats(trackingStocksId)
	if err != nil {
		log.Printf("⚠️ Recovery error: %v", err)
	}

	totalDailyTrades := 0
	totalOpenTrades := 0

	for _, stock := range stocks {
		if stock.Status == "INACTIVE" {
			continue
		}

		stats := statsMap[stock.ID]

		instrument, exists := runtime.InstrumentSvc.NSESymbolToInstrument[stock.TradingSymbol]
		if !exists {
			log.Printf("⚠️ Instrument not found for %s, skipping", stock.TradingSymbol)
			continue
		}

		instStr := fmt.Sprintf("%s:%s", stock.Exchange, stock.TradingSymbol)
		quote, exists := allStocksLTP[instStr]
		if !exists {
			log.Printf("⚠️ No data returned for %s", instStr)
			continue
		}

		trackedStock := buildTrackedStock(&stock, stats, quote.LastPrice, uint32(instrument.InstrumentToken))

		// Accumulate engine counters before attempting to add
		totalDailyTrades += stats.EntryCount
		if trackedStock.Direction != "" {
			totalOpenTrades++
		}

		if !runtime.TrackingManager.AddTrackingStock(trackedStock) {
			continue
		}
		loadedCount++
		log.Printf("✅ Loaded %s to tracking manager with base price %.2f, direction=%s, buyQty=%d, sellQty=%d",
			stock.TradingSymbol, trackedStock.BasePrice, trackedStock.Direction, trackedStock.BuyQuantity, trackedStock.SellQuantity)

		if err := runtime.TrackingStockRepo.UpdateTrackingStockStatus(ctx, stock.ID, "AUTO_ACTIVE"); err != nil {
			log.Printf("⚠️ Failed to update status for %s: %v", stock.TradingSymbol, err)
		}
	}

	runtime.AlgoEngine.SyncCounters(totalDailyTrades, totalOpenTrades)
	log.Printf("📈 Loaded %d stocks to tracking manager on startup", loadedCount)
	return nil
}

// RecoverStockState recovers the state of a single stock and adds it to the tracking manager.
func RecoverStockState(runtime *Runtime, stock *models.TrackingStock) error {
	ctx := context.Background()

	// 1. Sync today's orders for this stock from Kite
	if err := SyncOrdersOnStartup(runtime); err != nil {
		log.Printf("⚠️ Failed to sync orders during recovery for %s: %v", stock.TradingSymbol, err)
	}

	// 2. Get LTP
	instStr := fmt.Sprintf("%s:%s", stock.Exchange, stock.TradingSymbol)
	allStocksLTP, err := runtime.KiteClient.KiteConnect.GetLTP(instStr)
	if err != nil {
		return fmt.Errorf("failed to get LTP for %s: %w", stock.TradingSymbol, err)
	}
	quote, exists := allStocksLTP[instStr]
	if !exists {
		return fmt.Errorf("no LTP data returned for %s", stock.TradingSymbol)
	}

	// 3. Get Trade Stats
	statsMap, err := runtime.OrderSvc.AllStocksTradeStats([]int64{stock.ID})
	if err != nil {
		return fmt.Errorf("failed to get trade stats for %s: %w", stock.TradingSymbol, err)
	}
	stats := statsMap[stock.ID]

	// 4. Get Instrument
	instrument, exists := runtime.InstrumentSvc.NSESymbolToInstrument[stock.TradingSymbol]
	if !exists {
		return fmt.Errorf("instrument not found for %s", stock.TradingSymbol)
	}

	// 5. Build and register TrackedStock
	trackedStock := buildTrackedStock(stock, stats, quote.LastPrice, uint32(instrument.InstrumentToken))

	if !runtime.TrackingManager.AddTrackingStock(trackedStock) {
		return fmt.Errorf("failed to add %s to tracking manager", stock.TradingSymbol)
	}

	log.Printf("✅ Recovered and loaded %s to tracking manager with base price %.2f, direction=%s, buyQty=%d, sellQty=%d",
		stock.TradingSymbol, trackedStock.BasePrice, trackedStock.Direction, trackedStock.BuyQuantity, trackedStock.SellQuantity)

	if err := runtime.TrackingStockRepo.UpdateTrackingStockStatus(ctx, stock.ID, "ACTIVE"); err != nil {
		log.Printf("⚠️ Failed to update status for %s: %v", stock.TradingSymbol, err)
	}

	// 6. Recover any pending entry orders for this stock
	if err := RecoverPendingEntryOrdersOnStartup(runtime); err != nil {
		log.Printf("⚠️ Failed to recover pending entry orders during recovery for %s: %v", stock.TradingSymbol, err)
	}

	return nil
}

func RecoverPendingEntryOrdersOnStartup(runtime *Runtime) error {
	orders, err := runtime.OrderSvc.GetRecoverableEntryOrders()
	if err != nil {
		return err
	}

	for _, order := range orders {
		runtime.OrderEngine.RecoverPendingEntryOrder(order)
	}

	if len(orders) > 0 {
		log.Printf("♻️ Recovered %d pending entry order timers", len(orders))
	}

	return nil
}

// StartEnginesIfMarketOpen starts algo and order engines if market is currently open
func StartEnginesIfMarketOpen(runtime *Runtime) {
	if !utils.IsMarketTime() || !utils.IsTradingDay() {
		log.Println("⏸️ Market not open, engines will start at 9:15")
		return
	}

	log.Println("🚀 Market is open, starting engines...")
	runtime.AlgoEngine.Start()
	runtime.OrderEngine.Start()
}
