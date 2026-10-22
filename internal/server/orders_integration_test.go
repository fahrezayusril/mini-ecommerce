package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/config"
	"mini-ecommerce/internal/database"
)

type orderAPI struct {
	t       *testing.T
	pool    *pgxpool.Pool
	handler http.Handler
}

func setupOrderAPI(t *testing.T) *orderAPI {
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
	_, err = database.Seed(ctx, pool,
		config.SeedUser{Name: "Admin", Email: "admin@example.com", Password: "supersecret123"},
		config.SeedUser{Name: "Customer", Email: "customer@example.com", Password: "supersecret456"},
	)
	require.NoError(t, err)

	httpServer, err := NewServer(config.Config{Port: 8080, JWTSecret: strings.Repeat("e", 32), JWTTTL: time.Hour}, database.New(pool))
	require.NoError(t, err)
	return &orderAPI{t: t, pool: pool, handler: httpServer.Handler}
}

func (a *orderAPI) do(method, target, token, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a *orderAPI) token(path, body string, wantStatus int) string {
	a.t.Helper()
	rec := a.do(http.MethodPost, path, "", body)
	require.Equal(a.t, wantStatus, rec.Code, rec.Body.String())
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(a.t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.Data.Token
}

func (a *orderAPI) productID(name string) string {
	a.t.Helper()
	var id uuid.UUID
	require.NoError(a.t, a.pool.QueryRow(context.Background(), `SELECT id FROM products WHERE name = $1`, name).Scan(&id))
	return id.String()
}

func (a *orderAPI) stock(id string) int {
	a.t.Helper()
	var stock int
	require.NoError(a.t, a.pool.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, id).Scan(&stock))
	return stock
}

type orderBody struct {
	ID          string          `json:"id"`
	UserID      string          `json:"user_id"`
	User        json.RawMessage `json:"user"`
	Status      string          `json:"status"`
	TotalAmount json.RawMessage `json:"total_amount"`
	Items       []struct {
		ProductID   string          `json:"product_id"`
		ProductName string          `json:"product_name"`
		Quantity    int             `json:"quantity"`
		UnitPrice   json.RawMessage `json:"unit_price"`
		Subtotal    json.RawMessage `json:"subtotal"`
	} `json:"items"`
}

func decodeOrder(t *testing.T, rec *httptest.ResponseRecorder) orderBody {
	t.Helper()
	var out struct {
		Data orderBody `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.Data
}

func TestOrdersAPIEndToEnd(t *testing.T) {
	api := setupOrderAPI(t)

	admin := api.token("/api/v1/auth/login", `{"email":"admin@example.com","password":"supersecret123"}`, http.StatusOK)
	jane := api.token("/api/v1/auth/register", `{"name":"Jane","email":"jane@example.com","password":"supersecret"}`, http.StatusCreated)
	john := api.token("/api/v1/auth/register", `{"name":"John","email":"john@example.com","password":"supersecret"}`, http.StatusCreated)

	mug := api.productID("Ceramic Coffee Mug")
	bottle := api.productID("Stainless Steel Water Bottle")
	headphones := api.productID("Noise Cancelling Headphones")
	missing := uuid.Must(uuid.NewV7()).String()

	checkout := `{"items":[{"product_id":"` + mug + `","quantity":2},{"product_id":"` + bottle + `","quantity":1},{"product_id":"` + mug + `","quantity":1}]}`

	assert.Equal(t, http.StatusUnauthorized, api.do(http.MethodPost, "/api/v1/orders", "", checkout).Code)
	rec := api.do(http.MethodPost, "/api/v1/orders", admin, checkout)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"FORBIDDEN"`)

	rec = api.do(http.MethodPost, "/api/v1/orders", jane, checkout)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decodeOrder(t, rec)
	assert.Equal(t, "PENDING", created.Status)
	assert.Equal(t, "320000.00", string(created.TotalAmount))
	assert.Empty(t, created.User)
	require.Len(t, created.Items, 2)
	assert.Equal(t, 77, api.stock(mug))
	assert.Equal(t, 59, api.stock(bottle))

	rec = api.do(http.MethodPost, "/api/v1/orders", jane, `{"items":[{"product_id":"`+headphones+`","quantity":1},{"product_id":"`+mug+`","quantity":500}]}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	var conflict struct {
		Error struct {
			Code    string `json:"code"`
			Details []struct {
				ProductID string `json:"product_id"`
				Requested int    `json:"requested"`
				Available int    `json:"available"`
			} `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &conflict))
	assert.Equal(t, "INSUFFICIENT_STOCK", conflict.Error.Code)
	assert.Len(t, conflict.Error.Details, 2)
	for _, d := range conflict.Error.Details {
		switch d.ProductID {
		case headphones:
			assert.Equal(t, 1, d.Requested)
			assert.Zero(t, d.Available)
		case mug:
			assert.Equal(t, 500, d.Requested)
			assert.Equal(t, 77, d.Available)
		default:
			t.Errorf("unexpected product in details: %s", d.ProductID)
		}
	}
	assert.Equal(t, 77, api.stock(mug))

	rec = api.do(http.MethodPost, "/api/v1/orders", jane, `{"items":[{"product_id":"`+missing+`","quantity":1}]}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"PRODUCT_NOT_FOUND","message":"One or more products were not found","details":[{"product_id":"`+missing+`"}]}}`, rec.Body.String())

	rec = api.do(http.MethodPost, "/api/v1/orders", jane, `{"items":[]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	orderPath := "/api/v1/orders/" + created.ID
	assert.Equal(t, http.StatusOK, api.do(http.MethodGet, orderPath, jane, "").Code)
	rec = api.do(http.MethodGet, orderPath, john, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"NOT_FOUND"`)
	rec = api.do(http.MethodGet, orderPath, admin, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"`+created.UserID+`","name":"Jane","email":"jane@example.com"}`, string(decodeOrder(t, rec).User))
	assert.Equal(t, http.StatusBadRequest, api.do(http.MethodGet, "/api/v1/orders/not-a-uuid", jane, "").Code)

	rec = api.do(http.MethodGet, "/api/v1/orders", john, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":[],"meta":{"page":1,"limit":10,"total":0,"total_pages":0}}`, rec.Body.String())
	rec = api.do(http.MethodGet, "/api/v1/orders?status=PENDING", jane, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"total":1`)
	assert.NotContains(t, rec.Body.String(), `"user":`)
	rec = api.do(http.MethodGet, "/api/v1/orders", admin, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"user":{"id":"`+created.UserID+`"`)
	assert.Equal(t, http.StatusBadRequest, api.do(http.MethodGet, "/api/v1/orders?status=SHIPPED", admin, "").Code)

	statusPath := orderPath + "/status"
	assert.Equal(t, http.StatusForbidden, api.do(http.MethodPatch, statusPath, jane, `{"status":"CANCELLED"}`).Code)
	assert.Equal(t, http.StatusBadRequest, api.do(http.MethodPatch, statusPath, admin, `{"status":"PENDING"}`).Code)
	assert.Equal(t, http.StatusNotFound, api.do(http.MethodPatch, "/api/v1/orders/"+missing+"/status", admin, `{"status":"CANCELLED"}`).Code)

	rec = api.do(http.MethodPatch, statusPath, admin, `{"status":"CANCELLED"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "CANCELLED", decodeOrder(t, rec).Status)
	assert.Equal(t, 80, api.stock(mug))
	assert.Equal(t, 60, api.stock(bottle))

	rec = api.do(http.MethodPatch, statusPath, admin, `{"status":"CANCELLED"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"INVALID_STATUS_TRANSITION","message":"Order status cannot change from CANCELLED to CANCELLED","details":[{"current_status":"CANCELLED","requested_status":"CANCELLED"}]}}`, rec.Body.String())
	assert.Equal(t, 80, api.stock(mug))
}
