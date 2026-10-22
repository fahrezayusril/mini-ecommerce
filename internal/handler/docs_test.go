package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/api"
)

func TestDocsUI(t *testing.T) {
	rec := httptest.NewRecorder()
	NewDocsHandler(api.OpenAPI).UI(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, `https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js`)
	assert.Contains(t, body, `https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css`)
	assert.Contains(t, body, `integrity="sha384-`)
	assert.Contains(t, body, `url: "/docs/openapi.yaml"`)
	assert.Contains(t, body, `persistAuthorization: true`)
}

func TestDocsSpecMatchesFileExactly(t *testing.T) {
	onDisk, err := os.ReadFile("../../api/openapi.yaml")
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	NewDocsHandler(api.OpenAPI).Spec(rec, httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.Equal(t, onDisk, rec.Body.Bytes())
}

func TestDocsRedirectToUI(t *testing.T) {
	rec := httptest.NewRecorder()
	NewDocsHandler(api.OpenAPI).RedirectToUI(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))

	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "/docs", rec.Header().Get("Location"))
}

func TestFallbackHandlersUseErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	RouteNotFound(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"Route not found"}}`, rec.Body.String())

	rec = httptest.NewRecorder()
	MethodNotAllowed(rec, httptest.NewRequest(http.MethodDelete, "/health", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"Method not allowed for this route"}}`, rec.Body.String())
}
