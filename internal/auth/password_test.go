package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestNormalizeEmail(t *testing.T) {
	assert.Equal(t, "jane@example.com", NormalizeEmail("  Jane@Example.COM \t"))
}

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("supersecret", bcrypt.MinCost)
	require.NoError(t, err)
	assert.NotEqual(t, "supersecret", hash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("supersecret")))
}

func TestHashPasswordRejectsTooLongPassword(t *testing.T) {
	_, err := HashPassword(string(make([]byte, 73)), bcrypt.MinCost)
	assert.Error(t, err)
}
