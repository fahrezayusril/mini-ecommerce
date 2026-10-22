package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mini-ecommerce/internal/domain"
)

const productColumns = `id, name, description, price, category, stock, created_at, updated_at`

var productSortColumns = map[domain.ProductSort]string{
	domain.ProductSortPrice:     "price",
	domain.ProductSortCreatedAt: "created_at",
}

var sortDirections = map[domain.SortOrder]string{
	domain.SortAsc:  "ASC",
	domain.SortDesc: "DESC",
}

type ProductRepository struct {
	pool *pgxpool.Pool
}

func NewProductRepository(pool *pgxpool.Pool) *ProductRepository {
	return &ProductRepository{pool: pool}
}

func (r *ProductRepository) List(ctx context.Context, filter domain.ProductFilter) ([]domain.Product, int, error) {
	orderBy, err := productOrderBy(filter.Sort, filter.Order)
	if err != nil {
		return nil, 0, err
	}
	where, args := productListWhere(filter)

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM products WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	offset := int64(filter.Page-1) * int64(filter.Limit)
	pageArgs := append(args, filter.Limit, offset)
	query := fmt.Sprintf(
		`SELECT %s FROM products WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		productColumns, where, orderBy, len(args)+1, len(args)+2,
	)

	rows, err := r.pool.Query(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	products, err := pgx.CollectRows(rows, scanProduct)
	if err != nil {
		return nil, 0, fmt.Errorf("scan products: %w", err)
	}
	return products, total, nil
}

func (r *ProductRepository) FindByID(ctx context.Context, id uuid.UUID) (domain.Product, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+productColumns+` FROM products WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return domain.Product{}, fmt.Errorf("find product: %w", err)
	}
	return collectOneProduct(rows, "find product")
}

func (r *ProductRepository) Create(ctx context.Context, product domain.Product) (domain.Product, error) {
	rows, err := r.pool.Query(ctx,
		`INSERT INTO products (id, name, description, price, category, stock)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+productColumns,
		product.ID, product.Name, product.Description, product.Price, product.Category, product.Stock,
	)
	if err != nil {
		return domain.Product{}, fmt.Errorf("insert product: %w", err)
	}
	return collectOneProduct(rows, "insert product")
}

func (r *ProductRepository) Update(ctx context.Context, product domain.Product) (domain.Product, error) {
	rows, err := r.pool.Query(ctx,
		`UPDATE products
		 SET name = $2, description = $3, price = $4, category = $5, stock = $6, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING `+productColumns,
		product.ID, product.Name, product.Description, product.Price, product.Category, product.Stock,
	)
	if err != nil {
		return domain.Product{}, fmt.Errorf("update product: %w", err)
	}
	return collectOneProduct(rows, "update product")
}

func (r *ProductRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE products SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("soft delete product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func productListWhere(filter domain.ProductFilter) (string, []any) {
	conditions := []string{"deleted_at IS NULL"}
	var args []any
	addCondition := func(format string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(format, len(args)))
	}

	if filter.Category != nil {
		addCondition("lower(category) = lower($%d)", *filter.Category)
	}
	if filter.MinPrice != nil {
		addCondition("price >= $%d", *filter.MinPrice)
	}
	if filter.MaxPrice != nil {
		addCondition("price <= $%d", *filter.MaxPrice)
	}
	if filter.InStock != nil {
		if *filter.InStock {
			conditions = append(conditions, "stock > 0")
		} else {
			conditions = append(conditions, "stock = 0")
		}
	}
	return strings.Join(conditions, " AND "), args
}

func productOrderBy(sort domain.ProductSort, order domain.SortOrder) (string, error) {
	column, ok := productSortColumns[sort]
	if !ok {
		return "", fmt.Errorf("unsupported product sort %q", sort)
	}
	direction, ok := sortDirections[order]
	if !ok {
		return "", fmt.Errorf("unsupported sort order %q", order)
	}
	return column + " " + direction + ", id " + direction, nil
}

func scanProduct(row pgx.CollectableRow) (domain.Product, error) {
	var p domain.Product
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Category, &p.Stock, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func collectOneProduct(rows pgx.Rows, operation string) (domain.Product, error) {
	product, err := pgx.CollectExactlyOneRow(rows, scanProduct)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Product{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Product{}, fmt.Errorf("%s: %w", operation, err)
	}
	return product, nil
}
