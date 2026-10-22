package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/domain"
)

type fakeProductRepository struct {
	listItems  []domain.Product
	listTotal  int
	err        error
	listFilter domain.ProductFilter
	saved      []domain.Product
	deletedIDs []uuid.UUID
	foundByID  []uuid.UUID
}

func (f *fakeProductRepository) List(_ context.Context, filter domain.ProductFilter) ([]domain.Product, int, error) {
	f.listFilter = filter
	return f.listItems, f.listTotal, f.err
}

func (f *fakeProductRepository) FindByID(_ context.Context, id uuid.UUID) (domain.Product, error) {
	f.foundByID = append(f.foundByID, id)
	if f.err != nil {
		return domain.Product{}, f.err
	}
	return domain.Product{ID: id}, nil
}

func (f *fakeProductRepository) Create(_ context.Context, product domain.Product) (domain.Product, error) {
	f.saved = append(f.saved, product)
	return product, f.err
}

func (f *fakeProductRepository) Update(_ context.Context, product domain.Product) (domain.Product, error) {
	f.saved = append(f.saved, product)
	return product, f.err
}

func (f *fakeProductRepository) SoftDelete(_ context.Context, id uuid.UUID) error {
	f.deletedIDs = append(f.deletedIDs, id)
	return f.err
}

func TestTotalPages(t *testing.T) {
	tests := []struct {
		total, limit, want int
	}{
		{total: 0, limit: 10, want: 0},
		{total: 1, limit: 10, want: 1},
		{total: 10, limit: 10, want: 1},
		{total: 11, limit: 10, want: 2},
		{total: 42, limit: 10, want: 5},
		{total: 100, limit: 1, want: 100},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, TotalPages(tt.total, tt.limit), "total=%d limit=%d", tt.total, tt.limit)
	}
}

func TestProductListBuildsPageMeta(t *testing.T) {
	repo := &fakeProductRepository{listItems: []domain.Product{{Name: "A"}, {Name: "B"}}, listTotal: 12}
	svc := NewProductService(repo)
	filter := domain.ProductFilter{Page: 2, Limit: 5, Sort: domain.ProductSortPrice, Order: domain.SortAsc}

	page, err := svc.List(context.Background(), filter)

	require.NoError(t, err)
	assert.Equal(t, filter, repo.listFilter)
	assert.Equal(t, ProductPage{Items: repo.listItems, Page: 2, Limit: 5, Total: 12, TotalPages: 3}, page)
}

func TestProductListPageBeyondRange(t *testing.T) {
	svc := NewProductService(&fakeProductRepository{listTotal: 3})

	page, err := svc.List(context.Background(), domain.ProductFilter{Page: 9, Limit: 10})

	require.NoError(t, err)
	assert.Empty(t, page.Items)
	assert.Equal(t, 3, page.Total)
	assert.Equal(t, 1, page.TotalPages)
	assert.Equal(t, 9, page.Page)
}

func TestProductCreateAssignsV7IDAndNormalizesInput(t *testing.T) {
	repo := &fakeProductRepository{}
	svc := NewProductService(repo)

	product, err := svc.Create(context.Background(), ProductInput{
		Name:        "  Mouse ",
		Description: " Wireless ",
		Price:       decimal.RequireFromString("149000.5"),
		Category:    " Electronics ",
		Stock:       3,
	})

	require.NoError(t, err)
	assert.Equal(t, uuid.Version(7), product.ID.Version())
	assert.Equal(t, "Mouse", product.Name)
	assert.Equal(t, "Wireless", product.Description)
	assert.Equal(t, "Electronics", product.Category)
	assert.Equal(t, "149000.50", product.Price.StringFixed(2))
	assert.Equal(t, 3, product.Stock)
}

func TestProductUpdateKeepsID(t *testing.T) {
	repo := &fakeProductRepository{}
	svc := NewProductService(repo)
	id := uuid.Must(uuid.NewV7())

	product, err := svc.Update(context.Background(), id, ProductInput{Name: "Mouse", Price: decimal.Zero, Category: "Electronics"})

	require.NoError(t, err)
	assert.Equal(t, id, product.ID)
}

func TestProductServicePropagatesRepositoryErrors(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	ctx := context.Background()

	for _, repoErr := range []error{domain.ErrNotFound, errors.New("connection reset")} {
		svc := NewProductService(&fakeProductRepository{err: repoErr})

		_, err := svc.List(ctx, domain.ProductFilter{Page: 1, Limit: 10})
		assert.ErrorIs(t, err, repoErr)
		_, err = svc.Get(ctx, id)
		assert.ErrorIs(t, err, repoErr)
		_, err = svc.Create(ctx, ProductInput{})
		assert.ErrorIs(t, err, repoErr)
		_, err = svc.Update(ctx, id, ProductInput{})
		assert.ErrorIs(t, err, repoErr)
		assert.ErrorIs(t, svc.Delete(ctx, id), repoErr)
	}
}
