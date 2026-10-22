package handler

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"mini-ecommerce/internal/domain"
)

const (
	defaultPage       = 1
	maxPage           = 1_000_000
	defaultLimit      = 10
	maxLimit          = 100
	maxCategoryLength = 100
)

var productSorts = map[string]domain.ProductSort{
	"price":      domain.ProductSortPrice,
	"created_at": domain.ProductSortCreatedAt,
}

var sortOrders = map[string]domain.SortOrder{
	"asc":  domain.SortAsc,
	"desc": domain.SortDesc,
}

type queryReader struct {
	values url.Values
	errors []FieldError
}

func (q *queryReader) lookup(key string) (string, bool) {
	values, present := q.values[key]
	if !present {
		return "", false
	}
	value := ""
	if len(values) > 0 {
		value = strings.TrimSpace(values[0])
	}
	if value == "" {
		q.fail(key, "must not be empty")
		return "", false
	}
	return value, true
}

func (q *queryReader) fail(field, message string) {
	q.errors = append(q.errors, FieldError{Field: field, Message: message})
}

func (q *queryReader) intInRange(key string, fallback, minValue, maxValue int) int {
	raw, ok := q.lookup(key)
	if !ok {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	switch {
	case err != nil:
		q.fail(key, "must be an integer")
	case value < minValue:
		q.fail(key, "must be greater than or equal to "+strconv.Itoa(minValue))
	case value > maxValue:
		q.fail(key, "must be less than or equal to "+strconv.Itoa(maxValue))
	default:
		return value
	}
	return fallback
}

func (q *queryReader) nonNegativeDecimal(key string) *decimal.Decimal {
	raw, ok := q.lookup(key)
	if !ok {
		return nil
	}
	value, valid := parseDecimal(raw)
	switch {
	case !valid:
		q.fail(key, "must be a valid number")
	case value.IsNegative():
		q.fail(key, "must be greater than or equal to 0")
	default:
		return &value
	}
	return nil
}

func parseProductFilter(values url.Values) (domain.ProductFilter, []FieldError) {
	q := &queryReader{values: values}
	filter := domain.ProductFilter{
		Page:  q.intInRange("page", defaultPage, 1, maxPage),
		Limit: q.intInRange("limit", defaultLimit, 1, maxLimit),
		Sort:  domain.ProductSortCreatedAt,
		Order: domain.SortDesc,
	}

	if category, ok := q.lookup("category"); ok {
		if utf8.RuneCountInString(category) > maxCategoryLength {
			q.fail("category", "must be at most 100 characters")
		} else {
			filter.Category = &category
		}
	}

	filter.MinPrice = q.nonNegativeDecimal("min_price")
	filter.MaxPrice = q.nonNegativeDecimal("max_price")
	if filter.MinPrice != nil && filter.MaxPrice != nil && filter.MinPrice.GreaterThan(*filter.MaxPrice) {
		q.fail("min_price", "must be less than or equal to max_price")
	}

	if raw, ok := q.lookup("in_stock"); ok {
		switch raw {
		case "true":
			filter.InStock = new(true)
		case "false":
			filter.InStock = new(false)
		default:
			q.fail("in_stock", "must be one of true, false")
		}
	}

	if raw, ok := q.lookup("sort"); ok {
		if sort, known := productSorts[raw]; known {
			filter.Sort = sort
		} else {
			q.fail("sort", "must be one of price, created_at")
		}
	}

	if raw, ok := q.lookup("order"); ok {
		if order, known := sortOrders[raw]; known {
			filter.Order = order
		} else {
			q.fail("order", "must be one of asc, desc")
		}
	}

	return filter, q.errors
}
