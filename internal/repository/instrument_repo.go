package repository

import (
	"context"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type InstrumentRepository struct {
	DB *pgxpool.Pool
}

func (r *InstrumentRepository) UpsertInstruments(ctx context.Context, exchange string, instrumentsData []byte) (ID int64, err error) {
	query := `
        INSERT INTO instruments (exchange, instruments_data)
        VALUES ($1, $2)
        ON CONFLICT (exchange)
        DO UPDATE SET
            instruments_data = EXCLUDED.instruments_data,
            stored_at = NOW()
        RETURNING id`
	err = r.DB.QueryRow(ctx, query, exchange, instrumentsData).Scan(&ID)
	if err != nil {
		return 0, err
	}

	return ID, err
}

func (r *InstrumentRepository) GetStoredDateByExchange(ctx context.Context, exchange string) (*models.Instrument, error) {
	query := `SELECT id, stored_at FROM instruments WHERE exchange=$1`
	var inst models.Instrument
	err := r.DB.QueryRow(ctx, query, exchange).Scan(&inst.ID, &inst.StoredAt)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (r *InstrumentRepository) GetInstrumentsByExchange(ctx context.Context, exchange string) (*models.Instrument, error) {
	query := `SELECT id, exchange, instruments_data, stored_at FROM instruments WHERE exchange=$1`
	var inst models.Instrument

	err := r.DB.QueryRow(ctx, query, exchange).
		Scan(&inst.ID, &inst.Exchange, &inst.InstrumentsData, &inst.StoredAt)
	if err != nil {
		return nil, err
	}

	return &inst, nil
}

func (r *InstrumentRepository) UpdateInstruments(ctx context.Context, exchange string, instrumentsData string) error {
	query := `UPDATE instruments SET instruments_data=$1, stored_at=NOW() WHERE exchange=$2`
	_, err := r.DB.Exec(ctx, query, instrumentsData, exchange)
	return err
}
