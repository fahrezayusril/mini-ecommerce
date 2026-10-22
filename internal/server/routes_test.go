package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"

	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/handler"
	"mini-ecommerce/internal/service"
)

type fakeDatabase struct {
	pingErr error
}

func (f fakeDatabase) Ping(context.Context) error { return f.pingErr }
func (f fakeDatabase) Pool() *pgxpool.Pool        { return nil }
func (f fakeDatabase) Close() error               { return nil }

func TestRegisterRoutesHealth(t *testing.T) {
	tests := []struct {
		name       string
		db         fakeDatabase
		wantStatus int
		wantBody   string
	}{
		{
			name:       "healthy",
			db:         fakeDatabase{},
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok","database":"ok"}`,
		},
		{
			name:       "database unreachable",
			db:         fakeDatabase{pingErr: errors.New("connection refused")},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"degraded","database":"unreachable"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{db: tt.db}
			rec := httptest.NewRecorder()
			s.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestRegisterRoutesRootIsNotFound(t *testing.T) {
	s := &Server{db: fakeDatabase{}}
	rec := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

type fakeAuthService struct{}

func (fakeAuthService) Register(context.Context, service.RegisterInput) (service.AuthResult, error) {
	return service.AuthResult{}, domain.ErrEmailAlreadyExists
}

func (fakeAuthService) Login(context.Context, service.LoginInput) (service.AuthResult, error) {
	return service.AuthResult{}, domain.ErrInvalidCredentials
}

func TestRegisterRoutesAuth(t *testing.T) {
	s := &Server{db: fakeDatabase{}, authHandler: handler.NewAuthHandler(fakeAuthService{})}

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "register", method: http.MethodPost, path: "/api/v1/auth/register", body: `{"name":"Jane","email":"jane@example.com","password":"supersecret"}`, wantStatus: http.StatusConflict},
		{name: "login", method: http.MethodPost, path: "/api/v1/auth/login", body: `{"email":"jane@example.com","password":"supersecret"}`, wantStatus: http.StatusUnauthorized},
		{name: "register wrong method", method: http.MethodGet, path: "/api/v1/auth/register", wantStatus: http.StatusMethodNotAllowed},
		{name: "unprefixed path", method: http.MethodPost, path: "/auth/login", body: `{}`, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			s.RegisterRoutes().ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
