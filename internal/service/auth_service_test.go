package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"mini-ecommerce/internal/domain"
)

type fakeUserRepository struct {
	users     map[string]domain.User
	createErr error
	findErr   error
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: map[string]domain.User{}}
}

func (r *fakeUserRepository) Create(_ context.Context, user domain.User) (domain.User, error) {
	if r.createErr != nil {
		return domain.User{}, r.createErr
	}
	key := strings.ToLower(user.Email)
	if _, exists := r.users[key]; exists {
		return domain.User{}, domain.ErrEmailAlreadyExists
	}
	user.CreatedAt = time.Now()
	user.UpdatedAt = user.CreatedAt
	r.users[key] = user
	return user, nil
}

func (r *fakeUserRepository) FindByEmail(_ context.Context, email string) (domain.User, error) {
	if r.findErr != nil {
		return domain.User{}, r.findErr
	}
	user, ok := r.users[strings.ToLower(email)]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}

type fakeTokenIssuer struct {
	expiresAt time.Time
	err       error
	issuedFor []uuid.UUID
}

func (f *fakeTokenIssuer) Issue(userID uuid.UUID, role string) (string, time.Time, error) {
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	f.issuedFor = append(f.issuedFor, userID)
	return "token-for-" + role, f.expiresAt, nil
}

func newTestService(t *testing.T) (*AuthService, *fakeUserRepository, *fakeTokenIssuer) {
	t.Helper()
	users := newFakeUserRepository()
	tokens := &fakeTokenIssuer{expiresAt: time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)}
	svc, err := newAuthService(users, tokens, bcrypt.MinCost)
	require.NoError(t, err)
	return svc, users, tokens
}

func registerJane(t *testing.T, svc *AuthService) AuthResult {
	t.Helper()
	result, err := svc.Register(context.Background(), RegisterInput{
		Name:     "  Jane Doe  ",
		Email:    "  Jane.Doe@Example.COM ",
		Password: "supersecret",
	})
	require.NoError(t, err)
	return result
}

func TestRegisterCreatesCustomerWithLowercaseEmailAndHashedPassword(t *testing.T) {
	svc, users, tokens := newTestService(t)

	result := registerJane(t, svc)

	assert.Equal(t, "Jane Doe", result.User.Name)
	assert.Equal(t, "jane.doe@example.com", result.User.Email)
	assert.Equal(t, domain.RoleCustomer, result.User.Role)
	assert.Equal(t, uuid.Version(7), result.User.ID.Version())
	assert.Equal(t, "token-for-customer", result.Token)
	assert.Equal(t, tokens.expiresAt, result.ExpiresAt)
	assert.Equal(t, []uuid.UUID{result.User.ID}, tokens.issuedFor)

	stored := users.users["jane.doe@example.com"]
	assert.NotEqual(t, "supersecret", stored.PasswordHash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("supersecret")))
}

func TestRegisterRejectsDuplicateEmailCaseInsensitive(t *testing.T) {
	svc, _, _ := newTestService(t)
	registerJane(t, svc)

	_, err := svc.Register(context.Background(), RegisterInput{
		Name:     "Another Jane",
		Email:    "JANE.DOE@example.com",
		Password: "anothersecret",
	})
	assert.ErrorIs(t, err, domain.ErrEmailAlreadyExists)
}

func TestRegisterPropagatesRepositoryError(t *testing.T) {
	svc, users, _ := newTestService(t)
	users.createErr = errors.New("connection reset")

	_, err := svc.Register(context.Background(), RegisterInput{Name: "Jane", Email: "jane@example.com", Password: "supersecret"})
	assert.EqualError(t, err, "connection reset")
}

func TestRegisterPropagatesTokenError(t *testing.T) {
	svc, _, tokens := newTestService(t)
	tokens.err = errors.New("signing failed")

	_, err := svc.Register(context.Background(), RegisterInput{Name: "Jane", Email: "jane@example.com", Password: "supersecret"})
	assert.ErrorContains(t, err, "signing failed")
}

func TestLoginSucceedsWithAnyEmailCase(t *testing.T) {
	svc, _, _ := newTestService(t)
	registered := registerJane(t, svc)

	result, err := svc.Login(context.Background(), LoginInput{Email: " JANE.doe@example.com ", Password: "supersecret"})
	require.NoError(t, err)
	assert.Equal(t, registered.User.ID, result.User.ID)
	assert.Equal(t, "token-for-customer", result.Token)
}

func TestLoginWrongPasswordReturnsInvalidCredentials(t *testing.T) {
	svc, _, tokens := newTestService(t)
	registerJane(t, svc)
	tokens.issuedFor = nil

	_, err := svc.Login(context.Background(), LoginInput{Email: "jane.doe@example.com", Password: "wrongpassword"})
	assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
	assert.Empty(t, tokens.issuedFor)
}

func TestLoginUnknownEmailComparesAgainstDummyHash(t *testing.T) {
	svc, _, tokens := newTestService(t)

	var comparedHashes [][]byte
	svc.compareHash = func(hash, password []byte) error {
		comparedHashes = append(comparedHashes, hash)
		return bcrypt.CompareHashAndPassword(hash, password)
	}

	_, err := svc.Login(context.Background(), LoginInput{Email: "nobody@example.com", Password: "supersecret"})
	assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
	require.Len(t, comparedHashes, 1)
	assert.Equal(t, svc.dummyHash, comparedHashes[0])
	assert.Empty(t, tokens.issuedFor)

	dummyCost, err := bcrypt.Cost(svc.dummyHash)
	require.NoError(t, err)
	assert.Equal(t, svc.hashCost, dummyCost)
}

func TestLoginUnknownEmailAndWrongPasswordReturnSameError(t *testing.T) {
	svc, _, _ := newTestService(t)
	registerJane(t, svc)

	_, unknownEmailErr := svc.Login(context.Background(), LoginInput{Email: "nobody@example.com", Password: "supersecret"})
	_, wrongPasswordErr := svc.Login(context.Background(), LoginInput{Email: "jane.doe@example.com", Password: "wrongpassword"})
	assert.Equal(t, unknownEmailErr, wrongPasswordErr)
}

func TestLoginPropagatesRepositoryError(t *testing.T) {
	svc, users, _ := newTestService(t)
	users.findErr = errors.New("connection reset")

	_, err := svc.Login(context.Background(), LoginInput{Email: "jane@example.com", Password: "supersecret"})
	assert.EqualError(t, err, "connection reset")
}

func TestNewAuthServiceUsesDefaultCostForDummyHash(t *testing.T) {
	svc, err := NewAuthService(newFakeUserRepository(), &fakeTokenIssuer{})
	require.NoError(t, err)

	cost, err := bcrypt.Cost(svc.dummyHash)
	require.NoError(t, err)
	assert.Equal(t, bcrypt.DefaultCost, cost)
}
