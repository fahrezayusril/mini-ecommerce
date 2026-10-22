package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"mini-ecommerce/internal/domain"
)

type ProductRepository interface {
	List(ctx context.Context, filter domain.ProductFilter) ([]domain.Product, int, error)
	FindByID(ctx context.Context, id uuid.UUID) (domain.Product, error)
	Create(ctx context.Context, product domain.Product) (domain.Product, error)
	Update(ctx context.Context, product domain.Product) (domain.Product, error)
	SoftDelete(ctx context.Context, id uuid.UUID) error
}

type ProductInput struct {
	Name        string
	Description string
	Price       decimal.Decimal
	Category    string
	Stock       int
}

type ProductPage struct {
	Items      []domain.Product
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type ProductService struct {
	products ProductRepository
}

func NewProductService(products ProductRepository) *ProductService {
	return &ProductService{products: products}
}

func (s *ProductService) List(ctx context.Context, filter domain.ProductFilter) (ProductPage, error) {
	items, total, err := s.products.List(ctx, filter)
	if err != nil {
		return ProductPage{}, err
	}
	return ProductPage{
		Items:      items,
		Page:       filter.Page,
		Limit:      filter.Limit,
		Total:      total,
		TotalPages: TotalPages(total, filter.Limit),
	}, nil
}

func (s *ProductService) Get(ctx context.Context, id uuid.UUID) (domain.Product, error) {
	return s.products.FindByID(ctx, id)
}

func (s *ProductService) Create(ctx context.Context, input ProductInput) (domain.Product, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Product{}, fmt.Errorf("generate product id: %w", err)
	}
	return s.products.Create(ctx, input.toProduct(id))
}

func (s *ProductService) Update(ctx context.Context, id uuid.UUID, input ProductInput) (domain.Product, error) {
	return s.products.Update(ctx, input.toProduct(id))
}

func (s *ProductService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.products.SoftDelete(ctx, id)
}

func (in ProductInput) toProduct(id uuid.UUID) domain.Product {
	return domain.Product{
		ID:          id,
		Name:        strings.TrimSpace(in.Name),
		Description: strings.TrimSpace(in.Description),
		Price:       in.Price.Round(2),
		Category:    strings.TrimSpace(in.Category),
		Stock:       in.Stock,
	}
}

func TotalPages(total, limit int) int {
	if total <= 0 || limit <= 0 {
		return 0
	}
	return (total + limit - 1) / limit
}
