package repository_test

import (
	"bytes"
	"cmp"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/repository"
)

var fixtureBaseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type productFixture struct {
	key       string
	category  string
	price     string
	stock     int
	createdAt time.Duration
	deleted   bool
	id        uuid.UUID
}

type catalog struct {
	pool     *pgxpool.Pool
	repo     *repository.ProductRepository
	fixtures []productFixture
}

func (c *catalog) id(key string) uuid.UUID {
	for _, f := range c.fixtures {
		if f.key == key {
			return f.id
		}
	}
	panic("unknown fixture " + key)
}

func (c *catalog) ids(keys ...string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(keys))
	for _, key := range keys {
		out = append(out, c.id(key))
	}
	return out
}

func (c *catalog) expectedOrder(sort domain.ProductSort, order domain.SortOrder) []uuid.UUID {
	var active []productFixture
	for _, f := range c.fixtures {
		if !f.deleted {
			active = append(active, f)
		}
	}
	slices.SortFunc(active, func(a, b productFixture) int {
		var result int
		if sort == domain.ProductSortPrice {
			result = decimal.RequireFromString(a.price).Cmp(decimal.RequireFromString(b.price))
		} else {
			result = cmp.Compare(a.createdAt, b.createdAt)
		}
		if result == 0 {
			result = bytes.Compare(a.id[:], b.id[:])
		}
		if order == domain.SortDesc {
			result = -result
		}
		return result
	})
	out := make([]uuid.UUID, 0, len(active))
	for _, f := range active {
		out = append(out, f.id)
	}
	return out
}

func setupCatalog(t *testing.T) *catalog {
	t.Helper()
	pool := setupPool(t)
	c := &catalog{
		pool: pool,
		repo: repository.NewProductRepository(pool),
		fixtures: []productFixture{
			{key: "mouse", category: "Electronics", price: "149000.00", stock: 10, createdAt: 1 * time.Hour},
			{key: "keyboard", category: "electronics", price: "899000.00", stock: 0, createdAt: 2 * time.Hour},
			{key: "headphones", category: "ELECTRONICS", price: "2499000.00", stock: 5, createdAt: 3 * time.Hour},
			{key: "clean-code", category: "Books", price: "425000.00", stock: 18, createdAt: 4 * time.Hour},
			{key: "pragmatic", category: "Books", price: "425000.00", stock: 0, createdAt: 5 * time.Hour},
			{key: "mug", category: "Home", price: "45000.00", stock: 80, createdAt: 6 * time.Hour},
			{key: "bottle", category: "Home", price: "185000.00", stock: 60, createdAt: 6 * time.Hour},
			{key: "deleted-mouse", category: "Electronics", price: "100000.00", stock: 3, createdAt: 7 * time.Hour, deleted: true},
		},
	}

	ctx := context.Background()
	for i := range c.fixtures {
		f := &c.fixtures[i]
		f.id = uuid.Must(uuid.NewV7())
		createdAt := fixtureBaseTime.Add(f.createdAt)
		var deletedAt *time.Time
		if f.deleted {
			deletedAt = &createdAt
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO products (id, name, description, price, category, stock, created_at, updated_at, deleted_at)
			 VALUES ($1, $2, '', $3, $4, $5, $6, $6, $7)`,
			f.id, f.key, decimal.RequireFromString(f.price), f.category, f.stock, createdAt, deletedAt,
		)
		require.NoError(t, err)
	}
	return c
}

func listFilter(mutate func(*domain.ProductFilter)) domain.ProductFilter {
	filter := domain.ProductFilter{Page: 1, Limit: 100, Sort: domain.ProductSortCreatedAt, Order: domain.SortDesc}
	if mutate != nil {
		mutate(&filter)
	}
	return filter
}

func productIDs(products []domain.Product) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(products))
	for _, p := range products {
		out = append(out, p.ID)
	}
	return out
}

func decimalPtr(raw string) *decimal.Decimal {
	value := decimal.RequireFromString(raw)
	return &value
}

func TestProductListDefaultsExcludeSoftDeleted(t *testing.T) {
	c := setupCatalog(t)

	products, total, err := c.repo.List(context.Background(), listFilter(nil))

	require.NoError(t, err)
	assert.Equal(t, 7, total)
	assert.Equal(t, c.expectedOrder(domain.ProductSortCreatedAt, domain.SortDesc), productIDs(products))
	assert.NotContains(t, productIDs(products), c.id("deleted-mouse"))
}

func TestProductListFilters(t *testing.T) {
	c := setupCatalog(t)

	tests := []struct {
		name   string
		mutate func(*domain.ProductFilter)
		want   []string
	}{
		{
			name:   "category is case-insensitive and excludes deleted",
			mutate: func(f *domain.ProductFilter) { f.Category = new("eLeCtRoNiCs") },
			want:   []string{"headphones", "keyboard", "mouse"},
		},
		{
			name: "category with min price",
			mutate: func(f *domain.ProductFilter) {
				f.Category = new("electronics")
				f.MinPrice = decimalPtr("500000")
			},
			want: []string{"headphones", "keyboard"},
		},
		{
			name: "category in stock",
			mutate: func(f *domain.ProductFilter) {
				f.Category = new("Electronics")
				f.InStock = new(true)
			},
			want: []string{"headphones", "mouse"},
		},
		{
			name:   "out of stock",
			mutate: func(f *domain.ProductFilter) { f.InStock = new(false) },
			want:   []string{"pragmatic", "keyboard"},
		},
		{
			name: "inclusive price range",
			mutate: func(f *domain.ProductFilter) {
				f.MinPrice = decimalPtr("425000")
				f.MaxPrice = decimalPtr("425000.00")
			},
			want: []string{"pragmatic", "clean-code"},
		},
		{
			name: "all filters combined",
			mutate: func(f *domain.ProductFilter) {
				f.Category = new("BOOKS")
				f.InStock = new(true)
				f.MinPrice = decimalPtr("400000")
				f.MaxPrice = decimalPtr("500000")
			},
			want: []string{"clean-code"},
		},
		{
			name:   "max price only",
			mutate: func(f *domain.ProductFilter) { f.MaxPrice = decimalPtr("149000") },
			want:   []string{"mug", "mouse"},
		},
		{
			name:   "unknown category",
			mutate: func(f *domain.ProductFilter) { f.Category = new("Toys") },
			want:   []string{},
		},
		{
			name:   "category value is never interpreted as sql",
			mutate: func(f *domain.ProductFilter) { f.Category = new("x' OR '1'='1") },
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products, total, err := c.repo.List(context.Background(), listFilter(tt.mutate))

			require.NoError(t, err)
			assert.Equal(t, c.ids(tt.want...), productIDs(products))
			assert.Equal(t, len(tt.want), total)
		})
	}
}

func TestProductListSortBothDirections(t *testing.T) {
	c := setupCatalog(t)

	for _, sort := range []domain.ProductSort{domain.ProductSortPrice, domain.ProductSortCreatedAt} {
		for _, order := range []domain.SortOrder{domain.SortAsc, domain.SortDesc} {
			t.Run(string(sort)+" "+string(order), func(t *testing.T) {
				products, _, err := c.repo.List(context.Background(), listFilter(func(f *domain.ProductFilter) {
					f.Sort = sort
					f.Order = order
				}))

				require.NoError(t, err)
				assert.Equal(t, c.expectedOrder(sort, order), productIDs(products))
			})
		}
	}

	products, _, err := c.repo.List(context.Background(), listFilter(func(f *domain.ProductFilter) {
		f.Sort = domain.ProductSortPrice
		f.Order = domain.SortAsc
	}))
	require.NoError(t, err)
	assert.Equal(t, c.id("mug"), products[0].ID)
	assert.Equal(t, c.id("headphones"), products[len(products)-1].ID)
}

func TestProductListPaginationIsStable(t *testing.T) {
	c := setupCatalog(t)
	ctx := context.Background()

	for _, sort := range []domain.ProductSort{domain.ProductSortPrice, domain.ProductSortCreatedAt} {
		t.Run(string(sort), func(t *testing.T) {
			var collected []uuid.UUID
			for page := 1; page <= 3; page++ {
				products, total, err := c.repo.List(ctx, listFilter(func(f *domain.ProductFilter) {
					f.Page = page
					f.Limit = 3
					f.Sort = sort
				}))
				require.NoError(t, err)
				assert.Equal(t, 7, total)
				collected = append(collected, productIDs(products)...)
			}
			assert.Equal(t, c.expectedOrder(sort, domain.SortDesc), collected)

			products, total, err := c.repo.List(ctx, listFilter(func(f *domain.ProductFilter) {
				f.Page = 4
				f.Limit = 3
				f.Sort = sort
			}))
			require.NoError(t, err)
			assert.Empty(t, products)
			assert.Equal(t, 7, total)
		})
	}
}

func TestProductListFilteredPaginationCountsWithSameFilter(t *testing.T) {
	c := setupCatalog(t)

	products, total, err := c.repo.List(context.Background(), listFilter(func(f *domain.ProductFilter) {
		f.Category = new("electronics")
		f.Page = 2
		f.Limit = 2
	}))

	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Equal(t, c.ids("mouse"), productIDs(products))
}

func TestProductListRejectsUnknownSortValues(t *testing.T) {
	c := setupCatalog(t)
	ctx := context.Background()

	_, _, err := c.repo.List(ctx, listFilter(func(f *domain.ProductFilter) { f.Sort = "price; DROP TABLE products" }))
	assert.Error(t, err)
	_, _, err = c.repo.List(ctx, listFilter(func(f *domain.ProductFilter) { f.Order = "desc; DROP TABLE products" }))
	assert.Error(t, err)

	var count int
	require.NoError(t, c.pool.QueryRow(ctx, `SELECT count(*) FROM products`).Scan(&count))
	assert.Equal(t, 8, count)
}

func TestProductFindByID(t *testing.T) {
	c := setupCatalog(t)
	ctx := context.Background()

	product, err := c.repo.FindByID(ctx, c.id("clean-code"))
	require.NoError(t, err)
	assert.Equal(t, "clean-code", product.Name)
	assert.Equal(t, "425000.00", product.Price.StringFixed(2))
	assert.Equal(t, 18, product.Stock)

	_, err = c.repo.FindByID(ctx, c.id("deleted-mouse"))
	assert.ErrorIs(t, err, domain.ErrNotFound)

	_, err = c.repo.FindByID(ctx, uuid.Must(uuid.NewV7()))
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestProductCreate(t *testing.T) {
	c := setupCatalog(t)
	ctx := context.Background()

	created, err := c.repo.Create(ctx, domain.Product{
		ID:          uuid.Must(uuid.NewV7()),
		Name:        "Desk Lamp",
		Description: "LED lamp",
		Price:       decimal.RequireFromString("9999999999.99"),
		Category:    "Home",
		Stock:       4,
	})
	require.NoError(t, err)
	assert.False(t, created.CreatedAt.IsZero())
	assert.Equal(t, created.CreatedAt, created.UpdatedAt)

	found, err := c.repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "9999999999.99", found.Price.StringFixed(2))
	assert.Equal(t, "LED lamp", found.Description)
}

func TestProductUpdate(t *testing.T) {
	c := setupCatalog(t)
	ctx := context.Background()
	id := c.id("mouse")

	updated, err := c.repo.Update(ctx, domain.Product{
		ID:          id,
		Name:        "Mouse v2",
		Description: "Updated",
		Price:       decimal.RequireFromString("150000.50"),
		Category:    "Accessories",
		Stock:       0,
	})
	require.NoError(t, err)
	assert.Equal(t, "Mouse v2", updated.Name)
	assert.Equal(t, "150000.50", updated.Price.StringFixed(2))
	assert.Equal(t, "Accessories", updated.Category)
	assert.Zero(t, updated.Stock)
	assert.True(t, updated.CreatedAt.Equal(fixtureBaseTime.Add(time.Hour)))
	assert.True(t, updated.UpdatedAt.After(updated.CreatedAt))

	_, err = c.repo.Update(ctx, domain.Product{ID: c.id("deleted-mouse"), Name: "x", Price: decimal.Zero, Category: "x"})
	assert.ErrorIs(t, err, domain.ErrNotFound)
	_, err = c.repo.Update(ctx, domain.Product{ID: uuid.Must(uuid.NewV7()), Name: "x", Price: decimal.Zero, Category: "x"})
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestProductSoftDelete(t *testing.T) {
	c := setupCatalog(t)
	ctx := context.Background()
	id := c.id("mug")

	require.NoError(t, c.repo.SoftDelete(ctx, id))
	assert.ErrorIs(t, c.repo.SoftDelete(ctx, id), domain.ErrNotFound)
	assert.ErrorIs(t, c.repo.SoftDelete(ctx, c.id("deleted-mouse")), domain.ErrNotFound)
	assert.ErrorIs(t, c.repo.SoftDelete(ctx, uuid.Must(uuid.NewV7())), domain.ErrNotFound)

	_, err := c.repo.FindByID(ctx, id)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	products, total, err := c.repo.List(ctx, listFilter(nil))
	require.NoError(t, err)
	assert.Equal(t, 6, total)
	assert.NotContains(t, productIDs(products), id)

	var deletedAt *time.Time
	require.NoError(t, c.pool.QueryRow(ctx, `SELECT deleted_at FROM products WHERE id = $1`, id).Scan(&deletedAt))
	assert.NotNil(t, deletedAt)
}
