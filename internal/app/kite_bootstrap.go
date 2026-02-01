package app

import (
	"context"
	"log"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/algo"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	kcws "github.com/SM-Sclass/stock_client2-go_backend/internal/kite/ws"
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

	broadcaster := kite.NewTickBroadcaster()

	kiteWs, err := kcws.NewKiteWS(runtime.KiteClient, broadcaster, runtime.OrderSvc)
	if err != nil {
		return err
	}

	trackingManager := tracking.NewTrackingManager(kiteWs)
	signalQueue := algo.NewSignalQueue()

	// Wire TrackingManager as the BasePriceUpdater for OrderService
	// This breaks the circular dependency by using an interface
	runtime.OrderSvc.SetBasePriceUpdater(trackingManager)

	// Initialize AlgoEngine
	algoEngine := algo.NewAlgoEngine(
		trackingManager,
		broadcaster,
		signalQueue,
	)

	// Initialize OrderEngine
	orderEngine := order.NewOrderEngine(
		runtime.KiteClient,
		signalQueue,
		runtime.OrderSvc,
		runtime.TrackingStockRepo,
	)

	runtime.Broadcaster = broadcaster
	runtime.KiteWS = kiteWs
	runtime.TrackingManager = trackingManager
	runtime.SignalQueue = signalQueue
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

	// Job 3: Set stocks to AUTO_INACTIVE at 3:30 PM (market close)
	sched.AddJob(
		"MarketClose",
		15, 30,
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

// LoadTrackedStocksOnStartup loads AUTO_INACTIVE stocks to tracking manager on server startup
// This is called when Kite is authenticated and it's a trading day
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

	loadedCount := 0
	for _, stock := range stocks {
		if stock.Status != "AUTO_INACTIVE" {
			continue
		}

		// Confirm instrument token from instrument service
		instrument, exists := runtime.InstrumentSvc.NSESymbolToInstrument[stock.StockSymbol]
		if !exists {
			log.Printf("⚠️ Instrument not found for %s, skipping", stock.StockSymbol)
			continue
		}

		// Add to tracking manager
		trackedStock := tracking.TrackedStock{
			StockSymbol:     stock.StockSymbol,
			InstrumentToken: uint32(instrument.InstrumentToken),
			BasePrice:       0, // Will be set when first tick comes
			Target:          stock.Target,
			StopLoss:        stock.StopLoss,
			Quantity:        stock.Quantity,
			Exchange:        stock.Exchange,
		}
		runtime.TrackingManager.AddTrackingStock(trackedStock)
		loadedCount++
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
