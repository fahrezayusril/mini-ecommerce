package handler

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	maxPriceLiteralLength = 32
	maxPriceExponent      = 40
)

var maxPrice = decimal.RequireFromString("9999999999.99")

type Money decimal.Decimal

func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(decimal.Decimal(m).StringFixed(2)), nil
}

type jsonNumber string

func (n *jsonNumber) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return &json.UnmarshalTypeError{Value: "string", Type: reflect.TypeFor[jsonNumber]()}
	}
	*n = jsonNumber(data)
	return nil
}

func parseDecimal(raw string) (decimal.Decimal, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxPriceLiteralLength {
		return decimal.Decimal{}, false
	}
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Decimal{}, false
	}
	if exp := value.Exponent(); !value.IsZero() && (exp > maxPriceExponent || exp < -maxPriceExponent) {
		return decimal.Decimal{}, false
	}
	return value, true
}

func validatePrice(raw string) (decimal.Decimal, string) {
	value, ok := parseDecimal(raw)
	switch {
	case !ok:
		return decimal.Decimal{}, "must be a valid number"
	case value.IsNegative():
		return decimal.Decimal{}, "must be greater than or equal to 0"
	case value.GreaterThan(maxPrice):
		return decimal.Decimal{}, "must be less than or equal to " + maxPrice.StringFixed(2)
	case !value.Equal(value.Round(2)):
		return decimal.Decimal{}, "must have at most 2 decimal places"
	default:
		return value, ""
	}
}
