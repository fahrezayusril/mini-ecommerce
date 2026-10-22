package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"mini-ecommerce/internal/domain"
	"mini-ecommerce/internal/handler"
	appmiddleware "mini-ecommerce/internal/middleware"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	healthHandler := handler.NewHealthHandler(s.db)
	r.Get("/health", healthHandler.Check)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", s.authHandler.Register)
		r.Post("/auth/login", s.authHandler.Login)

		r.Route("/products", func(r chi.Router) {
			r.Get("/", s.productHandler.List)
			r.Get("/{id}", s.productHandler.Get)

			r.Group(func(r chi.Router) {
				r.Use(appmiddleware.Authenticate(s.tokenParser), appmiddleware.RequireRole(domain.RoleAdmin))
				r.Post("/", s.productHandler.Create)
				r.Put("/{id}", s.productHandler.Update)
				r.Delete("/{id}", s.productHandler.Delete)
			})
		})
	})

	return r
}
