package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"

	"mini-ecommerce/internal/config"
)

const seedAdvisoryLockKey int64 = 7_203_114_001

type seedProduct struct {
	name        string
	description string
	price       decimal.Decimal
	category    string
	stock       int
}

var sampleProducts = []seedProduct{
	{"Wireless Mouse", "Ergonomic 2.4GHz wireless mouse with silent clicks", decimal.RequireFromString("149000.00"), "Electronics", 120},
	{"Mechanical Keyboard", "87-key mechanical keyboard with hot-swappable switches", decimal.RequireFromString("899000.00"), "Electronics", 35},
	{"Noise Cancelling Headphones", "Over-ear Bluetooth headphones with active noise cancelling", decimal.RequireFromString("2499000.00"), "Electronics", 0},
	{"Clean Code", "A handbook of agile software craftsmanship", decimal.RequireFromString("425000.00"), "Books", 18},
	{"The Pragmatic Programmer", "Your journey to mastery, 20th anniversary edition", decimal.RequireFromString("389500.00"), "Books", 7},
	{"Designing Data-Intensive Applications", "The big ideas behind reliable, scalable, and maintainable systems", decimal.RequireFromString("612750.00"), "Books", 3},
	{"Cotton Crew Neck T-Shirt", "Unisex 100% combed cotton t-shirt", decimal.RequireFromString("79900.00"), "Fashion", 250},
	{"Denim Jacket", "Classic fit denim jacket with button closure", decimal.RequireFromString("459000.00"), "Fashion", 12},
	{"Ceramic Coffee Mug", "350ml stoneware mug, dishwasher safe", decimal.RequireFromString("45000.00"), "Home", 80},
	{"Stainless Steel Water Bottle", "750ml vacuum insulated bottle keeps drinks cold for 24 hours", decimal.RequireFromString("185000.00"), "Home", 60},
}

type SeedResult struct {
	AdminCreated    bool
	ProductsCreated int
}

func Seed(ctx context.Context, pool *pgxpool.Pool, admin config.SeedAdmin) (SeedResult, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(admin.Password), bcrypt.DefaultCost)
	if err != nil {
		return SeedResult{}, fmt.Errorf("hash admin password: %w", err)
	}

	var result SeedResult
	err = WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, seedAdvisoryLockKey); err != nil {
			return fmt.Errorf("acquire seed lock: %w", err)
		}

		created, err := seedAdmin(ctx, tx, admin, string(passwordHash))
		if err != nil {
			return err
		}
		result.AdminCreated = created

		count, err := seedProducts(ctx, tx)
		if err != nil {
			return err
		}
		result.ProductsCreated = count
		return nil
	})
	if err != nil {
		return SeedResult{}, err
	}
	return result, nil
}

func seedAdmin(ctx context.Context, tx pgx.Tx, admin config.SeedAdmin, passwordHash string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = lower($1))`, admin.Email).Scan(&exists); err != nil {
		return false, fmt.Errorf("check admin existence: %w", err)
	}
	if exists {
		return false, nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("generate admin id: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO users (id, name, email, password_hash, role) VALUES ($1, $2, $3, $4, 'admin')`,
		id, admin.Name, strings.ToLower(admin.Email), passwordHash,
	)
	if err != nil {
		return false, fmt.Errorf("insert admin: %w", err)
	}
	return true, nil
}

func seedProducts(ctx context.Context, tx pgx.Tx) (int, error) {
	var hasProducts bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM products)`).Scan(&hasProducts); err != nil {
		return 0, fmt.Errorf("check products existence: %w", err)
	}
	if hasProducts {
		return 0, nil
	}

	batch := &pgx.Batch{}
	for _, product := range sampleProducts {
		id, err := uuid.NewV7()
		if err != nil {
			return 0, fmt.Errorf("generate product id: %w", err)
		}
		batch.Queue(
			`INSERT INTO products (id, name, description, price, category, stock) VALUES ($1, $2, $3, $4, $5, $6)`,
			id, product.name, product.description, product.price, product.category, product.stock,
		)
	}

	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return 0, fmt.Errorf("insert sample products: %w", err)
	}
	return len(sampleProducts), nil
}
