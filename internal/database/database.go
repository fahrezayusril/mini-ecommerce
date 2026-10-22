package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service interface {
	Ping(ctx context.Context) error
	Pool() *pgxpool.Pool
	Close() error
}

type service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) Service {
	return &service{pool: pool}
}

func (s *service) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *service) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *service) Close() error {
	s.pool.Close()
	return nil
}
