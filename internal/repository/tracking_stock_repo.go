package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
)

type TrackingStocksRepository struct {
	DB *pgxpool.Pool
}

func (r *TrackingStocksRepository) AddTrackingStock(ctx context.Context, ts *models.TrackingStock) (ID int64, err error) {
	query := `INSERT INTO tracking_stocks (stock_symbol, instrument_token, target, stoploss, status) VALUES ($1, $2, $3, $4, $5) RETURNING id`
	err = r.DB.QueryRow(ctx, query, ts.StockSymbol, ts.InstrumentToken, ts.Target, ts.StopLoss, ts.Status).Scan(&ID)
	if err != nil {
		return 0, err
	}
	return ID, err
}

func (r *TrackingStocksRepository) GetAllTrackingStocks(ctx context.Context) (trackingStocks []models.TrackingStock, err error) {
	query := `SELECT id, stock_symbol, instrument_token, target, stoploss, status, created_at FROM tracking_stocks WHERE status IN ('AUTO_INACTIVE')`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ts models.TrackingStock
		err := rows.Scan(&ts.ID, &ts.StockSymbol, &ts.InstrumentToken, &ts.Target, &ts.StopLoss, &ts.Status, &ts.CreatedAt)
		if err != nil {
			return nil, err
		}   
		trackingStocks = append(trackingStocks, ts)
	}

	return trackingStocks, nil
}

func (r *TrackingStocksRepository) GetTrackingStockByID(ctx context.Context, id int64) (*models.TrackingStock, error) {
	query := `SELECT id, stock_symbol, instrument_token, target, stoploss, status, created_at FROM tracking_stocks WHERE id=$1`
	
	var ts models.TrackingStock
	err := r.DB.QueryRow(ctx, query, id).
		Scan(&ts.ID, &ts.StockSymbol, &ts.InstrumentToken, &ts.Target, &ts.StopLoss, &ts.Status, &ts.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ts, nil
}

func (r *TrackingStocksRepository) GetTrackingStockByTradingSymbol(ctx context.Context, trading_symbol string) (*models.TrackingStock, error) {
	query := `SELECT id, stock_symbol, instrument_token, target, stoploss, status, created_at FROM tracking_stocks WHERE stock_symbol=$1`
	
	var ts models.TrackingStock
	err := r.DB.QueryRow(ctx, query, trading_symbol).
		Scan(&ts.ID, &ts.StockSymbol, &ts.InstrumentToken, &ts.Target, &ts.StopLoss, &ts.Status, &ts.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ts, nil
} 

func (r *TrackingStocksRepository) UpdateTrackingStock(ctx context.Context, ts *models.TrackingStock, ID int64) error {
	query := `UPDATE tracking_stocks SET target=$1, stoploss=$2 WHERE id=$3`
	_, err := r.DB.Exec(ctx, query, ts.Target, ts.StopLoss, ID)
	return err
}

func (r *TrackingStocksRepository) UpdateTrackingStockStatus(ctx context.Context, id int64, status string) error {
	query := `UPDATE tracking_stocks SET status=$1 WHERE id=$2`
	_, err := r.DB.Exec(ctx, query, status, id)
	return err
}

func (r *TrackingStocksRepository) DeleteTrackingStock(ctx context.Context, id int64) error {
	query := `DELETE FROM tracking_stocks WHERE id=$1`
	_, err := r.DB.Exec(ctx, query, id)
	return err
}

