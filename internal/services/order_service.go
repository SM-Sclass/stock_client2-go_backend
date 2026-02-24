package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/utils"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// BasePriceUpdater is implemented by TrackingManager to update base price. when an order completes. Using an interface avoids circular dependency.
type Manager interface {
	UpdateBasePrice(instrumentToken uint32, price float64)
	UnlockStock(instrumentToken uint32)
	SetSellQuantity(instrumentToken uint32, quantity uint32)
	GetStockIDByTradingSymbol(tradingSymbol string) (int64, bool)
}

type OrderRepo interface {
	AddOrder(ctx context.Context, o *models.Order) (ID int64, err error)
	UpdateOrder(ctx context.Context, o *models.Order, ID int64) error
	GetOrderByKiteOrderID(ctx context.Context, orderID string) (*models.Order, error)
	GetAllStocksOrderImbalance(ctx context.Context, trackingStockIds []int64) (imbalances []repository.OrderImabalance, err error)
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
	_, err := s.OrderRepo.GetOrderByKiteOrderID(ctx, orderUpdate.OrderID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.cachePendingUpdate(orderUpdate)
			return fmt.Errorf("failed to get order %s: %v", orderUpdate.OrderID, err)
		}
		log.Printf("Failed to get order but continued %s: %v", orderUpdate.OrderID, err)
	}
	
	stockID, exists := s.Manager.GetStockIDByTradingSymbol(orderUpdate.TradingSymbol)
	if !exists {
		log.Printf("⚠️ No tracking stock found for trading symbol %s in order update %s", orderUpdate.TradingSymbol, orderUpdate.OrderID)
		return nil
	}

	dbOrder := &models.Order{
		OrderID:         orderUpdate.OrderID,
		TrackingStockID: stockID,
		OrderType:       orderUpdate.OrderType,       

		ExchangeOrderID: utils.ToNullString(orderUpdate.ExchangeOrderID),
		ParentOrderID:   utils.ToNullString(orderUpdate.ParentOrderID),
		TransactionType: utils.ToNullString(orderUpdate.TransactionType),
		Exchange:        orderUpdate.Exchange,
		Product:         utils.ToNullString(orderUpdate.Product),
		Quantity:        orderUpdate.FilledQuantity,  // Using FilledQuantity for updates
		TriggerPrice:    utils.ToNullFloat(orderUpdate.TriggerPrice),
		PurchasePrice:   utils.ToNullFloat(orderUpdate.AveragePrice),
		StatusMessage:   utils.ToNullString(orderUpdate.StatusMessage),
		Status:          orderUpdate.Status,
		PlacedAt:        orderUpdate.OrderTimestamp.Time, 
	}

	if _, err := s.OrderRepo.UpsertOrder(ctx, dbOrder); err != nil {
		return fmt.Errorf("failed to update order %s: %v", orderUpdate.OrderID, err)
	}

	if s.Manager == nil {
		return nil
	}

	token := uint32(orderUpdate.InstrumentToken)

	switch orderUpdate.Status {

	case "COMPLETE":
		if orderUpdate.TransactionType == kiteconnect.TransactionTypeBuy {
			s.Manager.SetSellQuantity(token, uint32(orderUpdate.FilledQuantity))

		} else if orderUpdate.TransactionType == kiteconnect.TransactionTypeSell {

			remainingQty := orderUpdate.Quantity - orderUpdate.FilledQuantity
			s.Manager.SetSellQuantity(token, uint32(remainingQty))

			if remainingQty != 0 {
				log.Printf("⚠️ Partial SELL even in COMPLETE? token=%d", token)
				return nil
			}
		}

		// ✅ Only here:
		s.Manager.UpdateBasePrice(token, orderUpdate.AveragePrice)
		s.Manager.UnlockStock(token) // unlock

	case "PARTIALLY_FILLED":
		remainingQty := orderUpdate.Quantity - orderUpdate.FilledQuantity

		s.Manager.SetSellQuantity(token, uint32(remainingQty))

		log.Printf("⏳ Partial fill: token=%d filled=%f remaining=%f",
			token,
			orderUpdate.FilledQuantity,
			remainingQty,
		)

		// ❌ DO NOT unlock
		return nil

	case "REJECTED":
		log.Printf("❌ Order REJECTED: token=%d reason=%s",
			token,
			orderUpdate.StatusMessage,
		)

		s.Manager.UnlockStock(token)
		return nil

	case "CANCELLED":
		remainingQty := orderUpdate.Quantity - orderUpdate.FilledQuantity

		log.Printf("🚫 Order CANCELLED: token=%d remaining=%f",
			token,
			remainingQty,
		)

		s.Manager.SetSellQuantity(token, uint32(remainingQty))
		s.Manager.UnlockStock(token)

		return nil
	}

	return nil
}

func (s *OrderService) AllStocksImbalance(trackingStockIds []int64) (map[int64]int, error) {
	imbalances, err := s.OrderRepo.GetAllStocksOrderImbalance(context.Background(), trackingStockIds)
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

func (s *OrderService) SyncOrder(order *models.Order) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
