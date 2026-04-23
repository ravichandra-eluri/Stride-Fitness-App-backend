package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDKey is the context key for request ID.
type requestIDKey struct{}

// RequestIDHeader is the HTTP header for request ID.
const RequestIDHeader = "X-Request-ID"

// RequestID is a middleware that adds a unique request ID to each request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if request already has an ID (e.g., from a load balancer)
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			requestID = generateRequestID()
		}

		// Add request ID to response header
		w.Header().Set(RequestIDHeader, requestID)

		// Add request ID to context
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromCtx extracts the request ID from context.
func RequestIDFromCtx(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// generateRequestID creates a new unique request ID.
func generateRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a simpler approach if crypto/rand fails
		return "req-fallback"
	}
	return hex.EncodeToString(bytes)
}
