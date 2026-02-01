package repository

import (
	"context"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OrderRepository struct {
	DB *pgxpool.Pool
}

func (r *OrderRepository) AddOrder(ctx context.Context, o *models.Order) (ID int64, err error) {
	query := `INSERT INTO orders (tracking_stock_id, order_id, order_type, event_type, base_price, quantity, purchase_price, status, placed_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`

	err = r.DB.QueryRow(ctx, query, o.TrackingStockID, o.OrderID, o.OrderType, o.EventType, o.BasePrice, o.Quantity, o.PurchasePrice, o.Status, o.PlacedAt).Scan(&ID)
	if err != nil {
		return 0, err
	}

	return ID, err
}

func (r *OrderRepository) UpdateOrder(ctx context.Context, o *models.Order, ID int64) error {
	query := `UPDATE orders SET exchange_order_id=$1, parent_order_id=$2, transaction_type=$3, exchange=$4, product=$5, quantity=$6, trigger_price=$7, purchase_price=$8, status_message=$9, status=$10, updated_at=$11 WHERE id=$12`
	_, err := r.DB.Exec(ctx, query, o.ExchangeOrderID, o.ParentOrderID, o.TransactionType, o.Exchange, o.Product, o.Quantity, o.TriggerPrice, o.PurchasePrice, o.StatusMessage, o.Status, o.UpdatedAt, ID)
	return err
}

func (r *OrderRepository) GetOrderByKiteOrderID(ctx context.Context, orderID string) (*models.Order, error) {
	query := `SELECT id, tracking_stock_id, order_id, order_type, event_type, base_price, quantity, purchase_price, status, placed_at FROM orders WHERE order_id=$1`
	var o models.Order

	err := r.DB.QueryRow(ctx, query, orderID).
		Scan(&o.ID, &o.TrackingStockID, &o.OrderID, &o.OrderType, &o.EventType, &o.BasePrice, &o.Quantity, &o.PurchasePrice, &o.Status, &o.PlacedAt)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *OrderRepository) GetOrdersByTrackingStockID(ctx context.Context, trackingStockID int64, pageNumber int, limit int) (orders []models.Order, err error) {
	query := `SELECT id, tracking_stock_id, order_id, order_type, event_type, base_price, quantity, purchase_price, status, placed_at FROM orders WHERE tracking_stock_id=$1 LIMIT $2 OFFSET $3`

	rows, err := r.DB.Query(ctx, query, trackingStockID, limit, (pageNumber-1)*limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o models.Order
		err := rows.Scan(&o.ID, &o.TrackingStockID, &o.OrderID, &o.OrderType, &o.EventType, &o.BasePrice, &o.Quantity, &o.PurchasePrice, &o.Status, &o.PlacedAt)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, nil
}

func (r *OrderRepository) GetOrderByID(ctx context.Context, id int64) (*models.Order, error) {
	query := `SELECT id, tracking_stock_id, order_id, order_type, event_type, base_price, quantity, purchase_price, status, placed_at FROM orders WHERE id=$1`
	var o models.Order
	err := r.DB.QueryRow(ctx, query, id).
		Scan(&o.ID, &o.TrackingStockID, &o.OrderID, &o.OrderType, &o.EventType, &o.BasePrice, &o.Quantity, &o.PurchasePrice, &o.Status, &o.PlacedAt)
	if err != nil {
		return nil, err
	}

	return &o, nil
}

func (r *OrderRepository) GetAllOrders(ctx context.Context) (orders []models.Order, err error) {
	query := `SELECT id, tracking_stock_id, order_id, order_type, event_type, base_price, quantity, purchase_price, status, placed_at FROM orders`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o models.Order
		err := rows.Scan(&o.ID, &o.TrackingStockID, &o.OrderID, &o.OrderType, &o.EventType, &o.BasePrice, &o.Quantity, &o.PurchasePrice, &o.Status, &o.PlacedAt)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, nil
}

func (r *OrderRepository) UpdateOrderStatus(ctx context.Context, id int64, status string) error {
	query := `UPDATE orders SET status=$1, updated_at=NOW() WHERE id=$2`
	_, err := r.DB.Exec(ctx, query, status, id)
	return err
}

func (r *OrderRepository) DeleteOrder(ctx context.Context, id int64) error {
	query := `DELETE FROM orders WHERE id=$1`
	_, err := r.DB.Exec(ctx, query, id)
	return err
}
