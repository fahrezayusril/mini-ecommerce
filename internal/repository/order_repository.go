package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mini-ecommerce/internal/database"
	"mini-ecommerce/internal/domain"
)

const (
	orderColumns     = `o.id, o.user_id, o.status, o.total_amount, o.created_at, o.updated_at, u.name, u.email`
	orderItemColumns = `id, order_id, product_id, product_name, quantity, unit_price, subtotal`
)

type OrderRepository struct {
	pool *pgxpool.Pool
}

func NewOrderRepository(pool *pgxpool.Pool) *OrderRepository {
	return &OrderRepository{pool: pool}
}

func (r *OrderRepository) InTx(ctx context.Context, fn func(tx *OrderTx) error) error {
	return database.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		return fn(&OrderTx{tx: tx})
	})
}

func (r *OrderRepository) List(ctx context.Context, filter domain.OrderFilter) ([]domain.Order, int, error) {
	where, args := orderListWhere(filter)

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM orders o WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}

	offset := int64(filter.Page-1) * int64(filter.Limit)
	query := fmt.Sprintf(
		`SELECT %s FROM orders o JOIN users u ON u.id = o.user_id
		 WHERE %s
		 ORDER BY o.created_at DESC, o.id DESC
		 LIMIT $%d OFFSET $%d`,
		orderColumns, where, len(args)+1, len(args)+2,
	)
	rows, err := r.pool.Query(ctx, query, append(args, filter.Limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	orders, err := pgx.CollectRows(rows, scanOrder)
	if err != nil {
		return nil, 0, fmt.Errorf("scan orders: %w", err)
	}

	if err := r.attachItems(ctx, orders); err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

func (r *OrderRepository) FindByID(ctx context.Context, id uuid.UUID, ownerID *uuid.UUID) (domain.Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+orderColumns+` FROM orders o JOIN users u ON u.id = o.user_id
		 WHERE o.id = $1 AND ($2::uuid IS NULL OR o.user_id = $2)`,
		id, ownerID,
	)
	if err != nil {
		return domain.Order{}, fmt.Errorf("find order: %w", err)
	}
	order, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("find order: %w", err)
	}

	orders := []domain.Order{order}
	if err := r.attachItems(ctx, orders); err != nil {
		return domain.Order{}, err
	}
	return orders[0], nil
}

func (r *OrderRepository) attachItems(ctx context.Context, orders []domain.Order) error {
	if len(orders) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(orders))
	index := make(map[uuid.UUID]int, len(orders))
	for i, order := range orders {
		ids = append(ids, order.ID)
		index[order.ID] = i
		orders[i].Items = []domain.OrderItem{}
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+orderItemColumns+` FROM order_items WHERE order_id = ANY($1) ORDER BY order_id, id`,
		ids,
	)
	if err != nil {
		return fmt.Errorf("list order items: %w", err)
	}
	items, err := pgx.CollectRows(rows, scanOrderItem)
	if err != nil {
		return fmt.Errorf("scan order items: %w", err)
	}
	for _, item := range items {
		i := index[item.OrderID]
		orders[i].Items = append(orders[i].Items, item)
	}
	return nil
}

type OrderTx struct {
	tx pgx.Tx
}

func (t *OrderTx) LockActiveProducts(ctx context.Context, ids []uuid.UUID) ([]domain.Product, error) {
	rows, err := t.tx.Query(ctx,
		`SELECT id, name, price, stock FROM products
		 WHERE id = ANY($1) AND deleted_at IS NULL
		 ORDER BY id
		 FOR UPDATE`,
		ids,
	)
	if err != nil {
		return nil, fmt.Errorf("lock products: %w", err)
	}
	products, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Product, error) {
		var p domain.Product
		err := row.Scan(&p.ID, &p.Name, &p.Price, &p.Stock)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan locked products: %w", err)
	}
	return products, nil
}

func (t *OrderTx) DecrementStock(ctx context.Context, items []domain.CheckoutItem) error {
	return t.adjustStock(ctx, items, "-")
}

func (t *OrderTx) IncrementStock(ctx context.Context, items []domain.CheckoutItem) error {
	return t.adjustStock(ctx, items, "+")
}

func (t *OrderTx) adjustStock(ctx context.Context, items []domain.CheckoutItem, operator string) error {
	ids, quantities := splitCheckoutItems(items)
	tag, err := t.tx.Exec(ctx,
		`UPDATE products p SET stock = p.stock `+operator+` v.quantity
		 FROM unnest($1::uuid[], $2::int[]) AS v(id, quantity)
		 WHERE p.id = v.id`,
		ids, quantities,
	)
	if err != nil {
		return fmt.Errorf("adjust stock: %w", err)
	}
	if tag.RowsAffected() != int64(len(items)) {
		return fmt.Errorf("adjust stock: updated %d of %d products", tag.RowsAffected(), len(items))
	}
	return nil
}

func (t *OrderTx) CreateOrder(ctx context.Context, order domain.Order) (domain.Order, error) {
	err := t.tx.QueryRow(ctx,
		`INSERT INTO orders (id, user_id, total_amount, status)
		 VALUES ($1, $2, $3, $4)
		 RETURNING created_at, updated_at`,
		order.ID, order.UserID, order.TotalAmount, order.Status,
	).Scan(&order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return domain.Order{}, fmt.Errorf("insert order: %w", err)
	}

	n := len(order.Items)
	ids := make([]uuid.UUID, 0, n)
	productIDs := make([]uuid.UUID, 0, n)
	names := make([]string, 0, n)
	quantities := make([]int32, 0, n)
	unitPrices := make([]string, 0, n)
	subtotals := make([]string, 0, n)
	for _, item := range order.Items {
		ids = append(ids, item.ID)
		productIDs = append(productIDs, item.ProductID)
		names = append(names, item.ProductName)
		quantities = append(quantities, int32(item.Quantity))
		unitPrices = append(unitPrices, item.UnitPrice.StringFixed(2))
		subtotals = append(subtotals, item.Subtotal.StringFixed(2))
	}

	_, err = t.tx.Exec(ctx,
		`INSERT INTO order_items (id, order_id, product_id, product_name, quantity, unit_price, subtotal)
		 SELECT v.id, $1, v.product_id, v.product_name, v.quantity, v.unit_price, v.subtotal
		 FROM unnest($2::uuid[], $3::uuid[], $4::text[], $5::int[], $6::numeric[], $7::numeric[])
		   AS v(id, product_id, product_name, quantity, unit_price, subtotal)`,
		order.ID, ids, productIDs, names, quantities, unitPrices, subtotals,
	)
	if err != nil {
		return domain.Order{}, fmt.Errorf("insert order items: %w", err)
	}
	return order, nil
}

func (t *OrderTx) LockOrder(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	var order domain.Order
	err := t.tx.QueryRow(ctx,
		`SELECT id, user_id, status, total_amount, created_at, updated_at FROM orders WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(&order.ID, &order.UserID, &order.Status, &order.TotalAmount, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("lock order: %w", err)
	}
	return order, nil
}

func (t *OrderTx) OrderQuantities(ctx context.Context, orderID uuid.UUID) ([]domain.CheckoutItem, error) {
	rows, err := t.tx.Query(ctx,
		`SELECT product_id, sum(quantity)::int FROM order_items WHERE order_id = $1 GROUP BY product_id ORDER BY product_id`,
		orderID,
	)
	if err != nil {
		return nil, fmt.Errorf("load order quantities: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CheckoutItem, error) {
		var item domain.CheckoutItem
		err := row.Scan(&item.ProductID, &item.Quantity)
		return item, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan order quantities: %w", err)
	}
	return items, nil
}

func (t *OrderTx) LockProducts(ctx context.Context, ids []uuid.UUID) error {
	rows, err := t.tx.Query(ctx, `SELECT id FROM products WHERE id = ANY($1) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return fmt.Errorf("lock order products: %w", err)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("lock order products: %w", err)
	}
	return nil
}

func (t *OrderTx) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.OrderStatus) error {
	tag, err := t.tx.Exec(ctx, `UPDATE orders SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func orderListWhere(filter domain.OrderFilter) (string, []any) {
	conditions := []string{"TRUE"}
	var args []any
	if filter.UserID != nil {
		args = append(args, *filter.UserID)
		conditions = append(conditions, fmt.Sprintf("o.user_id = $%d", len(args)))
	}
	if filter.Status != nil {
		args = append(args, string(*filter.Status))
		conditions = append(conditions, fmt.Sprintf("o.status = $%d", len(args)))
	}
	return strings.Join(conditions, " AND "), args
}

func scanOrder(row pgx.CollectableRow) (domain.Order, error) {
	var order domain.Order
	var user domain.OrderUser
	err := row.Scan(&order.ID, &order.UserID, &order.Status, &order.TotalAmount, &order.CreatedAt, &order.UpdatedAt, &user.Name, &user.Email)
	user.ID = order.UserID
	order.User = &user
	return order, err
}

func scanOrderItem(row pgx.CollectableRow) (domain.OrderItem, error) {
	var item domain.OrderItem
	err := row.Scan(&item.ID, &item.OrderID, &item.ProductID, &item.ProductName, &item.Quantity, &item.UnitPrice, &item.Subtotal)
	return item, err
}

func splitCheckoutItems(items []domain.CheckoutItem) ([]uuid.UUID, []int32) {
	ids := make([]uuid.UUID, 0, len(items))
	quantities := make([]int32, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ProductID)
		quantities = append(quantities, int32(item.Quantity))
	}
	return ids, quantities
}
