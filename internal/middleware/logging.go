package middleware

import (
	"net/http"
	"time"

	"stride/backend/internal/logger"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	size        int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.size += n
	return n, err
}

// Logging returns a middleware that logs HTTP requests.
func Logging(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap the response writer
			rw := newResponseWriter(w)

			// Process the request
			next.ServeHTTP(rw, r)

			// Calculate latency
			latency := time.Since(start)

			// Get context values
			requestID := RequestIDFromCtx(r.Context())
			userID := UserIDFromCtx(r.Context())

			// Build log entry
			entry := log.With(
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"latency_ms", float64(latency.Microseconds())/1000.0,
				"size", rw.size,
				"ip", getClientIP(r),
			)

			if userID != "" {
				entry = entry.With("user_id", userID)
			}

			// Log based on status code
			switch {
			case rw.status >= 500:
				entry.Error("http request")
			case rw.status >= 400:
				entry.Warn("http request")
			default:
				entry.Info("http request")
			}
		})
	}
}

// LoggingWithSkipPaths returns a middleware that skips logging for certain paths.
func LoggingWithSkipPaths(log *logger.Logger, skipPaths ...string) func(http.Handler) http.Handler {
	skipMap := make(map[string]bool)
	for _, p := range skipPaths {
		skipMap[p] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logging for certain paths (e.g., health checks)
			if skipMap[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rw := newResponseWriter(w)
			next.ServeHTTP(rw, r)
			latency := time.Since(start)

			requestID := RequestIDFromCtx(r.Context())
			userID := UserIDFromCtx(r.Context())

			entry := log.With(
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"latency_ms", float64(latency.Microseconds())/1000.0,
				"size", rw.size,
				"ip", getClientIP(r),
			)

			if userID != "" {
				entry = entry.With("user_id", userID)
			}

			switch {
			case rw.status >= 500:
				entry.Error("http request")
			case rw.status >= 400:
				entry.Warn("http request")
			default:
				entry.Info("http request")
			}
		})
	}
}
