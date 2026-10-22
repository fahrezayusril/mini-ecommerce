package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "PENDING"
	OrderStatusCompleted OrderStatus = "COMPLETED"
	OrderStatusCancelled OrderStatus = "CANCELLED"
)

type CheckoutItem struct {
	ProductID uuid.UUID
	Quantity  int
}

type OrderUser struct {
	ID    uuid.UUID
	Name  string
	Email string
}

type OrderItem struct {
	ID          uuid.UUID
	OrderID     uuid.UUID
	ProductID   uuid.UUID
	ProductName string
	Quantity    int
	UnitPrice   decimal.Decimal
	Subtotal    decimal.Decimal
}

type Order struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Status      OrderStatus
	TotalAmount decimal.Decimal
	Items       []OrderItem
	User        *OrderUser
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type OrderFilter struct {
	Page   int
	Limit  int
	Status *OrderStatus
	UserID *uuid.UUID
}

type ProductsNotFoundError struct {
	ProductIDs []uuid.UUID
}

func (e *ProductsNotFoundError) Error() string {
	return fmt.Sprintf("%d product(s) not found", len(e.ProductIDs))
}

type StockShortage struct {
	ProductID uuid.UUID
	Requested int
	Available int
}

type InsufficientStockError struct {
	Shortages []StockShortage
}

func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("insufficient stock for %d product(s)", len(e.Shortages))
}

type InvalidStatusTransitionError struct {
	From OrderStatus
	To   OrderStatus
}

func (e *InvalidStatusTransitionError) Error() string {
	return fmt.Sprintf("order status cannot change from %s to %s", e.From, e.To)
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + " " + e.Message
}
