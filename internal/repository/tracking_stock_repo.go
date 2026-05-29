package repository

import (
	"context"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TrackingStocksRepository struct {
	DB *pgxpool.Pool
}

func (r *TrackingStocksRepository) AddTrackingStock(ctx context.Context, ts *models.TrackingStock) (ID int64, err error) {
	query := `
        INSERT INTO tracking_stocks (
            trading_symbol, instrument_token, target, stoploss, 
            order_price_limit, quantity, status, is_deleted, deleted_at
        ) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, FALSE, NULL)
        ON CONFLICT (trading_symbol) 
        DO UPDATE SET 
            instrument_token = EXCLUDED.instrument_token,
            target = EXCLUDED.target,
            stoploss = EXCLUDED.stoploss,
            order_price_limit = EXCLUDED.order_price_limit,
            quantity = EXCLUDED.quantity,
            status = EXCLUDED.status,
            is_deleted = FALSE,
            deleted_at = NULL,
            updated_at = NOW()
        RETURNING id`

	err = r.DB.QueryRow(ctx, query,
		ts.TradingSymbol,
		ts.InstrumentToken,
		ts.Target,
		ts.StopLoss,
		ts.OrderPriceLimit,
		ts.Quantity,
		ts.Status,
	).Scan(&ID)

	if err != nil {
		return 0, err
	}
	return ID, nil
}

func (r *TrackingStocksRepository) GetAllTrackingStocks(ctx context.Context) (trackingStocks []models.TrackingStock, err error) {
	// Added WHERE is_deleted = FALSE
	query := `SELECT id, trading_symbol, exchange, quantity, instrument_token, target, stoploss, order_price_limit, status, created_at 
              FROM tracking_stocks 
              WHERE is_deleted = FALSE`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ts models.TrackingStock
		err := rows.Scan(&ts.ID, &ts.TradingSymbol, &ts.Exchange, &ts.Quantity, &ts.InstrumentToken, &ts.Target, &ts.StopLoss, &ts.OrderPriceLimit, &ts.Status, &ts.CreatedAt)
		if err != nil {
			return nil, err
		}
		trackingStocks = append(trackingStocks, ts)
	}

	return trackingStocks, nil
}

func (r *TrackingStocksRepository) GetAllActiveTrackingStocks(ctx context.Context) (trackingStocks []models.TrackingStock, err error) {
	// Added WHERE is_deleted = FALSE
	query := `SELECT id, trading_symbol, exchange, quantity, instrument_token, target, stoploss, order_price_limit, status, created_at 
              FROM tracking_stocks 
              WHERE is_deleted = FALSE AND (status = 'ACTIVE' OR status = 'AUTO_ACTIVE')`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ts models.TrackingStock
		err := rows.Scan(&ts.ID, &ts.TradingSymbol, &ts.Exchange, &ts.Quantity, &ts.InstrumentToken, &ts.Target, &ts.StopLoss, &ts.OrderPriceLimit, &ts.Status, &ts.CreatedAt)
		if err != nil {
			return nil, err
		}
		trackingStocks = append(trackingStocks, ts)
	}

	return trackingStocks, nil
}

func (r *TrackingStocksRepository) GetAllAutoInactiveTrackingStocks(ctx context.Context) (trackingStocks []models.TrackingStock, err error) {
	// Added AND is_deleted = FALSE
	query := `SELECT id, trading_symbol, exchange, quantity, instrument_token, target, stoploss, order_price_limit, status, created_at 
              FROM tracking_stocks 
              WHERE status = 'AUTO_INACTIVE' AND is_deleted = FALSE`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ts models.TrackingStock
		err := rows.Scan(&ts.ID, &ts.TradingSymbol, &ts.Exchange, &ts.Quantity, &ts.InstrumentToken, &ts.Target, &ts.StopLoss, &ts.OrderPriceLimit, &ts.Status, &ts.CreatedAt)
		if err != nil {
			return nil, err
		}
		trackingStocks = append(trackingStocks, ts)
	}

	return trackingStocks, nil
}

func (r *TrackingStocksRepository) GetTrackingStockByID(ctx context.Context, id int64) (*models.TrackingStock, error) {
	// Added AND is_deleted = FALSE
	query := `SELECT id, trading_symbol, instrument_token, target, stoploss, order_price_limit, status, created_at 
              FROM tracking_stocks 
              WHERE id=$1 AND is_deleted = FALSE`

	var ts models.TrackingStock
	err := r.DB.QueryRow(ctx, query, id).
		Scan(&ts.ID, &ts.TradingSymbol, &ts.InstrumentToken, &ts.Target, &ts.StopLoss, &ts.OrderPriceLimit, &ts.Status, &ts.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ts, nil
}

func (r *TrackingStocksRepository) GetTrackingStockByTradingSymbol(ctx context.Context, trading_symbol string) (*models.TrackingStock, error) {
	// Added AND is_deleted = FALSE
	query := `SELECT id, trading_symbol, instrument_token, target, stoploss, order_price_limit, status, created_at 
              FROM tracking_stocks 
              WHERE trading_symbol=$1 AND is_deleted = FALSE`

	var ts models.TrackingStock
	err := r.DB.QueryRow(ctx, query, trading_symbol).
		Scan(&ts.ID, &ts.TradingSymbol, &ts.InstrumentToken, &ts.Target, &ts.StopLoss, &ts.OrderPriceLimit, &ts.Status, &ts.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ts, nil
}

func (r *TrackingStocksRepository) UpdateTrackingStock(ctx context.Context, ts *models.TrackingStock, ID int64) error {
	// Added AND is_deleted = FALSE to prevent updating "deleted" records
	query := `UPDATE tracking_stocks SET target=$1, stoploss=$2, quantity=$3, order_price_limit=$4, updated_at=NOW() 
              WHERE id=$5 AND is_deleted = FALSE`
	_, err := r.DB.Exec(ctx, query, ts.Target, ts.StopLoss, ts.Quantity, ts.OrderPriceLimit, ID)
	return err
}

func (r *TrackingStocksRepository) UpdateTrackingStockStatus(ctx context.Context, id int64, status string) error {
	// Added AND is_deleted = FALSE
	query := `UPDATE tracking_stocks SET status=$1, updated_at=NOW() 
              WHERE id=$2 AND is_deleted = FALSE`
	_, err := r.DB.Exec(ctx, query, status, id)
	return err
}

func (r *TrackingStocksRepository) DeleteTrackingStock(ctx context.Context, id int64) error {
	// Logic change: Perform Soft Delete
	query := `UPDATE tracking_stocks 
              SET is_deleted = TRUE, 
                  deleted_at = NOW(), 
                  updated_at = NOW() 
              WHERE id = $1 AND is_deleted = FALSE`
	_, err := r.DB.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	return nil
}
