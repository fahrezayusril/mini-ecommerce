package server

import (
	"fmt"
	"net/http"
	"time"

	"mini-ecommerce/internal/config"
	"mini-ecommerce/internal/database"
)

type Server struct {
	port int

	db database.Service
}

func NewServer(cfg config.Config, db database.Service) *http.Server {
	NewServer := &Server{
		port: cfg.Port,

		db: db,
	}

	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", NewServer.port),
		Handler:      NewServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}
