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

	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/service"
)

type fakeProductService struct {
	err        error
	listFilter *domain.ProductFilter
	page       service.ProductPage
	inputs     []service.ProductInput
	ids        []uuid.UUID
	calls      int
}

func (f *fakeProductService) List(_ context.Context, filter domain.ProductFilter) (service.ProductPage, error) {
	f.calls++
	f.listFilter = &filter
	return f.page, f.err
}

func (f *fakeProductService) Get(_ context.Context, id uuid.UUID) (domain.Product, error) {
	f.calls++
	f.ids = append(f.ids, id)
	if f.err != nil {
		return domain.Product{}, f.err
	}
	return sampleProduct(id), nil
}

func (f *fakeProductService) Create(_ context.Context, input service.ProductInput) (domain.Product, error) {
	f.calls++
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return domain.Product{}, f.err
	}
	return productFromInput(testProductID, input), nil
}

func (f *fakeProductService) Update(_ context.Context, id uuid.UUID, input service.ProductInput) (domain.Product, error) {
	f.calls++
	f.ids = append(f.ids, id)
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return domain.Product{}, f.err
	}
	return productFromInput(id, input), nil
}

func (f *fakeProductService) Delete(_ context.Context, id uuid.UUID) error {
	f.calls++
	f.ids = append(f.ids, id)
	return f.err
}

var (
	testProductID   = uuid.MustParse("01920000-0000-7000-8000-0000000000aa")
	testProductTime = time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)
)

func productFromInput(id uuid.UUID, input service.ProductInput) domain.Product {
	return domain.Product{
		ID:          id,
		Name:        input.Name,
		Description: input.Description,
		Price:       input.Price,
		Category:    input.Category,
		Stock:       input.Stock,
		CreatedAt:   testProductTime,
		UpdatedAt:   testProductTime,
	}
}

func sampleProduct(id uuid.UUID) domain.Product {
	return productFromInput(id, service.ProductInput{
		Name:        "Wireless Mouse",
		Description: "Silent clicks",
		Price:       decimal.RequireFromString("149000"),
		Category:    "Electronics",
		Stock:       120,
	})
}

func productRouter(h *ProductHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/products", h.List)
	r.Post("/products", h.Create)
	r.Get("/products/{id}", h.Get)
	r.Put("/products/{id}", h.Update)
	r.Delete("/products/{id}", h.Delete)
	return r
}

func doRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

const sampleProductJSON = `{
	"id": "01920000-0000-7000-8000-0000000000aa",
	"name": "Wireless Mouse",
	"description": "Silent clicks",
	"price": 149000.00,
	"category": "Electronics",
	"stock": 120,
	"created_at": "2026-10-22T09:30:00Z",
	"updated_at": "2026-10-22T09:30:00Z"
}`

const validProductBody = `{"name":"Wireless Mouse","description":"Silent clicks","price":149000,"category":"Electronics","stock":120}`

func TestListProductsSuccess(t *testing.T) {
	svc := &fakeProductService{page: service.ProductPage{
		Items: []domain.Product{sampleProduct(testProductID)},
		Page:  2, Limit: 1, Total: 3, TotalPages: 3,
	}}

	rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodGet, "/products?page=2&limit=1&sort=price&order=asc", "")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":[`+sampleProductJSON+`],"meta":{"page":2,"limit":1,"total":3,"total_pages":3}}`, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"price":149000.00`)
	require.NotNil(t, svc.listFilter)
	assert.Equal(t, domain.ProductSortPrice, svc.listFilter.Sort)
	assert.Equal(t, domain.SortAsc, svc.listFilter.Order)
}

func TestListProductsEmptyIsArray(t *testing.T) {
	svc := &fakeProductService{page: service.ProductPage{Page: 1, Limit: 10}}

	rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodGet, "/products", "")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":[],"meta":{"page":1,"limit":10,"total":0,"total_pages":0}}`, rec.Body.String())
}

func TestListProductsRejectsInvalidQuery(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		field   string
		message string
	}{
		{name: "encoded sql in sort", target: "/products?sort=price%3Bdrop%20table%20products", field: "sort", message: "must be one of price, created_at"},
		{name: "raw semicolon in sort", target: "/products?sort=price;drop%20table%20products", field: "query", message: "must be a valid URL-encoded query string"},
		{name: "bad escape", target: "/products?category=%zz", field: "query", message: "must be a valid URL-encoded query string"},
		{name: "limit too large", target: "/products?limit=1000", field: "limit", message: "must be less than or equal to 100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeProductService{}
			rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodGet, tt.target, "")

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			envelope := decodeError(t, rec)
			assert.Equal(t, "VALIDATION_ERROR", envelope.Error.Code)
			assert.Equal(t, []FieldError{{Field: tt.field, Message: tt.message}}, envelope.Error.Details)
			assert.Zero(t, svc.calls)
		})
	}
}

func TestGetProduct(t *testing.T) {
	svc := &fakeProductService{}

	rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodGet, "/products/"+testProductID.String(), "")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":`+sampleProductJSON+`}`, rec.Body.String())
	assert.Equal(t, []uuid.UUID{testProductID}, svc.ids)
}

func TestProductInvalidIDIsBadRequest(t *testing.T) {
	ids := []string{"not-a-uuid", "123", "01920000000070008000000000000aa", "{01920000-0000-7000-8000-0000000000aa}"}
	methods := []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPut, validProductBody},
		{http.MethodDelete, ""},
	}

	for _, id := range ids {
		for _, m := range methods {
			t.Run(m.method+" "+id, func(t *testing.T) {
				svc := &fakeProductService{}
				rec := doRequest(productRouter(NewProductHandler(svc)), m.method, "/products/"+id, m.body)

				assert.Equal(t, http.StatusBadRequest, rec.Code)
				envelope := decodeError(t, rec)
				assert.Equal(t, "VALIDATION_ERROR", envelope.Error.Code)
				assert.Equal(t, []FieldError{{Field: "id", Message: "must be a valid UUID"}}, envelope.Error.Details)
				assert.Zero(t, svc.calls)
			})
		}
	}
}

func TestProductNotFound(t *testing.T) {
	target := "/products/" + testProductID.String()
	requests := []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPut, validProductBody},
		{http.MethodDelete, ""},
	}

	for _, req := range requests {
		t.Run(req.method, func(t *testing.T) {
			svc := &fakeProductService{err: domain.ErrNotFound}
			rec := doRequest(productRouter(NewProductHandler(svc)), req.method, target, req.body)

			assert.Equal(t, http.StatusNotFound, rec.Code)
			assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"Product not found"}}`, rec.Body.String())
		})
	}
}

func TestProductInternalError(t *testing.T) {
	svc := &fakeProductService{err: errors.New("connection reset by 10.0.0.5")}
	router := productRouter(NewProductHandler(svc))

	for _, rec := range []*httptest.ResponseRecorder{
		doRequest(router, http.MethodGet, "/products", ""),
		doRequest(router, http.MethodGet, "/products/"+testProductID.String(), ""),
		doRequest(router, http.MethodPost, "/products", validProductBody),
	} {
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.JSONEq(t, `{"error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred"}}`, rec.Body.String())
	}
}

func TestCreateProductSuccess(t *testing.T) {
	svc := &fakeProductService{}
	body := `{"name":"  Wireless Mouse ","description":" Silent clicks ","price":149000,"category":" Electronics ","stock":120}`

	rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodPost, "/products", body)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"data":`+sampleProductJSON+`}`, rec.Body.String())
	require.Len(t, svc.inputs, 1)
	input := svc.inputs[0]
	assert.Equal(t, "Wireless Mouse", input.Name)
	assert.Equal(t, "Silent clicks", input.Description)
	assert.Equal(t, "Electronics", input.Category)
	assert.Equal(t, 120, input.Stock)
	assert.True(t, decimal.RequireFromString("149000").Equal(input.Price))
}

func TestCreateProductAcceptsBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantPrice string
	}{
		{name: "zero price and stock with empty description", body: `{"name":"A","description":"","price":0,"category":"B","stock":0}`, wantPrice: "0.00"},
		{name: "max price", body: `{"name":"A","description":"","price":9999999999.99,"category":"B","stock":2147483647}`, wantPrice: "9999999999.99"},
		{name: "one decimal", body: `{"name":"A","description":"","price":12.5,"category":"B","stock":1}`, wantPrice: "12.50"},
		{name: "trailing zero decimals", body: `{"name":"A","description":"","price":12.500,"category":"B","stock":1}`, wantPrice: "12.50"},
		{name: "exponent notation", body: `{"name":"A","description":"","price":1.5e3,"category":"B","stock":1}`, wantPrice: "1500.00"},
		{name: "long fields", body: `{"name":"` + strings.Repeat("n", 200) + `","description":"` + strings.Repeat("d", 2000) + `","price":1,"category":"` + strings.Repeat("c", 100) + `","stock":1}`, wantPrice: "1.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeProductService{}
			rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodPost, "/products", tt.body)

			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), `"price":`+tt.wantPrice+`,`)
		})
	}
}

func TestProductBodyValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []FieldError
	}{
		{
			name: "all fields missing",
			body: `{}`,
			want: []FieldError{
				{Field: "name", Message: "is required"},
				{Field: "description", Message: "is required"},
				{Field: "price", Message: "is required"},
				{Field: "category", Message: "is required"},
				{Field: "stock", Message: "is required"},
			},
		},
		{
			name: "nulls count as missing",
			body: `{"name":null,"description":null,"price":null,"category":null,"stock":null}`,
			want: []FieldError{
				{Field: "name", Message: "is required"},
				{Field: "description", Message: "is required"},
				{Field: "price", Message: "is required"},
				{Field: "category", Message: "is required"},
				{Field: "stock", Message: "is required"},
			},
		},
		{
			name: "blank name and category",
			body: `{"name":"  ","description":"","price":1,"category":"","stock":1}`,
			want: []FieldError{
				{Field: "name", Message: "must not be empty"},
				{Field: "category", Message: "must not be empty"},
			},
		},
		{
			name: "too long fields",
			body: `{"name":"` + strings.Repeat("n", 201) + `","description":"` + strings.Repeat("d", 2001) + `","price":1,"category":"` + strings.Repeat("c", 101) + `","stock":1}`,
			want: []FieldError{
				{Field: "name", Message: "must be at most 200 characters"},
				{Field: "description", Message: "must be at most 2000 characters"},
				{Field: "category", Message: "must be at most 100 characters"},
			},
		},
		{
			name: "negative price and stock",
			body: `{"name":"A","description":"","price":-0.01,"category":"B","stock":-1}`,
			want: []FieldError{
				{Field: "price", Message: "must be greater than or equal to 0"},
				{Field: "stock", Message: "must be greater than or equal to 0"},
			},
		},
		{
			name: "too many decimals",
			body: `{"name":"A","description":"","price":12.345,"category":"B","stock":1}`,
			want: []FieldError{{Field: "price", Message: "must have at most 2 decimal places"}},
		},
		{
			name: "price over numeric 12,2",
			body: `{"name":"A","description":"","price":10000000000,"category":"B","stock":1}`,
			want: []FieldError{{Field: "price", Message: "must be less than or equal to 9999999999.99"}},
		},
		{
			name: "huge exponent price",
			body: `{"name":"A","description":"","price":1e999999999,"category":"B","stock":1}`,
			want: []FieldError{{Field: "price", Message: "must be a valid number"}},
		},
		{
			name: "tiny exponent price",
			body: `{"name":"A","description":"","price":1e-999999999,"category":"B","stock":1}`,
			want: []FieldError{{Field: "price", Message: "must be a valid number"}},
		},
		{
			name: "stock over int range",
			body: `{"name":"A","description":"","price":1,"category":"B","stock":2147483648}`,
			want: []FieldError{{Field: "stock", Message: "must be less than or equal to 2147483647"}},
		},
	}

	for _, tt := range tests {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			t.Run(method+" "+tt.name, func(t *testing.T) {
				target := "/products"
				if method == http.MethodPut {
					target += "/" + testProductID.String()
				}
				svc := &fakeProductService{}
				rec := doRequest(productRouter(NewProductHandler(svc)), method, target, tt.body)

				assert.Equal(t, http.StatusBadRequest, rec.Code)
				envelope := decodeError(t, rec)
				assert.Equal(t, "VALIDATION_ERROR", envelope.Error.Code)
				assert.Equal(t, tt.want, envelope.Error.Details)
				assert.Zero(t, svc.calls)
			})
		}
	}
}

func TestProductBodyInvalidJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		message string
	}{
		{name: "price as string", body: `{"name":"A","description":"","price":"12.50","category":"B","stock":1}`, message: `Field "price" has an invalid type`},
		{name: "stock as decimal", body: `{"name":"A","description":"","price":1,"category":"B","stock":1.5}`, message: `Field "stock" has an invalid type`},
		{name: "stock as string", body: `{"name":"A","description":"","price":1,"category":"B","stock":"1"}`, message: `Field "stock" has an invalid type`},
		{name: "name as number", body: `{"name":1,"description":"","price":1,"category":"B","stock":1}`, message: `Field "name" has an invalid type`},
		{name: "unknown field", body: `{"name":"A","description":"","price":1,"category":"B","stock":1,"deleted_at":null}`, message: `Request body contains unknown field "deleted_at"`},
		{name: "empty body", body: ``, message: "Request body must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeProductService{}
			rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodPost, "/products", tt.body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			envelope := decodeError(t, rec)
			assert.Equal(t, "INVALID_JSON", envelope.Error.Code)
			assert.Equal(t, tt.message, envelope.Error.Message)
			assert.Zero(t, svc.calls)
		})
	}
}

func TestUpdateProductSuccess(t *testing.T) {
	svc := &fakeProductService{}
	body := `{"name":"Mouse v2","description":"Updated","price":150000.50,"category":"Electronics","stock":5}`

	rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodPut, "/products/"+testProductID.String(), body)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"price":150000.50`)
	assert.Contains(t, rec.Body.String(), `"name":"Mouse v2"`)
	assert.Equal(t, []uuid.UUID{testProductID}, svc.ids)
}

func TestDeleteProductSuccess(t *testing.T) {
	svc := &fakeProductService{}

	rec := doRequest(productRouter(NewProductHandler(svc)), http.MethodDelete, "/products/"+testProductID.String(), "")

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, []uuid.UUID{testProductID}, svc.ids)
}

func TestMoneyMarshalsWithTwoDecimals(t *testing.T) {
	for raw, want := range map[string]string{"0": "0.00", "1.5": "1.50", "149000": "149000.00", "9999999999.99": "9999999999.99"} {
		got, err := Money(decimal.RequireFromString(raw)).MarshalJSON()
		require.NoError(t, err)
		assert.Equal(t, want, string(got))
	}
}

func TestProductTimestampsKeepFractionalSeconds(t *testing.T) {
	product := sampleProduct(testProductID)
	product.UpdatedAt = testProductTime.Add(123456 * time.Microsecond)

	response := newProductResponse(product)

	assert.Equal(t, "2026-10-22T09:30:00Z", response.CreatedAt)
	assert.Equal(t, "2026-10-22T09:30:00.123456Z", response.UpdatedAt)
}
