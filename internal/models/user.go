package models

import "time"

type User struct {
	ID        int64     `db:"id"`
	Phone     string    `db:"phone"`
	Password  string    `db:"password"`
	CreatedAt time.Time `db:"created_at"`
}