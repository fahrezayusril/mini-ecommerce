package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"mini-ecommerce/internal/domain"
)

var ErrInvalidToken = errors.New("invalid token")

type Principal struct {
	UserID uuid.UUID
	Role   string
}

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl, now: time.Now}
}

func (m *TokenManager) Issue(userID uuid.UUID, role string) (string, time.Time, error) {
	issuedAt := m.now().UTC().Truncate(time.Second)
	expiresAt := issuedAt.Add(m.ttl)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	})

	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

func (m *TokenManager) Parse(tokenString string) (Principal, error) {
	var parsed claims
	_, err := jwt.ParseWithClaims(tokenString, &parsed,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	userID, err := uuid.Parse(parsed.Subject)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: subject is not a uuid", ErrInvalidToken)
	}
	if !domain.IsValidRole(parsed.Role) {
		return Principal{}, fmt.Errorf("%w: unknown role", ErrInvalidToken)
	}
	return Principal{UserID: userID, Role: parsed.Role}, nil
}
