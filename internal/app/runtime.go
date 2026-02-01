package app

import (
	"sync"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/algo"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	kcws "github.com/SM-Sclass/stock_client2-go_backend/internal/kite/ws"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/order"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/scheduler"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/tracking"
)

type Runtime struct {
	mu sync.RWMutex

	KiteClient      *kite.KiteClient
	Broadcaster     *kite.TickBroadcaster
	KiteWS          *kcws.KiteWS
	TrackingManager *tracking.TrackingManager

	//services
	OrderSvc      *services.OrderService
	InstrumentSvc *services.InstrumentService

	// Engines
	AlgoEngine  *algo.AlgoEngine
	OrderEngine *order.OrderEngine
	SignalQueue *algo.SignalQueue

	// Scheduler
	Scheduler *scheduler.Scheduler

	// Repositories (needed for cron jobs)
	TrackingStockRepo *repository.TrackingStocksRepository

	KiteReady bool
}
