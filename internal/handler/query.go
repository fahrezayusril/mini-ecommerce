package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	defaultPage  = 1
	maxPage      = 1_000_000
	defaultLimit = 10
	maxLimit     = 100
)

type queryReader struct {
	values url.Values
	errors []FieldError
}

func parseQueryValues(r *http.Request) (url.Values, *requestError) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, validationFailed([]FieldError{{Field: "query", Message: "must be a valid URL-encoded query string"}})
	}
	return values, nil
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

func (q *queryReader) page() int {
	return q.intInRange("page", defaultPage, 1, maxPage)
}

func (q *queryReader) limit() int {
	return q.intInRange("limit", defaultLimit, 1, maxLimit)
}
