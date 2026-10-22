package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/repository"
	"mini-ecommerce/internal/service"
)

const deadlockDetected = "40P01"

type orderFixture struct {
	pool    *pgxpool.Pool
	repo    *repository.OrderRepository
	service *service.OrderService
}

func setupOrders(t *testing.T) *orderFixture {
	t.Helper()
	pool := setupPool(t)
	repo := repository.NewOrderRepository(pool)
	return &orderFixture{
		pool: pool,
		repo: repo,
		service: service.NewOrderService(repo, func(ctx context.Context, fn func(service.OrderTxStore) error) error {
			return repo.InTx(ctx, func(tx *repository.OrderTx) error { return fn(tx) })
		}),
	}
}

func (f *orderFixture) customer(t *testing.T, email string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO users (id, name, email, password_hash, role) VALUES ($1, $2, $3, 'x', 'customer')`,
		id, "Customer "+email, email,
	)
	require.NoError(t, err)
	return id
}

func (f *orderFixture) product(t *testing.T, name, price string, stock int) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := f.pool.Exec(context.Background(),
		`INSERT INTO products (id, name, price, category, stock) VALUES ($1, $2, $3, 'Test', $4)`,
		id, name, decimal.RequireFromString(price), stock,
	)
	require.NoError(t, err)
	return id
}

func (f *orderFixture) stock(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var stock int
	require.NoError(t, f.pool.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, id).Scan(&stock))
	return stock
}

func (f *orderFixture) rowCounts(t *testing.T) (orders, items int) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM orders), (SELECT count(*) FROM order_items)`,
	).Scan(&orders, &items))
	return orders, items
}

func (f *orderFixture) softDelete(t *testing.T, id uuid.UUID) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `UPDATE products SET deleted_at = now() WHERE id = $1`, id)
	require.NoError(t, err)
}

func items(pairs ...any) []domain.CheckoutItem {
	out := make([]domain.CheckoutItem, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, domain.CheckoutItem{ProductID: pairs[i].(uuid.UUID), Quantity: pairs[i+1].(int)})
	}
	return out
}

func sortedPair(a, b uuid.UUID) []uuid.UUID {
	if a.String() < b.String() {
		return []uuid.UUID{a, b}
	}
	return []uuid.UUID{b, a}
}

func TestCheckoutDeductsStockAndComputesTotals(t *testing.T) {
	f := setupOrders(t)
	ctx := context.Background()
	userID := f.customer(t, "jane@example.com")
	mouse := f.product(t, "Wireless Mouse", "149000.00", 10)
	pencil := f.product(t, "Pencil", "0.10", 4)

	order, err := f.service.Checkout(ctx, userID, items(mouse, 2, pencil, 3, mouse, 1))
	require.NoError(t, err)

	assert.Equal(t, domain.OrderStatusPending, order.Status)
	assert.Equal(t, "447000.30", order.TotalAmount.StringFixed(2))
	assert.Equal(t, 7, f.stock(t, mouse))
	assert.Equal(t, 1, f.stock(t, pencil))

	stored, err := f.repo.FindByID(ctx, order.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, userID, stored.UserID)
	assert.Equal(t, "447000.30", stored.TotalAmount.StringFixed(2))
	require.Len(t, stored.Items, 2)

	byProduct := map[uuid.UUID]domain.OrderItem{}
	for _, item := range stored.Items {
		byProduct[item.ProductID] = item
	}
	assert.Equal(t, 3, byProduct[mouse].Quantity)
	assert.Equal(t, "Wireless Mouse", byProduct[mouse].ProductName)
	assert.Equal(t, "149000.00", byProduct[mouse].UnitPrice.StringFixed(2))
	assert.Equal(t, "447000.00", byProduct[mouse].Subtotal.StringFixed(2))
	assert.Equal(t, "0.30", byProduct[pencil].Subtotal.StringFixed(2))

	_, err = f.pool.Exec(ctx, `UPDATE products SET name = 'Renamed', price = 1 WHERE id = $1`, mouse)
	require.NoError(t, err)
	stored, err = f.repo.FindByID(ctx, order.ID, nil)
	require.NoError(t, err)
	for _, item := range stored.Items {
		if item.ProductID == mouse {
			assert.Equal(t, "Wireless Mouse", item.ProductName)
			assert.Equal(t, "149000.00", item.UnitPrice.StringFixed(2))
		}
	}
}

func TestCheckoutInsufficientStockLeavesDatabaseUnchanged(t *testing.T) {
	f := setupOrders(t)
	userID := f.customer(t, "jane@example.com")
	plenty := f.product(t, "Plenty", "10.00", 10)
	scarce := f.product(t, "Scarce", "10.00", 1)
	empty := f.product(t, "Empty", "10.00", 0)

	_, err := f.service.Checkout(context.Background(), userID, items(plenty, 5, scarce, 2, empty, 1))

	var insufficient *domain.InsufficientStockError
	require.ErrorAs(t, err, &insufficient)
	expected := []domain.StockShortage{
		{ProductID: scarce, Requested: 2, Available: 1},
		{ProductID: empty, Requested: 1, Available: 0},
	}
	assert.ElementsMatch(t, expected, insufficient.Shortages)
	assert.Equal(t, 10, f.stock(t, plenty))
	assert.Equal(t, 1, f.stock(t, scarce))
	assert.Equal(t, 0, f.stock(t, empty))
	orders, orderItems := f.rowCounts(t)
	assert.Zero(t, orders)
	assert.Zero(t, orderItems)
}

type failingAfterCreateStore struct {
	service.OrderTxStore
}

var errInjected = errors.New("injected failure after writes")

func (s failingAfterCreateStore) CreateOrder(ctx context.Context, order domain.Order) (domain.Order, error) {
	if _, err := s.OrderTxStore.CreateOrder(ctx, order); err != nil {
		return domain.Order{}, err
	}
	return domain.Order{}, errInjected
}

func TestCheckoutRollsBackWritesOnLateFailure(t *testing.T) {
	f := setupOrders(t)
	userID := f.customer(t, "jane@example.com")
	productID := f.product(t, "Mouse", "100.00", 5)

	svc := service.NewOrderService(f.repo, func(ctx context.Context, fn func(service.OrderTxStore) error) error {
		return f.repo.InTx(ctx, func(tx *repository.OrderTx) error { return fn(failingAfterCreateStore{tx}) })
	})

	_, err := svc.Checkout(context.Background(), userID, items(productID, 3))

	assert.ErrorIs(t, err, errInjected)
	assert.Equal(t, 5, f.stock(t, productID))
	orders, orderItems := f.rowCounts(t)
	assert.Zero(t, orders)
	assert.Zero(t, orderItems)
}

func TestCheckoutMissingOrDeletedProducts(t *testing.T) {
	f := setupOrders(t)
	userID := f.customer(t, "jane@example.com")
	active := f.product(t, "Active", "10.00", 10)
	deleted := f.product(t, "Deleted", "10.00", 10)
	f.softDelete(t, deleted)
	missing := uuid.Must(uuid.NewV7())

	_, err := f.service.Checkout(context.Background(), userID, items(active, 1, missing, 1, deleted, 1))

	var notFound *domain.ProductsNotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, sortedPair(deleted, missing), notFound.ProductIDs)
	assert.Equal(t, 10, f.stock(t, active))
	assert.Equal(t, 10, f.stock(t, deleted))
	orders, _ := f.rowCounts(t)
	assert.Zero(t, orders)
}

func TestCheckoutConcurrentBuyersNeverOversell(t *testing.T) {
	f := setupOrders(t)
	userID := f.customer(t, "jane@example.com")
	productID := f.product(t, "Limited", "50000.00", 5)

	const buyers = 20
	var wg sync.WaitGroup
	results := make(chan error, buyers)
	start := make(chan struct{})
	for range buyers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.service.Checkout(context.Background(), userID, items(productID, 1))
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var succeeded, outOfStock int
	for err := range results {
		var insufficient *domain.InsufficientStockError
		switch {
		case err == nil:
			succeeded++
		case errors.As(err, &insufficient):
			outOfStock++
			assert.Equal(t, 1, insufficient.Shortages[0].Requested)
			assert.Zero(t, insufficient.Shortages[0].Available)
		default:
			t.Errorf("unexpected checkout error: %v", err)
		}
	}

	assert.Equal(t, 5, succeeded)
	assert.Equal(t, 15, outOfStock)
	assert.Zero(t, f.stock(t, productID))
	orders, orderItems := f.rowCounts(t)
	assert.Equal(t, 5, orders)
	assert.Equal(t, 5, orderItems)
}

func TestCheckoutOppositeItemOrderDoesNotDeadlock(t *testing.T) {
	f := setupOrders(t)
	userID := f.customer(t, "jane@example.com")
	productA := f.product(t, "A", "10.00", 1000)
	productB := f.product(t, "B", "20.00", 1000)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const rounds = 15
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)
	start := make(chan struct{})
	for range rounds {
		for _, order := range [][]domain.CheckoutItem{items(productA, 1, productB, 1), items(productB, 1, productA, 1)} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := f.service.Checkout(ctx, userID, order)
				errs <- err
			}()
		}
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			assert.NotEqual(t, deadlockDetected, pgErr.Code, "deadlock detected")
		}
		assert.NoError(t, err)
	}
	assert.Equal(t, 1000-rounds*2, f.stock(t, productA))
	assert.Equal(t, 1000-rounds*2, f.stock(t, productB))
}

func TestCancelRestoresStockOnce(t *testing.T) {
	f := setupOrders(t)
	ctx := context.Background()
	userID := f.customer(t, "jane@example.com")
	kept := f.product(t, "Kept", "10.00", 10)
	retired := f.product(t, "Retired", "20.00", 5)

	order, err := f.service.Checkout(ctx, userID, items(kept, 4, retired, 2))
	require.NoError(t, err)
	assert.Equal(t, 6, f.stock(t, kept))
	assert.Equal(t, 3, f.stock(t, retired))
	f.softDelete(t, retired)

	_, err = f.pool.Exec(ctx, `UPDATE orders SET updated_at = updated_at - interval '1 hour' WHERE id = $1`, order.ID)
	require.NoError(t, err)

	cancelled, err := f.service.UpdateStatus(ctx, order.ID, domain.OrderStatusCancelled)
	require.NoError(t, err)
	assert.Equal(t, domain.OrderStatusCancelled, cancelled.Status)
	assert.True(t, cancelled.UpdatedAt.After(cancelled.CreatedAt))
	assert.Len(t, cancelled.Items, 2)
	require.NotNil(t, cancelled.User)
	assert.Equal(t, "jane@example.com", cancelled.User.Email)
	assert.Equal(t, 10, f.stock(t, kept))
	assert.Equal(t, 5, f.stock(t, retired))

	for _, next := range []domain.OrderStatus{domain.OrderStatusCancelled, domain.OrderStatusCompleted} {
		_, err = f.service.UpdateStatus(ctx, order.ID, next)
		var transitionErr *domain.InvalidStatusTransitionError
		require.ErrorAs(t, err, &transitionErr)
		assert.Equal(t, domain.OrderStatusCancelled, transitionErr.From)
	}
	assert.Equal(t, 10, f.stock(t, kept))
	assert.Equal(t, 5, f.stock(t, retired))
}

func TestCompleteKeepsStockAndBlocksFurtherChanges(t *testing.T) {
	f := setupOrders(t)
	ctx := context.Background()
	userID := f.customer(t, "jane@example.com")
	productID := f.product(t, "Mouse", "10.00", 10)

	order, err := f.service.Checkout(ctx, userID, items(productID, 3))
	require.NoError(t, err)

	completed, err := f.service.UpdateStatus(ctx, order.ID, domain.OrderStatusCompleted)
	require.NoError(t, err)
	assert.Equal(t, domain.OrderStatusCompleted, completed.Status)
	assert.Equal(t, 7, f.stock(t, productID))

	_, err = f.service.UpdateStatus(ctx, order.ID, domain.OrderStatusCancelled)
	var transitionErr *domain.InvalidStatusTransitionError
	require.ErrorAs(t, err, &transitionErr)
	assert.Equal(t, 7, f.stock(t, productID))

	_, err = f.service.UpdateStatus(ctx, uuid.Must(uuid.NewV7()), domain.OrderStatusCompleted)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestOrderListAndGetVisibility(t *testing.T) {
	f := setupOrders(t)
	ctx := context.Background()
	jane := f.customer(t, "jane@example.com")
	john := f.customer(t, "john@example.com")
	productID := f.product(t, "Mouse", "10.00", 100)

	var janeOrders []uuid.UUID
	for qty := 1; qty <= 3; qty++ {
		order, err := f.service.Checkout(ctx, jane, items(productID, qty))
		require.NoError(t, err)
		janeOrders = append(janeOrders, order.ID)
	}
	johnOrder, err := f.service.Checkout(ctx, john, items(productID, 1))
	require.NoError(t, err)
	_, err = f.service.UpdateStatus(ctx, janeOrders[0], domain.OrderStatusCompleted)
	require.NoError(t, err)

	janePrincipal := auth.Principal{UserID: jane, Role: domain.RoleCustomer}
	johnPrincipal := auth.Principal{UserID: john, Role: domain.RoleCustomer}
	adminPrincipal := auth.Principal{UserID: uuid.Must(uuid.NewV7()), Role: domain.RoleAdmin}

	page, err := f.service.List(ctx, janePrincipal, domain.OrderFilter{Page: 1, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 3, page.Total)
	require.Len(t, page.Items, 3)
	assert.Equal(t, []uuid.UUID{janeOrders[2], janeOrders[1], janeOrders[0]}, []uuid.UUID{page.Items[0].ID, page.Items[1].ID, page.Items[2].ID})
	for _, order := range page.Items {
		assert.Equal(t, jane, order.UserID)
		require.Len(t, order.Items, 1)
		assert.Equal(t, order.ID, order.Items[0].OrderID)
	}
	assert.Equal(t, 3, page.Items[0].Items[0].Quantity)

	page, err = f.service.List(ctx, janePrincipal, domain.OrderFilter{Page: 2, Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, page.Total)
	assert.Equal(t, 2, page.TotalPages)
	require.Len(t, page.Items, 1)
	assert.Equal(t, janeOrders[0], page.Items[0].ID)

	pending := domain.OrderStatusPending
	page, err = f.service.List(ctx, janePrincipal, domain.OrderFilter{Page: 1, Limit: 10, Status: &pending})
	require.NoError(t, err)
	assert.Equal(t, 2, page.Total)

	completed := domain.OrderStatusCompleted
	page, err = f.service.List(ctx, adminPrincipal, domain.OrderFilter{Page: 1, Limit: 10, Status: &completed})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Total)

	page, err = f.service.List(ctx, adminPrincipal, domain.OrderFilter{Page: 1, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 4, page.Total)
	assert.Equal(t, johnOrder.ID, page.Items[0].ID)
	require.NotNil(t, page.Items[0].User)
	assert.Equal(t, domain.OrderUser{ID: john, Name: "Customer john@example.com", Email: "john@example.com"}, *page.Items[0].User)

	_, err = f.service.Get(ctx, johnPrincipal, janeOrders[0])
	assert.ErrorIs(t, err, domain.ErrNotFound)
	order, err := f.service.Get(ctx, janePrincipal, janeOrders[0])
	require.NoError(t, err)
	assert.Equal(t, domain.OrderStatusCompleted, order.Status)
	_, err = f.service.Get(ctx, adminPrincipal, janeOrders[0])
	assert.NoError(t, err)
}
