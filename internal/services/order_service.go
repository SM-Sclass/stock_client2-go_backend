package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// BasePriceUpdater is implemented by TrackingManager to update base price
// when an order completes. Using an interface avoids circular dependency.
type BasePriceUpdater interface {
	UpdateBasePrice(instrumentToken uint32, price float64)
}

type OrderRepo interface {
	AddOrder(ctx context.Context, o *models.Order) (ID int64, err error)
	UpdateOrder(ctx context.Context, o *models.Order, ID int64) error
	GetOrderByKiteOrderID(ctx context.Context, orderID string) (*models.Order, error)
}

type TrackingStockRepo interface {
	GetTrackingStockByID(ctx context.Context, id int64) (*models.TrackingStock, error)
}

type OrderService struct {
	OrderRepo         OrderRepo
	TrackingStockRepo TrackingStockRepo
	BasePriceUpdater  BasePriceUpdater
	now               func() time.Time
	pendingUpdates    map[string]pendingOrderUpdate
	mu                sync.Mutex
}

const pendingUpdateTTL = 2 * time.Minute

type pendingOrderUpdate struct {
	update     kiteconnect.Order
	receivedAt time.Time
}

// SetBasePriceUpdater sets the BasePriceUpdater after TrackingManager is initialized.
// This is needed to avoid circular dependency during initialization.
func (s *OrderService) SetBasePriceUpdater(updater BasePriceUpdater) {
	s.BasePriceUpdater = updater
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
	order, err := s.OrderRepo.GetOrderByKiteOrderID(ctx, orderUpdate.OrderID)
	if err != nil {
		if errors.Is(err, repository.ErrOrderNotFound) {
			s.cachePendingUpdate(orderUpdate)
			return nil
		}
		return fmt.Errorf("failed to get order %s: %v", orderUpdate.OrderID, err)
	}

	order.ExchangeOrderID = orderUpdate.ExchangeOrderID
	order.ParentOrderID = orderUpdate.ParentOrderID
	order.TransactionType = orderUpdate.TransactionType
	order.Exchange = orderUpdate.Exchange
	order.Product = orderUpdate.Product
	order.Quantity = orderUpdate.Quantity
	order.TriggerPrice = orderUpdate.TriggerPrice
	order.PurchasePrice = orderUpdate.AveragePrice
	order.StatusMessage = orderUpdate.StatusMessage
	order.Status = orderUpdate.Status
	order.UpdatedAt = time.Now()

	if err := s.OrderRepo.UpdateOrder(ctx, order, order.ID); err != nil {
		return fmt.Errorf("failed to update order %s: %v", orderUpdate.OrderID, err)
	}

	// On successful COMPLETE, update the tracking stock basePrice with AveragePrice
	if orderUpdate.Status == "COMPLETE" && s.BasePriceUpdater != nil {
		// Get the tracking stock to find the instrument token
		trackingStock, err := s.TrackingStockRepo.GetTrackingStockByID(ctx, order.TrackingStockID)
		if err == nil && trackingStock != nil {
			s.BasePriceUpdater.UpdateBasePrice(uint32(trackingStock.InstrumentToken), orderUpdate.AveragePrice)
		}
	}

	return nil
}
