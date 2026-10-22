package handler

import (
	"net/http"

	"mini-ecommerce/internal/httpx"
)

func RouteNotFound(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Route not found", nil)
}

func MethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeNotFound, "Method not allowed for this route", nil)
}
