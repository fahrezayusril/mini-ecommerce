package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mini-ecommerce/internal/database"
	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/repository"
)

func setupPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, database.MigrateDown(pool))
	require.NoError(t, database.MigrateUp(pool))
	return pool
}

func newUser(email string) domain.User {
	return domain.User{
		ID:           uuid.Must(uuid.NewV7()),
		Name:         "Jane Doe",
		Email:        email,
		PasswordHash: "$2a$10$abcdefghijklmnopqrstuuJ0eY7Qm2b4X0k1ZQm7xQ5r5yq3eW6a",
		Role:         domain.RoleCustomer,
	}
}

func TestUserRepositoryCreateAndFindByEmail(t *testing.T) {
	repo := repository.NewUserRepository(setupPool(t))
	ctx := context.Background()

	created, err := repo.Create(ctx, newUser("jane@example.com"))
	require.NoError(t, err)
	assert.False(t, created.CreatedAt.IsZero())
	assert.False(t, created.UpdatedAt.IsZero())

	found, err := repo.FindByEmail(ctx, "JANE@Example.COM")
	require.NoError(t, err)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, "jane@example.com", found.Email)
	assert.Equal(t, created.PasswordHash, found.PasswordHash)
	assert.Equal(t, domain.RoleCustomer, found.Role)
}

func TestUserRepositoryFindByEmailNotFound(t *testing.T) {
	repo := repository.NewUserRepository(setupPool(t))

	_, err := repo.FindByEmail(context.Background(), "missing@example.com")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestUserRepositoryCreateRejectsDuplicateEmailCaseInsensitive(t *testing.T) {
	repo := repository.NewUserRepository(setupPool(t))
	ctx := context.Background()

	_, err := repo.Create(ctx, newUser("jane@example.com"))
	require.NoError(t, err)

	_, err = repo.Create(ctx, newUser("Jane@Example.com"))
	assert.ErrorIs(t, err, domain.ErrEmailAlreadyExists)
}
