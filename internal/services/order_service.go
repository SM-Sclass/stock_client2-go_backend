package services

import (
	"context"
	"fmt"
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

type OrderService struct {
	OrderRepo         *repository.OrderRepository
	TrackingStockRepo *repository.TrackingStocksRepository
	BasePriceUpdater  BasePriceUpdater
}

// SetBasePriceUpdater sets the BasePriceUpdater after TrackingManager is initialized.
// This is needed to avoid circular dependency during initialization.
func (s *OrderService) SetBasePriceUpdater(updater BasePriceUpdater) {
	s.BasePriceUpdater = updater
}

func (s *OrderService) AddPlacedOrder(ctx context.Context, order *models.Order) error {
	if _, err := s.OrderRepo.AddOrder(ctx, order); err != nil {
		return err
	}

	return nil
}

func (s *OrderService) ProcessOrderUpdate(ctx context.Context, orderUpdate kiteconnect.Order) error {
	order, err := s.OrderRepo.GetOrderByKiteOrderID(ctx, orderUpdate.OrderID)
	if err != nil {
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
