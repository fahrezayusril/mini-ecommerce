package database

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service interface {
	Health() map[string]string
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

func (s *service) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		return map[string]string{"status": "down", "message": "database unreachable"}
	}

	stats := s.pool.Stat()
	return map[string]string{
		"status":               "up",
		"message":              "It's healthy",
		"total_connections":    strconv.Itoa(int(stats.TotalConns())),
		"acquired_connections": strconv.Itoa(int(stats.AcquiredConns())),
		"idle_connections":     strconv.Itoa(int(stats.IdleConns())),
		"max_connections":      strconv.Itoa(int(stats.MaxConns())),
	}
}

func (s *service) Close() error {
	s.pool.Close()
	return nil
}
