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
	err := validate.Struct(payload)
	if err == nil {
		return nil
	}

	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return &requestError{status: http.StatusInternalServerError, code: httpx.CodeInternalError, message: "An unexpected error occurred"}
	}

	details := make([]FieldError, 0, len(validationErrs))
	for _, fieldErr := range validationErrs {
		details = append(details, FieldError{Field: fieldErr.Field(), Message: fieldErrorMessage(fieldErr)})
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
		return fmt.Sprintf("must be at least %s characters", fieldErr.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fieldErr.Param())
	case "maxbytes":
		return fmt.Sprintf("must be at most %s bytes", fieldErr.Param())
	default:
		return "is invalid"
	}
}
