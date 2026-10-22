package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func TestHealthHandlerCheck(t *testing.T) {
	tests := []struct {
		name       string
		pinger     pingerFunc
		wantStatus int
		wantBody   string
	}{
		{
			name:       "database reachable",
			pinger:     func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok","database":"ok"}`,
		},
		{
			name:       "database ping fails",
			pinger:     func(context.Context) error { return errors.New("connection refused") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"degraded","database":"unreachable"}`,
		},
		{
			name: "database ping exceeds timeout",
			pinger: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"degraded","database":"unreachable"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &HealthHandler{db: tt.pinger, pingTimeout: 50 * time.Millisecond}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/health", nil)

			start := time.Now()
			h.Check(rec, req)
			require.Less(t, time.Since(start), time.Second)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}
