package repository

import (
	"context"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OrderRepository struct {
	DB *pgxpool.Pool
}

type StockOrdersResponse struct {
	Orders     []models.Order `json:"orders"`
	TotalCount int            `json:"total_count"`
}

type OrderImabalance struct {
	TrackingStockID int64 `json:"tracking_stock_id"`
	Imbalance       int   `json:"imbalance"`
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

func (r *OrderRepository) UpsertOrder(ctx context.Context, o *models.Order) (int64, error) {
	query := `
		INSERT INTO orders (
			tracking_stock_id, order_id, exchange_order_id, parent_order_id, 
			order_type, event_type, transaction_type, exchange, product, 
			quantity, base_price, trigger_price, purchase_price, 
			status_message, status, placed_at, updated_at
		) 
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (order_id) DO UPDATE SET
			exchange_order_id = EXCLUDED.exchange_order_id,
			parent_order_id = EXCLUDED.parent_order_id,
			transaction_type = EXCLUDED.transaction_type,
			exchange = EXCLUDED.exchange,
			product = EXCLUDED.product,
			quantity = EXCLUDED.quantity,
			trigger_price = EXCLUDED.trigger_price,
			purchase_price = EXCLUDED.purchase_price,
			status_message = EXCLUDED.status_message,
			status = EXCLUDED.status,
			updated_at = NOW()
		RETURNING id;`

	var id int64
	err := r.DB.QueryRow(ctx, query,
		o.TrackingStockID, o.OrderID, o.ExchangeOrderID, o.ParentOrderID,
		o.OrderType, o.EventType, o.TransactionType, o.Exchange, o.Product,
		o.Quantity, o.BasePrice, o.TriggerPrice, o.PurchasePrice,
		o.StatusMessage, o.Status, o.PlacedAt, time.Now(),
	).Scan(&id)

	return id, err
}

func (r *OrderRepository) GetOrderByKiteOrderID(ctx context.Context, orderID string) (*models.Order, error) {
	query := `SELECT id, tracking_stock_id, order_id, order_type, event_type, base_price, quantity, status, placed_at FROM orders WHERE order_id=$1`
	var o models.Order

	err := r.DB.QueryRow(ctx, query, orderID).
		Scan(&o.ID, &o.TrackingStockID, &o.OrderID, &o.OrderType, &o.EventType, &o.BasePrice, &o.Quantity, &o.Status, &o.PlacedAt)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *OrderRepository) GetOrdersByTrackingStockID(ctx context.Context, trackingStockID int64, pageNumber int, limit int) (StockOrdersResponse, error) {
	query := `SELECT id, tracking_stock_id, order_id, order_type, event_type, transaction_type, base_price, quantity, purchase_price, status, placed_at FROM orders WHERE tracking_stock_id=$1 LIMIT $2 OFFSET $3`
	query2 := `SELECT count(*) FROM orders WHERE tracking_stock_id=$1`

	rows, err := r.DB.Query(ctx, query, trackingStockID, limit, (pageNumber-1)*limit)
	if err != nil {
		return StockOrdersResponse{}, err
	}
	defer rows.Close()

	var orders []models.Order
	for rows.Next() {
		var o models.Order
		err := rows.Scan(&o.ID, &o.TrackingStockID, &o.OrderID, &o.OrderType, &o.EventType, &o.TransactionType, &o.BasePrice, &o.Quantity, &o.PurchasePrice, &o.Status, &o.PlacedAt)
		if err != nil {
			return StockOrdersResponse{}, err
		}
		orders = append(orders, o)
	}

	var totalCount int
	err = r.DB.QueryRow(ctx, query2, trackingStockID).Scan(&totalCount)
	if err != nil {
		return StockOrdersResponse{}, err
	}

	return StockOrdersResponse{Orders: orders, TotalCount: totalCount}, nil
}

func (r *OrderRepository) GetOrderByID(ctx context.Context, id int64) (*models.Order, error) {
	query := `SELECT id, tracking_stock_id, order_id, order_type, event_type, transaction_type, base_price, quantity, purchase_price, status, placed_at FROM orders WHERE id=$1`
	var o models.Order
	err := r.DB.QueryRow(ctx, query, id).
		Scan(&o.ID, &o.TrackingStockID, &o.OrderID, &o.OrderType, &o.EventType, &o.TransactionType, &o.BasePrice, &o.Quantity, &o.PurchasePrice, &o.Status, &o.PlacedAt)
	if err != nil {
		return nil, err
	}

	return &o, nil
}

func (r *OrderRepository) GetAllOrders(ctx context.Context) (orders []models.Order, err error) {
	query := `SELECT id, tracking_stock_id, order_id, order_type, event_type, transaction_type, base_price, quantity, purchase_price, status, placed_at FROM orders`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var o models.Order
		err := rows.Scan(&o.ID, &o.TrackingStockID, &o.OrderID, &o.OrderType, &o.EventType, &o.TransactionType, &o.BasePrice, &o.Quantity, &o.PurchasePrice, &o.Status, &o.PlacedAt)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, nil
}

func (r *OrderRepository) GetAllStocksOrderImbalance(ctx context.Context, trackingStockIDs []int64) (imbalances []OrderImabalance, err error) {
	query := `SELECT 
	tracking_stock_id,
  SUM(CASE WHEN transaction_type='BUY' THEN quantity ELSE 0 END) - 
  SUM(CASE WHEN transaction_type='SELL' THEN quantity ELSE 0 END) AS imbalance 
	FROM orders
	WHERE status = 'COMPLETE' AND placed_at::date = CURRENT_DATE AND tracking_stock_id = ANY($1)
	GROUP BY tracking_stock_id;`
	rows, err := r.DB.Query(ctx, query, trackingStockIDs)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var oi OrderImabalance
		err = rows.Scan(&oi.TrackingStockID, &oi.Imbalance)
		if err != nil {
			return nil, err
		}
		imbalances = append(imbalances, oi)
	}

	return imbalances, err
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