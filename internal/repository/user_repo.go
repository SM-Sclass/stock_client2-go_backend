package repository

import (
	"context"
	"log"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	DB *pgxpool.Pool
}

func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	query := `INSERT INTO users (full_name, phone, password, created_at) VALUES ($1, $2, $3, $4) RETURNING id`
	err := r.DB.QueryRow(ctx, query, user.FullName, user.Phone, user.Password, user.CreatedAt).Scan(&user.ID)
	return err
}

func (r *UserRepository) GetByPhone(ctx context.Context, phone string) (*models.User, error) {
	query := `SELECT id, full_name, phone, password, created_at FROM users WHERE phone=$1`

	var user models.User
	err := r.DB.QueryRow(ctx, query, phone).
		Scan(&user.ID, &user.FullName, &user.Phone, &user.Password, &user.CreatedAt)

	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *UserRepository) GetUserProfile(ctx context.Context, id int64) (*models.User, error) {
	query := `SELECT id, full_name, phone FROM users WHERE id=$1`
	var user models.User
	err := r.DB.QueryRow(ctx, query, id).
		Scan(&user.ID, &user.FullName, &user.Phone)

	if err != nil {
		log.Println("Error: ", err)
		return nil, err
	}

	return &user, nil
}