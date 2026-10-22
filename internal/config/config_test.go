package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://app:app@localhost:5432/app?sslmode=disable")
	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv("JWT_TTL", "")
	t.Setenv("SEED_ON_START", "false")
}

func TestLoadValid(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("s", 32), cfg.JWTSecret)
	assert.Equal(t, 24*time.Hour, cfg.JWTTTL)
}

func TestLoadCustomJWTTTL(t *testing.T) {
	setValidEnv(t)
	t.Setenv("JWT_TTL", "90m")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 90*time.Minute, cfg.JWTTTL)
}

func TestLoadRejectsInvalidJWTSettings(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "empty secret", key: "JWT_SECRET", value: "", wantErr: "JWT_SECRET must be at least 32 characters"},
		{name: "whitespace secret", key: "JWT_SECRET", value: "   ", wantErr: "JWT_SECRET must be at least 32 characters"},
		{name: "short secret", key: "JWT_SECRET", value: strings.Repeat("s", 31), wantErr: "JWT_SECRET must be at least 32 characters"},
		{name: "unparsable ttl", key: "JWT_TTL", value: "one day", wantErr: "JWT_TTL must be a positive duration"},
		{name: "zero ttl", key: "JWT_TTL", value: "0s", wantErr: "JWT_TTL must be a positive duration"},
		{name: "negative ttl", key: "JWT_TTL", value: "-1h", wantErr: "JWT_TTL must be a positive duration"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(tt.key, tt.value)

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

var seedKeys = []string{
	"SEED_ADMIN_NAME",
	"SEED_ADMIN_EMAIL",
	"SEED_ADMIN_PASSWORD",
	"SEED_CUSTOMER_NAME",
	"SEED_CUSTOMER_EMAIL",
	"SEED_CUSTOMER_PASSWORD",
}

func setValidSeedEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SEED_ON_START", "true")
	t.Setenv("SEED_ADMIN_NAME", "Administrator")
	t.Setenv("SEED_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("SEED_ADMIN_PASSWORD", "supersecret123")
	t.Setenv("SEED_CUSTOMER_NAME", "Customer")
	t.Setenv("SEED_CUSTOMER_EMAIL", "customer@example.com")
	t.Setenv("SEED_CUSTOMER_PASSWORD", "supersecret456")
}

func TestLoadSeedUsers(t *testing.T) {
	setValidEnv(t)
	setValidSeedEnv(t)

	cfg, err := Load()
	require.NoError(t, err)
	assert.True(t, cfg.SeedOnStart)
	assert.Equal(t, SeedUser{Name: "Administrator", Email: "admin@example.com", Password: "supersecret123"}, cfg.SeedAdmin)
	assert.Equal(t, SeedUser{Name: "Customer", Email: "customer@example.com", Password: "supersecret456"}, cfg.SeedCustomer)
}

func TestLoadIgnoresSeedUsersWhenSeedingDisabled(t *testing.T) {
	setValidEnv(t)
	for _, key := range seedKeys {
		t.Setenv(key, "")
	}

	_, err := Load()
	require.NoError(t, err)
}

func TestLoadRequiresEverySeedVariable(t *testing.T) {
	for _, key := range seedKeys {
		t.Run(key, func(t *testing.T) {
			setValidEnv(t)
			setValidSeedEnv(t)
			t.Setenv(key, "")

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), key+" must be")
		})
	}
}

func TestLoadRejectsInvalidSeedValues(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "whitespace customer name", key: "SEED_CUSTOMER_NAME", value: "   ", wantErr: "SEED_CUSTOMER_NAME must be 1-100 characters"},
		{name: "long customer name", key: "SEED_CUSTOMER_NAME", value: strings.Repeat("n", 101), wantErr: "SEED_CUSTOMER_NAME must be 1-100 characters"},
		{name: "customer email without at", key: "SEED_CUSTOMER_EMAIL", value: "customer.example.com", wantErr: "SEED_CUSTOMER_EMAIL must be a valid email"},
		{name: "short customer password", key: "SEED_CUSTOMER_PASSWORD", value: "short", wantErr: "SEED_CUSTOMER_PASSWORD must be 8-72 characters"},
		{name: "long customer password", key: "SEED_CUSTOMER_PASSWORD", value: strings.Repeat("p", 73), wantErr: "SEED_CUSTOMER_PASSWORD must be 8-72 characters"},
		{name: "customer email equals admin email", key: "SEED_CUSTOMER_EMAIL", value: " ADMIN@Example.com ", wantErr: "SEED_ADMIN_EMAIL and SEED_CUSTOMER_EMAIL must be different"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnv(t)
			setValidSeedEnv(t)
			t.Setenv(tt.key, tt.value)

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
