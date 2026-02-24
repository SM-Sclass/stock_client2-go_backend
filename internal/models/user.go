package models

import "time"

type User struct {
	ID        int64     `db:"id"`
	FullName  string    `db:"full_name"`
	Phone     string    `db:"phone"`
	Password  string    `db:"password"`
	CreatedAt time.Time `db:"created_at"`
}