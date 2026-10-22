package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ProductSort string

const (
	ProductSortPrice     ProductSort = "price"
	ProductSortCreatedAt ProductSort = "created_at"
)

type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

type Product struct {
	ID          uuid.UUID
	Name        string
	Description string
	Price       decimal.Decimal
	Category    string
	Stock       int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProductFilter struct {
	Page     int
	Limit    int
	Category *string
	MinPrice *decimal.Decimal
	MaxPrice *decimal.Decimal
	InStock  *bool
	Sort     ProductSort
	Order    SortOrder
}
