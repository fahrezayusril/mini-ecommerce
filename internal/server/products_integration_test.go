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

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/config"
	"mini-ecommerce/internal/database"
	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/handler"
	"mini-ecommerce/internal/repository"
	"mini-ecommerce/internal/service"
)

type productAPI struct {
	pool          *pgxpool.Pool
	handler       http.Handler
	adminToken    string
	customerToken string
}

func setupProductAPI(t *testing.T) *productAPI {
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

	tokens := auth.NewTokenManager(strings.Repeat("i", 32), time.Hour)
	adminToken, _, err := tokens.Issue(uuid.Must(uuid.NewV7()), domain.RoleAdmin)
	require.NoError(t, err)
	customerToken, _, err := tokens.Issue(uuid.Must(uuid.NewV7()), domain.RoleCustomer)
	require.NoError(t, err)

	s := &Server{
		db:             database.New(pool),
		tokenParser:    tokens,
		productHandler: handler.NewProductHandler(service.NewProductService(repository.NewProductRepository(pool))),
	}
	return &productAPI{pool: pool, handler: s.RegisterRoutes(), adminToken: adminToken, customerToken: customerToken}
}

func (a *productAPI) do(t *testing.T, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a *productAPI) productCount(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, a.pool.QueryRow(context.Background(), `SELECT count(*) FROM products`).Scan(&count))
	return count
}

type productListBody struct {
	Data []struct {
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		Price    json.RawMessage `json:"price"`
		Category string          `json:"category"`
		Stock    int             `json:"stock"`
	} `json:"data"`
	Meta struct {
		Page       int `json:"page"`
		Limit      int `json:"limit"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	} `json:"meta"`
}

func TestProductsAPIRejectsSQLInjectionAttempts(t *testing.T) {
	api := setupProductAPI(t)
	before := api.productCount(t)

	rejected := []string{
		"/api/v1/products?sort=price%3Bdrop%20table%20products",
		"/api/v1/products?sort=price;drop%20table%20products",
		"/api/v1/products?sort=price%20desc%2C%20(SELECT%20pg_sleep(1))",
		"/api/v1/products?order=desc%3B--",
		"/api/v1/products?order=asc;delete%20from%20products",
		"/api/v1/products?page=1%3Bdrop%20table%20products",
		"/api/v1/products?min_price=0%20OR%201%3D1",
	}
	for _, target := range rejected {
		t.Run(target, func(t *testing.T) {
			rec := api.do(t, http.MethodGet, target, "", "")
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), `"code":"VALIDATION_ERROR"`)
		})
	}

	rec := api.do(t, http.MethodGet, "/api/v1/products?category=x%27%20OR%20%271%27%3D%271", "", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":[],"meta":{"page":1,"limit":10,"total":0,"total_pages":0}}`, rec.Body.String())

	assert.Equal(t, before, api.productCount(t))
}

func TestProductsAPIListWithSeedData(t *testing.T) {
	api := setupProductAPI(t)

	rec := api.do(t, http.MethodGet, "/api/v1/products?category=books&sort=price&order=asc&limit=2", "", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body productListBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 3, body.Meta.Total)
	assert.Equal(t, 2, body.Meta.TotalPages)
	require.Len(t, body.Data, 2)
	assert.Equal(t, "The Pragmatic Programmer", body.Data[0].Name)
	assert.Equal(t, "389500.00", string(body.Data[0].Price))
	assert.Equal(t, "425000.00", string(body.Data[1].Price))

	rec = api.do(t, http.MethodGet, "/api/v1/products?in_stock=false", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	assert.Equal(t, "Noise Cancelling Headphones", body.Data[0].Name)
	assert.Zero(t, body.Data[0].Stock)
}

func TestProductsAPIAdminLifecycle(t *testing.T) {
	api := setupProductAPI(t)
	createBody := `{"name":"Desk Lamp","description":"LED desk lamp","price":275000.5,"category":"Home","stock":15}`

	assert.Equal(t, http.StatusUnauthorized, api.do(t, http.MethodPost, "/api/v1/products", "", createBody).Code)
	assert.Equal(t, http.StatusForbidden, api.do(t, http.MethodPost, "/api/v1/products", api.customerToken, createBody).Code)

	rec := api.do(t, http.MethodPost, "/api/v1/products", api.adminToken, createBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"price":275000.50`)

	var created struct {
		Data struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	id, err := uuid.Parse(created.Data.ID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(7), id.Version())
	productPath := "/api/v1/products/" + created.Data.ID

	rec = api.do(t, http.MethodGet, productPath, "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Desk Lamp"`)

	_, err = api.pool.Exec(context.Background(), `UPDATE products SET updated_at = updated_at - interval '1 hour' WHERE id = $1`, id)
	require.NoError(t, err)

	partial := `{"name":"Desk Lamp v2"}`
	rec = api.do(t, http.MethodPut, productPath, api.adminToken, partial)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	updateBody := `{"name":"Desk Lamp v2","description":"","price":300000,"category":"Home","stock":0}`
	assert.Equal(t, http.StatusForbidden, api.do(t, http.MethodPut, productPath, api.customerToken, updateBody).Code)
	rec = api.do(t, http.MethodPut, productPath, api.adminToken, updateBody)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"name":"Desk Lamp v2"`)
	assert.Contains(t, rec.Body.String(), `"price":300000.00`)
	assert.Contains(t, rec.Body.String(), `"stock":0`)

	var updatedAt time.Time
	var createdAt time.Time
	require.NoError(t, api.pool.QueryRow(context.Background(), `SELECT created_at, updated_at FROM products WHERE id = $1`, id).Scan(&createdAt, &updatedAt))
	assert.True(t, updatedAt.After(createdAt))

	assert.Equal(t, http.StatusForbidden, api.do(t, http.MethodDelete, productPath, api.customerToken, "").Code)
	rec = api.do(t, http.MethodDelete, productPath, api.adminToken, "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())

	assert.Equal(t, http.StatusNotFound, api.do(t, http.MethodGet, productPath, "", "").Code)
	assert.Equal(t, http.StatusNotFound, api.do(t, http.MethodDelete, productPath, api.adminToken, "").Code)
	assert.Equal(t, http.StatusNotFound, api.do(t, http.MethodPut, productPath, api.adminToken, updateBody).Code)

	rec = api.do(t, http.MethodGet, "/api/v1/products?limit=100", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), created.Data.ID)
	assert.Contains(t, rec.Body.String(), `"total":10`)

	assert.Equal(t, http.StatusBadRequest, api.do(t, http.MethodGet, "/api/v1/products/not-a-uuid", "", "").Code)
	assert.Equal(t, http.StatusNotFound, api.do(t, http.MethodGet, "/api/v1/products/"+uuid.Must(uuid.NewV7()).String(), "", "").Code)
}
