package server

import (
	"fmt"
	"net/http"
	"time"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/config"
	"mini-ecommerce/internal/database"
	"mini-ecommerce/internal/handler"
	appmiddleware "mini-ecommerce/internal/middleware"
	"mini-ecommerce/internal/repository"
	"mini-ecommerce/internal/service"
)

type Server struct {
	port int

	db database.Service

	tokenParser    appmiddleware.TokenParser
	authHandler    *handler.AuthHandler
	productHandler *handler.ProductHandler
}

func NewServer(cfg config.Config, db database.Service) (*http.Server, error) {
	tokenManager := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTTTL)
	userRepository := repository.NewUserRepository(db.Pool())
	authService, err := service.NewAuthService(userRepository, tokenManager)
	if err != nil {
		return nil, fmt.Errorf("create auth service: %w", err)
	}

	NewServer := &Server{
		port: cfg.Port,

		db: db,

		tokenParser:    tokenManager,
		authHandler:    handler.NewAuthHandler(authService),
		productHandler: handler.NewProductHandler(service.NewProductService(repository.NewProductRepository(db.Pool()))),
	}

	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", NewServer.port),
		Handler:      NewServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server, nil
}
