package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"mini-ecommerce/internal/auth"
)

const (
	minJWTSecretLength = 32
	defaultJWTTTL      = "24h"
)

type SeedUser struct {
	Name     string
	Email    string
	Password string
}

type Config struct {
	Port         int
	DatabaseURL  string
	JWTSecret    string
	JWTTTL       time.Duration
	SeedOnStart  bool
	SeedAdmin    SeedUser
	SeedCustomer SeedUser
}

func Load() (Config, error) {
	var cfg Config
	var problems []error

	port, err := strconv.Atoi(envOrDefault("PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		problems = append(problems, errors.New("PORT must be a number between 1 and 65535"))
	}
	cfg.Port = port

	cfg.DatabaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if cfg.DatabaseURL == "" {
		problems = append(problems, errors.New("DATABASE_URL is required"))
	}

	cfg.JWTSecret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if len(cfg.JWTSecret) < minJWTSecretLength {
		problems = append(problems, fmt.Errorf("JWT_SECRET must be at least %d characters", minJWTSecretLength))
	}

	jwtTTL, err := time.ParseDuration(envOrDefault("JWT_TTL", defaultJWTTTL))
	if err != nil || jwtTTL <= 0 {
		problems = append(problems, errors.New("JWT_TTL must be a positive duration such as 24h"))
	}
	cfg.JWTTTL = jwtTTL

	seedOnStart, err := strconv.ParseBool(envOrDefault("SEED_ON_START", "false"))
	if err != nil {
		problems = append(problems, errors.New("SEED_ON_START must be a boolean"))
	}
	cfg.SeedOnStart = seedOnStart

	cfg.SeedAdmin = loadSeedUser("SEED_ADMIN")
	cfg.SeedCustomer = loadSeedUser("SEED_CUSTOMER")
	if cfg.SeedOnStart {
		problems = append(problems, cfg.SeedAdmin.validate("SEED_ADMIN")...)
		problems = append(problems, cfg.SeedCustomer.validate("SEED_CUSTOMER")...)
		if cfg.SeedAdmin.Email != "" && auth.NormalizeEmail(cfg.SeedAdmin.Email) == auth.NormalizeEmail(cfg.SeedCustomer.Email) {
			problems = append(problems, errors.New("SEED_ADMIN_EMAIL and SEED_CUSTOMER_EMAIL must be different"))
		}
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(problems...))
	}
	return cfg, nil
}

func loadSeedUser(prefix string) SeedUser {
	return SeedUser{
		Name:     strings.TrimSpace(os.Getenv(prefix + "_NAME")),
		Email:    strings.TrimSpace(os.Getenv(prefix + "_EMAIL")),
		Password: os.Getenv(prefix + "_PASSWORD"),
	}
}

func (u SeedUser) validate(prefix string) []error {
	var problems []error
	if u.Name == "" || len(u.Name) > 100 {
		problems = append(problems, fmt.Errorf("%s_NAME must be 1-100 characters", prefix))
	}
	if !strings.Contains(u.Email, "@") || len(u.Email) > 255 {
		problems = append(problems, fmt.Errorf("%s_EMAIL must be a valid email", prefix))
	}
	if len(u.Password) < 8 || len(u.Password) > 72 {
		problems = append(problems, fmt.Errorf("%s_PASSWORD must be 8-72 characters", prefix))
	}
	return problems
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
