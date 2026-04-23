package middleware

import (
	"context"
	"net/http"
	"time"

	apperrors "stride/backend/internal/errors"
)

// Timeout creates a middleware that adds a timeout to the request context.
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			// Create a channel to signal when the handler completes
			done := make(chan struct{})

			// Wrap the response writer to detect if we've already written
			tw := &timeoutWriter{
				ResponseWriter: w,
				written:        false,
			}

			go func() {
				next.ServeHTTP(tw, r.WithContext(ctx))
				close(done)
			}()

			select {
			case <-done:
				// Handler completed normally
			case <-ctx.Done():
				// Timeout occurred
				tw.mu.Lock()
				if !tw.written {
					apperrors.WriteError(w, apperrors.NewTimeoutError("request"))
				}
				tw.mu.Unlock()
			}
		})
	}
}

// timeoutWriter wraps ResponseWriter to track if headers have been written.
type timeoutWriter struct {
	http.ResponseWriter
	written bool
	mu      noCopy
}

// noCopy is a simple mutex-like struct
type noCopy struct {
	locked bool
}

func (n *noCopy) Lock()   { n.locked = true }
func (n *noCopy) Unlock() { n.locked = false }

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	tw.written = true
	tw.mu.Unlock()
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	tw.written = true
	tw.mu.Unlock()
	return tw.ResponseWriter.Write(b)
}

// ── Pre-configured timeouts ─────────────────────────────────────────────────

// TimeoutPresets provides common timeout configurations.
var TimeoutPresets = struct {
	Default  time.Duration
	AI       time.Duration
	Database time.Duration
	Health   time.Duration
}{
	Default:  30 * time.Second,
	AI:       90 * time.Second,
	Database: 10 * time.Second,
	Health:   5 * time.Second,
}

// TimeoutDefault is middleware with the default timeout.
func TimeoutDefault(next http.Handler) http.Handler {
	return Timeout(TimeoutPresets.Default)(next)
}

// TimeoutAI is middleware with the AI timeout.
func TimeoutAI(next http.Handler) http.Handler {
	return Timeout(TimeoutPresets.AI)(next)
}

// TimeoutHealth is middleware with the health check timeout.
func TimeoutHealth(next http.Handler) http.Handler {
	return Timeout(TimeoutPresets.Health)(next)
}
