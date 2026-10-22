package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/httpx"
	"mini-ecommerce/internal/middleware"
	"mini-ecommerce/internal/service"
)

var orderStatuses = map[string]domain.OrderStatus{
	string(domain.OrderStatusPending):   domain.OrderStatusPending,
	string(domain.OrderStatusCompleted): domain.OrderStatusCompleted,
	string(domain.OrderStatusCancelled): domain.OrderStatusCancelled,
}

var orderStatusTargets = map[string]domain.OrderStatus{
	string(domain.OrderStatusCompleted): domain.OrderStatusCompleted,
	string(domain.OrderStatusCancelled): domain.OrderStatusCancelled,
}

type OrderService interface {
	Checkout(ctx context.Context, userID uuid.UUID, items []domain.CheckoutItem) (domain.Order, error)
	List(ctx context.Context, principal auth.Principal, filter domain.OrderFilter) (service.OrderPage, error)
	Get(ctx context.Context, principal auth.Principal, id uuid.UUID) (domain.Order, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.OrderStatus) (domain.Order, error)
}

type CheckoutItemRequest struct {
	ProductID *string `json:"product_id" validate:"required,uuid"`
	Quantity  *int64  `json:"quantity" validate:"required,gte=1,lte=1000000"`
}

type CheckoutRequest struct {
	Items []CheckoutItemRequest `json:"items" validate:"required,min=1,max=50,dive"`
}

type OrderStatusRequest struct {
	Status *string `json:"status" validate:"required"`
}

type OrderItemResponse struct {
	ID          string `json:"id"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    int    `json:"quantity"`
	UnitPrice   Money  `json:"unit_price"`
	Subtotal    Money  `json:"subtotal"`
}

type OrderUserResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type OrderResponse struct {
	ID          string              `json:"id"`
	UserID      string              `json:"user_id"`
	User        *OrderUserResponse  `json:"user,omitempty"`
	Status      string              `json:"status"`
	TotalAmount Money               `json:"total_amount"`
	Items       []OrderItemResponse `json:"items"`
	CreatedAt   string              `json:"created_at"`
	UpdatedAt   string              `json:"updated_at"`
}

type ProductNotFoundDetail struct {
	ProductID string `json:"product_id"`
}

type StockShortageDetail struct {
	ProductID string `json:"product_id"`
	Requested int    `json:"requested"`
	Available int    `json:"available"`
}

type StatusTransitionDetail struct {
	CurrentStatus   string `json:"current_status"`
	RequestedStatus string `json:"requested_status"`
}

type OrderHandler struct {
	service OrderService
}

func NewOrderHandler(service OrderService) *OrderHandler {
	return &OrderHandler{service: service}
}

func (h *OrderHandler) Create(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}

	var req CheckoutRequest
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		reqErr.write(w)
		return
	}
	if reqErr := validateStruct(req); reqErr != nil {
		reqErr.write(w)
		return
	}

	items := make([]domain.CheckoutItem, 0, len(req.Items))
	for i, item := range req.Items {
		productID, err := uuid.Parse(*item.ProductID)
		if err != nil {
			validationFailed([]FieldError{{Field: fmt.Sprintf("items[%d].product_id", i), Message: "must be a valid UUID"}}).write(w)
			return
		}
		items = append(items, domain.CheckoutItem{ProductID: productID, Quantity: int(*item.Quantity)})
	}

	order, err := h.service.Checkout(r.Context(), principal.UserID, items)
	if err != nil {
		writeOrderError(w, "checkout", err)
		return
	}
	httpx.WriteData(w, http.StatusCreated, newOrderResponse(order, principal))
}

func (h *OrderHandler) List(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	values, reqErr := parseQueryValues(r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}
	filter, details := parseOrderFilter(values)
	if reqErr := validationFailed(details); reqErr != nil {
		reqErr.write(w)
		return
	}

	page, err := h.service.List(r.Context(), principal, filter)
	if err != nil {
		writeOrderError(w, "list orders", err)
		return
	}

	items := make([]OrderResponse, 0, len(page.Items))
	for _, order := range page.Items {
		items = append(items, newOrderResponse(order, principal))
	}
	httpx.WriteList(w, items, httpx.PaginationMeta{
		Page:       page.Page,
		Limit:      page.Limit,
		Total:      page.Total,
		TotalPages: page.TotalPages,
	})
}

func (h *OrderHandler) Get(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	id, reqErr := parseIDParam(r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}

	order, err := h.service.Get(r.Context(), principal, id)
	if err != nil {
		writeOrderError(w, "get order", err)
		return
	}
	httpx.WriteData(w, http.StatusOK, newOrderResponse(order, principal))
}

func (h *OrderHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	id, reqErr := parseIDParam(r)
	if reqErr != nil {
		reqErr.write(w)
		return
	}

	var req OrderStatusRequest
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		reqErr.write(w)
		return
	}
	if reqErr := validateStruct(req); reqErr != nil {
		reqErr.write(w)
		return
	}
	status, known := orderStatusTargets[*req.Status]
	if !known {
		validationFailed([]FieldError{{Field: "status", Message: "must be one of COMPLETED, CANCELLED"}}).write(w)
		return
	}

	order, err := h.service.UpdateStatus(r.Context(), id, status)
	if err != nil {
		writeOrderError(w, "update order status", err)
		return
	}
	httpx.WriteData(w, http.StatusOK, newOrderResponse(order, principal))
}

func parseOrderFilter(values url.Values) (domain.OrderFilter, []FieldError) {
	q := &queryReader{values: values}
	filter := domain.OrderFilter{Page: q.page(), Limit: q.limit()}

	if raw, ok := q.lookup("status"); ok {
		if status, known := orderStatuses[raw]; known {
			filter.Status = &status
		} else {
			q.fail("status", "must be one of PENDING, COMPLETED, CANCELLED")
		}
	}
	return filter, q.errors
}

func requirePrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication required", nil)
	}
	return principal, ok
}

func writeOrderError(w http.ResponseWriter, operation string, err error) {
	var validationErr *domain.ValidationError
	var notFoundErr *domain.ProductsNotFoundError
	var stockErr *domain.InsufficientStockError
	var transitionErr *domain.InvalidStatusTransitionError

	switch {
	case errors.As(err, &validationErr):
		validationFailed([]FieldError{{Field: validationErr.Field, Message: validationErr.Message}}).write(w)
	case errors.As(err, &notFoundErr):
		details := make([]ProductNotFoundDetail, 0, len(notFoundErr.ProductIDs))
		for _, id := range notFoundErr.ProductIDs {
			details = append(details, ProductNotFoundDetail{ProductID: id.String()})
		}
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeProductNotFound, "One or more products were not found", details)
	case errors.As(err, &stockErr):
		details := make([]StockShortageDetail, 0, len(stockErr.Shortages))
		for _, shortage := range stockErr.Shortages {
			details = append(details, StockShortageDetail{
				ProductID: shortage.ProductID.String(),
				Requested: shortage.Requested,
				Available: shortage.Available,
			})
		}
		httpx.WriteError(w, http.StatusConflict, httpx.CodeInsufficientStock, "Insufficient stock for one or more products", details)
	case errors.As(err, &transitionErr):
		httpx.WriteError(w, http.StatusConflict, httpx.CodeInvalidStatusTransition,
			"Order status cannot change from "+string(transitionErr.From)+" to "+string(transitionErr.To),
			[]StatusTransitionDetail{{CurrentStatus: string(transitionErr.From), RequestedStatus: string(transitionErr.To)}},
		)
	case errors.Is(err, domain.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Order not found", nil)
	default:
		log.Printf("%s: %v", operation, err)
		httpx.WriteInternalError(w)
	}
}

func newOrderResponse(order domain.Order, viewer auth.Principal) OrderResponse {
	items := make([]OrderItemResponse, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, OrderItemResponse{
			ID:          item.ID.String(),
			ProductID:   item.ProductID.String(),
			ProductName: item.ProductName,
			Quantity:    item.Quantity,
			UnitPrice:   Money(item.UnitPrice),
			Subtotal:    Money(item.Subtotal),
		})
	}

	response := OrderResponse{
		ID:          order.ID.String(),
		UserID:      order.UserID.String(),
		Status:      string(order.Status),
		TotalAmount: Money(order.TotalAmount),
		Items:       items,
		CreatedAt:   order.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   order.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if viewer.Role == domain.RoleAdmin && order.User != nil {
		response.User = &OrderUserResponse{
			ID:    order.User.ID.String(),
			Name:  order.User.Name,
			Email: order.User.Email,
		}
	}
	return response
}
