package database_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"mini-ecommerce/internal/config"
	"mini-ecommerce/internal/database"
)

const checkViolation = "23514"

func setupPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, database.MigrateDown(pool))
	require.NoError(t, database.MigrateUp(pool))
	return pool
}

func testAdmin() config.SeedUser {
	return config.SeedUser{Name: "Test Admin", Email: "Admin@Example.com", Password: "supersecret123"}
}

func testCustomer() config.SeedUser {
	return config.SeedUser{Name: "Test Customer", Email: " Customer@Example.com ", Password: "supersecret456"}
}

func TestMigrationsCreateAllTables(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()

	for _, table := range []string{"users", "products", "orders", "order_items"} {
		var exists bool
		err := pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists, "table %s should exist", table)
	}

	require.NoError(t, database.MigrateUp(pool), "re-running migrations should be a no-op")
}

func TestMigrationsAreReversible(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()

	require.NoError(t, database.MigrateDown(pool))

	var exists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&exists))
	assert.False(t, exists)

	require.NoError(t, database.MigrateUp(pool))
}

func TestSeedIsIdempotent(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()

	first, err := database.Seed(ctx, pool, testAdmin(), testCustomer())
	require.NoError(t, err)
	assert.True(t, first.AdminCreated)
	assert.True(t, first.CustomerCreated)
	assert.Equal(t, 10, first.ProductsCreated)

	adminDifferentCase := testAdmin()
	adminDifferentCase.Email = "admin@example.com"
	customerDifferentCase := testCustomer()
	customerDifferentCase.Email = "CUSTOMER@example.com"
	second, err := database.Seed(ctx, pool, adminDifferentCase, customerDifferentCase)
	require.NoError(t, err)
	assert.False(t, second.AdminCreated)
	assert.False(t, second.CustomerCreated)
	assert.Zero(t, second.ProductsCreated)

	var admins, customers, users int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE role = 'admin'), count(*) FILTER (WHERE role = 'customer'), count(*) FROM users`,
	).Scan(&admins, &customers, &users))
	assert.Equal(t, 1, admins)
	assert.Equal(t, 1, customers)
	assert.Equal(t, 2, users)

	var products, categories, outOfStock int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*), count(DISTINCT category), count(*) FILTER (WHERE stock = 0) FROM products`,
	).Scan(&products, &categories, &outOfStock))
	assert.Equal(t, 10, products)
	assert.GreaterOrEqual(t, categories, 3)
	assert.LessOrEqual(t, categories, 4)
	assert.GreaterOrEqual(t, outOfStock, 1)

	var orders int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&orders))
	assert.Zero(t, orders)
}

func TestSeedStoresHashedPasswordAndVersion7IDs(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()

	_, err := database.Seed(ctx, pool, testAdmin(), testCustomer())
	require.NoError(t, err)

	cases := []struct {
		role     string
		name     string
		email    string
		password string
	}{
		{role: "admin", name: "Test Admin", email: "admin@example.com", password: testAdmin().Password},
		{role: "customer", name: "Test Customer", email: "customer@example.com", password: testCustomer().Password},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			var id uuid.UUID
			var name, email, passwordHash string
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT id, name, email, password_hash FROM users WHERE role = $1`, tc.role,
			).Scan(&id, &name, &email, &passwordHash))
			assert.Equal(t, uuid.Version(7), id.Version())
			assert.Equal(t, tc.name, name)
			assert.Equal(t, tc.email, email)
			assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(tc.password)))
		})
	}
}

func TestUniqueEmailIsCaseInsensitive(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()

	insert := `INSERT INTO users (id, name, email, password_hash, role) VALUES ($1, 'User', $2, 'hash', 'customer')`
	_, err := pool.Exec(ctx, insert, uuid.Must(uuid.NewV7()), "john@example.com")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, insert, uuid.Must(uuid.NewV7()), "JOHN@example.com")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23505", pgErr.Code)
}

func TestCheckConstraints(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()

	_, err := database.Seed(ctx, pool, testAdmin(), testCustomer())
	require.NoError(t, err)

	var userID, productID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM users LIMIT 1`).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM products LIMIT 1`).Scan(&productID))
	orderID := uuid.Must(uuid.NewV7())
	_, err = pool.Exec(ctx, `INSERT INTO orders (id, user_id, total_amount) VALUES ($1, $2, 20.00)`, orderID, userID)
	require.NoError(t, err)

	cases := []struct {
		name  string
		query string
		args  []any
	}{
		{"user role", `INSERT INTO users (id, name, email, password_hash, role) VALUES ($1, 'x', 'x@example.com', 'h', 'superuser')`, []any{uuid.Must(uuid.NewV7())}},
		{"product negative price", `INSERT INTO products (id, name, price, category, stock) VALUES ($1, 'x', -1, 'c', 1)`, []any{uuid.Must(uuid.NewV7())}},
		{"product negative stock", `INSERT INTO products (id, name, price, category, stock) VALUES ($1, 'x', 1, 'c', -1)`, []any{uuid.Must(uuid.NewV7())}},
		{"order status", `INSERT INTO orders (id, user_id, total_amount, status) VALUES ($1, $2, 0, 'SHIPPED')`, []any{uuid.Must(uuid.NewV7()), userID}},
		{"order item quantity", `INSERT INTO order_items (id, order_id, product_id, product_name, quantity, unit_price, subtotal) VALUES ($1, $2, $3, 'x', 0, 10, 0)`, []any{uuid.Must(uuid.NewV7()), orderID, productID}},
		{"order item subtotal", `INSERT INTO order_items (id, order_id, product_id, product_name, quantity, unit_price, subtotal) VALUES ($1, $2, $3, 'x', 2, 10, 15)`, []any{uuid.Must(uuid.NewV7()), orderID, productID}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tc.query, tc.args...)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Equal(t, checkViolation, pgErr.Code)
		})
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO order_items (id, order_id, product_id, product_name, quantity, unit_price, subtotal) VALUES ($1, $2, $3, 'x', 2, 10.00, 20.00)`,
		uuid.Must(uuid.NewV7()), orderID, productID,
	)
	require.NoError(t, err)
}

func TestWithTxRollsBackOnError(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()

	id := uuid.Must(uuid.NewV7())
	err := database.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO products (id, name, price, category, stock) VALUES ($1, 'Temp', 1, 'c', 1)`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO products (id, name, price, category, stock) VALUES ($1, 'Bad', -1, 'c', 1)`, uuid.Must(uuid.NewV7()))
		return err
	})
	require.Error(t, err)

	var exists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1)`, id).Scan(&exists))
	assert.False(t, exists)
}
