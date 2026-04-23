package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	apperrors "stride/backend/internal/errors"
	"stride/backend/internal/logger"
)

// Recovery returns a middleware that recovers from panics.
func Recovery(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// Get stack trace
					stack := debug.Stack()

					// Log the panic with context
					log.WithContext(r.Context()).Error("panic recovered",
						"panic", fmt.Sprintf("%v", rec),
						"stack", string(stack),
						"method", r.Method,
						"path", r.URL.Path,
					)

					// Return internal server error
					apperrors.WriteError(w, apperrors.NewInternalError(
						fmt.Errorf("panic: %v", rec),
					))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RecoverySimple is a simpler recovery middleware that logs to standard logger.
// Use this when you don't have a structured logger available.
func RecoverySimple(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// Log the panic
				fmt.Printf("PANIC: %v\n%s\n", rec, debug.Stack())

				// Return internal server error
				apperrors.WriteError(w, apperrors.NewInternalError(
					fmt.Errorf("panic: %v", rec),
				))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
