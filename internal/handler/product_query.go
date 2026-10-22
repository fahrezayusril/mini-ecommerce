package handler

import (
	"net/url"
	"unicode/utf8"

	"mini-ecommerce/internal/domain"
)

const maxCategoryLength = 100

var productSorts = map[string]domain.ProductSort{
	"price":      domain.ProductSortPrice,
	"created_at": domain.ProductSortCreatedAt,
}

var sortOrders = map[string]domain.SortOrder{
	"asc":  domain.SortAsc,
	"desc": domain.SortDesc,
}

func parseProductFilter(values url.Values) (domain.ProductFilter, []FieldError) {
	q := &queryReader{values: values}
	filter := domain.ProductFilter{
		Page:  q.page(),
		Limit: q.limit(),
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
