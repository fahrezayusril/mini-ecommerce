package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/middleware"
	"mini-ecommerce/internal/service"
)

var (
	testOrderID    = uuid.MustParse("01920000-0000-7000-8000-0000000000d1")
	testItemID     = uuid.MustParse("01920000-0000-7000-8000-0000000000e1")
	testCustomerID = uuid.MustParse("01920000-0000-7000-8000-0000000000c1")
	testAdminID    = uuid.MustParse("01920000-0000-7000-8000-0000000000a1")
	testOrderTime  = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

	customerPrincipal = auth.Principal{UserID: testCustomerID, Role: domain.RoleCustomer}
	adminPrincipal    = auth.Principal{UserID: testAdminID, Role: domain.RoleAdmin}
)

type fakeOrderService struct {
	err           error
	order         domain.Order
	page          service.OrderPage
	checkoutUser  uuid.UUID
	checkoutItems []domain.CheckoutItem
	listFilter    *domain.OrderFilter
	principal     auth.Principal
	status        domain.OrderStatus
	calls         int
}

func (f *fakeOrderService) Checkout(_ context.Context, userID uuid.UUID, items []domain.CheckoutItem) (domain.Order, error) {
	f.calls++
	f.checkoutUser = userID
	f.checkoutItems = items
	return f.order, f.err
}

func (f *fakeOrderService) List(_ context.Context, principal auth.Principal, filter domain.OrderFilter) (service.OrderPage, error) {
	f.calls++
	f.principal = principal
	f.listFilter = &filter
	return f.page, f.err
}

func (f *fakeOrderService) Get(_ context.Context, principal auth.Principal, _ uuid.UUID) (domain.Order, error) {
	f.calls++
	f.principal = principal
	return f.order, f.err
}

func (f *fakeOrderService) UpdateStatus(_ context.Context, _ uuid.UUID, status domain.OrderStatus) (domain.Order, error) {
	f.calls++
	f.status = status
	return f.order, f.err
}

func sampleOrder() domain.Order {
	return domain.Order{
		ID:          testOrderID,
		UserID:      testCustomerID,
		Status:      domain.OrderStatusPending,
		TotalAmount: decimal.RequireFromString("298000"),
		User:        &domain.OrderUser{ID: testCustomerID, Name: "Jane Doe", Email: "jane@example.com"},
		Items: []domain.OrderItem{{
			ID:          testItemID,
			OrderID:     testOrderID,
			ProductID:   testProductID,
			ProductName: "Wireless Mouse",
			Quantity:    2,
			UnitPrice:   decimal.RequireFromString("149000"),
			Subtotal:    decimal.RequireFromString("298000"),
		}},
		CreatedAt: testOrderTime,
		UpdatedAt: testOrderTime,
	}
}

const sampleOrderCustomerJSON = `{
	"id": "01920000-0000-7000-8000-0000000000d1",
	"user_id": "01920000-0000-7000-8000-0000000000c1",
	"status": "PENDING",
	"total_amount": 298000.00,
	"items": [{
		"id": "01920000-0000-7000-8000-0000000000e1",
		"product_id": "01920000-0000-7000-8000-0000000000aa",
		"product_name": "Wireless Mouse",
		"quantity": 2,
		"unit_price": 149000.00,
		"subtotal": 298000.00
	}],
	"created_at": "2026-10-22T10:00:00Z",
	"updated_at": "2026-10-22T10:00:00Z"
}`

const sampleOrderAdminJSON = `{
	"id": "01920000-0000-7000-8000-0000000000d1",
	"user_id": "01920000-0000-7000-8000-0000000000c1",
	"user": {"id": "01920000-0000-7000-8000-0000000000c1", "name": "Jane Doe", "email": "jane@example.com"},
	"status": "PENDING",
	"total_amount": 298000.00,
	"items": [{
		"id": "01920000-0000-7000-8000-0000000000e1",
		"product_id": "01920000-0000-7000-8000-0000000000aa",
		"product_name": "Wireless Mouse",
		"quantity": 2,
		"unit_price": 149000.00,
		"subtotal": 298000.00
	}],
	"created_at": "2026-10-22T10:00:00Z",
	"updated_at": "2026-10-22T10:00:00Z"
}`

func orderRouter(h *OrderHandler) http.Handler {
	r := chi.NewRouter()
	r.Post("/orders", h.Create)
	r.Get("/orders", h.List)
	r.Get("/orders/{id}", h.Get)
	r.Patch("/orders/{id}/status", h.UpdateStatus)
	return r
}

func doOrderRequest(h *OrderHandler, principal *auth.Principal, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if principal != nil {
		req = req.WithContext(middleware.WithPrincipal(req.Context(), *principal))
	}
	rec := httptest.NewRecorder()
	orderRouter(h).ServeHTTP(rec, req)
	return rec
}

func TestCreateOrderSuccess(t *testing.T) {
	svc := &fakeOrderService{order: sampleOrder()}
	body := `{"items":[{"product_id":"01920000-0000-7000-8000-0000000000aa","quantity":1},{"product_id":"01920000-0000-7000-8000-0000000000AA","quantity":1}]}`

	rec := doOrderRequest(NewOrderHandler(svc), &customerPrincipal, http.MethodPost, "/orders", body)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"data":`+sampleOrderCustomerJSON+`}`, rec.Body.String())
	assert.Equal(t, testCustomerID, svc.checkoutUser)
	assert.Equal(t, []domain.CheckoutItem{
		{ProductID: testProductID, Quantity: 1},
		{ProductID: testProductID, Quantity: 1},
	}, svc.checkoutItems)
}

func TestCreateOrderValidation(t *testing.T) {
	validID := testProductID.String()
	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = `{"product_id":"` + validID + `","quantity":1}`
	}

	tests := []struct {
		name string
		body string
		want []FieldError
	}{
		{name: "items missing", body: `{}`, want: []FieldError{{Field: "items", Message: "is required"}}},
		{name: "items null", body: `{"items":null}`, want: []FieldError{{Field: "items", Message: "is required"}}},
		{name: "items empty", body: `{"items":[]}`, want: []FieldError{{Field: "items", Message: "must contain at least 1 item"}}},
		{name: "too many items", body: `{"items":[` + strings.Join(tooMany, ",") + `]}`, want: []FieldError{{Field: "items", Message: "must contain at most 50 items"}}},
		{
			name: "item fields missing",
			body: `{"items":[{}]}`,
			want: []FieldError{
				{Field: "items[0].product_id", Message: "is required"},
				{Field: "items[0].quantity", Message: "is required"},
			},
		},
		{
			name: "invalid product id and quantities",
			body: `{"items":[{"product_id":"abc","quantity":1},{"product_id":"` + validID + `","quantity":0},{"product_id":"` + validID + `","quantity":1000001}]}`,
			want: []FieldError{
				{Field: "items[0].product_id", Message: "must be a valid UUID"},
				{Field: "items[1].quantity", Message: "must be greater than or equal to 1"},
				{Field: "items[2].quantity", Message: "must be less than or equal to 1000000"},
			},
		},
		{
			name: "negative quantity",
			body: `{"items":[{"product_id":"` + validID + `","quantity":-3}]}`,
			want: []FieldError{{Field: "items[0].quantity", Message: "must be greater than or equal to 1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeOrderService{}
			rec := doOrderRequest(NewOrderHandler(svc), &customerPrincipal, http.MethodPost, "/orders", tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			envelope := decodeError(t, rec)
			assert.Equal(t, "VALIDATION_ERROR", envelope.Error.Code)
			assert.Equal(t, tt.want, envelope.Error.Details)
			assert.Zero(t, svc.calls)
		})
	}
}

func TestCreateOrderInvalidJSON(t *testing.T) {
	tests := []struct {
		body    string
		message string
	}{
		{body: `{"items":[{"product_id":"x","quantity":1.5}]}`, message: `Field "items.quantity" has an invalid type`},
		{body: `{"items":[{"product_id":"x","quantity":"2"}]}`, message: `Field "items.quantity" has an invalid type`},
		{body: `{"items":{}}`, message: `Field "items" has an invalid type`},
		{body: `{"items":[],"user_id":"x"}`, message: `Request body contains unknown field "user_id"`},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			svc := &fakeOrderService{}
			rec := doOrderRequest(NewOrderHandler(svc), &customerPrincipal, http.MethodPost, "/orders", tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			envelope := decodeError(t, rec)
			assert.Equal(t, "INVALID_JSON", envelope.Error.Code)
			assert.Equal(t, tt.message, envelope.Error.Message)
		})
	}
}

func TestOrderErrorMapping(t *testing.T) {
	missingA := uuid.MustParse("01920000-0000-7000-8000-0000000000f1")
	missingB := uuid.MustParse("01920000-0000-7000-8000-0000000000f2")

	tests := []struct {
		name     string
		err      error
		status   int
		wantBody string
	}{
		{
			name:     "products not found",
			err:      &domain.ProductsNotFoundError{ProductIDs: []uuid.UUID{missingA, missingB}},
			status:   http.StatusNotFound,
			wantBody: `{"error":{"code":"PRODUCT_NOT_FOUND","message":"One or more products were not found","details":[{"product_id":"01920000-0000-7000-8000-0000000000f1"},{"product_id":"01920000-0000-7000-8000-0000000000f2"}]}}`,
		},
		{
			name: "insufficient stock",
			err: &domain.InsufficientStockError{Shortages: []domain.StockShortage{
				{ProductID: missingA, Requested: 3, Available: 1},
				{ProductID: missingB, Requested: 2, Available: 0},
			}},
			status:   http.StatusConflict,
			wantBody: `{"error":{"code":"INSUFFICIENT_STOCK","message":"Insufficient stock for one or more products","details":[{"product_id":"01920000-0000-7000-8000-0000000000f1","requested":3,"available":1},{"product_id":"01920000-0000-7000-8000-0000000000f2","requested":2,"available":0}]}}`,
		},
		{
			name:     "total too large",
			err:      &domain.ValidationError{Field: "items", Message: "order total must not exceed 9999999999.99"},
			status:   http.StatusBadRequest,
			wantBody: `{"error":{"code":"VALIDATION_ERROR","message":"Request validation failed","details":[{"field":"items","message":"order total must not exceed 9999999999.99"}]}}`,
		},
		{
			name:     "internal",
			err:      errors.New("pq: deadlock detected"),
			status:   http.StatusInternalServerError,
			wantBody: `{"error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeOrderService{err: tt.err}
			body := `{"items":[{"product_id":"` + testProductID.String() + `","quantity":1}]}`
			rec := doOrderRequest(NewOrderHandler(svc), &customerPrincipal, http.MethodPost, "/orders", body)

			assert.Equal(t, tt.status, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestListOrdersCustomerAndAdmin(t *testing.T) {
	tests := []struct {
		name      string
		principal auth.Principal
		wantOrder string
	}{
		{name: "customer sees no user", principal: customerPrincipal, wantOrder: sampleOrderCustomerJSON},
		{name: "admin sees user", principal: adminPrincipal, wantOrder: sampleOrderAdminJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeOrderService{page: service.OrderPage{Items: []domain.Order{sampleOrder()}, Page: 1, Limit: 10, Total: 1, TotalPages: 1}}
			rec := doOrderRequest(NewOrderHandler(svc), &tt.principal, http.MethodGet, "/orders?status=PENDING", "")

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t, `{"data":[`+tt.wantOrder+`],"meta":{"page":1,"limit":10,"total":1,"total_pages":1}}`, rec.Body.String())
			assert.Equal(t, tt.principal, svc.principal)
			require.NotNil(t, svc.listFilter.Status)
			assert.Equal(t, domain.OrderStatusPending, *svc.listFilter.Status)
		})
	}
}

func TestListOrdersEmptyIsArray(t *testing.T) {
	svc := &fakeOrderService{page: service.OrderPage{Page: 3, Limit: 5}}

	rec := doOrderRequest(NewOrderHandler(svc), &customerPrincipal, http.MethodGet, "/orders?page=3&limit=5", "")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":[],"meta":{"page":3,"limit":5,"total":0,"total_pages":0}}`, rec.Body.String())
	assert.Equal(t, domain.OrderFilter{Page: 3, Limit: 5}, *svc.listFilter)
}

func TestListOrdersRejectsInvalidQuery(t *testing.T) {
	tests := []struct {
		target string
		want   FieldError
	}{
		{target: "/orders?status=pending", want: FieldError{Field: "status", Message: "must be one of PENDING, COMPLETED, CANCELLED"}},
		{target: "/orders?status=SHIPPED", want: FieldError{Field: "status", Message: "must be one of PENDING, COMPLETED, CANCELLED"}},
		{target: "/orders?status=", want: FieldError{Field: "status", Message: "must not be empty"}},
		{target: "/orders?page=0", want: FieldError{Field: "page", Message: "must be greater than or equal to 1"}},
		{target: "/orders?limit=101", want: FieldError{Field: "limit", Message: "must be less than or equal to 100"}},
		{target: "/orders?status=PENDING;DROP", want: FieldError{Field: "query", Message: "must be a valid URL-encoded query string"}},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			svc := &fakeOrderService{}
			rec := doOrderRequest(NewOrderHandler(svc), &adminPrincipal, http.MethodGet, tt.target, "")

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, []FieldError{tt.want}, decodeError(t, rec).Error.Details)
			assert.Zero(t, svc.calls)
		})
	}
}

func TestGetOrder(t *testing.T) {
	svc := &fakeOrderService{order: sampleOrder()}

	rec := doOrderRequest(NewOrderHandler(svc), &customerPrincipal, http.MethodGet, "/orders/"+testOrderID.String(), "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":`+sampleOrderCustomerJSON+`}`, rec.Body.String())

	rec = doOrderRequest(NewOrderHandler(svc), &adminPrincipal, http.MethodGet, "/orders/"+testOrderID.String(), "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":`+sampleOrderAdminJSON+`}`, rec.Body.String())
}

func TestGetOrderNotFoundAndInvalidID(t *testing.T) {
	svc := &fakeOrderService{err: domain.ErrNotFound}

	rec := doOrderRequest(NewOrderHandler(svc), &customerPrincipal, http.MethodGet, "/orders/"+testOrderID.String(), "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"Order not found"}}`, rec.Body.String())

	svc = &fakeOrderService{}
	rec = doOrderRequest(NewOrderHandler(svc), &customerPrincipal, http.MethodGet, "/orders/not-a-uuid", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, []FieldError{{Field: "id", Message: "must be a valid UUID"}}, decodeError(t, rec).Error.Details)
	assert.Zero(t, svc.calls)
}

func TestUpdateOrderStatus(t *testing.T) {
	for _, status := range []domain.OrderStatus{domain.OrderStatusCompleted, domain.OrderStatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			order := sampleOrder()
			order.Status = status
			svc := &fakeOrderService{order: order}

			rec := doOrderRequest(NewOrderHandler(svc), &adminPrincipal, http.MethodPatch, "/orders/"+testOrderID.String()+"/status", `{"status":"`+string(status)+`"}`)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), `"status":"`+string(status)+`"`)
			assert.Contains(t, rec.Body.String(), `"user":{`)
			assert.Equal(t, status, svc.status)
		})
	}
}

func TestUpdateOrderStatusValidation(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   string
		code   string
		want   []FieldError
	}{
		{name: "missing status", body: `{}`, code: "VALIDATION_ERROR", want: []FieldError{{Field: "status", Message: "is required"}}},
		{name: "pending", body: `{"status":"PENDING"}`, code: "VALIDATION_ERROR", want: []FieldError{{Field: "status", Message: "must be one of COMPLETED, CANCELLED"}}},
		{name: "lowercase", body: `{"status":"completed"}`, code: "VALIDATION_ERROR", want: []FieldError{{Field: "status", Message: "must be one of COMPLETED, CANCELLED"}}},
		{name: "unknown", body: `{"status":"SHIPPED"}`, code: "VALIDATION_ERROR", want: []FieldError{{Field: "status", Message: "must be one of COMPLETED, CANCELLED"}}},
		{name: "invalid id", target: "/orders/123/status", body: `{"status":"COMPLETED"}`, code: "VALIDATION_ERROR", want: []FieldError{{Field: "id", Message: "must be a valid UUID"}}},
		{name: "number status", body: `{"status":1}`, code: "INVALID_JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.target
			if target == "" {
				target = "/orders/" + testOrderID.String() + "/status"
			}
			svc := &fakeOrderService{}
			rec := doOrderRequest(NewOrderHandler(svc), &adminPrincipal, http.MethodPatch, target, tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			envelope := decodeError(t, rec)
			assert.Equal(t, tt.code, envelope.Error.Code)
			if tt.want != nil {
				assert.Equal(t, tt.want, envelope.Error.Details)
			}
			assert.Zero(t, svc.calls)
		})
	}
}

func TestUpdateOrderStatusErrors(t *testing.T) {
	target := "/orders/" + testOrderID.String() + "/status"

	svc := &fakeOrderService{err: &domain.InvalidStatusTransitionError{From: domain.OrderStatusCancelled, To: domain.OrderStatusCompleted}}
	rec := doOrderRequest(NewOrderHandler(svc), &adminPrincipal, http.MethodPatch, target, `{"status":"COMPLETED"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"INVALID_STATUS_TRANSITION","message":"Order status cannot change from CANCELLED to COMPLETED","details":[{"current_status":"CANCELLED","requested_status":"COMPLETED"}]}}`, rec.Body.String())

	svc = &fakeOrderService{err: domain.ErrNotFound}
	rec = doOrderRequest(NewOrderHandler(svc), &adminPrincipal, http.MethodPatch, target, `{"status":"CANCELLED"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOrderHandlersRequirePrincipal(t *testing.T) {
	svc := &fakeOrderService{}
	h := NewOrderHandler(svc)

	for _, rec := range []*httptest.ResponseRecorder{
		doOrderRequest(h, nil, http.MethodPost, "/orders", `{}`),
		doOrderRequest(h, nil, http.MethodGet, "/orders", ""),
		doOrderRequest(h, nil, http.MethodGet, "/orders/"+testOrderID.String(), ""),
		doOrderRequest(h, nil, http.MethodPatch, "/orders/"+testOrderID.String()+"/status", `{"status":"COMPLETED"}`),
	} {
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	}
	assert.Zero(t, svc.calls)
}
