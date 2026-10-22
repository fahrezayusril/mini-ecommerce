package middleware

import (
	"errors"
	"log"
	"net/http"
	"runtime/debug"

	"mini-ecommerce/internal/httpx"
)

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(recovered)
			}
			log.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.Path, recovered, debug.Stack())
			httpx.WriteInternalError(w)
		}()
		next.ServeHTTP(w, r)
	})
}
