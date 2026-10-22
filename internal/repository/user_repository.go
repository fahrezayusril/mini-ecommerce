package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mini-ecommerce/internal/domain"
)

const (
	uniqueViolation       = "23505"
	usersEmailUniqueIndex = "users_email_lower_key"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Create(ctx context.Context, user domain.User) (domain.User, error) {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (id, name, email, password_hash, role)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at, updated_at`,
		user.ID, user.Name, user.Email, user.PasswordHash, user.Role,
	).Scan(&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == usersEmailUniqueIndex {
			return domain.User{}, domain.ErrEmailAlreadyExists
		}
		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}
	return user, nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	var user domain.User
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, email, password_hash, role, created_at, updated_at
		 FROM users
		 WHERE lower(email) = lower($1)`,
		email,
	).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("find user by email: %w", err)
	}
	return user, nil
}
