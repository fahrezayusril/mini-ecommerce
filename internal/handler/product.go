package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/httpx"
	"mini-ecommerce/internal/service"
)

const canonicalUUIDLength = 36

type ProductService interface {
	List(ctx context.Context, filter domain.ProductFilter) (service.ProductPage, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Product, error)
	Create(ctx context.Context, input service.ProductInput) (domain.Product, error)
	Update(ctx context.Context, id uuid.UUID, input service.ProductInput) (domain.Product, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type ProductRequest struct {
	Name        *string     `json:"name" validate:"required,min=1,max=200"`
	Description *string     `json:"description" validate:"required,max=2000"`
	Price       *jsonNumber `json:"price" validate:"required"`
	Category    *string     `json:"category" validate:"required,min=1,max=100"`
	Stock       *int64      `json:"stock" validate:"required,gte=0,lte=2147483647"`
}

var productRequestFieldOrder = []string{"name", "description", "price", "category", "stock"}

type ProductResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       Money  `json:"price"`
	Category    string `json:"category"`
	Stock       int    `json:"stock"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type ProductHandler struct {
	service ProductService
}

func NewProductHandler(service ProductService) *ProductHandler {
	return &ProductHandler{service: service}
}

func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) {
	values, reqErr := parseQueryValues(r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}
	filter, details := parseProductFilter(values)
	if reqErr := validationFailed(details); reqErr != nil {
		reqErr.write(w)
		return
	}

	page, err := h.service.List(r.Context(), filter)
	if err != nil {
		log.Printf("list products: %v", err)
		httpx.WriteInternalError(w)
		return
	}

	items := make([]ProductResponse, 0, len(page.Items))
	for _, product := range page.Items {
		items = append(items, newProductResponse(product))
	}
	httpx.WriteList(w, items, httpx.PaginationMeta{
		Page:       page.Page,
		Limit:      page.Limit,
		Total:      page.Total,
		TotalPages: page.TotalPages,
	})
}

func (h *ProductHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, reqErr := parseIDParam(r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}

	product, err := h.service.Get(r.Context(), id)
	if err != nil {
		writeProductError(w, "get product", err)
		return
	}
	httpx.WriteData(w, http.StatusOK, newProductResponse(product))
}

func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	input, reqErr := decodeProductRequest(w, r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}

	product, err := h.service.Create(r.Context(), input)
	if err != nil {
		writeProductError(w, "create product", err)
		return
	}
	httpx.WriteData(w, http.StatusCreated, newProductResponse(product))
}

func (h *ProductHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, reqErr := parseIDParam(r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}
	input, reqErr := decodeProductRequest(w, r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}

	product, err := h.service.Update(r.Context(), id, input)
	if err != nil {
		writeProductError(w, "update product", err)
		return
	}
	httpx.WriteData(w, http.StatusOK, newProductResponse(product))
}

func (h *ProductHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, reqErr := parseIDParam(r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}

	if err := h.service.Delete(r.Context(), id); err != nil {
		writeProductError(w, "delete product", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeProductRequest(w http.ResponseWriter, r *http.Request) (service.ProductInput, *requestError) {
	var req ProductRequest
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		return service.ProductInput{}, reqErr
	}
	for _, field := range []*string{req.Name, req.Description, req.Category} {
		if field != nil {
			*field = strings.TrimSpace(*field)
		}
	}

	details, err := structFieldErrors(req)
	if err != nil {
		return service.ProductInput{}, &requestError{status: http.StatusInternalServerError, code: httpx.CodeInternalError, message: "An unexpected error occurred"}
	}

	var price decimal.Decimal
	if req.Price != nil {
		var message string
		price, message = validatePrice(string(*req.Price))
		if message != "" {
			details = append(details, FieldError{Field: "price", Message: message})
		}
	}

	slices.SortStableFunc(details, func(a, b FieldError) int {
		return slices.Index(productRequestFieldOrder, a.Field) - slices.Index(productRequestFieldOrder, b.Field)
	})
	if reqErr := validationFailed(details); reqErr != nil {
		return service.ProductInput{}, reqErr
	}

	return service.ProductInput{
		Name:        *req.Name,
		Description: *req.Description,
		Price:       price,
		Category:    *req.Category,
		Stock:       int(*req.Stock),
	}, nil
}

func parseIDParam(r *http.Request) (uuid.UUID, *requestError) {
	raw := chi.URLParam(r, "id")
	id, err := uuid.Parse(raw)
	if err != nil || len(raw) != canonicalUUIDLength {
		return uuid.Nil, validationFailed([]FieldError{{Field: "id", Message: "must be a valid UUID"}})
	}
	return id, nil
}

func writeProductError(w http.ResponseWriter, operation string, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Product not found", nil)
		return
	}
	log.Printf("%s: %v", operation, err)
	httpx.WriteInternalError(w)
}

func newProductResponse(product domain.Product) ProductResponse {
	return ProductResponse{
		ID:          product.ID.String(),
		Name:        product.Name,
		Description: product.Description,
		Price:       Money(product.Price),
		Category:    product.Category,
		Stock:       product.Stock,
		CreatedAt:   product.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   product.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}
