package handler

import (
	"net/url"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/domain"
)

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	values, err := url.ParseQuery(raw)
	require.NoError(t, err)
	return values
}

func TestParseProductFilterDefaults(t *testing.T) {
	filter, errs := parseProductFilter(url.Values{})

	assert.Empty(t, errs)
	assert.Equal(t, domain.ProductFilter{
		Page:  1,
		Limit: 10,
		Sort:  domain.ProductSortCreatedAt,
		Order: domain.SortDesc,
	}, filter)
}

func TestParseProductFilterAllParams(t *testing.T) {
	filter, errs := parseProductFilter(mustQuery(t,
		"page=3&limit=25&category=%20Books%20&min_price=10.5&max_price=500000&in_stock=true&sort=price&order=asc&unknown=ignored",
	))

	require.Empty(t, errs)
	assert.Equal(t, 3, filter.Page)
	assert.Equal(t, 25, filter.Limit)
	require.NotNil(t, filter.Category)
	assert.Equal(t, "Books", *filter.Category)
	require.NotNil(t, filter.MinPrice)
	assert.True(t, decimal.RequireFromString("10.5").Equal(*filter.MinPrice))
	require.NotNil(t, filter.MaxPrice)
	assert.True(t, decimal.RequireFromString("500000").Equal(*filter.MaxPrice))
	require.NotNil(t, filter.InStock)
	assert.True(t, *filter.InStock)
	assert.Equal(t, domain.ProductSortPrice, filter.Sort)
	assert.Equal(t, domain.SortAsc, filter.Order)
}

func TestParseProductFilterAcceptedValues(t *testing.T) {
	tests := []struct {
		query string
		check func(t *testing.T, filter domain.ProductFilter)
	}{
		{"in_stock=false", func(t *testing.T, f domain.ProductFilter) { require.NotNil(t, f.InStock); assert.False(t, *f.InStock) }},
		{"sort=created_at&order=asc", func(t *testing.T, f domain.ProductFilter) {
			assert.Equal(t, domain.ProductSortCreatedAt, f.Sort)
			assert.Equal(t, domain.SortAsc, f.Order)
		}},
		{"order=desc", func(t *testing.T, f domain.ProductFilter) { assert.Equal(t, domain.SortDesc, f.Order) }},
		{"limit=1", func(t *testing.T, f domain.ProductFilter) { assert.Equal(t, 1, f.Limit) }},
		{"limit=100", func(t *testing.T, f domain.ProductFilter) { assert.Equal(t, 100, f.Limit) }},
		{"min_price=0&max_price=0", func(t *testing.T, f domain.ProductFilter) {
			assert.True(t, f.MinPrice.IsZero())
			assert.True(t, f.MaxPrice.IsZero())
		}},
		{"min_price=100&max_price=100", func(t *testing.T, f domain.ProductFilter) { assert.True(t, f.MinPrice.Equal(*f.MaxPrice)) }},
		{"page=1&page=99", func(t *testing.T, f domain.ProductFilter) { assert.Equal(t, 1, f.Page) }},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			filter, errs := parseProductFilter(mustQuery(t, tt.query))
			require.Empty(t, errs)
			tt.check(t, filter)
		})
	}
}

func TestParseProductFilterRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		query string
		want  []FieldError
	}{
		{"page=0", []FieldError{{Field: "page", Message: "must be greater than or equal to 1"}}},
		{"page=-1", []FieldError{{Field: "page", Message: "must be greater than or equal to 1"}}},
		{"page=abc", []FieldError{{Field: "page", Message: "must be an integer"}}},
		{"page=1.5", []FieldError{{Field: "page", Message: "must be an integer"}}},
		{"page=1000001", []FieldError{{Field: "page", Message: "must be less than or equal to 1000000"}}},
		{"page=99999999999999999999", []FieldError{{Field: "page", Message: "must be an integer"}}},
		{"page=", []FieldError{{Field: "page", Message: "must not be empty"}}},
		{"limit=0", []FieldError{{Field: "limit", Message: "must be greater than or equal to 1"}}},
		{"limit=101", []FieldError{{Field: "limit", Message: "must be less than or equal to 100"}}},
		{"category=", []FieldError{{Field: "category", Message: "must not be empty"}}},
		{"category=%20%20", []FieldError{{Field: "category", Message: "must not be empty"}}},
		{"min_price=-1", []FieldError{{Field: "min_price", Message: "must be greater than or equal to 0"}}},
		{"min_price=abc", []FieldError{{Field: "min_price", Message: "must be a valid number"}}},
		{"max_price=1e999999999", []FieldError{{Field: "max_price", Message: "must be a valid number"}}},
		{"max_price=NaN", []FieldError{{Field: "max_price", Message: "must be a valid number"}}},
		{"min_price=500&max_price=100", []FieldError{{Field: "min_price", Message: "must be less than or equal to max_price"}}},
		{"in_stock=yes", []FieldError{{Field: "in_stock", Message: "must be one of true, false"}}},
		{"in_stock=1", []FieldError{{Field: "in_stock", Message: "must be one of true, false"}}},
		{"in_stock=TRUE", []FieldError{{Field: "in_stock", Message: "must be one of true, false"}}},
		{"sort=name", []FieldError{{Field: "sort", Message: "must be one of price, created_at"}}},
		{"sort=price%3Bdrop%20table%20products", []FieldError{{Field: "sort", Message: "must be one of price, created_at"}}},
		{"sort=price%20desc,%20(SELECT%201)", []FieldError{{Field: "sort", Message: "must be one of price, created_at"}}},
		{"sort=PRICE", []FieldError{{Field: "sort", Message: "must be one of price, created_at"}}},
		{"order=random", []FieldError{{Field: "order", Message: "must be one of asc, desc"}}},
		{"order=desc%3B--", []FieldError{{Field: "order", Message: "must be one of asc, desc"}}},
		{
			"page=0&limit=500&min_price=x&in_stock=maybe&sort=id&order=up",
			[]FieldError{
				{Field: "page", Message: "must be greater than or equal to 1"},
				{Field: "limit", Message: "must be less than or equal to 100"},
				{Field: "min_price", Message: "must be a valid number"},
				{Field: "in_stock", Message: "must be one of true, false"},
				{Field: "sort", Message: "must be one of price, created_at"},
				{Field: "order", Message: "must be one of asc, desc"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			_, errs := parseProductFilter(mustQuery(t, tt.query))
			assert.Equal(t, tt.want, errs)
		})
	}
}

func TestParseProductFilterRejectsLongCategory(t *testing.T) {
	long := make([]byte, 101)
	for i := range long {
		long[i] = 'a'
	}
	_, errs := parseProductFilter(url.Values{"category": {string(long)}})
	assert.Equal(t, []FieldError{{Field: "category", Message: "must be at most 100 characters"}}, errs)
}
