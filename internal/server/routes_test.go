package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
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
