package service

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/domain"
)

const (
	MaxCheckoutItems    = 50
	MaxCheckoutQuantity = 1_000_000
)

type OrderTxStore interface {
	LockActiveProducts(ctx context.Context, ids []uuid.UUID) ([]domain.Product, error)
	DecrementStock(ctx context.Context, items []domain.CheckoutItem) error
	CreateOrder(ctx context.Context, order domain.Order) (domain.Order, error)
	LockOrder(ctx context.Context, id uuid.UUID) (domain.Order, error)
	OrderQuantities(ctx context.Context, orderID uuid.UUID) ([]domain.CheckoutItem, error)
	LockProducts(ctx context.Context, ids []uuid.UUID) error
	IncrementStock(ctx context.Context, items []domain.CheckoutItem) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.OrderStatus) error
}

type TxFunc func(ctx context.Context, fn func(store OrderTxStore) error) error

type OrderReader interface {
	List(ctx context.Context, filter domain.OrderFilter) ([]domain.Order, int, error)
	FindByID(ctx context.Context, id uuid.UUID, ownerID *uuid.UUID) (domain.Order, error)
}

type OrderPage struct {
	Items      []domain.Order
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type OrderService struct {
	orders OrderReader
	inTx   TxFunc
}

func NewOrderService(orders OrderReader, inTx TxFunc) *OrderService {
	return &OrderService{orders: orders, inTx: inTx}
}

func MergeCheckoutItems(items []domain.CheckoutItem) []domain.CheckoutItem {
	quantities := make(map[uuid.UUID]int, len(items))
	merged := make([]domain.CheckoutItem, 0, len(items))
	for _, item := range items {
		if _, seen := quantities[item.ProductID]; !seen {
			merged = append(merged, domain.CheckoutItem{ProductID: item.ProductID})
		}
		quantities[item.ProductID] += item.Quantity
	}
	for i := range merged {
		merged[i].Quantity = quantities[merged[i].ProductID]
	}
	slices.SortFunc(merged, func(a, b domain.CheckoutItem) int {
		return bytes.Compare(a.ProductID[:], b.ProductID[:])
	})
	return merged
}

func ValidateCheckoutItems(items []domain.CheckoutItem) error {
	if len(items) == 0 {
		return &domain.ValidationError{Field: "items", Message: "must contain at least 1 item"}
	}
	if len(items) > MaxCheckoutItems {
		return &domain.ValidationError{Field: "items", Message: fmt.Sprintf("must contain at most %d items", MaxCheckoutItems)}
	}
	for i, item := range items {
		if item.ProductID == uuid.Nil {
			return &domain.ValidationError{Field: fmt.Sprintf("items[%d].product_id", i), Message: "must be a valid UUID"}
		}
		if item.Quantity < 1 || item.Quantity > MaxCheckoutQuantity {
			return &domain.ValidationError{Field: fmt.Sprintf("items[%d].quantity", i), Message: fmt.Sprintf("must be between 1 and %d", MaxCheckoutQuantity)}
		}
	}
	return nil
}

func (s *OrderService) Checkout(ctx context.Context, userID uuid.UUID, items []domain.CheckoutItem) (domain.Order, error) {
	if err := ValidateCheckoutItems(items); err != nil {
		return domain.Order{}, err
	}
	merged := MergeCheckoutItems(items)

	orderID, err := uuid.NewV7()
	if err != nil {
		return domain.Order{}, fmt.Errorf("generate order id: %w", err)
	}

	var created domain.Order
	err = s.inTx(ctx, func(store OrderTxStore) error {
		products, err := store.LockActiveProducts(ctx, productIDs(merged))
		if err != nil {
			return err
		}

		order, err := BuildOrder(orderID, userID, merged, products)
		if err != nil {
			return err
		}

		if err := store.DecrementStock(ctx, merged); err != nil {
			return err
		}
		created, err = store.CreateOrder(ctx, order)
		return err
	})
	if err != nil {
		return domain.Order{}, err
	}
	return created, nil
}

func BuildOrder(orderID, userID uuid.UUID, items []domain.CheckoutItem, products []domain.Product) (domain.Order, error) {
	byID := make(map[uuid.UUID]domain.Product, len(products))
	for _, product := range products {
		byID[product.ID] = product
	}

	var missing []uuid.UUID
	var shortages []domain.StockShortage
	for _, item := range items {
		product, found := byID[item.ProductID]
		if !found {
			missing = append(missing, item.ProductID)
			continue
		}
		if product.Stock < item.Quantity {
			shortages = append(shortages, domain.StockShortage{
				ProductID: item.ProductID,
				Requested: item.Quantity,
				Available: product.Stock,
			})
		}
	}
	if len(missing) > 0 {
		return domain.Order{}, &domain.ProductsNotFoundError{ProductIDs: missing}
	}
	if len(shortages) > 0 {
		return domain.Order{}, &domain.InsufficientStockError{Shortages: shortages}
	}

	order := domain.Order{
		ID:          orderID,
		UserID:      userID,
		Status:      domain.OrderStatusPending,
		TotalAmount: decimal.Zero,
		Items:       make([]domain.OrderItem, 0, len(items)),
	}
	for _, item := range items {
		product := byID[item.ProductID]
		itemID, err := uuid.NewV7()
		if err != nil {
			return domain.Order{}, fmt.Errorf("generate order item id: %w", err)
		}
		subtotal := product.Price.Mul(decimal.NewFromInt(int64(item.Quantity)))
		order.Items = append(order.Items, domain.OrderItem{
			ID:          itemID,
			OrderID:     orderID,
			ProductID:   product.ID,
			ProductName: product.Name,
			Quantity:    item.Quantity,
			UnitPrice:   product.Price,
			Subtotal:    subtotal,
		})
		order.TotalAmount = order.TotalAmount.Add(subtotal)
	}
	if order.TotalAmount.GreaterThan(domain.MaxAmount) {
		return domain.Order{}, &domain.ValidationError{
			Field:   "items",
			Message: "order total must not exceed " + domain.MaxAmount.StringFixed(2),
		}
	}
	return order, nil
}

func CanTransition(from, to domain.OrderStatus) bool {
	return from == domain.OrderStatusPending && (to == domain.OrderStatusCompleted || to == domain.OrderStatusCancelled)
}

func (s *OrderService) UpdateStatus(ctx context.Context, id uuid.UUID, to domain.OrderStatus) (domain.Order, error) {
	err := s.inTx(ctx, func(store OrderTxStore) error {
		order, err := store.LockOrder(ctx, id)
		if err != nil {
			return err
		}
		if !CanTransition(order.Status, to) {
			return &domain.InvalidStatusTransitionError{From: order.Status, To: to}
		}

		if to == domain.OrderStatusCancelled {
			quantities, err := store.OrderQuantities(ctx, id)
			if err != nil {
				return err
			}
			if err := store.LockProducts(ctx, productIDs(quantities)); err != nil {
				return err
			}
			if err := store.IncrementStock(ctx, quantities); err != nil {
				return err
			}
		}
		return store.UpdateStatus(ctx, id, to)
	})
	if err != nil {
		return domain.Order{}, err
	}
	return s.orders.FindByID(ctx, id, nil)
}

func (s *OrderService) List(ctx context.Context, principal auth.Principal, filter domain.OrderFilter) (OrderPage, error) {
	filter.UserID = ownerScope(principal)
	orders, total, err := s.orders.List(ctx, filter)
	if err != nil {
		return OrderPage{}, err
	}
	return OrderPage{
		Items:      orders,
		Page:       filter.Page,
		Limit:      filter.Limit,
		Total:      total,
		TotalPages: TotalPages(total, filter.Limit),
	}, nil
}

func (s *OrderService) Get(ctx context.Context, principal auth.Principal, id uuid.UUID) (domain.Order, error) {
	return s.orders.FindByID(ctx, id, ownerScope(principal))
}

func ownerScope(principal auth.Principal) *uuid.UUID {
	if principal.Role == domain.RoleAdmin {
		return nil
	}
	return &principal.UserID
}

func productIDs(items []domain.CheckoutItem) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ProductID)
	}
	return ids
}
