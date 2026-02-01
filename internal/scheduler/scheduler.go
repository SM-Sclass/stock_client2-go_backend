package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
)

var ist = time.FixedZone("IST", 5*60*60+30*60) // UTC+5:30

type CronJob struct {
	Name    string
	Hour    int
	Minute  int
	RunFunc func() error
	LastRun time.Time
}

type Scheduler struct {
	jobs     []*CronJob
	stopChan chan struct{}
	running  bool
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		jobs:     make([]*CronJob, 0),
		stopChan: make(chan struct{}),
	}
}


func (s *Scheduler) AddJob(name string, hour, minute int, runFunc func() error) {
	job := &CronJob{
		Name:    name,
		Hour:    hour,
		Minute:  minute,
		RunFunc: runFunc,
	}
	s.jobs = append(s.jobs, job)
}

// Start begins the scheduler loop
func (s *Scheduler) Start() {
	if s.running {
		return
	}
	s.running = true
	s.stopChan = make(chan struct{})

	go s.runLoop()
	log.Println("⏰ Scheduler started")
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	if !s.running {
		return
	}
	s.running = false
	close(s.stopChan)
	log.Println("⏰ Scheduler stopped")
}

// runLoop checks jobs every minute
func (s *Scheduler) runLoop() {
	// Check immediately on start
	s.checkAndRunJobs()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.checkAndRunJobs()
		}
	}
}

// checkAndRunJobs checks if any job should run at current time
func (s *Scheduler) checkAndRunJobs() {
	now := time.Now().In(ist)
	currentHour := now.Hour()
	currentMinute := now.Minute()
	today := now.Truncate(24 * time.Hour)

	for _, job := range s.jobs {
		// Check if job matches current time
		if job.Hour == currentHour && job.Minute == currentMinute {
			// Check if already ran today
			if job.LastRun.In(ist).Truncate(24 * time.Hour).Equal(today) {
				continue
			}

			// Run the job
			log.Printf("⏰ Running cron job: %s", job.Name)
			if err := job.RunFunc(); err != nil {
				log.Printf("❌ Cron job %s failed: %v", job.Name, err)
			} else {
				log.Printf("✅ Cron job %s completed", job.Name)
			}
			job.LastRun = now
		}
	}
}


func (s *Scheduler) RunJobNow(name string) error {
	for _, job := range s.jobs {
		if job.Name == name {
			log.Printf("⏰ Running cron job manually: %s", job.Name)
			return job.RunFunc()
		}
	}
	return nil
}


func CreateInstrumentFetchJob(instrumentSvc *services.InstrumentService) func() error {
	return func() error {
		log.Println("📊 Fetching instruments from Kite...")

		if err := instrumentSvc.FetchAndLoadInstruments(); err != nil {
			return err
		}

		if err := instrumentSvc.StoreInstrument(); err != nil {
			return err
		}

		instrumentSvc.BuildInstrumentMaps()
		log.Println("📊 Instruments fetched and stored successfully")
		return nil
	}
}


func CreateMarketOpenJob(
	trackingRepo *repository.TrackingStocksRepository,
	instrumentSvc *services.InstrumentService,
	trackingManager *tracking.TrackingManager,
	startAlgoFunc func() error,
) func() error {
	return func() error {
		log.Println("🔔 Market opening - Loading stocks and starting algo...")

		ctx := context.Background()

		// Fetch all stocks from DB (they should be AUTO_INACTIVE from previous day)
		stocks, err := trackingRepo.GetAllTrackingStocks(ctx)
		if err != nil {
			return err
		}

		// Load stocks to tracking manager with confirmed instrument tokens
		for _, stock := range stocks {
			// Confirm instrument token from instrument service
			instrument, exists := instrumentSvc.NSESymbolToInstrument[stock.StockSymbol]
			if !exists {
				log.Printf("⚠️ Instrument not found for %s, skipping", stock.StockSymbol)
				continue
			}

			
			trackedStock := tracking.TrackedStock{
				StockSymbol:     stock.StockSymbol,
				InstrumentToken: uint32(instrument.InstrumentToken),
				BasePrice:       0, // Will be set when first tick comes
				Target:          stock.Target,
				StopLoss:        stock.StopLoss,
				Quantity:        stock.Quantity,
				Exchange:        stock.Exchange,
			}
			trackingManager.AddTrackingStock(trackedStock)

			
			if err := trackingRepo.UpdateTrackingStockStatus(ctx, stock.ID, "AUTO_ACTIVE"); err != nil {
				log.Printf("⚠️ Failed to update status for %s: %v", stock.StockSymbol, err)
			}
		}

		log.Printf("📈 Loaded %d stocks to tracking manager", trackingManager.CountStocks())

		
		if startAlgoFunc != nil {
			return startAlgoFunc()
		}

		return nil
	}
}


func CreateMarketCloseJob(
	trackingRepo *repository.TrackingStocksRepository,
	trackingManager *tracking.TrackingManager,
	stopEnginesFunc func(),
) func() error {
	return func() error {
		log.Println("🔔 Market closing - Setting stocks to AUTO_INACTIVE...")

		ctx := context.Background()

		// Get all stocks from DB
		stocks, err := trackingRepo.GetAllTrackingStocks(ctx)
		if err != nil {
			return err
		}

		// Update all stocks to AUTO_INACTIVE
		for _, stock := range stocks {
			if stock.Status == "AUTO_ACTIVE" || stock.Status == "ACTIVE" {
				if err := trackingRepo.UpdateTrackingStockStatus(ctx, stock.ID, "AUTO_INACTIVE"); err != nil {
					log.Printf("⚠️ Failed to update status for %s: %v", stock.StockSymbol, err)
				}
			}
		}

		// Stop the engines
		if stopEnginesFunc != nil {
			stopEnginesFunc()
		}

		log.Printf("🌙 Market closed - %d stocks set to AUTO_INACTIVE", len(stocks))
		return nil
	}
}
