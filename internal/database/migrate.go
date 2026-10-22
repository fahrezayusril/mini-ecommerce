package database

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"mini-ecommerce/migrations"
)

func newMigrator(pool *pgxpool.Pool) (*migrate.Migrate, error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("open migration source: %w", err)
	}

	driver, err := migratepgx.WithInstance(stdlib.OpenDBFromPool(pool), &migratepgx.Config{})
	if err != nil {
		return nil, fmt.Errorf("open migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return m, nil
}

func MigrateUp(pool *pgxpool.Pool) error {
	m, err := newMigrator(pool)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func MigrateDown(pool *pgxpool.Pool) error {
	m, err := newMigrator(pool)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("revert migrations: %w", err)
	}
	return nil
}
