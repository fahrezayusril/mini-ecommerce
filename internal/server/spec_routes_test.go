package server

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"mini-ecommerce/api"
)

var (
	pathParam         = regexp.MustCompile(`\{[^}]+\}`)
	openAPIOperations = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
)

func normalizeRoute(method, path string) string {
	path = pathParam.ReplaceAllString(path, "{}")
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return strings.ToUpper(method) + " " + path
}

func routerOperations(t *testing.T) map[string]bool {
	t.Helper()
	s := &Server{db: fakeDatabase{}}
	router, ok := s.RegisterRoutes().(chi.Routes)
	require.True(t, ok)

	operations := map[string]bool{}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		operations[normalizeRoute(method, route)] = true
		return nil
	})
	require.NoError(t, err)
	return operations
}

func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(api.OpenAPI, &spec))
	require.NotEmpty(t, spec.Paths)

	operations := map[string]bool{}
	for path, item := range spec.Paths {
		for key := range item {
			if slices.Contains(openAPIOperations, key) {
				operations[normalizeRoute(key, path)] = true
			}
		}
	}
	return operations
}

func missingFrom(source, target map[string]bool) []string {
	var missing []string
	for operation := range source {
		if !target[operation] {
			missing = append(missing, operation)
		}
	}
	slices.Sort(missing)
	return missing
}

func TestEveryRouteIsDocumentedAndEverySpecOperationIsRouted(t *testing.T) {
	routes := routerOperations(t)
	documented := specOperations(t)

	require.Empty(t, missingFrom(routes, documented), "routes missing from api/openapi.yaml")
	require.Empty(t, missingFrom(documented, routes), "spec operations without a route")
	require.Contains(t, routes, "GET /health")
	require.Contains(t, routes, "GET /docs")
	require.Contains(t, routes, "GET /docs/openapi.yaml")
}

func TestNormalizeRoute(t *testing.T) {
	cases := map[[2]string]string{
		{"get", "/api/v1/products/"}:                 "GET /api/v1/products",
		{"GET", "/api/v1/products/{id}"}:             "GET /api/v1/products/{}",
		{"patch", "/api/v1/orders/{orderId}/status"}: "PATCH /api/v1/orders/{}/status",
		{"GET", "/"}: "GET /",
	}
	for input, want := range cases {
		require.Equal(t, want, normalizeRoute(input[0], input[1]))
	}
}
