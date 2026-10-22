package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/httpx"
	"mini-ecommerce/internal/service"
)

type AuthService interface {
	Register(ctx context.Context, input service.RegisterInput) (service.AuthResult, error)
	Login(ctx context.Context, input service.LoginInput) (service.AuthResult, error)
}

type RegisterRequest struct {
	Name     string `json:"name" validate:"required,max=100"`
	Email    string `json:"email" validate:"required,max=255,email"`
	Password string `json:"password" validate:"required,min=8,maxbytes=72"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type UserResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type AuthResponse struct {
	User      UserResponse `json:"user"`
	Token     string       `json:"token"`
	ExpiresAt string       `json:"expires_at"`
}

type AuthHandler struct {
	service AuthService
}

func NewAuthHandler(service AuthService) *AuthHandler {
	return &AuthHandler{service: service}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		reqErr.write(w)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	if reqErr := validateStruct(req); reqErr != nil {
		reqErr.write(w)
		return
	}

	result, err := h.service.Register(r.Context(), service.RegisterInput{
		Name:     req.Name,
		Email:    req.Email,
		Password: req.Password,
	})
	if errors.Is(err, domain.ErrEmailAlreadyExists) {
		httpx.WriteError(w, http.StatusConflict, httpx.CodeEmailAlreadyExists, "Email is already registered", nil)
		return
	}
	if err != nil {
		log.Printf("register: %v", err)
		httpx.WriteInternalError(w)
		return
	}

	httpx.WriteData(w, http.StatusCreated, newAuthResponse(result))
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		reqErr.write(w)
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if reqErr := validateStruct(req); reqErr != nil {
		reqErr.write(w)
		return
	}

	result, err := h.service.Login(r.Context(), service.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if errors.Is(err, domain.ErrInvalidCredentials) {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeInvalidCredentials, "Invalid email or password", nil)
		return
	}
	if err != nil {
		log.Printf("login: %v", err)
		httpx.WriteInternalError(w)
		return
	}

	httpx.WriteData(w, http.StatusOK, newAuthResponse(result))
}

func newAuthResponse(result service.AuthResult) AuthResponse {
	return AuthResponse{
		User: UserResponse{
			ID:    result.User.ID.String(),
			Name:  result.User.Name,
			Email: result.User.Email,
			Role:  result.User.Role,
		},
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339),
	}
}
