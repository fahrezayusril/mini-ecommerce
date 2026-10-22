package handler

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"

	"mini-ecommerce/internal/httpx"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	if err := v.RegisterValidation("maxbytes", validateMaxBytes); err != nil {
		panic(err)
	}
	return v
}

func validateMaxBytes(fl validator.FieldLevel) bool {
	limit, err := strconv.Atoi(fl.Param())
	if err != nil {
		return false
	}
	return len(fl.Field().String()) <= limit
}

func validateStruct(payload any) *requestError {
	details, err := structFieldErrors(payload)
	if err != nil {
		return &requestError{status: http.StatusInternalServerError, code: httpx.CodeInternalError, message: "An unexpected error occurred"}
	}
	return validationFailed(details)
}

func structFieldErrors(payload any) ([]FieldError, error) {
	err := validate.Struct(payload)
	if err == nil {
		return nil, nil
	}

	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return nil, err
	}

	details := make([]FieldError, 0, len(validationErrs))
	for _, fieldErr := range validationErrs {
		details = append(details, FieldError{Field: fieldErr.Field(), Message: fieldErrorMessage(fieldErr)})
	}
	return details, nil
}

func validationFailed(details []FieldError) *requestError {
	if len(details) == 0 {
		return nil
	}
	return &requestError{
		status:  http.StatusBadRequest,
		code:    httpx.CodeValidationError,
		message: "Request validation failed",
		details: details,
	}
}

func fieldErrorMessage(fieldErr validator.FieldError) string {
	switch fieldErr.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email"
	case "min":
		if fieldErr.Param() == "1" {
			return "must not be empty"
		}
		return fmt.Sprintf("must be at least %s characters", fieldErr.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fieldErr.Param())
	case "gte":
		return fmt.Sprintf("must be greater than or equal to %s", fieldErr.Param())
	case "lte":
		return fmt.Sprintf("must be less than or equal to %s", fieldErr.Param())
	case "maxbytes":
		return fmt.Sprintf("must be at most %s bytes", fieldErr.Param())
	default:
		return "is invalid"
	}
}
