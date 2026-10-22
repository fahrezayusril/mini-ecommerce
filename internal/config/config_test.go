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
