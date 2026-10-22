package handler

import (
	"context"
	"log"
	"net/http"
	"time"

	"mini-ecommerce/internal/httpx"
)

const healthPingTimeout = 2 * time.Second

const (
	healthStatusOK            = "ok"
	healthStatusDegraded      = "degraded"
	databaseStatusOK          = "ok"
	databaseStatusUnreachable = "unreachable"
)

type DatabasePinger interface {
	Ping(ctx context.Context) error
}

type HealthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

type HealthHandler struct {
	db          DatabasePinger
	pingTimeout time.Duration
}

func NewHealthHandler(db DatabasePinger) *HealthHandler {
	return &HealthHandler{db: db, pingTimeout: healthPingTimeout}
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.pingTimeout)
	defer cancel()

	w.Header().Set("Cache-Control", "no-store")

	if err := h.db.Ping(ctx); err != nil {
		log.Printf("health check: database ping failed: %v", err)
		httpx.WriteJSON(w, http.StatusServiceUnavailable, HealthResponse{
			Status:   healthStatusDegraded,
			Database: databaseStatusUnreachable,
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, HealthResponse{
		Status:   healthStatusOK,
		Database: databaseStatusOK,
	})
}
