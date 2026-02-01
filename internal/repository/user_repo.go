package repository

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
)

type UserRepository struct {
	DB *pgxpool.Pool
}

func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	query := `INSERT INTO users (phone, password, created_at) VALUES ($1, $2, $3) RETURNING id`
	err := r.DB.QueryRow(ctx, query, user.Phone, user.Password, user.CreatedAt).Scan(&user.ID)
	return err
}

func (r *UserRepository) GetByPhone(ctx context.Context, phone string) (*models.User, error) {
	query := `SELECT id, phone, password, created_at FROM users WHERE phone=$1`

	var user models.User
	err := r.DB.QueryRow(ctx, query, phone).
		Scan(&user.ID, &user.Phone, &user.Password, &user.CreatedAt)

	if err != nil {
		return nil, err
	}

	return &user, nil
}