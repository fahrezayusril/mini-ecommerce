package httpx

import (
	"encoding/json"
	"log"
	"net/http"
)

const (
	CodeValidationError         = "VALIDATION_ERROR"
	CodeInvalidJSON             = "INVALID_JSON"
	CodeUnauthorized            = "UNAUTHORIZED"
	CodeInvalidCredentials      = "INVALID_CREDENTIALS"
	CodeForbidden               = "FORBIDDEN"
	CodeNotFound                = "NOT_FOUND"
	CodeProductNotFound         = "PRODUCT_NOT_FOUND"
	CodeEmailAlreadyExists      = "EMAIL_ALREADY_EXISTS"
	CodeInsufficientStock       = "INSUFFICIENT_STOCK"
	CodeInvalidStatusTransition = "INVALID_STATUS_TRANSITION"
	CodeInternalError           = "INTERNAL_ERROR"
)

type DataResponse struct {
	Data any `json:"data"`
}

type PaginationMeta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type ListResponse struct {
	Data any            `json:"data"`
	Meta PaginationMeta `json:"meta"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write json response: %v", err)
	}
}

func WriteData(w http.ResponseWriter, status int, data any) {
	WriteJSON(w, status, DataResponse{Data: data})
}

func WriteList(w http.ResponseWriter, data any, meta PaginationMeta) {
	WriteJSON(w, http.StatusOK, ListResponse{Data: data, Meta: meta})
}

func WriteError(w http.ResponseWriter, status int, code, message string, details any) {
	WriteJSON(w, status, ErrorResponse{Error: ErrorBody{Code: code, Message: message, Details: details}})
}

func WriteInternalError(w http.ResponseWriter) {
	WriteError(w, http.StatusInternalServerError, CodeInternalError, "An unexpected error occurred", nil)
}
