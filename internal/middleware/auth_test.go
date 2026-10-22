package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/domain"
)

var testSecret = strings.Repeat("m", 32)

func newTokenManager() *auth.TokenManager {
	return auth.NewTokenManager(testSecret, time.Hour)
}

func issueToken(t *testing.T, userID uuid.UUID, role string) string {
	t.Helper()
	token, _, err := newTokenManager().Issue(userID, role)
	require.NoError(t, err)
	return token
}

func signToken(t *testing.T, secret string, userID uuid.UUID, role string, expiresAt time.Time) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  userID.String(),
		"role": role,
		"exp":  expiresAt.Unix(),
	}).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}

type capturedRequest struct {
	called    bool
	principal auth.Principal
	found     bool
}

func captureHandler(captured *capturedRequest) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.called = true
		captured.principal, captured.found = PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
}

func serve(handler http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	assert.Equal(t, status, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"code":"`+code+`"`)
}

func TestAuthenticateRejectsInvalidRequests(t *testing.T) {
	userID := uuid.Must(uuid.NewV7())
	validToken := issueToken(t, userID, domain.RoleCustomer)

	tests := []struct {
		name          string
		authorization string
	}{
		{name: "missing header", authorization: ""},
		{name: "wrong scheme", authorization: "Token " + validToken},
		{name: "basic scheme", authorization: "Basic dXNlcjpwYXNz"},
		{name: "scheme without token", authorization: "Bearer"},
		{name: "scheme with blank token", authorization: "Bearer    "},
		{name: "token without scheme", authorization: validToken},
		{name: "extra segment", authorization: "Bearer " + validToken + " extra"},
		{name: "not a jwt", authorization: "Bearer not-a-jwt"},
		{name: "wrong signature", authorization: "Bearer " + signToken(t, strings.Repeat("x", 32), userID, domain.RoleCustomer, time.Now().Add(time.Hour))},
		{name: "tampered signature", authorization: "Bearer " + validToken[:len(validToken)-2] + "xx"},
		{name: "expired", authorization: "Bearer " + signToken(t, testSecret, userID, domain.RoleCustomer, time.Now().Add(-time.Minute))},
		{name: "unknown role", authorization: "Bearer " + signToken(t, testSecret, userID, "superuser", time.Now().Add(time.Hour))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured capturedRequest
			handler := Authenticate(newTokenManager())(captureHandler(&captured))

			rec := serve(handler, tt.authorization)

			assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
			assert.Equal(t, `Bearer realm="api"`, rec.Header().Get("WWW-Authenticate"))
			assert.False(t, captured.called)
		})
	}
}

func TestAuthenticateValidTokenStoresPrincipal(t *testing.T) {
	userID := uuid.Must(uuid.NewV7())
	token := issueToken(t, userID, domain.RoleAdmin)

	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		t.Run(scheme, func(t *testing.T) {
			var captured capturedRequest
			handler := Authenticate(newTokenManager())(captureHandler(&captured))

			rec := serve(handler, scheme+" "+token)

			assert.Equal(t, http.StatusNoContent, rec.Code)
			require.True(t, captured.called)
			require.True(t, captured.found)
			assert.Equal(t, auth.Principal{UserID: userID, Role: domain.RoleAdmin}, captured.principal)
		})
	}
}

func TestRequireRole(t *testing.T) {
	userID := uuid.Must(uuid.NewV7())

	tests := []struct {
		name       string
		allowed    []string
		tokenRole  string
		wantStatus int
		wantCode   string
	}{
		{name: "admin allowed", allowed: []string{domain.RoleAdmin}, tokenRole: domain.RoleAdmin, wantStatus: http.StatusNoContent},
		{name: "customer allowed", allowed: []string{domain.RoleCustomer}, tokenRole: domain.RoleCustomer, wantStatus: http.StatusNoContent},
		{name: "any of several roles", allowed: []string{domain.RoleAdmin, domain.RoleCustomer}, tokenRole: domain.RoleCustomer, wantStatus: http.StatusNoContent},
		{name: "customer on admin route", allowed: []string{domain.RoleAdmin}, tokenRole: domain.RoleCustomer, wantStatus: http.StatusForbidden, wantCode: "FORBIDDEN"},
		{name: "admin on customer route", allowed: []string{domain.RoleCustomer}, tokenRole: domain.RoleAdmin, wantStatus: http.StatusForbidden, wantCode: "FORBIDDEN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured capturedRequest
			handler := Authenticate(newTokenManager())(RequireRole(tt.allowed...)(captureHandler(&captured)))

			rec := serve(handler, "Bearer "+issueToken(t, userID, tt.tokenRole))

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantCode != "" {
				assertErrorResponse(t, rec, tt.wantStatus, tt.wantCode)
				assert.False(t, captured.called)
				return
			}
			assert.True(t, captured.called)
			assert.Equal(t, tt.tokenRole, captured.principal.Role)
		})
	}
}

func TestRequireRoleWithoutAuthenticateIsUnauthorized(t *testing.T) {
	var captured capturedRequest
	handler := RequireRole(domain.RoleAdmin)(captureHandler(&captured))

	rec := serve(handler, "")

	assertErrorResponse(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
	assert.False(t, captured.called)
}

func TestPrincipalFromContextMissing(t *testing.T) {
	_, ok := PrincipalFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context())
	assert.False(t, ok)
}
