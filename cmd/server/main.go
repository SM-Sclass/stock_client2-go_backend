package main

import (
	"log"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/app"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/config"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/database"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/handlers"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/routes"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/gin-gonic/gin"
)

func main() {
	config.MustLoad()

	db := database.ConnectPostgresDB()
	kiteClient := kite.NewKiteClient()

	// Initialize repositories
	userRepo := &repository.UserRepository{DB: db}
	trackingStockRepo := &repository.TrackingStocksRepository{DB: db}
	orderRepo := &repository.OrderRepository{DB: db}
	instrumentRepo := &repository.InstrumentRepository{DB: db}

	instrumentSvc := &services.InstrumentService{
		Kite: kiteClient,
		Repo: instrumentRepo,
	}
	orderSvc := &services.OrderService{
		OrderRepo:         orderRepo,
		TrackingStockRepo: trackingStockRepo,
	}

	//Initialize Kite Client
	runtime := &app.Runtime{
		KiteClient:        kiteClient,
		TrackingStockRepo: trackingStockRepo,
		InstrumentSvc:     instrumentSvc,
		OrderSvc:          orderSvc,
	}

	// Try to authenticate and start Kite runtime
	if err := kiteClient.EnsureAuthenticated(); err != nil {
		log.Printf("⚠️ Kite auth failed (non-fatal): %v", err)
	} else {
		// Kite is authenticated
		if err := app.StartKiteRuntime(runtime); err != nil {
			log.Printf("⚠️ Kite runtime failed (non-fatal): %v", err)
		} else {
			// Load tracked stocks on startup (AUTO_INACTIVE stocks)
			if err := app.LoadTrackedStocksOnStartup(runtime); err != nil {
				log.Printf("⚠️ Failed to load tracked stocks: %v", err)
			}

			// Start engines if market is currently open
			app.StartEnginesIfMarketOpen(runtime)
		}
	}

	// Load instruments from DB or fetch fresh
	stale, _ := instrumentSvc.IsInstrumentsDataStale()
	if stale {
		instrumentSvc.FetchAndLoadInstruments()
		log.Println("📊 Instruments data is stale, will be refreshed on next cron job or auth")
	} else {
		_ = instrumentSvc.LoadInstrumentFromDB()
		instrumentSvc.BuildInstrumentMaps()
	}

	// Setup and start scheduler (cron jobs)
	scheduler := app.SetupScheduler(runtime)
	scheduler.Start()
	defer scheduler.Stop()

	// Initialize handlers
	trackingStockHandler := &handlers.TrackingStockHandler{TrackingStockRepo: trackingStockRepo, Runtime: runtime}
	authHandler := &handlers.AuthHandler{UserRepo: userRepo}
	orderHandler := &handlers.OrderHandler{OrderRepo: orderRepo}
	kiteCallbackHandler := &handlers.KiteCallbackHandler{Kc: kiteClient, Runtime: runtime}
	stockQueryHandler := &handlers.StockQueryHandler{InstrumentService: instrumentSvc}

	router := gin.Default()
	routes.RegisterRoutes(router, 
		authHandler, 
		trackingStockHandler, 
		kiteCallbackHandler, 
		orderHandler, 
		stockQueryHandler)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	port := config.ServerConfig.Port
	log.Printf("🌍 Server starting on :%s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatal("Failed to run server: ", err)
	}
}
