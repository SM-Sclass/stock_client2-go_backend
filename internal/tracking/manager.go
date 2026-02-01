package tracking

import (
	"sync"

	kcws "github.com/SM-Sclass/stock_client2-go_backend/internal/kite/ws"
)

type TrackedStock struct {
	StockSymbol     string
	InstrumentToken uint32
	BasePrice       float64
	Target          float64
	StopLoss        float64
	Quantity        uint32
	Exchange        string
}

type TrackingManager struct {
	tracked map[uint32]TrackedStock
	mu      sync.RWMutex
	ws      *kcws.KiteWS
}

type TokenSubscriber interface {
	SubscribeToken(token uint32)
	UnsubscribeToken(token uint32)
}

func NewTrackingManager(ws *kcws.KiteWS) *TrackingManager {
	return &TrackingManager{
		tracked: make(map[uint32]TrackedStock),
		ws:      ws,
	}
}

func (tm *TrackingManager) AddTrackingStock(stock TrackedStock) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.tracked[stock.InstrumentToken] = stock
	tm.ws.SubscribeToken(stock.InstrumentToken)
}

func (tm *TrackingManager) UpdateStockParameters(stock TrackedStock) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if existing, exists := tm.tracked[stock.InstrumentToken]; exists {
		existing.Target = stock.Target
		existing.StopLoss = stock.StopLoss
		existing.Quantity = stock.Quantity
		tm.tracked[stock.InstrumentToken] = existing
	}
}

func (tm *TrackingManager) RemoveStockFromTracking(token uint32) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.tracked, token)
	tm.ws.UnsubscribeToken(token)
}

func (tm *TrackingManager) GetStock(token uint32) (TrackedStock, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	stock, exists := tm.tracked[token]
	return stock, exists
}

func (tm *TrackingManager) GetAllStock() []TrackedStock {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	stocks := make([]TrackedStock, 0, len(tm.tracked))
	for _, stock := range tm.tracked {
		stocks = append(stocks, stock)
	}
	return stocks
}

func (tm *TrackingManager) CountStocks() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.tracked)
}

func (tm *TrackingManager) IsStockTracked(token uint32) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	_, exists := tm.tracked[token]
	return exists
}

// UpdateBasePrice updates the base price of a tracked stock when an order completes.
// This implements the services.BasePriceUpdater interface.
func (tm *TrackingManager) UpdateBasePrice(instrumentToken uint32, price float64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if stock, exists := tm.tracked[instrumentToken]; exists {
		stock.BasePrice = price
		tm.tracked[instrumentToken] = stock
	}
}
