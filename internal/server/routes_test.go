package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/api"
	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/handler"
	"mini-ecommerce/internal/service"
)

type fakeDatabase struct {
	pingErr error
}

func (f fakeDatabase) Ping(context.Context) error { return f.pingErr }
func (f fakeDatabase) Pool() *pgxpool.Pool        { return nil }
func (f fakeDatabase) Close() error               { return nil }

func TestRegisterRoutesHealth(t *testing.T) {
	tests := []struct {
		name       string
		db         fakeDatabase
		wantStatus int
		wantBody   string
	}{
		{
			name:       "healthy",
			db:         fakeDatabase{},
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok","database":"ok"}`,
		},
		{
			name:       "database unreachable",
			db:         fakeDatabase{pingErr: errors.New("connection refused")},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"degraded","database":"unreachable"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{db: tt.db}
			rec := httptest.NewRecorder()
			s.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestRegisterRoutesRootIsNotFound(t *testing.T) {
	s := &Server{db: fakeDatabase{}}
	rec := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

type fakeAuthService struct{}

func (fakeAuthService) Register(context.Context, service.RegisterInput) (service.AuthResult, error) {
	return service.AuthResult{}, domain.ErrEmailAlreadyExists
}

func (fakeAuthService) Login(context.Context, service.LoginInput) (service.AuthResult, error) {
	return service.AuthResult{}, domain.ErrInvalidCredentials
}

func TestRegisterRoutesAuth(t *testing.T) {
	s := &Server{db: fakeDatabase{}, authHandler: handler.NewAuthHandler(fakeAuthService{})}

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "register", method: http.MethodPost, path: "/api/v1/auth/register", body: `{"name":"Jane","email":"jane@example.com","password":"supersecret"}`, wantStatus: http.StatusConflict},
		{name: "login", method: http.MethodPost, path: "/api/v1/auth/login", body: `{"email":"jane@example.com","password":"supersecret"}`, wantStatus: http.StatusUnauthorized},
		{name: "register wrong method", method: http.MethodGet, path: "/api/v1/auth/register", wantStatus: http.StatusMethodNotAllowed},
		{name: "unprefixed path", method: http.MethodPost, path: "/auth/login", body: `{}`, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			s.RegisterRoutes().ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

type fakeProductService struct {
	calls int
}

func (f *fakeProductService) List(context.Context, domain.ProductFilter) (service.ProductPage, error) {
	f.calls++
	return service.ProductPage{Page: 1, Limit: 10}, nil
}

func (f *fakeProductService) Get(_ context.Context, id uuid.UUID) (domain.Product, error) {
	f.calls++
	return domain.Product{ID: id}, nil
}

func (f *fakeProductService) Create(_ context.Context, input service.ProductInput) (domain.Product, error) {
	f.calls++
	return domain.Product{ID: uuid.Must(uuid.NewV7()), Name: input.Name}, nil
}

func (f *fakeProductService) Update(_ context.Context, id uuid.UUID, input service.ProductInput) (domain.Product, error) {
	f.calls++
	return domain.Product{ID: id, Name: input.Name}, nil
}

func (f *fakeProductService) Delete(context.Context, uuid.UUID) error {
	f.calls++
	return nil
}

func TestRegisterRoutesProductsAccess(t *testing.T) {
	tokens := auth.NewTokenManager(strings.Repeat("r", 32), time.Hour)
	adminToken, _, err := tokens.Issue(uuid.Must(uuid.NewV7()), domain.RoleAdmin)
	require.NoError(t, err)
	customerToken, _, err := tokens.Issue(uuid.Must(uuid.NewV7()), domain.RoleCustomer)
	require.NoError(t, err)

	productPath := "/api/v1/products/" + uuid.Must(uuid.NewV7()).String()
	body := `{"name":"A","description":"","price":1,"category":"B","stock":1}`

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		wantStatus int
		wantCalled bool
	}{
		{name: "list is public", method: http.MethodGet, path: "/api/v1/products", wantStatus: http.StatusOK, wantCalled: true},
		{name: "list with trailing slash", method: http.MethodGet, path: "/api/v1/products/", wantStatus: http.StatusOK, wantCalled: true},
		{name: "detail is public", method: http.MethodGet, path: productPath, wantStatus: http.StatusOK, wantCalled: true},
		{name: "create without token", method: http.MethodPost, path: "/api/v1/products", wantStatus: http.StatusUnauthorized},
		{name: "update without token", method: http.MethodPut, path: productPath, wantStatus: http.StatusUnauthorized},
		{name: "delete without token", method: http.MethodDelete, path: productPath, wantStatus: http.StatusUnauthorized},
		{name: "create as customer", method: http.MethodPost, path: "/api/v1/products", token: customerToken, wantStatus: http.StatusForbidden},
		{name: "update as customer", method: http.MethodPut, path: productPath, token: customerToken, wantStatus: http.StatusForbidden},
		{name: "delete as customer", method: http.MethodDelete, path: productPath, token: customerToken, wantStatus: http.StatusForbidden},
		{name: "create as admin", method: http.MethodPost, path: "/api/v1/products", token: adminToken, wantStatus: http.StatusCreated, wantCalled: true},
		{name: "update as admin", method: http.MethodPut, path: productPath, token: adminToken, wantStatus: http.StatusOK, wantCalled: true},
		{name: "delete as admin", method: http.MethodDelete, path: productPath, token: adminToken, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "invalid uuid checked after auth", method: http.MethodDelete, path: "/api/v1/products/not-a-uuid", wantStatus: http.StatusUnauthorized},
		{name: "patch not allowed", method: http.MethodPatch, path: productPath, token: adminToken, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := &fakeProductService{}
			s := &Server{db: fakeDatabase{}, tokenParser: tokens, productHandler: handler.NewProductHandler(products)}

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(body))
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			s.RegisterRoutes().ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tt.wantCalled, products.calls > 0)
		})
	}
}

type fakeOrderService struct {
	calls int
}

func (f *fakeOrderService) Checkout(_ context.Context, userID uuid.UUID, _ []domain.CheckoutItem) (domain.Order, error) {
	f.calls++
	return domain.Order{ID: uuid.Must(uuid.NewV7()), UserID: userID, Status: domain.OrderStatusPending}, nil
}

func (f *fakeOrderService) List(context.Context, auth.Principal, domain.OrderFilter) (service.OrderPage, error) {
	f.calls++
	return service.OrderPage{Page: 1, Limit: 10}, nil
}

func (f *fakeOrderService) Get(_ context.Context, _ auth.Principal, id uuid.UUID) (domain.Order, error) {
	f.calls++
	return domain.Order{ID: id}, nil
}

func (f *fakeOrderService) UpdateStatus(_ context.Context, id uuid.UUID, status domain.OrderStatus) (domain.Order, error) {
	f.calls++
	return domain.Order{ID: id, Status: status}, nil
}

func TestRegisterRoutesOrdersAccess(t *testing.T) {
	tokens := auth.NewTokenManager(strings.Repeat("o", 32), time.Hour)
	adminToken, _, err := tokens.Issue(uuid.Must(uuid.NewV7()), domain.RoleAdmin)
	require.NoError(t, err)
	customerToken, _, err := tokens.Issue(uuid.Must(uuid.NewV7()), domain.RoleCustomer)
	require.NoError(t, err)

	orderPath := "/api/v1/orders/" + uuid.Must(uuid.NewV7()).String()
	checkoutBody := `{"items":[{"product_id":"` + uuid.Must(uuid.NewV7()).String() + `","quantity":1}]}`
	statusBody := `{"status":"COMPLETED"}`

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		body       string
		wantStatus int
		wantCalled bool
	}{
		{name: "checkout without token", method: http.MethodPost, path: "/api/v1/orders", body: checkoutBody, wantStatus: http.StatusUnauthorized},
		{name: "list without token", method: http.MethodGet, path: "/api/v1/orders", wantStatus: http.StatusUnauthorized},
		{name: "detail without token", method: http.MethodGet, path: orderPath, wantStatus: http.StatusUnauthorized},
		{name: "status without token", method: http.MethodPatch, path: orderPath + "/status", body: statusBody, wantStatus: http.StatusUnauthorized},
		{name: "checkout as customer", method: http.MethodPost, path: "/api/v1/orders", token: customerToken, body: checkoutBody, wantStatus: http.StatusCreated, wantCalled: true},
		{name: "checkout as admin", method: http.MethodPost, path: "/api/v1/orders", token: adminToken, body: checkoutBody, wantStatus: http.StatusForbidden},
		{name: "list as customer", method: http.MethodGet, path: "/api/v1/orders", token: customerToken, wantStatus: http.StatusOK, wantCalled: true},
		{name: "list as admin", method: http.MethodGet, path: "/api/v1/orders", token: adminToken, wantStatus: http.StatusOK, wantCalled: true},
		{name: "detail as customer", method: http.MethodGet, path: orderPath, token: customerToken, wantStatus: http.StatusOK, wantCalled: true},
		{name: "status as customer", method: http.MethodPatch, path: orderPath + "/status", token: customerToken, body: statusBody, wantStatus: http.StatusForbidden},
		{name: "status as admin", method: http.MethodPatch, path: orderPath + "/status", token: adminToken, body: statusBody, wantStatus: http.StatusOK, wantCalled: true},
		{name: "put not allowed", method: http.MethodPut, path: orderPath, token: adminToken, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orders := &fakeOrderService{}
			s := &Server{db: fakeDatabase{}, tokenParser: tokens, orderHandler: handler.NewOrderHandler(orders)}

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			s.RegisterRoutes().ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tt.wantCalled, orders.calls > 0)
		})
	}
}

func TestRegisterRoutesDocs(t *testing.T) {
	s := &Server{db: fakeDatabase{}, docsHandler: handler.NewDocsHandler(api.OpenAPI)}

	tests := []struct {
		path        string
		wantStatus  int
		contentType string
		location    string
	}{
		{path: "/docs", wantStatus: http.StatusOK, contentType: "text/html; charset=utf-8"},
		{path: "/docs/openapi.yaml", wantStatus: http.StatusOK, contentType: "application/yaml"},
		{path: "/docs/", wantStatus: http.StatusMovedPermanently, location: "/docs"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.contentType != "" {
				assert.Equal(t, tt.contentType, rec.Header().Get("Content-Type"))
			}
			if tt.location != "" {
				assert.Equal(t, tt.location, rec.Header().Get("Location"))
			}
		})
	}
}

func TestRegisterRoutesFallbacksUseJSON(t *testing.T) {
	s := &Server{db: fakeDatabase{}}

	rec := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"Route not found"}}`, rec.Body.String())

	rec = httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/health", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"Method not allowed for this route"}}`, rec.Body.String())
}

func TestRegisterRoutesRecoversFromPanics(t *testing.T) {
	s := &Server{db: panickingDatabase{}}

	rec := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred"}}`, rec.Body.String())
}

type panickingDatabase struct {
	fakeDatabase
}

func (panickingDatabase) Ping(context.Context) error { panic("driver exploded") }
