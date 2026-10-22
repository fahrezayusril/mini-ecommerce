package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/domain"
)

var (
	productA = uuid.MustParse("01920000-0000-7000-8000-00000000000a")
	productB = uuid.MustParse("01920000-0000-7000-8000-00000000000b")
	productC = uuid.MustParse("01920000-0000-7000-8000-00000000000c")
)

type fakeOrderStore struct {
	products   []domain.Product
	order      domain.Order
	quantities []domain.CheckoutItem
	errs       map[string]error
	calls      []string
	lockedIDs  []uuid.UUID
	decrements []domain.CheckoutItem
	increments []domain.CheckoutItem
	created    *domain.Order
	newStatus  domain.OrderStatus
}

func (f *fakeOrderStore) record(name string) error {
	f.calls = append(f.calls, name)
	return f.errs[name]
}

func (f *fakeOrderStore) LockActiveProducts(_ context.Context, ids []uuid.UUID) ([]domain.Product, error) {
	f.lockedIDs = ids
	if err := f.record("LockActiveProducts"); err != nil {
		return nil, err
	}
	var out []domain.Product
	for _, p := range f.products {
		for _, id := range ids {
			if p.ID == id {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (f *fakeOrderStore) DecrementStock(_ context.Context, items []domain.CheckoutItem) error {
	f.decrements = items
	return f.record("DecrementStock")
}

func (f *fakeOrderStore) CreateOrder(_ context.Context, order domain.Order) (domain.Order, error) {
	f.created = &order
	return order, f.record("CreateOrder")
}

func (f *fakeOrderStore) LockOrder(context.Context, uuid.UUID) (domain.Order, error) {
	return f.order, f.record("LockOrder")
}

func (f *fakeOrderStore) OrderQuantities(context.Context, uuid.UUID) ([]domain.CheckoutItem, error) {
	return f.quantities, f.record("OrderQuantities")
}

func (f *fakeOrderStore) LockProducts(_ context.Context, ids []uuid.UUID) error {
	f.lockedIDs = ids
	return f.record("LockProducts")
}

func (f *fakeOrderStore) IncrementStock(_ context.Context, items []domain.CheckoutItem) error {
	f.increments = items
	return f.record("IncrementStock")
}

func (f *fakeOrderStore) UpdateStatus(_ context.Context, _ uuid.UUID, status domain.OrderStatus) error {
	f.newStatus = status
	return f.record("UpdateStatus")
}

type fakeOrderReader struct {
	filter    domain.OrderFilter
	ownerID   *uuid.UUID
	orders    []domain.Order
	total     int
	findOrder domain.Order
	err       error
}

func (f *fakeOrderReader) List(_ context.Context, filter domain.OrderFilter) ([]domain.Order, int, error) {
	f.filter = filter
	return f.orders, f.total, f.err
}

func (f *fakeOrderReader) FindByID(_ context.Context, _ uuid.UUID, ownerID *uuid.UUID) (domain.Order, error) {
	f.ownerID = ownerID
	return f.findOrder, f.err
}

func newOrderTestService(store *fakeOrderStore, reader *fakeOrderReader) (*OrderService, *int) {
	txCount := 0
	inTx := func(_ context.Context, fn func(OrderTxStore) error) error {
		txCount++
		return fn(store)
	}
	return NewOrderService(reader, inTx), &txCount
}

func product(id uuid.UUID, name, price string, stock int) domain.Product {
	return domain.Product{ID: id, Name: name, Price: decimal.RequireFromString(price), Stock: stock}
}

func TestMergeCheckoutItems(t *testing.T) {
	merged := MergeCheckoutItems([]domain.CheckoutItem{
		{ProductID: productC, Quantity: 1},
		{ProductID: productA, Quantity: 2},
		{ProductID: productC, Quantity: 4},
		{ProductID: productB, Quantity: 3},
		{ProductID: productA, Quantity: 1},
	})

	assert.Equal(t, []domain.CheckoutItem{
		{ProductID: productA, Quantity: 3},
		{ProductID: productB, Quantity: 3},
		{ProductID: productC, Quantity: 5},
	}, merged)
}

func TestValidateCheckoutItems(t *testing.T) {
	tooMany := make([]domain.CheckoutItem, MaxCheckoutItems+1)
	for i := range tooMany {
		tooMany[i] = domain.CheckoutItem{ProductID: productA, Quantity: 1}
	}
	maxItems := tooMany[:MaxCheckoutItems]

	tests := []struct {
		name      string
		items     []domain.CheckoutItem
		wantField string
	}{
		{name: "empty", items: nil, wantField: "items"},
		{name: "too many", items: tooMany, wantField: "items"},
		{name: "nil product id", items: []domain.CheckoutItem{{ProductID: productA, Quantity: 1}, {Quantity: 1}}, wantField: "items[1].product_id"},
		{name: "zero quantity", items: []domain.CheckoutItem{{ProductID: productA, Quantity: 0}}, wantField: "items[0].quantity"},
		{name: "negative quantity", items: []domain.CheckoutItem{{ProductID: productA, Quantity: -2}}, wantField: "items[0].quantity"},
		{name: "quantity too large", items: []domain.CheckoutItem{{ProductID: productA, Quantity: MaxCheckoutQuantity + 1}}, wantField: "items[0].quantity"},
		{name: "valid max items", items: maxItems},
		{name: "valid max quantity", items: []domain.CheckoutItem{{ProductID: productA, Quantity: MaxCheckoutQuantity}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCheckoutItems(tt.items)
			if tt.wantField == "" {
				assert.NoError(t, err)
				return
			}
			var validationErr *domain.ValidationError
			require.ErrorAs(t, err, &validationErr)
			assert.Equal(t, tt.wantField, validationErr.Field)
		})
	}
}

func TestCheckoutInvalidItemsSkipTransaction(t *testing.T) {
	svc, txCount := newOrderTestService(&fakeOrderStore{}, &fakeOrderReader{})

	_, err := svc.Checkout(context.Background(), uuid.Must(uuid.NewV7()), nil)

	var validationErr *domain.ValidationError
	assert.ErrorAs(t, err, &validationErr)
	assert.Zero(t, *txCount)
}

func TestCheckoutSuccessComputesTotalsFromDatabasePrices(t *testing.T) {
	store := &fakeOrderStore{products: []domain.Product{
		product(productA, "Pencil", "0.10", 10),
		product(productB, "Notebook", "12500.55", 4),
	}}
	svc, txCount := newOrderTestService(store, &fakeOrderReader{})
	userID := uuid.Must(uuid.NewV7())

	order, err := svc.Checkout(context.Background(), userID, []domain.CheckoutItem{
		{ProductID: productB, Quantity: 1},
		{ProductID: productA, Quantity: 2},
		{ProductID: productB, Quantity: 2},
		{ProductID: productA, Quantity: 1},
	})

	require.NoError(t, err)
	assert.Equal(t, 1, *txCount)
	assert.Equal(t, []string{"LockActiveProducts", "DecrementStock", "CreateOrder"}, store.calls)
	assert.Equal(t, []uuid.UUID{productA, productB}, store.lockedIDs)
	assert.Equal(t, []domain.CheckoutItem{{ProductID: productA, Quantity: 3}, {ProductID: productB, Quantity: 3}}, store.decrements)

	assert.Equal(t, uuid.Version(7), order.ID.Version())
	assert.Equal(t, userID, order.UserID)
	assert.Equal(t, domain.OrderStatusPending, order.Status)
	assert.Equal(t, "37501.95", order.TotalAmount.StringFixed(2))
	require.Len(t, order.Items, 2)

	pencil := order.Items[0]
	assert.Equal(t, productA, pencil.ProductID)
	assert.Equal(t, "Pencil", pencil.ProductName)
	assert.Equal(t, 3, pencil.Quantity)
	assert.Equal(t, "0.10", pencil.UnitPrice.StringFixed(2))
	assert.Equal(t, "0.30", pencil.Subtotal.StringFixed(2))
	assert.True(t, pencil.Subtotal.Equal(decimal.RequireFromString("0.3")))
	assert.Equal(t, order.ID, pencil.OrderID)
	assert.Equal(t, uuid.Version(7), pencil.ID.Version())

	notebook := order.Items[1]
	assert.Equal(t, "37501.65", notebook.Subtotal.StringFixed(2))
}

func TestCheckoutCollectsAllMissingProducts(t *testing.T) {
	store := &fakeOrderStore{products: []domain.Product{product(productB, "Notebook", "1", 10)}}
	svc, _ := newOrderTestService(store, &fakeOrderReader{})

	_, err := svc.Checkout(context.Background(), uuid.Must(uuid.NewV7()), []domain.CheckoutItem{
		{ProductID: productC, Quantity: 1},
		{ProductID: productB, Quantity: 1},
		{ProductID: productA, Quantity: 1},
	})

	var notFound *domain.ProductsNotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, []uuid.UUID{productA, productC}, notFound.ProductIDs)
	assert.Equal(t, []string{"LockActiveProducts"}, store.calls)
}

func TestCheckoutCollectsAllShortages(t *testing.T) {
	store := &fakeOrderStore{products: []domain.Product{
		product(productA, "A", "1", 1),
		product(productB, "B", "1", 10),
		product(productC, "C", "1", 0),
	}}
	svc, _ := newOrderTestService(store, &fakeOrderReader{})

	_, err := svc.Checkout(context.Background(), uuid.Must(uuid.NewV7()), []domain.CheckoutItem{
		{ProductID: productA, Quantity: 1},
		{ProductID: productC, Quantity: 2},
		{ProductID: productA, Quantity: 1},
		{ProductID: productB, Quantity: 10},
	})

	var insufficient *domain.InsufficientStockError
	require.ErrorAs(t, err, &insufficient)
	assert.Equal(t, []domain.StockShortage{
		{ProductID: productA, Requested: 2, Available: 1},
		{ProductID: productC, Requested: 2, Available: 0},
	}, insufficient.Shortages)
	assert.Equal(t, []string{"LockActiveProducts"}, store.calls)
}

func TestCheckoutRejectsTotalAboveNumericLimit(t *testing.T) {
	store := &fakeOrderStore{products: []domain.Product{product(productA, "Gold", "9999999999.99", 5)}}
	svc, _ := newOrderTestService(store, &fakeOrderReader{})

	_, err := svc.Checkout(context.Background(), uuid.Must(uuid.NewV7()), []domain.CheckoutItem{{ProductID: productA, Quantity: 2}})

	var validationErr *domain.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "items", validationErr.Field)
	assert.Equal(t, []string{"LockActiveProducts"}, store.calls)
}

func TestCheckoutStopsOnStoreError(t *testing.T) {
	for _, failing := range []string{"LockActiveProducts", "DecrementStock", "CreateOrder"} {
		t.Run(failing, func(t *testing.T) {
			storeErr := errors.New("boom")
			store := &fakeOrderStore{
				products: []domain.Product{product(productA, "A", "1", 5)},
				errs:     map[string]error{failing: storeErr},
			}
			svc, _ := newOrderTestService(store, &fakeOrderReader{})

			_, err := svc.Checkout(context.Background(), uuid.Must(uuid.NewV7()), []domain.CheckoutItem{{ProductID: productA, Quantity: 1}})

			assert.ErrorIs(t, err, storeErr)
			assert.Equal(t, failing, store.calls[len(store.calls)-1])
		})
	}
}

func TestCanTransition(t *testing.T) {
	statuses := []domain.OrderStatus{domain.OrderStatusPending, domain.OrderStatusCompleted, domain.OrderStatusCancelled}
	allowed := map[[2]domain.OrderStatus]bool{
		{domain.OrderStatusPending, domain.OrderStatusCompleted}: true,
		{domain.OrderStatusPending, domain.OrderStatusCancelled}: true,
	}

	for _, from := range statuses {
		for _, to := range statuses {
			assert.Equal(t, allowed[[2]domain.OrderStatus{from, to}], CanTransition(from, to), "%s -> %s", from, to)
		}
	}
}

func TestUpdateStatusComplete(t *testing.T) {
	orderID := uuid.Must(uuid.NewV7())
	store := &fakeOrderStore{order: domain.Order{ID: orderID, Status: domain.OrderStatusPending}}
	reader := &fakeOrderReader{findOrder: domain.Order{ID: orderID, Status: domain.OrderStatusCompleted}}
	svc, _ := newOrderTestService(store, reader)

	order, err := svc.UpdateStatus(context.Background(), orderID, domain.OrderStatusCompleted)

	require.NoError(t, err)
	assert.Equal(t, domain.OrderStatusCompleted, order.Status)
	assert.Equal(t, []string{"LockOrder", "UpdateStatus"}, store.calls)
	assert.Equal(t, domain.OrderStatusCompleted, store.newStatus)
	assert.Nil(t, reader.ownerID)
}

func TestUpdateStatusCancelRestoresStock(t *testing.T) {
	orderID := uuid.Must(uuid.NewV7())
	quantities := []domain.CheckoutItem{{ProductID: productA, Quantity: 2}, {ProductID: productB, Quantity: 1}}
	store := &fakeOrderStore{order: domain.Order{ID: orderID, Status: domain.OrderStatusPending}, quantities: quantities}
	svc, _ := newOrderTestService(store, &fakeOrderReader{})

	_, err := svc.UpdateStatus(context.Background(), orderID, domain.OrderStatusCancelled)

	require.NoError(t, err)
	assert.Equal(t, []string{"LockOrder", "OrderQuantities", "LockProducts", "IncrementStock", "UpdateStatus"}, store.calls)
	assert.Equal(t, []uuid.UUID{productA, productB}, store.lockedIDs)
	assert.Equal(t, quantities, store.increments)
	assert.Equal(t, domain.OrderStatusCancelled, store.newStatus)
}

func TestUpdateStatusRejectsInvalidTransitions(t *testing.T) {
	for _, from := range []domain.OrderStatus{domain.OrderStatusCompleted, domain.OrderStatusCancelled} {
		for _, to := range []domain.OrderStatus{domain.OrderStatusCompleted, domain.OrderStatusCancelled} {
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				store := &fakeOrderStore{order: domain.Order{Status: from}}
				svc, _ := newOrderTestService(store, &fakeOrderReader{})

				_, err := svc.UpdateStatus(context.Background(), uuid.Must(uuid.NewV7()), to)

				var transitionErr *domain.InvalidStatusTransitionError
				require.ErrorAs(t, err, &transitionErr)
				assert.Equal(t, from, transitionErr.From)
				assert.Equal(t, to, transitionErr.To)
				assert.Equal(t, []string{"LockOrder"}, store.calls)
			})
		}
	}
}

func TestUpdateStatusOrderNotFound(t *testing.T) {
	store := &fakeOrderStore{errs: map[string]error{"LockOrder": domain.ErrNotFound}}
	svc, _ := newOrderTestService(store, &fakeOrderReader{})

	_, err := svc.UpdateStatus(context.Background(), uuid.Must(uuid.NewV7()), domain.OrderStatusCompleted)

	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestOrderListAndGetScopeByRole(t *testing.T) {
	customerID := uuid.Must(uuid.NewV7())
	reader := &fakeOrderReader{total: 21}
	svc, _ := newOrderTestService(&fakeOrderStore{}, reader)
	ctx := context.Background()

	page, err := svc.List(ctx, auth.Principal{UserID: customerID, Role: domain.RoleCustomer}, domain.OrderFilter{Page: 2, Limit: 10})
	require.NoError(t, err)
	require.NotNil(t, reader.filter.UserID)
	assert.Equal(t, customerID, *reader.filter.UserID)
	assert.Equal(t, OrderPage{Page: 2, Limit: 10, Total: 21, TotalPages: 3}, page)

	_, err = svc.List(ctx, auth.Principal{UserID: uuid.Must(uuid.NewV7()), Role: domain.RoleAdmin}, domain.OrderFilter{Page: 1, Limit: 10})
	require.NoError(t, err)
	assert.Nil(t, reader.filter.UserID)

	_, err = svc.Get(ctx, auth.Principal{UserID: customerID, Role: domain.RoleCustomer}, uuid.Must(uuid.NewV7()))
	require.NoError(t, err)
	require.NotNil(t, reader.ownerID)
	assert.Equal(t, customerID, *reader.ownerID)

	_, err = svc.Get(ctx, auth.Principal{UserID: uuid.Must(uuid.NewV7()), Role: domain.RoleAdmin}, uuid.Must(uuid.NewV7()))
	require.NoError(t, err)
	assert.Nil(t, reader.ownerID)
}
