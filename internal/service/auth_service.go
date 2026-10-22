package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/domain"
)

const dummyPassword = "timing-equalization-password"

type UserRepository interface {
	Create(ctx context.Context, user domain.User) (domain.User, error)
	FindByEmail(ctx context.Context, email string) (domain.User, error)
}

type TokenIssuer interface {
	Issue(userID uuid.UUID, role string) (string, time.Time, error)
}

type RegisterInput struct {
	Name     string
	Email    string
	Password string
}

type LoginInput struct {
	Email    string
	Password string
}

type AuthResult struct {
	User      domain.User
	Token     string
	ExpiresAt time.Time
}

type AuthService struct {
	users       UserRepository
	tokens      TokenIssuer
	hashCost    int
	dummyHash   []byte
	compareHash func(hash, password []byte) error
}

func NewAuthService(users UserRepository, tokens TokenIssuer) (*AuthService, error) {
	return newAuthService(users, tokens, bcrypt.DefaultCost)
}

func newAuthService(users UserRepository, tokens TokenIssuer, hashCost int) (*AuthService, error) {
	dummyHash, err := bcrypt.GenerateFromPassword([]byte(dummyPassword), hashCost)
	if err != nil {
		return nil, fmt.Errorf("hash dummy password: %w", err)
	}
	return &AuthService{
		users:       users,
		tokens:      tokens,
		hashCost:    hashCost,
		dummyHash:   dummyHash,
		compareHash: bcrypt.CompareHashAndPassword,
	}, nil
}

func (s *AuthService) Register(ctx context.Context, input RegisterInput) (AuthResult, error) {
	passwordHash, err := auth.HashPassword(input.Password, s.hashCost)
	if err != nil {
		return AuthResult{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return AuthResult{}, fmt.Errorf("generate user id: %w", err)
	}

	user, err := s.users.Create(ctx, domain.User{
		ID:           id,
		Name:         strings.TrimSpace(input.Name),
		Email:        auth.NormalizeEmail(input.Email),
		PasswordHash: passwordHash,
		Role:         domain.RoleCustomer,
	})
	if err != nil {
		return AuthResult{}, err
	}
	return s.issue(user)
}

func (s *AuthService) Login(ctx context.Context, input LoginInput) (AuthResult, error) {
	user, err := s.users.FindByEmail(ctx, auth.NormalizeEmail(input.Email))
	if errors.Is(err, domain.ErrNotFound) {
		_ = s.compareHash(s.dummyHash, []byte(input.Password))
		return AuthResult{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return AuthResult{}, err
	}

	err = s.compareHash([]byte(user.PasswordHash), []byte(input.Password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return AuthResult{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return AuthResult{}, fmt.Errorf("compare password: %w", err)
	}
	return s.issue(user)
}

func (s *AuthService) issue(user domain.User) (AuthResult, error) {
	token, expiresAt, err := s.tokens.Issue(user.ID, user.Role)
	if err != nil {
		return AuthResult{}, fmt.Errorf("issue token: %w", err)
	}
	return AuthResult{User: user, Token: token, ExpiresAt: expiresAt}, nil
}
