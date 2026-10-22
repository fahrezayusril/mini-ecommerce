package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"mini-ecommerce/internal/config"
	"mini-ecommerce/internal/database"
	"mini-ecommerce/internal/server"
)

func gracefulShutdown(apiServer *http.Server, done chan bool) {
	// Create context that listens for the interrupt signal from the OS.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Listen for the interrupt signal.
	<-ctx.Done()

	log.Println("shutting down gracefully, press Ctrl+C again to force")
	stop() // Allow Ctrl+C to force shutdown

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown with error: %v", err)
	}

	log.Println("Server exiting")

	// Notify the main goroutine that the shutdown is complete
	done <- true
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := bootstrapDatabase(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	server := server.NewServer(cfg, db)

	// Create a done channel to signal when the shutdown is complete
	done := make(chan bool, 1)

	// Run graceful shutdown in a separate goroutine
	go gracefulShutdown(server, done)

	log.Printf("http server listening on :%d", cfg.Port)
	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("http server error: %s", err))
	}

	// Wait for the graceful shutdown to complete
	<-done
	log.Println("Graceful shutdown complete.")
}

func bootstrapDatabase(cfg config.Config) (database.Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	if err := database.MigrateUp(pool); err != nil {
		pool.Close()
		return nil, err
	}
	log.Println("database migrations applied")

	if cfg.SeedOnStart {
		result, err := database.Seed(ctx, pool, cfg.SeedAdmin)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("seed database: %w", err)
		}
		log.Printf("database seeded: admin_created=%t products_created=%d", result.AdminCreated, result.ProductsCreated)
	}

	return database.New(pool), nil
}
