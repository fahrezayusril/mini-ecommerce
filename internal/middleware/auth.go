package middleware

import (
	"net/http"
	"slices"
	"strings"

	"mini-ecommerce/internal/auth"
	"mini-ecommerce/internal/httpx"
)

const bearerScheme = "bearer"

type TokenParser interface {
	Parse(token string) (auth.Principal, error)
}

func Authenticate(parser TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeUnauthorized(w, "Missing or malformed bearer token")
				return
			}

			principal, err := parser.Parse(token)
			if err != nil {
				writeUnauthorized(w, "Invalid or expired token")
				return
			}

			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				writeUnauthorized(w, "Authentication required")
				return
			}
			if !slices.Contains(roles, principal.Role) {
				httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "You do not have permission to perform this action", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, bearerScheme) {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
	httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, message, nil)
}
