package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/utils"
	"github.com/jackc/pgx/v5"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// BasePriceUpdater is implemented by TrackingManager to update base price. when an order completes. Using an interface avoids circular dependency.
type Manager interface {
	UpdateBasePrice(instrumentToken uint32, price float64)
	UnlockStock(instrumentToken uint32)
	SetSellQuantity(instrumentToken uint32, quantity uint32)
	SetBuyQuantity(instrumentToken uint32, quantity uint32)
	SetDirection(instrumentToken uint32, direction string)
	GetStockIDByTradingSymbol(tradingSymbol string) (int64, bool)
	GetBuyAndSellQuantityByToken(token uint32) (uint32, uint32, bool)
	GetBasePriceByToken(token uint32) (float64, bool)
	DecrementMaxExecutableOrders(instrumentToken uint32)
}

type OrderRepo interface {
	AddOrder(ctx context.Context, o *models.Order) (ID int64, err error)
	UpdateOrder(ctx context.Context, o *models.Order, ID int64) error
	GetOrderByKiteOrderID(ctx context.Context, orderID string) (*models.Order, error)
	GetAllStocksOrderImbalance(ctx context.Context, trackingStockIds []int64) (imbalances []repository.OrderImabalance, err error)
	GetDailyTradeStats(ctx context.Context, trackingStockIds []int64) (stats []repository.TradeStats, err error)
	GetRecoverableEntryOrders(ctx context.Context) (orders []models.Order, err error)
	UpsertOrder(ctx context.Context, o *models.Order) (int64, error)
}

type TrackingStockRepo interface {
	GetTrackingStockByID(ctx context.Context, id int64) (*models.TrackingStock, error)
}

type OrderService struct {
	OrderRepo         OrderRepo
	TrackingStockRepo TrackingStockRepo
	Manager           Manager

	now            func() time.Time
	pendingUpdates map[string]pendingOrderUpdate
	mu             sync.Mutex
}

const pendingUpdateTTL = 2 * time.Minute

type pendingOrderUpdate struct {
	update     kiteconnect.Order
	receivedAt time.Time
}

// SetManager sets the Manager after TrackingManager is initialized. This is needed to avoid circular dependency during initialization.
func (s *OrderService) SetManager(manager Manager) {
	s.Manager = manager
}

func (s *OrderService) nowTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *OrderService) cachePendingUpdate(orderUpdate kiteconnect.Order) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingUpdates == nil {
		s.pendingUpdates = make(map[string]pendingOrderUpdate)
	}
	now := s.nowTime()
	s.prunePendingUpdatesLocked(now)
	s.pendingUpdates[orderUpdate.OrderID] = pendingOrderUpdate{
		update:     orderUpdate,
		receivedAt: now,
	}
}

func (s *OrderService) popPendingUpdate(orderID string) (kiteconnect.Order, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingUpdates == nil {
		return kiteconnect.Order{}, false
	}
	s.prunePendingUpdatesLocked(s.nowTime())
	pending, ok := s.pendingUpdates[orderID]
	if ok {
		delete(s.pendingUpdates, orderID)
	}
	return pending.update, ok
}

func (s *OrderService) prunePendingUpdatesLocked(now time.Time) {
	for orderID, pending := range s.pendingUpdates {
		if now.Sub(pending.receivedAt) > pendingUpdateTTL {
			delete(s.pendingUpdates, orderID)
		}
	}
}

func (s *OrderService) AddPlacedOrder(ctx context.Context, order *models.Order) error {
	if _, err := s.OrderRepo.AddOrder(ctx, order); err != nil {
		return err
	}

	if update, ok := s.popPendingUpdate(order.OrderID); ok {
		return s.ProcessOrderUpdate(ctx, update)
	}

	return nil
}

func (s *OrderService) ProcessOrderUpdate(ctx context.Context, orderUpdate kiteconnect.Order) error {
	dbOrder, err := s.OrderRepo.GetOrderByKiteOrderID(ctx, orderUpdate.OrderID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.cachePendingUpdate(orderUpdate)
			return fmt.Errorf("failed to get order %s: %v", orderUpdate.OrderID, err)
		}
		log.Printf("Failed to get order but continued %s: %v", orderUpdate.OrderID, err)
	}

	stockID, exists := s.Manager.GetStockIDByTradingSymbol(orderUpdate.TradingSymbol)
	buyQuantity, sellQuantity, exists := s.Manager.GetBuyAndSellQuantityByToken(uint32(orderUpdate.InstrumentToken))
	basePrice, exists := s.Manager.GetBasePriceByToken(uint32(orderUpdate.InstrumentToken))
	if !exists {
		log.Printf("⚠️ No tracking stock found for trading symbol %s in order update %s", orderUpdate.TradingSymbol, orderUpdate.OrderID)
		return nil
	}

	// isBuyTransaction := orderUpdate.TransactionType == kiteconnect.TransactionTypeBuy
	// Determine if this is an entry order from our saved event type.
	isEntryOrder := false
	if dbOrder != nil {
		isEntryOrder = dbOrder.EventType == "ENTRY_BUY" || dbOrder.EventType == "ENTRY_SELL"
	} else {
		// If DB record doesn't exist yet, we infer intent from our Manager's current state
		if orderUpdate.TransactionType == kiteconnect.TransactionTypeBuy {
			// If we are buying and have no current sell obligations, it's likely an entry
			isEntryOrder = sellQuantity == 0
		} else if orderUpdate.TransactionType == kiteconnect.TransactionTypeSell {
			// If we are selling and have no current buy holdings, it's likely a short entry
			isEntryOrder = buyQuantity == 0
		}
	}
	isBuyTransaction := orderUpdate.TransactionType == kiteconnect.TransactionTypeBuy
	token := uint32(orderUpdate.InstrumentToken)

	updatedOrder := &models.Order{
		OrderID:         orderUpdate.OrderID,
		TrackingStockID: stockID,
		OrderType:       orderUpdate.OrderType,

		EventType:       s.buildEventType(isEntryOrder, orderUpdate, basePrice),
		ExchangeOrderID: utils.ToNullString(orderUpdate.ExchangeOrderID),
		ParentOrderID:   utils.ToNullString(orderUpdate.ParentOrderID),
		TransactionType: utils.ToNullString(orderUpdate.TransactionType),
		Exchange:        orderUpdate.Exchange,
		Product:         utils.ToNullString(orderUpdate.Product),
		Quantity:        orderUpdate.FilledQuantity,
		TriggerPrice:    utils.ToNullFloat(orderUpdate.TriggerPrice),
		PurchasePrice:   utils.ToNullFloat(orderUpdate.AveragePrice),
		StatusMessage:   utils.ToNullString(orderUpdate.StatusMessage),
		Status:          orderUpdate.Status,
		PlacedAt:        orderUpdate.OrderTimestamp.Time,
	}

	log.Printf(
		"UPSERT order=%s status=%s UpdatedQuantity=%f ResponseQuantity=%f and is it an entry order? %t",
		orderUpdate.OrderID,
		orderUpdate.Status,
		updatedOrder.Quantity,
		orderUpdate.FilledQuantity,
		isEntryOrder,
	)

	if _, err := s.OrderRepo.UpsertOrder(ctx, updatedOrder); err != nil {
		return fmt.Errorf("failed to update order %s: %v", orderUpdate.OrderID, err)
	}

	if s.Manager == nil {
		return nil
	}

	switch orderUpdate.Status {
	case "COMPLETE":
		if isEntryOrder {
			if isBuyTransaction {
				s.Manager.SetBuyQuantity(token, uint32(orderUpdate.FilledQuantity))
				s.Manager.SetDirection(token, "BUY")
			} else {
				s.Manager.SetSellQuantity(token, uint32(orderUpdate.FilledQuantity))
				s.Manager.SetDirection(token, "SELL")
			}
		} else {
			// Exit fill: reduce the opposing quantity
			if isBuyTransaction {
				// Buying to cover a short
				newQty := uint32(0)
				if sellQuantity > uint32(orderUpdate.FilledQuantity) {
					newQty = sellQuantity - uint32(orderUpdate.FilledQuantity)
				}
				s.Manager.SetSellQuantity(token, newQty)
			} else {
				// Selling to close a long
				newQty := uint32(0)
				if buyQuantity > uint32(orderUpdate.FilledQuantity) {
					newQty = buyQuantity - uint32(orderUpdate.FilledQuantity)
				}
				s.Manager.SetBuyQuantity(token, newQty)
			}
		}

		s.Manager.DecrementMaxExecutableOrders(token)
		s.Manager.UpdateBasePrice(token, orderUpdate.AveragePrice)
		s.Manager.UnlockStock(token)
		log.Printf("✅ Order complete: token=%d filled=%f at price=%f", token, orderUpdate.FilledQuantity, orderUpdate.AveragePrice)

	case "PARTIALLY_FILLED":
		// Update the quantities based on what has been filled so far
		if isEntryOrder {
			if isBuyTransaction {
				s.Manager.SetBuyQuantity(token, uint32(orderUpdate.FilledQuantity))
				s.Manager.SetDirection(token, "BUY")
			} else {
				s.Manager.SetSellQuantity(token, uint32(orderUpdate.FilledQuantity))
				s.Manager.SetDirection(token, "SELL")
			}
		}
		// Note: We don't unlock here because the order is still "active"
		log.Printf("⏳ Partial fill: token=%d filled=%f", token, orderUpdate.FilledQuantity)

	case "REJECTED", "CANCELLED":
		log.Printf("❌ Order %s: token=%d", orderUpdate.Status, token)
		// If an entry order is cancelled/rejected before any fill,
		// we should ensure the manager reflects 0 quantity.
		if orderUpdate.FilledQuantity == 0 && isEntryOrder {
			if isBuyTransaction {
				s.Manager.SetBuyQuantity(token, 0)
			} else {
				s.Manager.SetSellQuantity(token, 0)
			}
		}
		s.Manager.UnlockStock(token)
	}

	return nil
}

func (s *OrderService) AllStocksImbalance(trackingStockIds []int64) (map[int64]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	imbalances, err := s.OrderRepo.GetAllStocksOrderImbalance(ctx, trackingStockIds)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return make(map[int64]int), nil // No imbalances found, return empty map
		}
		return nil, err
	}

	result := make(map[int64]int)
	for _, imbalance := range imbalances {
		result[imbalance.TrackingStockID] = imbalance.Imbalance
	}
	return result, nil
}

func (s *OrderService) AllStocksTradeStats(trackingStockIds []int64) (map[int64]repository.TradeStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats, err := s.OrderRepo.GetDailyTradeStats(ctx, trackingStockIds)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return make(map[int64]repository.TradeStats), nil // No imbalances found, return empty map
		}
		return nil, err
	}

	result := make(map[int64]repository.TradeStats)
	for _, stat := range stats {
		result[stat.TrackingStockID] = stat
	}
	return result, nil
}

func (s *OrderService) GetRecoverableEntryOrders() ([]models.Order, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	orders, err := s.OrderRepo.GetRecoverableEntryOrders(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []models.Order{}, nil
		}
		return nil, err
	}

	return orders, nil
}

func (s *OrderService) SyncOrder(order *models.Order) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	existingOrder, err := s.OrderRepo.GetOrderByKiteOrderID(ctx, order.OrderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err := s.AddPlacedOrder(ctx, order)
			return err
		}
		return err
	}

	order.UpdatedAt = time.Now()
	return s.OrderRepo.UpdateOrder(ctx, order, existingOrder.ID)
}

// const (
// 	SignalEntryBuy    SignalType = "ENTRY_BUY"    // open a long position
// 	SignalEntrySell   SignalType = "ENTRY_SELL"   // open a short position
// 	SignalTargetHit   SignalType = "TARGET_HIT"   // close position at target (LIMIT)
// 	SignalStopLossHit SignalType = "STOPLOSS_HIT" // close position at stoploss (MARKET)
// 	SignalForceExit   SignalType = "FORCE_EXIT"   // 15:10 PM forced close (MARKET)
// 	SignalNone        SignalType = "NONE"
// )

func (s *OrderService) buildEventType(isEntryOrder bool, orderUpdate kiteconnect.Order, basePrice float64) string {
	if isEntryOrder {
		if orderUpdate.TransactionType == kiteconnect.TransactionTypeBuy {
			return "ENTRY_BUY"
		}
		return "ENTRY_SELL"
	}

	// For Exits, we compare Fill Price vs Entry Price
	fillPrice := orderUpdate.AveragePrice
	if fillPrice == 0 {
		return "NONE"
	}

	if orderUpdate.TransactionType == kiteconnect.TransactionTypeSell {
		// We are exiting a LONG position
		if fillPrice >= basePrice {
			return "TARGET_HIT" // Sold higher than we bought
		}
		return "STOPLOSS_HIT" // Sold lower than we bought
	}

	if orderUpdate.TransactionType == kiteconnect.TransactionTypeBuy {
		// We are exiting (covering) a SHORT position
		if fillPrice <= basePrice {
			return "TARGET_HIT" // Bought back lower than we sold
		}
		return "STOPLOSS_HIT" // Bought back higher than we sold
	}

	return "NONE"
}
