package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	RoleAdmin    = "admin"
	RoleCustomer = "customer"
)

type User struct {
	ID           uuid.UUID
	Name         string
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func IsValidRole(role string) bool {
	return role == RoleAdmin || role == RoleCustomer
}
