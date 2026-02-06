package services

import (
	"context"
	"testing"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/repository"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

type fakeOrderRepo struct {
	orders      map[string]*models.Order
	updated     *models.Order
	addCalls    int
	updateCalls int
	getErr      error
}

func (f *fakeOrderRepo) AddOrder(_ context.Context, o *models.Order) (int64, error) {
	if f.orders == nil {
		f.orders = make(map[string]*models.Order)
	}
	f.addCalls++
	if o.ID == 0 {
		o.ID = int64(len(f.orders) + 1)
	}
	copied := *o
	f.orders[o.OrderID] = &copied
	return o.ID, nil
}

func (f *fakeOrderRepo) UpdateOrder(_ context.Context, o *models.Order, _ int64) error {
	f.updateCalls++
	copied := *o
	f.updated = &copied
	return nil
}

func (f *fakeOrderRepo) GetOrderByKiteOrderID(_ context.Context, orderID string) (*models.Order, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.orders == nil {
		return nil, repository.ErrOrderNotFound
	}
	order, ok := f.orders[orderID]
	if !ok {
		return nil, repository.ErrOrderNotFound
	}
	copied := *order
	return &copied, nil
}

type fakeTrackingRepo struct {
	trackingStock *models.TrackingStock
}

func (f *fakeTrackingRepo) GetTrackingStockByID(_ context.Context, _ int64) (*models.TrackingStock, error) {
	return f.trackingStock, nil
}

func TestOrderService_ReplaysPendingUpdateAfterAdd(t *testing.T) {
	ctx := context.Background()
	repo := &fakeOrderRepo{}
	svc := &OrderService{
		OrderRepo:         repo,
		TrackingStockRepo: &fakeTrackingRepo{},
	}

	update := kiteconnect.Order{
		OrderID:      "order-123",
		Status:       "COMPLETE",
		AveragePrice: 123.45,
		Quantity:     10,
		Exchange:     "NSE",
	}

	if err := svc.ProcessOrderUpdate(ctx, update); err != nil {
		t.Fatalf("ProcessOrderUpdate returned error: %v", err)
	}

	order := &models.Order{
		TrackingStockID: 1,
		OrderID:         "order-123",
		OrderType:       "BUY",
		Status:          "PENDING",
		PlacedAt:        time.Now(),
	}

	if err := svc.AddPlacedOrder(ctx, order); err != nil {
		t.Fatalf("AddPlacedOrder returned error: %v", err)
	}

	if repo.updateCalls != 1 {
		t.Fatalf("expected UpdateOrder to be called once, got %d", repo.updateCalls)
	}

	if repo.updated == nil || repo.updated.Status != "COMPLETE" {
		t.Fatalf("expected order status to be COMPLETE, got %+v", repo.updated)
	}
}

func TestOrderService_ProcessOrderUpdateUpdatesExistingOrder(t *testing.T) {
	ctx := context.Background()
	repo := &fakeOrderRepo{
		orders: map[string]*models.Order{
			"order-456": {
				ID:              5,
				TrackingStockID: 2,
				OrderID:         "order-456",
				Status:          "PENDING",
				PlacedAt:        time.Now().Add(-time.Minute),
			},
		},
	}
	svc := &OrderService{
		OrderRepo:         repo,
		TrackingStockRepo: &fakeTrackingRepo{},
	}

	before := time.Now()
	update := kiteconnect.Order{
		OrderID:         "order-456",
		Status:          "REJECTED",
		StatusMessage:   "Insufficient funds",
		Exchange:        "NSE",
		Product:         "MIS",
		Quantity:        5,
		AveragePrice:    99.5,
		TriggerPrice:    98.0,
		ExchangeOrderID: "exch-1",
		ParentOrderID:   "parent-1",
		TransactionType: "BUY",
	}

	if err := svc.ProcessOrderUpdate(ctx, update); err != nil {
		t.Fatalf("ProcessOrderUpdate returned error: %v", err)
	}

	if repo.updateCalls != 1 {
		t.Fatalf("expected UpdateOrder to be called once, got %d", repo.updateCalls)
	}

	if repo.updated == nil {
		t.Fatal("expected updated order to be set")
	}
	if repo.updated.Status != "REJECTED" || repo.updated.StatusMessage != "Insufficient funds" {
		t.Fatalf("unexpected updated status: %+v", repo.updated)
	}
	if !repo.updated.UpdatedAt.After(before) {
		t.Fatalf("expected UpdatedAt to be set, got %v", repo.updated.UpdatedAt)
	}
}

func TestOrderService_DropsExpiredPendingUpdates(t *testing.T) {
	base := time.Now()
	ctx := context.Background()
	repo := &fakeOrderRepo{}
	svc := &OrderService{
		OrderRepo:         repo,
		TrackingStockRepo: &fakeTrackingRepo{},
		now:               func() time.Time { return base },
	}

	update := kiteconnect.Order{
		OrderID: "order-expired",
		Status:  "COMPLETE",
	}

	if err := svc.ProcessOrderUpdate(ctx, update); err != nil {
		t.Fatalf("ProcessOrderUpdate returned error: %v", err)
	}

	svc.now = func() time.Time { return base.Add(pendingUpdateTTL + time.Second) }

	order := &models.Order{
		TrackingStockID: 1,
		OrderID:         "order-expired",
		OrderType:       "BUY",
		Status:          "PENDING",
		PlacedAt:        time.Now(),
	}

	if err := svc.AddPlacedOrder(ctx, order); err != nil {
		t.Fatalf("AddPlacedOrder returned error: %v", err)
	}

	if repo.updateCalls != 0 {
		t.Fatalf("expected UpdateOrder not to be called for expired pending update, got %d", repo.updateCalls)
	}
}
