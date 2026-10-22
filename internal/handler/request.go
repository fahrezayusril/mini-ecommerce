package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"mini-ecommerce/internal/httpx"
)

const maxRequestBodyBytes = 1 << 20

type requestError struct {
	status  int
	code    string
	message string
	details any
}

func (e *requestError) Error() string {
	return e.message
}

func (e *requestError) write(w http.ResponseWriter) {
	httpx.WriteError(w, e.status, e.code, e.message, e.details)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) *requestError {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return invalidJSON(describeDecodeError(err))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return invalidJSON("Request body must contain a single JSON object")
	}
	return nil
}

func describeDecodeError(err error) string {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	var maxBytesErr *http.MaxBytesError

	switch {
	case errors.Is(err, io.EOF):
		return "Request body must not be empty"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &syntaxErr):
		return "Request body contains malformed JSON"
	case errors.As(err, &typeErr):
		if typeErr.Field == "" {
			return "Request body must be a JSON object"
		}
		return fmt.Sprintf("Field %q has an invalid type", typeErr.Field)
	case errors.As(err, &maxBytesErr):
		return fmt.Sprintf("Request body must not exceed %d bytes", maxBytesErr.Limit)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return fmt.Sprintf("Request body contains unknown field %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	default:
		return "Request body contains malformed JSON"
	}
}

func invalidJSON(message string) *requestError {
	return &requestError{status: http.StatusBadRequest, code: httpx.CodeInvalidJSON, message: message}
}
