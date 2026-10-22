package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/domain"
)

var testSecret = strings.Repeat("k", 32)

func newTestManager(now time.Time) *TokenManager {
	manager := NewTokenManager(testSecret, 24*time.Hour)
	manager.now = func() time.Time { return now }
	return manager
}

func signClaims(t *testing.T, method jwt.SigningMethod, key any, c jwt.Claims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(method, c).SignedString(key)
	require.NoError(t, err)
	return signed
}

func TestIssueAndParse(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	manager := newTestManager(now)
	userID := uuid.Must(uuid.NewV7())

	token, expiresAt, err := manager.Issue(userID, domain.RoleCustomer)
	require.NoError(t, err)
	assert.Equal(t, now.Add(24*time.Hour), expiresAt)

	principal, err := manager.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, Principal{UserID: userID, Role: domain.RoleCustomer}, principal)

	parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	require.NoError(t, err)
	assert.Equal(t, "HS256", parsed.Method.Alg())
	mapClaims := parsed.Claims.(jwt.MapClaims)
	assert.Equal(t, userID.String(), mapClaims["sub"])
	assert.Equal(t, domain.RoleCustomer, mapClaims["role"])
	assert.EqualValues(t, expiresAt.Unix(), mapClaims["exp"])
}

func TestParseRejectsInvalidTokens(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	manager := newTestManager(now)
	userID := uuid.Must(uuid.NewV7()).String()
	future := jwt.NewNumericDate(now.Add(time.Hour))

	validClaims := func() claims {
		return claims{Role: domain.RoleAdmin, RegisteredClaims: jwt.RegisteredClaims{Subject: userID, ExpiresAt: future}}
	}

	expired := validClaims()
	expired.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Second))
	missingExpiry := validClaims()
	missingExpiry.ExpiresAt = nil
	badSubject := validClaims()
	badSubject.Subject = "not-a-uuid"
	badRole := validClaims()
	badRole.Role = "superuser"

	tests := []struct {
		name  string
		token string
	}{
		{name: "malformed", token: "not.a.jwt"},
		{name: "wrong secret", token: signClaims(t, jwt.SigningMethodHS256, []byte(strings.Repeat("x", 32)), validClaims())},
		{name: "none algorithm", token: signClaims(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, validClaims())},
		{name: "other hmac algorithm", token: signClaims(t, jwt.SigningMethodHS512, []byte(testSecret), validClaims())},
		{name: "expired", token: signClaims(t, jwt.SigningMethodHS256, []byte(testSecret), expired)},
		{name: "missing expiry", token: signClaims(t, jwt.SigningMethodHS256, []byte(testSecret), missingExpiry)},
		{name: "subject not uuid", token: signClaims(t, jwt.SigningMethodHS256, []byte(testSecret), badSubject)},
		{name: "unknown role", token: signClaims(t, jwt.SigningMethodHS256, []byte(testSecret), badRole)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.Parse(tt.token)
			assert.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}
